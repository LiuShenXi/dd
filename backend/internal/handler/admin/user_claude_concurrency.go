package admin

import (
	"bytes"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// A value receiver field distinguishes omission from explicit null. The latter
// is invalid: removing an override is not an implicit meaning of null.
type optionalClaudeConcurrency struct{ value *int }

func (v *optionalClaudeConcurrency) UnmarshalJSON(raw []byte) error {
	var value int
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return service.ErrInvalidClaudeConcurrency
	}
	if err := json.Unmarshal(raw, &value); err != nil || !service.ValidClaudeSubscriptionConcurrency(value) {
		return service.ErrInvalidClaudeConcurrency
	}
	v.value = &value
	return nil
}

func (h *UserHandler) claudeUserResponse(c *gin.Context, user *service.User) *dto.AdminUser {
	if user != nil {
		user.ClaudeConcurrencyView = h.concurrencyService.ClaudeConcurrencyView(c.Request.Context(), user)
	}
	return dto.UserFromServiceAdmin(user)
}
