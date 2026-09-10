package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/releasedrain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

type releaseAuthInvalidator interface {
	InvalidateReleaseAuthCache(context.Context, []int64, []int64) error
}

type releaseControlRequest struct {
	OperationID string  `json:"operation_id"`
	UserIDs     []int64 `json:"user_ids,omitempty"`
	GroupIDs    []int64 `json:"group_ids,omitempty"`
}

func registerReleaseRoutes(r *gin.Engine, controller *releasedrain.Controller, adminAuth middleware.AdminAuthMiddleware, auditLog middleware.AuditLogMiddleware, invalidator releaseAuthInvalidator) {
	admin := r.Group("/api/v1/admin/release", gin.HandlerFunc(adminAuth), gin.HandlerFunc(auditLog))
	admin.GET("/status", func(c *gin.Context) { response.Success(c, controller.Status()) })
	admin.POST("/drain", func(c *gin.Context) {
		status, err := controller.Drain()
		if err != nil {
			releaseError(c, err)
			return
		}
		response.Success(c, status)
	})
	admin.POST("/lock", func(c *gin.Context) {
		body, ok := decodeReleaseRequest(c)
		if !ok {
			return
		}
		if err := controller.LockMigration(body.OperationID); err != nil {
			releaseError(c, err)
			return
		}
		response.Success(c, controller.Status())
	})
	admin.POST("/cancel", func(c *gin.Context) {
		body, ok := decodeReleaseRequest(c)
		if !ok {
			return
		}
		if err := controller.Cancel(body.OperationID); err != nil {
			releaseError(c, err)
			return
		}
		response.Success(c, controller.Status())
	})
	admin.POST("/resume", func(c *gin.Context) {
		body, ok := decodeReleaseRequest(c)
		if !ok {
			return
		}
		if !validReleaseIDs(body.UserIDs) || !validReleaseIDs(body.GroupIDs) {
			middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_RELEASE_SCOPE", "Positive distinct user_ids and group_ids are required")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		err := controller.Resume(ctx, body.OperationID, func(ctx context.Context) error {
			if invalidator == nil {
				return errors.New("release cache invalidator unavailable")
			}
			return invalidator.InvalidateReleaseAuthCache(ctx, body.UserIDs, body.GroupIDs)
		})
		if err != nil {
			releaseError(c, err)
			return
		}
		response.Success(c, controller.Status())
	})
}

func validReleaseIDs(ids []int64) bool {
	if len(ids) == 0 || len(ids) > 500 {
		return false
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func decodeReleaseRequest(c *gin.Context) (releaseControlRequest, bool) {
	var body releaseControlRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&body)
	if err == nil && body.OperationID != "" && len(body.OperationID) <= 64 {
		var trailing any
		if errors.Is(decoder.Decode(&trailing), io.EOF) {
			return body, true
		}
	}
	middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_RELEASE_REQUEST", "A valid release operation_id is required")
	return body, false
}

func releaseError(c *gin.Context, err error) {
	if errors.Is(err, releasedrain.ErrState) || errors.Is(err, releasedrain.ErrBusy) {
		middleware.AbortWithError(c, http.StatusConflict, "RELEASE_STATE_CONFLICT", err.Error())
		return
	}
	middleware.AbortWithError(c, http.StatusServiceUnavailable, "RELEASE_REFRESH_FAILED", "Release remains held; verify migration and cache state before retrying")
}
