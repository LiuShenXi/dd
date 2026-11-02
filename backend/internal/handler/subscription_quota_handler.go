package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	quotahttp "github.com/Wei-Shaw/sub2api/internal/handler/subscriptionquota"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type SubscriptionQuotaReader interface {
	ListSummaries(context.Context, int64) ([]domain.SubscriptionQuotaSummary, error)
	GetSummary(context.Context, int64, int64) (*domain.SubscriptionQuotaSummary, error)
	ListTerms(context.Context, int64, int64, service.SubscriptionQuotaPageInput) (*domain.SubscriptionQuotaTermPage, error)
	ListUsage(context.Context, int64, int64, service.SubscriptionQuotaPageInput) (*domain.SubscriptionQuotaUsagePage, error)
}

type SubscriptionQuotaHandler struct{ service SubscriptionQuotaReader }

func NewSubscriptionQuotaHandler(svc SubscriptionQuotaReader) *SubscriptionQuotaHandler {
	return &SubscriptionQuotaHandler{service: svc}
}

func (h *SubscriptionQuotaHandler) List(c *gin.Context) {
	userID, ok := quotahttp.Subject(c, false)
	if !ok {
		return
	}
	summaries, err := h.service.ListSummaries(c.Request.Context(), userID)
	if response.ErrorFrom(c, err) {
		return
	}
	items := make([]dto.SubscriptionQuotaSummary, 0, len(summaries))
	for i := range summaries {
		items = append(items, *dto.SubscriptionQuotaSummaryFromDomain(&summaries[i]))
	}
	response.Success(c, items)
}

func (h *SubscriptionQuotaHandler) GetSummary(c *gin.Context) {
	userID, ok := quotahttp.Subject(c, false)
	if !ok {
		return
	}
	groupID, ok := quotahttp.PositiveID(c, c.Param("group_id"))
	if !ok {
		return
	}
	summary, err := h.service.GetSummary(c.Request.Context(), userID, groupID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, dto.SubscriptionQuotaSummaryFromDomain(summary))
}

func (h *SubscriptionQuotaHandler) ListTerms(c *gin.Context) {
	userID, ok := quotahttp.Subject(c, false)
	if !ok {
		return
	}
	groupID, ok := quotahttp.PositiveID(c, c.Param("group_id"))
	if !ok {
		return
	}
	page, ok := quotahttp.Page(c)
	if !ok {
		return
	}
	result, err := h.service.ListTerms(c.Request.Context(), userID, groupID, page)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, dto.SubscriptionQuotaTermPageFromDomain(result))
}

func (h *SubscriptionQuotaHandler) ListUsage(c *gin.Context) {
	userID, ok := quotahttp.Subject(c, false)
	if !ok {
		return
	}
	termID, ok := quotahttp.PositiveID(c, c.Param("term_id"))
	if !ok {
		return
	}
	page, ok := quotahttp.Page(c)
	if !ok {
		return
	}
	result, err := h.service.ListUsage(c.Request.Context(), userID, termID, page)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, dto.SubscriptionQuotaUsagePageFromDomain(result))
}
