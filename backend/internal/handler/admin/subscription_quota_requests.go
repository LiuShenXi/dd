package admin

import (
	"context"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	quotahttp "github.com/Wei-Shaw/sub2api/internal/handler/subscriptionquota"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type SubscriptionQuotaRequestQuery interface {
	ListRequests(context.Context, service.SubscriptionQuotaRequestQueryInput) (*domain.SubscriptionQuotaRequestPage, error)
}
type SubscriptionQuotaRequestResolver interface {
	ResolveRequest(context.Context, int64, int64, service.SubscriptionQuotaResolveInput, string) (*domain.SubscriptionQuotaResolveResult, error)
}

func (h *SubscriptionQuotaHandler) SetRequestAdministration(query SubscriptionQuotaRequestQuery, resolver SubscriptionQuotaRequestResolver) {
	h.requests, h.resolution = query, resolver
}

func (h *SubscriptionQuotaHandler) ListRequests(c *gin.Context) {
	if _, ok := quotahttp.Subject(c, true); !ok {
		return
	}
	for key, values := range c.Request.URL.Query() {
		if (key != "cursor" && key != "limit" && key != "status" && key != "user_id" && key != "group_id") || len(values) != 1 {
			quotahttp.BadRequest(c, "INVALID_REQUEST", "Invalid request query")
			return
		}
	}
	page, ok := quotahttp.Page(c)
	if !ok {
		return
	}
	in := service.SubscriptionQuotaRequestQueryInput{SubscriptionQuotaPageInput: page, Status: c.Query("status")}
	for _, filter := range []struct {
		key    string
		target **int64
	}{{"user_id", &in.UserID}, {"group_id", &in.GroupID}} {
		if values, present := c.Request.URL.Query()[filter.key]; present {
			id, ok := quotahttp.PositiveID(c, values[0])
			if !ok {
				return
			}
			*filter.target = &id
		}
	}
	if h.requests == nil {
		response.ErrorFrom(c, service.ErrSubscriptionQuotaBillingUnavailable)
		return
	}
	result, err := h.requests.ListRequests(c.Request.Context(), in)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, dto.SubscriptionQuotaRequestPageFromDomain(result))
}

func (h *SubscriptionQuotaHandler) ResolveRequest(c *gin.Context) {
	actor, ok := quotahttp.Subject(c, true)
	if !ok {
		return
	}
	rowID, ok := quotahttp.PositiveID(c, c.Param("request_id"))
	if !ok {
		return
	}
	key, ok := quotahttp.IdempotencyKey(c)
	if !ok {
		return
	}
	in, ok := quotahttp.ReadResolve(c)
	if !ok {
		return
	}
	if h.resolution == nil {
		response.ErrorFrom(c, service.ErrSubscriptionQuotaBillingUnavailable)
		return
	}
	result, err := h.resolution.ResolveRequest(c.Request.Context(), actor, rowID, in, key)
	if response.ErrorFrom(c, err) {
		return
	}
	status := http.StatusAccepted
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, response.Response{Code: 0, Message: "success", Data: dto.SubscriptionQuotaResolveFromDomain(result)})
}
