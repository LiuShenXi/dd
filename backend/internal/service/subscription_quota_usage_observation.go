package service

import (
	"bytes"
	"encoding/json"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const subscriptionQuotaMeteringKey = "subscription_quota_raw_metering"

type subscriptionQuotaMeteringObservation struct {
	mu                                         sync.Mutex
	observed, input, output, terminal, invalid bool
	source                                     string
	usage                                      ClaudeUsage
}

func beginSubscriptionQuotaMetering(c *gin.Context) {
	if c == nil {
		return
	}
	if _, term := SubscriptionQuotaForwardStateFromContext(c.Request.Context()); !term {
		return
	}
	c.Set(subscriptionQuotaMeteringKey, &subscriptionQuotaMeteringObservation{})
	beginUpstreamResponseModelObservation(c)
}

func subscriptionQuotaMetering(c *gin.Context) *subscriptionQuotaMeteringObservation {
	if c == nil {
		return nil
	}
	value, _ := c.Get(subscriptionQuotaMeteringKey)
	o, _ := value.(*subscriptionQuotaMeteringObservation)
	return o
}

func validTermUsageInteger(raw json.RawMessage) (present, valid bool) {
	if len(raw) == 0 {
		return false, false
	}
	text := string(bytes.TrimSpace(raw))
	value, err := strconv.ParseInt(text, 10, 64)
	return true, err == nil && value >= 0 && uint64(value) <= uint64(^uint(0)>>1)
}

// observeSubscriptionQuotaMetering reads provider bytes before conversion or
// client-facing rewrites. It never trusts a zero-valued decoded struct.
func observeSubscriptionQuotaMetering(c *gin.Context, data []byte, source string) {
	o := subscriptionQuotaMetering(c)
	if o == nil {
		return
	}
	var event map[string]json.RawMessage
	if json.Unmarshal(data, &event) != nil {
		return
	}
	if observer := upstreamResponseModelObserverFromContext(c); observer != nil {
		observer.ObserveAnthropic(data)
	}
	var kind string
	_ = json.Unmarshal(event["type"], &kind)
	usageRaw := event["usage"]
	if kind == "message_start" {
		var message map[string]json.RawMessage
		if json.Unmarshal(event["message"], &message) == nil {
			usageRaw = message["usage"]
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if subscriptionQuotaHasServerToolEvidence(gjson.ParseBytes(data)) {
		o.invalid = true
	}
	if kind == "message_stop" {
		o.terminal = true
	}
	if raw := event["amazon-bedrock-invocationMetrics"]; len(raw) > 0 {
		var metrics map[string]json.RawMessage
		if json.Unmarshal(raw, &metrics) == nil {
			ip, iv := validTermUsageInteger(metrics["inputTokenCount"])
			op, ov := validTermUsageInteger(metrics["outputTokenCount"])
			if ip && iv && op && ov {
				o.observed = true
				o.input = true
				o.output = true
				o.terminal = true
				o.source = "bedrock_invocation_metrics"
				o.usage.InputTokens = int(gjson.GetBytes(raw, "inputTokenCount").Int())
				o.usage.OutputTokens = int(gjson.GetBytes(raw, "outputTokenCount").Int())
			} else {
				o.invalid = true
			}
		}
		if _, ok := metrics["inputTokenCount"]; !ok {
			o.invalid = true
		}
	}
	if len(usageRaw) == 0 {
		return
	}
	var usage map[string]json.RawMessage
	if json.Unmarshal(usageRaw, &usage) != nil || usage == nil {
		o.invalid = true
		return
	}
	any := false
	for _, name := range []string{"input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens", "cached_tokens", "prompt_tokens", "prompt_cache_hit_tokens", "prompt_cache_miss_tokens"} {
		present, valid := validTermUsageInteger(usage[name])
		if !present {
			continue
		}
		any = true
		if !valid {
			o.invalid = true
			continue
		}
		if name == "input_tokens" || name == "prompt_tokens" || name == "prompt_cache_miss_tokens" {
			o.input = true
		}
		if name == "output_tokens" && kind != "message_start" {
			o.output = true
		}
	}
	if raw := usage["cache_creation"]; len(raw) > 0 {
		var cache map[string]json.RawMessage
		if json.Unmarshal(raw, &cache) != nil || cache == nil {
			o.invalid = true
		} else {
			for _, name := range []string{"ephemeral_5m_input_tokens", "ephemeral_1h_input_tokens"} {
				if present, valid := validTermUsageInteger(cache[name]); present {
					any = true
					if !valid {
						o.invalid = true
					}
				}
			}
		}
	}
	if raw := usage["prompt_tokens_details"]; len(raw) > 0 {
		var details map[string]json.RawMessage
		if json.Unmarshal(raw, &details) != nil || details == nil {
			o.invalid = true
		} else if present, valid := validTermUsageInteger(details["cached_tokens"]); present && !valid {
			o.invalid = true
		}
	}
	if !o.invalid {
		node := gjson.ParseBytes(usageRaw)
		for _, field := range []struct {
			name string
			dst  *int
		}{{"input_tokens", &o.usage.InputTokens}, {"output_tokens", &o.usage.OutputTokens}, {"cache_creation_input_tokens", &o.usage.CacheCreationInputTokens}, {"cache_read_input_tokens", &o.usage.CacheReadInputTokens}, {"cache_creation.ephemeral_5m_input_tokens", &o.usage.CacheCreation5mTokens}, {"cache_creation.ephemeral_1h_input_tokens", &o.usage.CacheCreation1hTokens}} {
			if value := node.Get(field.name); value.Exists() {
				*field.dst = int(value.Int())
			}
		}
		if !node.Get("cache_read_input_tokens").Exists() {
			if value := node.Get("cached_tokens"); value.Exists() {
				o.usage.CacheReadInputTokens = int(value.Int())
			}
		}
		if !node.Get("cache_creation_input_tokens").Exists() && (node.Get("cache_creation.ephemeral_5m_input_tokens").Exists() || node.Get("cache_creation.ephemeral_1h_input_tokens").Exists()) {
			o.usage.CacheCreationInputTokens = o.usage.CacheCreation5mTokens + o.usage.CacheCreation1hTokens
		}
		normalizeAnthropicCompatiblePromptUsage(node, &o.usage)
	}
	if any {
		o.observed = true
		o.source = source
	}
	if kind == "message_delta" {
		var delta map[string]json.RawMessage
		if json.Unmarshal(event["delta"], &delta) == nil {
			var stop string
			_ = json.Unmarshal(delta["stop_reason"], &stop)
			if stop != "" {
				o.terminal = true
			}
		}
	}
	if kind == "" || kind == "message" {
		o.terminal = true
	}
	// A null usage object is never equivalent to an explicit all-zero bill.
	if bytes.Equal(bytes.TrimSpace(usageRaw), []byte("null")) {
		o.invalid = true
	}
}

func attachSubscriptionQuotaMetering(c *gin.Context, result *ForwardResult) {
	o := subscriptionQuotaMetering(c)
	if o == nil || result == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	result.UsageObserved = o.observed && !o.invalid
	result.UsageFinal = o.input && o.output && o.terminal && !o.invalid
	result.UsageEvidenceSource = o.source
	result.Usage = o.usage
	result.UpstreamResponseModel = observedUpstreamResponseModel(c)
	result.UpstreamResponseModelConflict = observedUpstreamResponseModelConflict(c)
	result.UpstreamResponseServiceTier = observedUpstreamResponseServiceTier(c)
}

func subscriptionQuotaHasServerToolEvidence(node gjson.Result) bool {
	if node.Get("usage.server_tool_use").Exists() || node.Get("message.usage.server_tool_use").Exists() {
		return true
	}
	blocks := append(node.Get("content").Array(), node.Get("message.content").Array()...)
	blocks = append(blocks, node.Get("content_block"))
	for _, block := range blocks {
		switch block.Get("type").String() {
		case "server_tool_use", "web_search_tool_result", "web_fetch_tool_result", "code_execution_tool_result":
			return true
		}
	}
	return false
}

func completeSubscriptionQuotaMetering(c *gin.Context) bool {
	o := subscriptionQuotaMetering(c)
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.input && o.output && o.terminal && !o.invalid
}
