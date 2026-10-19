// Package subscriptionquota contains the shared HTTP contract for term quota
// administration and self-service reads.
package subscriptionquota

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type PreviewRequest struct {
	GroupID   int64   `json:"group_id"`
	Operation string  `json:"operation"`
	QuotaUSD  *string `json:"quota_usd"`
	PlanID    *int64  `json:"plan_id"`
	Notes     string  `json:"notes"`
}

type OpenRequest struct {
	PreviewRequest
	ExpectedRevision *int64 `json:"expected_revision"`
}

type CancelRequest struct {
	ExpectedRevision *int64 `json:"expected_revision"`
	Reason           string `json:"reason"`
}

var previewFields = []string{"group_id", "operation", "quota_usd", "plan_id", "notes"}

// Decode accepts one JSON object with exact schema field names. In particular,
// null, duplicate keys and trailing JSON must not erase required-field presence.
func Decode(c *gin.Context, target any, fields []string) bool {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	if err == nil {
		err = decodeObject(body, target, fields)
	}
	if err != nil {
		BadRequest(c, "INVALID_REQUEST", "Invalid subscription quota request")
		return false
	}
	return true
}

func decodeObject(body []byte, target any, fields []string) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return errors.New("expected object")
	}
	allowed := make(map[string]bool, len(fields))
	for _, key := range fields {
		allowed[key] = true
	}
	seen := make(map[string]bool, len(fields))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || seen[key] {
			return errors.New("unknown or duplicate field")
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("null field")
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON")
	}
	decoder = json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func ReadPreview(c *gin.Context) (service.SubscriptionQuotaOpenInput, bool) {
	var req PreviewRequest
	if !Decode(c, &req, previewFields) {
		return service.SubscriptionQuotaOpenInput{}, false
	}
	return req.input(c)
}

func ReadOpen(c *gin.Context) (service.SubscriptionQuotaOpenInput, bool) {
	var req OpenRequest
	fields := append(append([]string{}, previewFields...), "expected_revision")
	if !Decode(c, &req, fields) {
		return service.SubscriptionQuotaOpenInput{}, false
	}
	if req.ExpectedRevision == nil || *req.ExpectedRevision < 0 {
		BadRequest(c, "INVALID_REVISION", "expected_revision must be present and non-negative")
		return service.SubscriptionQuotaOpenInput{}, false
	}
	in, ok := req.PreviewRequest.input(c)
	in.ExpectedRevision = req.ExpectedRevision
	return in, ok
}

func (req PreviewRequest) input(c *gin.Context) (service.SubscriptionQuotaOpenInput, bool) {
	if req.GroupID <= 0 || (req.Operation != "open" && req.Operation != "renew") || (req.PlanID != nil && *req.PlanID <= 0) || utf8.RuneCountInString(req.Notes) > 500 {
		BadRequest(c, "INVALID_REQUEST", "Invalid group, operation, plan or notes")
		return service.SubscriptionQuotaOpenInput{}, false
	}
	// Exact monetary syntax and normalization belong to the shared service, so
	// preview, mutations and internal callers cannot develop different rules.
	return service.SubscriptionQuotaOpenInput{GroupID: req.GroupID, Operation: req.Operation, QuotaUSD: req.QuotaUSD, PlanID: req.PlanID, Notes: req.Notes}, true
}

func ReadCancel(c *gin.Context) (service.SubscriptionQuotaCancelInput, bool) {
	var req CancelRequest
	if !Decode(c, &req, []string{"expected_revision", "reason"}) {
		return service.SubscriptionQuotaCancelInput{}, false
	}
	if req.ExpectedRevision == nil || *req.ExpectedRevision < 0 || strings.TrimSpace(req.Reason) == "" || utf8.RuneCountInString(req.Reason) > 500 {
		BadRequest(c, "INVALID_REQUEST", "Cancellation requires a non-negative revision and reason of 1 to 500 characters")
		return service.SubscriptionQuotaCancelInput{}, false
	}
	return service.SubscriptionQuotaCancelInput{ExpectedRevision: req.ExpectedRevision, Reason: req.Reason}, true
}

func IdempotencyKey(c *gin.Context) (string, bool) {
	keys := c.Request.Header.Values("Idempotency-Key")
	if len(keys) != 1 || len(keys[0]) < 16 || len(keys[0]) > 128 || strings.TrimSpace(keys[0]) != keys[0] || strings.ContainsAny(keys[0], "\r\n\t\x00") {
		BadRequest(c, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must contain 16 to 128 characters")
		return "", false
	}
	return keys[0], true
}

func PositiveID(c *gin.Context, value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		BadRequest(c, "INVALID_ID", "A positive integer ID is required")
		return 0, false
	}
	return id, true
}

func Page(c *gin.Context) (service.SubscriptionQuotaPageInput, bool) {
	limit := 20
	if values, present := c.Request.URL.Query()["limit"]; present {
		if len(values) != 1 {
			BadRequest(c, "INVALID_PAGINATION", "limit must be a single integer")
			return service.SubscriptionQuotaPageInput{}, false
		}
		parsed, err := strconv.Atoi(values[0])
		if err != nil || parsed < 1 || parsed > 100 {
			BadRequest(c, "INVALID_PAGINATION", "limit must be between 1 and 100")
			return service.SubscriptionQuotaPageInput{}, false
		}
		limit = parsed
	}
	return service.SubscriptionQuotaPageInput{Cursor: c.Query("cursor"), Limit: limit}, true
}

func Subject(c *gin.Context, admin bool) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not found in context")
		return 0, false
	}
	if admin {
		role, _ := middleware.GetUserRoleFromContext(c)
		if role != service.RoleAdmin {
			response.Forbidden(c, "Admin access required")
			return 0, false
		}
	}
	return subject.UserID, true
}

func BadRequest(c *gin.Context, reason, message string) {
	response.ErrorWithDetails(c, http.StatusBadRequest, message, reason, nil)
}
