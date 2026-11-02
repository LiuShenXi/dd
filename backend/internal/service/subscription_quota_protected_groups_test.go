//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type protectedGroupMemoryRepo struct {
	state      map[string]string
	payload    map[string]json.RawMessage
	admissions int
	err        error
}

func newProtectedGroupMemoryRepo() *protectedGroupMemoryRepo {
	return &protectedGroupMemoryRepo{state: map[string]string{}, payload: map[string]json.RawMessage{}}
}
func (r *protectedGroupMemoryRepo) AdmitProtectedGroup(ctx context.Context, in SubscriptionQuotaProtectedAdmission) error {
	r.admissions++
	if r.err != nil {
		return r.err
	}
	r.state[in.RequestID] = "admitted"
	return ctx.Err()
}
func (r *protectedGroupMemoryRepo) RecordProtectedGroupUsage(ctx context.Context, id string, p json.RawMessage) error {
	if r.err != nil {
		return r.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.state[id] = "usage_pending"
	r.payload[id] = append(json.RawMessage(nil), p...)
	return nil
}
func (r *protectedGroupMemoryRepo) FinishProtectedGroup(ctx context.Context, id, state, reason string) error {
	if r.err != nil {
		return r.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.state[id] = state
	return nil
}
func (r *protectedGroupMemoryRepo) CountProtectedGroupUnfinished(ctx context.Context, groupID int64) (int64, error) {
	var n int64
	for _, state := range r.state {
		if state != "completed" && state != "no_charge" {
			n++
		}
	}
	return n, ctx.Err()
}

func protectedTestKey(groupID int64) *APIKey {
	return &APIKey{ID: 10, UserID: 20, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformAnthropic, SubscriptionType: SubscriptionTypeStandard}}
}

func TestSubscriptionQuotaProtectedAdmissionIsolation(t *testing.T) {
	repo := newProtectedGroupMemoryRepo()
	gate := NewSubscriptionQuotaProtectedGroupService(repo, []int64{6})
	key := protectedTestKey(6)
	for _, platform := range []string{PlatformOpenAI, PlatformGemini, PlatformAntigravity} {
		_, err := gate.Admit(context.Background(), key, key, platform)
		require.ErrorIs(t, err, ErrSubscriptionQuotaProtectedUnavailable)
	}
	_, err := gate.Admit(context.Background(), key, protectedTestKey(7), PlatformAnthropic)
	require.ErrorIs(t, err, ErrSubscriptionQuotaProtectedUnavailable)
	unprotected := protectedTestKey(2)
	unprotected.Group.Platform = PlatformOpenAI
	got, err := gate.Admit(context.Background(), unprotected, unprotected, PlatformOpenAI)
	require.NoError(t, err)
	require.Nil(t, got)
	require.Zero(t, repo.admissions, "GPT never calls the protected repository")
	repo.err = errors.New("db unavailable")
	_, err = gate.Admit(context.Background(), key, key, PlatformAnthropic)
	require.ErrorIs(t, err, ErrSubscriptionQuotaProtectedUnavailable)
}

func TestSubscriptionQuotaProtectedDrainWaitsForUsage(t *testing.T) {
	repo := newProtectedGroupMemoryRepo()
	gate := NewSubscriptionQuotaProtectedGroupService(repo, []int64{6})
	key := protectedTestKey(6)
	request, err := gate.Admit(context.Background(), key, key, PlatformAnthropic)
	require.NoError(t, err)
	result := &ForwardResult{RequestID: "upstream-1", Model: "claude-test", Usage: ClaudeUsage{InputTokens: 10, OutputTokens: 2}, ClientDisconnect: true}
	require.NoError(t, request.PrepareUsage(result, 30, time.Now()))
	require.NoError(t, request.ForwardEnded(), "HTTP exit after enqueue does not release the marker")
	count, err := gate.CountUnfinished(context.Background(), 6)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Equal(t, "usage_pending", repo.state[request.id])
	var saved map[string]any
	require.NoError(t, json.Unmarshal(repo.payload[request.id], &saved))
	require.Equal(t, "upstream-1", saved["upstream_request_id"])
	require.EqualValues(t, 30, saved["account_id"])
	require.Equal(t, true, saved["client_disconnect"])
	require.NoError(t, request.FinishUsage(nil), "trusted usage remains chargeable after client disconnect")
	count, err = gate.CountUnfinished(context.Background(), 6)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestSubscriptionQuotaProtectedUnknownAndFailureStayHeld(t *testing.T) {
	for _, scenario := range []string{"no usage", "zero is ambiguous", "billing failure", "persistence failure", "crash before usage"} {
		t.Run(scenario, func(t *testing.T) {
			repo := newProtectedGroupMemoryRepo()
			gate := NewSubscriptionQuotaProtectedGroupService(repo, []int64{6})
			key := protectedTestKey(6)
			request, err := gate.Admit(context.Background(), key, key, PlatformAnthropic)
			require.NoError(t, err)
			switch scenario {
			case "no usage":
				require.NoError(t, request.ForwardEnded())
			case "zero is ambiguous":
				require.NoError(t, request.PrepareUsage(&ForwardResult{}, 30, time.Now()))
				require.NoError(t, request.FinishUsage(nil))
			case "billing failure":
				require.NoError(t, request.PrepareUsage(&ForwardResult{Usage: ClaudeUsage{InputTokens: 1}}, 30, time.Now()))
				require.NoError(t, request.FinishUsage(errors.New("settlement failed")))
			case "persistence failure":
				repo.err = errors.New("storage down")
				require.Error(t, request.PrepareUsage(&ForwardResult{Usage: ClaudeUsage{InputTokens: 1}}, 30, time.Now()))
				repo.err = nil
				require.NoError(t, request.FinishUsage(nil))
			case "crash before usage": // Do not invoke any process-local callback; durable admitted is still counted.
			}
			count, err := gate.CountUnfinished(context.Background(), 6)
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestSubscriptionQuotaProtectedDisabledOrMissingDependency(t *testing.T) {
	key := protectedTestKey(6)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.SubscriptionQuota.ProtectedGroupIDs = []int64{6}
	billing := &BillingCacheService{cfg: cfg}
	_, err := billing.AdmitSubscriptionQuotaProtectedGroup(context.Background(), key, key, PlatformAnthropic)
	require.ErrorIs(t, err, ErrSubscriptionQuotaProtectedUnavailable)
	empty := NewSubscriptionQuotaProtectedGroupService(nil, nil)
	got, err := empty.Admit(context.Background(), key, key, PlatformAnthropic)
	require.NoError(t, err)
	require.Nil(t, got)
	protected := NewSubscriptionQuotaProtectedGroupService(nil, []int64{6})
	_, err = protected.Admit(context.Background(), key, key, PlatformAnthropic)
	require.ErrorIs(t, err, ErrSubscriptionQuotaProtectedUnavailable)
	err = (&GatewayService{}).RecordProtectedGroupUsage(context.Background(), nil, &SubscriptionQuotaProtectedRequest{})
	require.Error(t, err, "no legacy multi-write fallback after an admission")
}

func TestSubscriptionQuotaProtectedRouteAllowlist(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses", "/v1/messages/count_tokens", "/messages/count_tokens"} {
		require.True(t, SubscriptionQuotaProtectedRouteAllowed("POST", path), path)
	}
	for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses", "/antigravity/models", "/v1beta/models", "/models/a/b"} {
		require.False(t, SubscriptionQuotaProtectedRouteAllowed("GET", path), path)
	}
	for _, path := range []string{"/v1/responses/compact", "/responses/input_tokens", "/v1/live", "/v1/images/generations", "/v1beta/models/test:generateContent", "/antigravity/v1/messages", "/api/v3/contents/generations/tasks"} {
		require.False(t, SubscriptionQuotaProtectedRouteAllowed("POST", path), path)
	}
}

func TestSubscriptionQuotaProtectedCountTokensDeferredExit(t *testing.T) {
	repo := newProtectedGroupMemoryRepo()
	gate := NewSubscriptionQuotaProtectedGroupService(repo, []int64{6})
	key := protectedTestKey(6)
	request, err := gate.Admit(context.Background(), key, key, PlatformAnthropic)
	require.NoError(t, err)
	require.NoError(t, request.FinishCountTokens())
	require.NoError(t, request.ForwardEnded(), "deferred HTTP exit must not attempt to reverse known no-charge")
	require.Equal(t, "no_charge", repo.state[request.id])
	count, err := gate.CountUnfinished(context.Background(), 6)
	require.NoError(t, err)
	require.Zero(t, count)
}
