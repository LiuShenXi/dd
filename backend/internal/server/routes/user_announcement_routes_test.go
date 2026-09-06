package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type announcementVersionRouteRepo struct {
	service.AnnouncementRepository
}

func (*announcementVersionRouteRepo) ListActive(context.Context, time.Time) ([]service.Announcement, error) {
	return []service.Announcement{}, nil
}

type announcementVersionRouteUserRepo struct {
	service.UserRepository
}

func (*announcementVersionRouteUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	return &service.User{ID: id}, nil
}

type announcementVersionRouteSubscriptionRepo struct {
	service.UserSubscriptionRepository
}

func (*announcementVersionRouteSubscriptionRepo) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	return []service.UserSubscription{}, nil
}

func TestAnnouncementVersionRouteRequiresAuthenticationAndReachesHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	announcementService := service.NewAnnouncementService(
		&announcementVersionRouteRepo{},
		nil,
		&announcementVersionRouteUserRepo{},
		&announcementVersionRouteSubscriptionRepo{},
	)
	handlers := &handler.Handlers{
		Announcement: handler.NewAnnouncementHandler(announcementService),
	}
	jwtAuth := servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 42})
		c.Next()
	})
	auditLog := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })

	router := gin.New()
	RegisterUserRoutes(router.Group("/api/v1"), handlers, jwtAuth, auditLog, nil, nil)

	t.Run("unauthenticated", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/announcements/version", nil)
		router.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusUnauthorized, recorder.Code)
	})

	t.Run("authenticated", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/announcements/version", nil)
		request.Header.Set("Authorization", "Bearer test-token")
		router.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code)
		require.JSONEq(t, `{
			"code": 0,
			"message": "success",
			"data": {
				"version": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				"unread_count": 0
			}
		}`, recorder.Body.String())
	})
}
