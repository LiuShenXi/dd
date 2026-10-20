package admin

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	quotahttp "github.com/Wei-Shaw/sub2api/internal/handler/subscriptionquota"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type SubscriptionQuotaRolloutAdmin interface {
	Get(context.Context, int64) (*domain.SubscriptionQuotaRolloutState, error)
	Maintain(context.Context, int64, int64, service.SubscriptionQuotaMaintenanceInput, string) (*domain.SubscriptionQuotaRolloutState, error)
	Preview(context.Context, int64, int64, []domain.SubscriptionQuotaAllocation) (*domain.SubscriptionQuotaRolloutState, error)
	Convert(context.Context, int64, int64, service.SubscriptionQuotaConversionInput, string) (*domain.SubscriptionQuotaRolloutResult, error)
}

func (h *SubscriptionQuotaHandler) SetRollout(svc SubscriptionQuotaRolloutAdmin) { h.rollout = svc }
func (h *SubscriptionQuotaHandler) GetRollout(c *gin.Context) {
	if _, ok := quotahttp.Subject(c, true); !ok {
		return
	}
	groupID, ok := quotahttp.PositiveID(c, c.Param("id"))
	if !ok {
		return
	}
	if h.rollout == nil {
		response.ErrorFrom(c, service.ErrSubscriptionQuotaBillingUnavailable)
		return
	}
	out, err := h.rollout.Get(c.Request.Context(), groupID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *SubscriptionQuotaHandler) MaintainRollout(c *gin.Context) {
	actor, ok := quotahttp.Subject(c, true)
	if !ok {
		return
	}
	groupID, ok := quotahttp.PositiveID(c, c.Param("id"))
	if !ok {
		return
	}
	key, ok := quotahttp.IdempotencyKey(c)
	if !ok {
		return
	}
	var request struct {
		Enabled          *bool  `json:"enabled"`
		ExpectedRevision *int64 `json:"expected_revision"`
		Reason           string `json:"reason"`
	}
	if !quotahttp.Decode(c, &request, []string{"enabled", "expected_revision", "reason"}) {
		return
	}
	if request.Enabled == nil {
		response.ErrorFrom(c, service.ErrSubscriptionQuotaInput)
		return
	}
	in := service.SubscriptionQuotaMaintenanceInput{Enabled: *request.Enabled, ExpectedRevision: request.ExpectedRevision, Reason: request.Reason}
	if h.rollout == nil {
		response.ErrorFrom(c, service.ErrSubscriptionQuotaBillingUnavailable)
		return
	}
	out, err := h.rollout.Maintain(c.Request.Context(), groupID, actor, in, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *SubscriptionQuotaHandler) PreviewRollout(c *gin.Context) {
	actor, ok := quotahttp.Subject(c, true)
	if !ok {
		return
	}
	groupID, ok := quotahttp.PositiveID(c, c.Param("id"))
	if !ok {
		return
	}
	var in struct {
		Allocations []domain.SubscriptionQuotaAllocation `json:"allocations"`
	}
	if !quotahttp.Decode(c, &in, []string{"allocations"}) {
		return
	}
	if h.rollout == nil {
		response.ErrorFrom(c, service.ErrSubscriptionQuotaBillingUnavailable)
		return
	}
	out, err := h.rollout.Preview(c.Request.Context(), groupID, actor, in.Allocations)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *SubscriptionQuotaHandler) ConvertRollout(c *gin.Context) {
	actor, ok := quotahttp.Subject(c, true)
	if !ok {
		return
	}
	groupID, ok := quotahttp.PositiveID(c, c.Param("id"))
	if !ok {
		return
	}
	key, ok := quotahttp.IdempotencyKey(c)
	if !ok {
		return
	}
	var in service.SubscriptionQuotaConversionInput
	if !quotahttp.Decode(c, &in, []string{"expected_revision", "preview_token", "allocations"}) {
		return
	}
	if h.rollout == nil {
		response.ErrorFrom(c, service.ErrSubscriptionQuotaBillingUnavailable)
		return
	}
	out, err := h.rollout.Convert(c.Request.Context(), groupID, actor, in, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
