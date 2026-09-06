package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CarpoolHandler struct{ service *service.CarpoolService }

func NewCarpoolHandler(s *service.CarpoolService) *CarpoolHandler { return &CarpoolHandler{service: s} }

func carpoolSubject(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return 0, false
	}
	return subject.UserID, true
}

func (h *CarpoolHandler) Details(c *gin.Context) {
	userID, ok := carpoolSubject(c)
	if !ok {
		return
	}
	result, err := h.service.Details(c.Request.Context(), userID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
func (h *CarpoolHandler) BoostStatus(c *gin.Context) {
	userID, ok := carpoolSubject(c)
	if !ok {
		return
	}
	result, err := h.service.BoostStatus(c.Request.Context(), userID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
func (h *CarpoolHandler) ClaimBoost(c *gin.Context) {
	userID, ok := carpoolSubject(c)
	if !ok {
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		response.BadRequest(c, "Idempotency-Key is required")
		return
	}
	result, err := h.service.ClaimBoost(c.Request.Context(), userID, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
