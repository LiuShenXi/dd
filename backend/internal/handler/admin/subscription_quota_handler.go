package admin

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	quotahttp "github.com/Wei-Shaw/sub2api/internal/handler/subscriptionquota"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type SubscriptionQuotaAdminService interface {
	GetSummary(context.Context, int64, int64) (*domain.SubscriptionQuotaSummary, error)
	ListTerms(context.Context, int64, int64, service.SubscriptionQuotaPageInput) (*domain.SubscriptionQuotaTermPage, error)
	Preview(context.Context, int64, int64, service.SubscriptionQuotaOpenInput) (*domain.SubscriptionQuotaPreview, error)
	OpenTerm(context.Context, int64, int64, service.SubscriptionQuotaOpenInput, string) (*domain.SubscriptionQuotaMutationResult, error)
	CancelTerm(context.Context, int64, int64, service.SubscriptionQuotaCancelInput, string) (*domain.SubscriptionQuotaMutationResult, error)
}

type SubscriptionQuotaHandler struct {
	service    SubscriptionQuotaAdminService
	requests   SubscriptionQuotaRequestQuery
	resolution SubscriptionQuotaRequestResolver
}

func NewSubscriptionQuotaHandler(svc SubscriptionQuotaAdminService) *SubscriptionQuotaHandler {
	return &SubscriptionQuotaHandler{service: svc}
}

func (h *SubscriptionQuotaHandler) GetSummary(c *gin.Context) {
	if _, ok := quotahttp.Subject(c, true); !ok {
		return
	}
	userID, ok := quotahttp.PositiveID(c, c.Param("id"))
	if !ok {
		return
	}
	groupID, ok := quotahttp.PositiveID(c, c.Query("group_id"))
	if !ok {
		return
	}
	result, err := h.service.GetSummary(c.Request.Context(), userID, groupID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, dto.SubscriptionQuotaSummaryFromDomain(result))
}

func (h *SubscriptionQuotaHandler) ListTerms(c *gin.Context) {
	if _, ok := quotahttp.Subject(c, true); !ok {
		return
	}
	userID, ok := quotahttp.PositiveID(c, c.Param("id"))
	if !ok {
		return
	}
	groupID, ok := quotahttp.PositiveID(c, c.Query("group_id"))
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

func (h *SubscriptionQuotaHandler) Preview(c *gin.Context) {
	actorID, ok := quotahttp.Subject(c, true)
	if !ok {
		return
	}
	userID, ok := quotahttp.PositiveID(c, c.Param("id"))
	if !ok {
		return
	}
	in, ok := quotahttp.ReadPreview(c)
	if !ok {
		return
	}
	result, err := h.service.Preview(c.Request.Context(), userID, actorID, in)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, dto.SubscriptionQuotaPreviewFromDomain(result))
}

func (h *SubscriptionQuotaHandler) OpenTerm(c *gin.Context) {
	actorID, ok := quotahttp.Subject(c, true)
	if !ok {
		return
	}
	userID, ok := quotahttp.PositiveID(c, c.Param("id"))
	if !ok {
		return
	}
	key, ok := quotahttp.IdempotencyKey(c)
	if !ok {
		return
	}
	in, ok := quotahttp.ReadOpen(c)
	if !ok {
		return
	}
	result, err := h.service.OpenTerm(c.Request.Context(), userID, actorID, in, key)
	if response.ErrorFrom(c, err) {
		return
	}
	if result.Replayed {
		response.Success(c, dto.SubscriptionQuotaMutationFromDomain(result))
	} else {
		response.Created(c, dto.SubscriptionQuotaMutationFromDomain(result))
	}
}

func (h *SubscriptionQuotaHandler) CancelTerm(c *gin.Context) {
	actorID, ok := quotahttp.Subject(c, true)
	if !ok {
		return
	}
	termID, ok := quotahttp.PositiveID(c, c.Param("term_id"))
	if !ok {
		return
	}
	key, ok := quotahttp.IdempotencyKey(c)
	if !ok {
		return
	}
	in, ok := quotahttp.ReadCancel(c)
	if !ok {
		return
	}
	result, err := h.service.CancelTerm(c.Request.Context(), termID, actorID, in, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, dto.SubscriptionQuotaMutationFromDomain(result))
}
