package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const codexSentinelCapacity = 1024
const codexSentinelCooldown = 30 * time.Second

type codexSentinelEvent struct {
	Seq                   uint64    `json:"seq"`
	AccountID             int64     `json:"account_id"`
	Model                 string    `json:"model"`
	Kind                  string    `json:"kind"`
	ExpectedTicketVersion string    `json:"expected_ticket_version"`
	ActualModel           string    `json:"actual_model,omitempty"`
	ObservedAt            time.Time `json:"observed_at"`
	sentAt                time.Time
}
type codexSentinelTarget struct {
	AccountID        int64      `json:"account_id"`
	Model            string     `json:"model"`
	Eligible         bool       `json:"eligible"`
	Ready            bool       `json:"ready"`
	TicketVersion    string     `json:"ticket_version"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	CapturedAt       *time.Time `json:"captured_at,omitempty"`
	RemainingSeconds int64      `json:"remaining_seconds"`
}
type codexSentinel struct {
	tokenHash   [32]byte
	instance    string
	accounts    []int64
	models      []string
	allowed     map[string]bool
	enabled     atomic.Bool
	mu          sync.Mutex
	events      [codexSentinelCapacity]codexSentinelEvent
	seq         uint64
	nextAttempt map[string]time.Time
}

// Configuration is read once. Any missing or malformed input fails closed.
func newCodexSentinelFromEnv() *codexSentinel {
	path := strings.TrimSpace(os.Getenv("CODEX_SENTINEL_TOKEN_FILE"))
	if path == "" {
		return nil
	}
	file, err := os.Open(path) // #nosec G703 -- Path is a trusted administrator environment setting, never request input; file type, permissions and bounded size are checked below.
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) > 4096 {
		return nil
	}
	token := strings.TrimSpace(string(raw))
	// 32 random bytes encoded as hex/base64 are the supported deployment format.
	if len(token) < 43 || len(token) > 256 || strings.ContainsAny(token, " \t\r\n") {
		return nil
	}
	s := &codexSentinel{tokenHash: sha256.Sum256([]byte(token)), allowed: make(map[string]bool), nextAttempt: make(map[string]time.Time)}
	ids := strings.Split(os.Getenv("CODEX_SENTINEL_ACCOUNT_IDS"), ",")
	models := strings.Split(os.Getenv("CODEX_SENTINEL_MODELS"), ",")
	if len(ids) > 32 || len(models) > 8 {
		return nil
	}
	seen := make(map[int64]bool)
	for _, raw := range ids {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id <= 0 {
			return nil
		}
		if !seen[id] {
			s.accounts = append(s.accounts, id)
			seen[id] = true
		}
	}
	seenModels := make(map[string]bool)
	for _, raw := range models {
		model := normalizeOpenAICodexTicketModel(raw)
		if model == "" || len(model) > 200 {
			return nil
		}
		for _, c := range model {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' && c != '_' {
				return nil
			}
		}
		if !seenModels[model] {
			s.models = append(s.models, model)
			seenModels[model] = true
		}
	}
	for _, id := range s.accounts {
		for _, model := range s.models {
			s.allowed[openAICodexTicketKey(id, model)] = true
		}
	}
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return nil
	}
	s.instance = hex.EncodeToString(nonce)
	return s
}

func (s *OpenAIGatewayService) codexSentinelAuthorize(c *gin.Context) *codexSentinel {
	if s == nil || s.codexSentinel == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return nil
	}
	auth := c.GetHeader("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || len(auth) > 263 {
		c.AbortWithStatus(http.StatusUnauthorized)
		return nil
	}
	digest := sha256.Sum256([]byte(strings.TrimPrefix(auth, "Bearer ")))
	if subtle.ConstantTimeCompare(digest[:], s.codexSentinel.tokenHash[:]) != 1 {
		c.AbortWithStatus(http.StatusUnauthorized)
		return nil
	}
	c.Header("Cache-Control", "no-store")
	return s.codexSentinel
}

func codexTicketVersion(ticket *openAICodexTicket) string {
	if ticket == nil {
		return ""
	}
	digest := sha256.Sum256([]byte(ticket.State))
	return hex.EncodeToString(digest[:])
}
func (s *OpenAIGatewayService) sentinelEligible(ctx context.Context, a *Account, model string) bool {
	return a != nil && s.codexSentinel.allowed[openAICodexTicketKey(a.ID, model)] && isOpenAICodexTicketAccount(a) && a.IsSchedulable() && s.openAICodexTicketGatedModel(model) && s.openAICodexTicketHarvestProxyURLContext(ctx) != ""
}
func (s *OpenAIGatewayService) sentinelVersion(a *Account, model string) string {
	ticket := s.lookupOpenAICodexTicket(a, model)
	if !ticket.valid(time.Now(), s.openAICodexTicketConfig().TargetLength) {
		return ""
	}
	return codexTicketVersion(ticket)
}

func (s *OpenAIGatewayService) CodexSentinelStatus(c *gin.Context) {
	sentinel := s.codexSentinelAuthorize(c)
	if sentinel == nil {
		return
	}
	after := uint64(0)
	if raw := c.Query("after"); raw != "" {
		var err error
		after, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			c.AbortWithStatus(400)
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	enabled := s.openAICodexTicketEnabledContext(ctx)
	sentinel.enabled.Store(enabled)
	events, latest, oldest := sentinel.snapshot(after)
	targets := make([]codexSentinelTarget, 0, len(sentinel.allowed))
	for _, id := range sentinel.accounts {
		var account *Account
		if s.accountRepo != nil {
			account, _ = s.accountRepo.GetByID(ctx, id)
		}
		for _, model := range sentinel.models {
			t := codexSentinelTarget{AccountID: id, Model: model, Eligible: s.sentinelEligible(ctx, account, model)}
			if enabled && t.Eligible {
				ticket := s.lookupOpenAICodexTicket(account, model)
				if ticket.valid(time.Now(), s.openAICodexTicketConfig().TargetLength) {
					t.Ready = true
					t.TicketVersion = codexTicketVersion(ticket)
					t.ExpiresAt = &ticket.ExpiresAt
					t.CapturedAt = &ticket.CapturedAt
					t.RemainingSeconds = int64(time.Until(ticket.ExpiresAt).Seconds())
				}
			}
			targets = append(targets, t)
		}
	}
	dropped := uint64(0)
	if oldest > after && oldest-after > 1 {
		dropped = oldest - after - 1
	}
	c.JSON(200, gin.H{"protocol_version": 1, "instance_id": sentinel.instance, "enabled": enabled, "latest_seq": latest, "oldest_seq": oldest, "events": events, "targets": targets, "dropped_events": dropped})
}
func (s *codexSentinel) snapshot(after uint64) ([]codexSentinelEvent, uint64, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldest := uint64(1)
	if s.seq >= codexSentinelCapacity {
		oldest = s.seq - codexSentinelCapacity + 1
	}
	if s.seq == 0 {
		oldest = 1
	}
	events := make([]codexSentinelEvent, 0)
	start := oldest
	if after >= start {
		if after >= s.seq {
			return events, s.seq, oldest
		}
		start = after + 1
	}
	for i := start; i <= s.seq; i++ {
		events = append(events, s.events[(i-1)%codexSentinelCapacity])
	}
	return events, s.seq, oldest
}

// Observations retain only a ticket digest and routing metadata, never the blob.
// The timestamp belongs to the outbound attempt, so a late completion cannot
// refresh a ticket that has already been recaptured (even if its blob is equal).
type codexSentinelObservation struct {
	sentinel       *codexSentinel
	accountID      int64
	model, version string
	sentAt         time.Time
}

func (s *OpenAIGatewayService) sentinelObservation(account *Account, model string, h http.Header) *codexSentinelObservation {
	sentinel := s.codexSentinel
	model = normalizeOpenAICodexTicketModel(model)
	if sentinel == nil || account == nil || !sentinel.enabled.Load() || !sentinel.allowed[openAICodexTicketKey(account.ID, model)] {
		return nil
	}
	state := strings.TrimSpace(h.Get(openAICodexTurnStateHeader))
	if len(state) != 292 || !strings.HasPrefix(state, openAICodexTicketStatePrefix) {
		return nil
	}
	digest := sha256.Sum256([]byte(state))
	return &codexSentinelObservation{sentinel: sentinel, accountID: account.ID, model: model, version: hex.EncodeToString(digest[:]), sentAt: time.Now()}
}
func (o *codexSentinelObservation) forModel(model string) *codexSentinelObservation {
	if o == nil {
		return nil
	}
	copy := *o
	copy.model = normalizeOpenAICodexTicketModel(model)
	// Keep the physical handshake timestamp for reused WS connections.
	return &copy
}
func (o *codexSentinelObservation) emit(kind, actual string) {
	if o == nil || !o.sentinel.enabled.Load() || !o.sentinel.allowed[openAICodexTicketKey(o.accountID, o.model)] {
		return
	}
	s := o.sentinel
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	s.events[(s.seq-1)%codexSentinelCapacity] = codexSentinelEvent{Seq: s.seq, AccountID: o.accountID, Model: o.model, Kind: kind, ExpectedTicketVersion: o.version, ActualModel: normalizeObservedUpstreamResponseModel(actual), ObservedAt: time.Now().UTC(), sentAt: o.sentAt}
}
func (o *codexSentinelObservation) completed(actual string) {
	if o != nil && actual != "" && !strings.EqualFold(o.model, strings.TrimSpace(actual)) {
		o.emit("model_mismatch", actual)
	}
}
func (o *codexSentinelObservation) header(h http.Header) {
	if o != nil && len(strings.TrimSpace(h.Get(openAICodexTurnStateHeader))) == 312 {
		o.emit("turn_state_312", "")
	}
}

type codexSentinelObservationKey struct{}

func (s *OpenAIGatewayService) bindSentinelRequest(c *gin.Context, req *http.Request, account *Account, model string) *http.Request {
	observation := s.sentinelObservation(account, model, req.Header)
	if c != nil {
		observer := upstreamResponseModelObserverFromContext(c)
		if observer == nil {
			observer = beginUpstreamResponseModelObservation(c)
		}
		observer.codexSentinel = observation
	}
	return req.WithContext(context.WithValue(req.Context(), codexSentinelObservationKey{}, observation))
}
func sentinelRequestObservation(req *http.Request) *codexSentinelObservation {
	if req == nil {
		return nil
	}
	o, _ := req.Context().Value(codexSentinelObservationKey{}).(*codexSentinelObservation)
	return o
}
func (s *OpenAIGatewayService) sentinelHandshake(account *Account, model string) func(http.Header) *codexSentinelObservation {
	return func(request http.Header) *codexSentinelObservation {
		return s.sentinelObservation(account, model, request)
	}
}

type codexSentinelRefreshRequest struct {
	AccountID       int64  `json:"account_id"`
	Model           string `json:"model"`
	Reason          string `json:"reason"`
	ExpectedVersion string `json:"expected_ticket_version"`
	EventID         string `json:"event_id"`
}

func (s *OpenAIGatewayService) CodexSentinelRefresh(c *gin.Context) {
	sentinel := s.codexSentinelAuthorize(c)
	if sentinel == nil {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var in codexSentinelRefreshRequest
	if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF || len(in.ExpectedVersion) > 64 || len(in.EventID) > 100 {
		c.AbortWithStatus(400)
		return
	}
	if in.Model != normalizeOpenAICodexTicketModel(in.Model) {
		c.AbortWithStatus(400)
		return
	}
	switch in.Reason {
	case "turn_state_312", "model_mismatch", "expiry", "missing":
	default:
		c.AbortWithStatus(400)
		return
	}
	var previousCaptured, captured time.Time
	var previousVersion string
	reply := func(code int, status, reason string, retry int, version string) {
		body := gin.H{"status": status, "account_id": in.AccountID, "model": in.Model}
		if reason != "" {
			body["reason"] = reason
		}
		if retry > 0 {
			body["retry_after_seconds"] = retry
		}
		if version != "" {
			body["ticket_version"] = version
		}
		if status == "refreshed" || status == "renewed" {
			body["persisted"] = true
			body["ready"] = true
		}
		if status == "renewed" {
			body["previous_ticket_version"] = previousVersion
			body["captured_at_unix_ms"] = captured.UnixMilli()
			body["previous_captured_at_unix_ms"] = previousCaptured.UnixMilli()
		}
		c.JSON(code, body)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	enabled := s.openAICodexTicketEnabledContext(ctx)
	sentinel.enabled.Store(enabled)
	if !enabled {
		reply(200, "disabled", "", 0, "")
		return
	}
	key := openAICodexTicketKey(in.AccountID, in.Model)
	if !sentinel.allowed[key] || s.accountRepo == nil {
		reply(200, "ineligible", "", 0, "")
		return
	}
	account, err := s.accountRepo.GetByID(ctx, in.AccountID)
	if err != nil || !s.sentinelEligible(ctx, account, in.Model) {
		reply(200, "ineligible", "", 0, "")
		return
	}
	if s.sentinelVersion(account, in.Model) != in.ExpectedVersion {
		reply(200, "stale", "", 0, "")
		return
	}
	expectedCaptured := time.Time{}
	if current := s.lookupOpenAICodexTicket(account, in.Model); current != nil {
		expectedCaptured = current.CapturedAt
		previousCaptured = current.CapturedAt
		previousVersion = codexTicketVersion(current)
	}
	sentinel.mu.Lock()
	// Anomaly requests must reference an event from this process, not a made-up
	// timestamp. Expired ring entries are safely stale and never trigger a probe.
	stale := false
	if in.Reason == "turn_state_312" || in.Reason == "model_mismatch" {
		prefix := sentinel.instance + ":"
		seq, parseErr := strconv.ParseUint(strings.TrimPrefix(in.EventID, prefix), 10, 64)
		if !strings.HasPrefix(in.EventID, prefix) || parseErr != nil || seq == 0 || seq > sentinel.seq || sentinel.seq-seq >= codexSentinelCapacity {
			stale = true
		} else {
			event := sentinel.events[(seq-1)%codexSentinelCapacity]
			ticket := s.lookupOpenAICodexTicket(account, in.Model)
			stale = event.AccountID != in.AccountID || event.Model != in.Model || event.Kind != in.Reason || event.ExpectedTicketVersion != in.ExpectedVersion || ticket != nil && ticket.CapturedAt.After(event.sentAt)
		}
	}
	if stale {
		sentinel.mu.Unlock()
		reply(200, "stale", "", 0, "")
		return
	}
	now := time.Now()
	next := sentinel.nextAttempt[key]
	if now.Before(next) {
		retry := int(time.Until(next).Seconds()) + 1
		sentinel.mu.Unlock()
		reply(429, "cooldown", "", retry, "")
		return
	}
	sentinel.nextAttempt[key] = now.Add(codexSentinelCooldown)
	sentinel.mu.Unlock()
	ticket, err := s.collectOpenAICodexTicket(ctx, account, in.Model, func() error {
		// Recheck inside singleflight immediately before collection: a native
		// harvester may have won between the HTTP validation and acquiring this key.
		latest, readErr := s.accountRepo.GetByID(ctx, in.AccountID)
		if readErr != nil {
			return errCodexTicketCollection
		}
		if !s.sentinelEligible(ctx, latest, in.Model) {
			return errCodexTicketStale
		}
		current := s.lookupOpenAICodexTicket(latest, in.Model)
		if s.sentinelVersion(latest, in.Model) != in.ExpectedVersion || current != nil && current.CapturedAt.After(expectedCaptured) {
			return errCodexTicketStale
		}
		return nil
	})
	if errors.Is(err, errCodexTicketStale) {
		reply(200, "stale", "", 0, "")
		return
	}
	if err != nil || ticket == nil {
		reply(503, "failed", "collection_failed", 30, "")
		return
	}
	if !s.openAICodexTicketEnabledContext(ctx) {
		sentinel.enabled.Store(false)
		reply(200, "disabled", "", 0, "")
		return
	}
	version := codexTicketVersion(ticket)
	captured = ticket.CapturedAt
	renewed := version == previousVersion
	if renewed && (previousCaptured.UnixMilli() <= 0 || captured.UnixMilli() <= previousCaptured.UnixMilli()) {
		reply(503, "failed", "recapture_unverified", 30, "")
		return
	}
	persisted, err := s.accountRepo.GetByID(ctx, in.AccountID)
	if err != nil || !codexTicketGenerationMatches(parseOpenAICodexTicketExtra(persisted, in.Model), ticket) {
		reply(503, "failed", "persistence_unverified", 30, "")
		return
	}
	if s.schedulerSnapshot == nil || s.schedulerSnapshot.cache == nil {
		reply(503, "failed", "cache_unavailable", 30, "")
		return
	}
	if err = s.schedulerSnapshot.UpdateAccountInCache(ctx, persisted); err != nil {
		reply(503, "failed", "cache_update_failed", 30, "")
		return
	}
	cached, err := s.schedulerSnapshot.cache.GetAccount(ctx, in.AccountID)
	if err != nil || !codexTicketGenerationMatches(parseOpenAICodexTicketExtra(cached, in.Model), ticket) {
		reply(503, "failed", "cache_unverified", 30, "")
		return
	}
	if !ticket.valid(time.Now(), s.openAICodexTicketConfig().TargetLength) {
		reply(503, "failed", "expired", 30, "")
		return
	}
	if renewed {
		reply(200, "renewed", "", 0, version)
	} else {
		reply(200, "refreshed", "", 0, version)
	}
}

// Matching only the blob cannot prove persistence of a same-blob renewal.
func codexTicketGenerationMatches(actual, expected *openAICodexTicket) bool {
	return actual != nil && expected != nil && actual.State == expected.State &&
		actual.CapturedAt.Equal(expected.CapturedAt) && actual.ExpiresAt.Equal(expected.ExpiresAt)
}

func parseOpenAICodexTicketExtra(account *Account, model string) *openAICodexTicket {
	if account == nil {
		return nil
	}
	return parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
}

var errCodexTicketCollection = errors.New("codex ticket collection failed")
var errCodexTicketStale = errors.New("codex ticket version changed")

func (s *OpenAIGatewayService) codexSentinelWSHeadersFactory(account *Account, model string) func(context.Context, http.Header) (http.Header, error) {
	return func(ctx context.Context, headers http.Header) (http.Header, error) {
		if err := s.applyOpenAICodexTicket(ctx, account, model, headers); err != nil {
			return nil, err
		}
		return s.refreshOpenAIAgentIdentityHeaders(ctx, account, headers)
	}
}
