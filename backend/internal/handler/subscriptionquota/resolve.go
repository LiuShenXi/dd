package subscriptionquota

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type resolveRequest struct {
	ExpectedRequestRevision *int64          `json:"expected_request_revision"`
	Resolution              string          `json:"resolution"`
	EvidenceReference       string          `json:"evidence_reference"`
	Reason                  string          `json:"reason"`
	VerifiedUsage           json.RawMessage `json:"verified_usage"`
}

type verifiedUsageRequest struct {
	RequestedModel        string `json:"requested_model"`
	UpstreamModel         string `json:"upstream_model"`
	ResponseModel         string `json:"response_model"`
	InputTokens           *int64 `json:"input_tokens"`
	OutputTokens          *int64 `json:"output_tokens"`
	CacheCreation5mTokens *int64 `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens *int64 `json:"cache_creation_1h_tokens"`
	CacheReadTokens       *int64 `json:"cache_read_tokens"`
	ServiceTier           string `json:"service_tier"`
	SearchCallCount       int64  `json:"search_call_count"`
}

func ReadResolve(c *gin.Context) (service.SubscriptionQuotaResolveInput, bool) {
	var req resolveRequest
	if !Decode(c, &req, []string{"expected_request_revision", "resolution", "evidence_reference", "reason", "verified_usage"}) {
		return service.SubscriptionQuotaResolveInput{}, false
	}
	in := service.SubscriptionQuotaResolveInput{ExpectedRequestRevision: req.ExpectedRequestRevision, Resolution: req.Resolution, EvidenceReference: req.EvidenceReference, Reason: req.Reason}
	invalid := func() (service.SubscriptionQuotaResolveInput, bool) {
		BadRequest(c, "INVALID_REQUEST", "Invalid request resolution or verified usage")
		return service.SubscriptionQuotaResolveInput{}, false
	}
	if req.ExpectedRequestRevision == nil || *req.ExpectedRequestRevision < 0 {
		return invalid()
	}
	switch req.Resolution {
	case "no_charge":
		if len(req.VerifiedUsage) != 0 {
			return invalid()
		}
	case "verified_usage":
		var verified verifiedUsageRequest
		fields := []string{"requested_model", "upstream_model", "response_model", "input_tokens", "output_tokens", "cache_creation_5m_tokens", "cache_creation_1h_tokens", "cache_read_tokens", "service_tier", "search_call_count"}
		if decodeObject(req.VerifiedUsage, &verified, fields) != nil {
			return invalid()
		}
		for _, value := range []*int64{verified.InputTokens, verified.OutputTokens, verified.CacheCreation5mTokens, verified.CacheCreation1hTokens, verified.CacheReadTokens} {
			if value == nil || *value < 0 {
				return invalid()
			}
		}
		if verified.SearchCallCount < 0 {
			return invalid()
		}
		in.VerifiedUsage = &service.SubscriptionQuotaVerifiedUsage{RequestedModel: verified.RequestedModel, UpstreamModel: verified.UpstreamModel, ResponseModel: verified.ResponseModel, InputTokens: *verified.InputTokens, OutputTokens: *verified.OutputTokens, CacheCreation5mTokens: *verified.CacheCreation5mTokens, CacheCreation1hTokens: *verified.CacheCreation1hTokens, CacheReadTokens: *verified.CacheReadTokens, ServiceTier: verified.ServiceTier, SearchCallCount: verified.SearchCallCount}
	default:
		return invalid()
	}
	return in, true
}
