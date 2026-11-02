package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
)

var excelBPSAttachments basispoints.AttachmentCache

type excelBPSAttachmentError struct {
	status        int
	code, message string
}

func (e *excelBPSAttachmentError) Error() string { return e.message }

func newExcelBPSAttachmentRequest(ctx context.Context, mediaType string, data []byte, token, accountID string) (*http.Request, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	filename := "image"
	// BPS checks the attachment filename, not just its Content-Type. Go 1.27
	// lists .jfif first for JPEG, but BPS inference only accepts .jpg/.jpeg.
	// Keep supported image filenames independent of the OS MIME database.
	switch mediaType {
	case "image/jpeg":
		filename += ".jpg"
	case "image/png":
		filename += ".png"
	case "image/gif":
		filename += ".gif"
	case "image/webp":
		filename += ".webp"
	default:
		if extensions, _ := mime.ExtensionsByType(mediaType); len(extensions) > 0 {
			filename += extensions[0]
		}
	}
	headers := make(textproto.MIMEHeader)
	headers.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filename}))
	headers.Set("Content-Type", mediaType)
	part, err := writer.CreatePart(headers)
	if err != nil {
		return nil, err
	}
	if _, err = part.Write(data); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	req, err := newExcelBPSRequest(ctx, body.Bytes(), token, accountID)
	if err != nil {
		return nil, err
	}
	req.URL, err = url.Parse(basispoints.AttachmentsURL)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (s *OpenAIGatewayService) uploadExcelBPSImage(ctx context.Context, account *Account, token, accountID, mediaType string, data []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ctx = WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileLongStream))
	req, err := newExcelBPSAttachmentRequest(ctx, mediaType, data, token, accountID)
	if err != nil {
		return "", &excelBPSAttachmentError{500, "basispoints_attachment_encoding", "Could not encode the image attachment"}
	}
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return "", &excelBPSAttachmentError{502, "basispoints_attachment_transport", "Excel BPS image upload failed; the model request was not submitted"}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Attachment errors can echo raw multipart bytes. Never log or return them.
		status := resp.StatusCode
		if status < 400 {
			status = http.StatusBadGateway
		}
		return "", &excelBPSAttachmentError{status, "basispoints_attachment_upload_error", fmt.Sprintf("Excel BPS image upload returned HTTP %d; the model request was not submitted", resp.StatusCode)}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	var result struct {
		FileID string `json:"openai_file_id"`
	}
	if err != nil || len(raw) > 64<<10 || json.Unmarshal(raw, &result) != nil || strings.TrimSpace(result.FileID) == "" {
		return "", &excelBPSAttachmentError{502, "basispoints_attachment_response_invalid", "Excel BPS image upload returned no usable file ID"}
	}
	return strings.TrimSpace(result.FileID), nil
}
