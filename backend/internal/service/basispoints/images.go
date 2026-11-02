package basispoints

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/url"
	"strings"
)

const maxInlineImageBytes = 20 << 20

// Inline images are uploaded by the gateway after the entire request is validated.
// Client-supplied file IDs remain unsupported: they have no verified account scope.
func validateImage(part object) error {
	raw, ok := part["image_url"].(string)
	if !ok || raw == "" {
		return fmt.Errorf("basispoints input_image requires an HTTPS or inline image_url; client file IDs are unsupported")
	}
	if isInlineImage(raw) {
		if _, _, err := decodeInlineImage(raw); err != nil {
			return err
		}
	} else {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || strings.TrimSpace(raw) != raw {
			return fmt.Errorf("basispoints input_image requires an absolute HTTPS image URL without embedded credentials")
		}
	}
	if fileID := text(part["file_id"]); fileID != "" {
		return fmt.Errorf("basispoints input_image does not support client file_id; provide only image_url")
	}
	if detail, exists := part["detail"]; exists && detail != nil {
		switch text(detail) {
		case "auto", "low", "high":
		default:
			return fmt.Errorf("basispoints image detail must be auto, low or high")
		}
	}
	return nil
}

func isInlineImage(raw string) bool {
	return len(raw) >= 5 && strings.EqualFold(raw[:5], "data:")
}

func decodeInlineImage(raw string) (string, []byte, error) {
	if !isInlineImage(raw) {
		return "", nil, fmt.Errorf("basispoints inline image requires a data URL")
	}
	metadata, encoded, found := strings.Cut(raw[5:], ",")
	if !found || !strings.HasSuffix(strings.ToLower(metadata), ";base64") {
		return "", nil, fmt.Errorf("basispoints inline image requires data:image/...;base64,...")
	}
	mediaType, _, err := mime.ParseMediaType(metadata[:len(metadata)-len(";base64")])
	if err != nil || !strings.HasPrefix(mediaType, "image/") {
		return "", nil, fmt.Errorf("basispoints inline image must declare an image media type")
	}
	// Bound allocation before percent/base64 decoding as well as decoded bytes.
	if len(encoded) > 3*base64.StdEncoding.EncodedLen(maxInlineImageBytes) {
		return "", nil, fmt.Errorf("basispoints inline image exceeds 20 MiB")
	}
	encoded, err = url.PathUnescape(encoded)
	if err != nil {
		return "", nil, fmt.Errorf("basispoints inline image has invalid percent encoding")
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(maxInlineImageBytes) {
		return "", nil, fmt.Errorf("basispoints inline image exceeds 20 MiB")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return "", nil, fmt.Errorf("basispoints inline image contains empty or invalid base64 data")
	}
	if len(data) > maxInlineImageBytes {
		return "", nil, fmt.Errorf("basispoints inline image exceeds 20 MiB")
	}
	return mediaType, data, nil
}
