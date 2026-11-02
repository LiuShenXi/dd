//go:build unit

package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type termConcurrencyRouterCache struct {
	fakeConcurrencyCache
	accounts, releases atomic.Int32
	events             []string
	beforeAcquire      func(int64)
}

type termConcurrencyRouterAccountRepo struct{ service.AccountRepository }

func (termConcurrencyRouterAccountRepo) SetRateLimited(context.Context, int64, time.Time) error {
	return nil
}
func (termConcurrencyRouterAccountRepo) UpdateSessionWindow(context.Context, int64, *time.Time, *time.Time, string) error {
	return nil
}

func (c *termConcurrencyRouterCache) AcquireAccountSlot(_ context.Context, id int64, _ int, _ string) (bool, error) {
	c.accounts.Add(1)
	c.events = append(c.events, fmt.Sprintf("acquire:%d", id))
	if c.beforeAcquire != nil {
		c.beforeAcquire(id)
	}
	return true, nil
}
func (c *termConcurrencyRouterCache) ReleaseAccountSlot(_ context.Context, id int64, _ string) error {
	c.releases.Add(1)
	c.events = append(c.events, fmt.Sprintf("release:%d", id))
	return nil
}

type termConcurrencyRouterBilling struct {
	termHandlerBilling
	noCharges atomic.Int32
}

func (b *termConcurrencyRouterBilling) MarkNoCharge(context.Context, *domain.SubscriptionQuotaBillingSnapshot, domain.SubscriptionQuotaNoChargeEvidence) error {
	b.noCharges.Add(1)
	return nil
}

type termConcurrencyRouterUpstream struct {
	mode               string
	calls              atomic.Int32
	before             func()
	entered, cancelled chan struct{}
	finish             <-chan struct{}
}

func (u *termConcurrencyRouterUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls.Add(1)
	if u.before != nil {
		u.before()
	}
	if u.mode == "panic" {
		panic("synthetic handler forward dependency panic")
	}
	if u.mode == "cancel_held" {
		close(u.entered)
		<-req.Context().Done()
		close(u.cancelled)
		<-u.finish
		return nil, req.Context().Err()
	}
	return &http.Response{StatusCode: 429, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"rate_limit_error","message":"synthetic rejection"}}`))}, nil
}
func (u *termConcurrencyRouterUpstream) DoWithTLS(req *http.Request, proxy string, id int64, limit int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, limit)
}

func TestSubscriptionQuotaConcurrencyV2ActualRouterForwardPanicAndNoPostSendSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []struct {
		path, body string
		invoke     func(*GatewayHandler, *gin.Context)
	}{
		{"/v1/messages", `{"model":"claude-sonnet-4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, (*GatewayHandler).Messages},
		{"/v1/chat/completions", `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}]}`, (*GatewayHandler).ChatCompletions},
		{"/v1/responses", `{"model":"claude-sonnet-4","input":"hi"}`, (*GatewayHandler).Responses},
	} {
		for _, mode := range []string{"panic", "rejected", "lease_lost_before_selection", "cancel_held", "pre_send_switch"} {
			t.Run(route.path+"/"+mode, func(t *testing.T) {
				key := termHandlerKey()
				key.Status = service.StatusActive
				key.Group.Hydrated, key.Group.Status = true, service.StatusActive
				key.User = &service.User{ID: key.UserID, Status: service.StatusActive, Concurrency: 5}
				cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 4 << 20}}
				cfg.SubscriptionQuota.AdmissionEnabled = true
				cfg.SubscriptionQuota.EnabledGroupIDs = []int64{key.Group.ID}
				billing := &termConcurrencyRouterBilling{termHandlerBilling: termHandlerBilling{unknown: make(chan struct{}, 1)}}
				cache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(cache.Stop)
				cache.SetSubscriptionQuotaGatewayBilling(billing)
				account := &service.Account{ID: 51, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: 1, Credentials: map[string]any{"api_key": "synthetic"}, Extra: map[string]any{"anthropic_passthrough": true}, AccountGroups: []service.AccountGroup{{AccountID: 51, GroupID: key.Group.ID}}}
				other := *account
				other.ID = 52
				other.Priority = 2
				other.AccountGroups = []service.AccountGroup{{AccountID: 52, GroupID: key.Group.ID}}
				scheduler := &countingGatewaySchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{account, &other}}}
				concurrencyCache := &termConcurrencyRouterCache{}
				if mode == "pre_send_switch" {
					key.Group.ProfitControlEnabled, key.Group.ProfitMinMargin = true, 0.5
					goodRate := 0.1
					account.RateMultiplier, other.RateMultiplier = &goodRate, &goodRate
					concurrencyCache.beforeAcquire = func(id int64) {
						if id != account.ID {
							return
						}
						// The first account becomes ineligible only after its slot is held.
						// The real terminal profit recheck must veto it before any sender/admit.
						bad := *account
						badRate := 2.0
						bad.RateMultiplier, bad.UpdatedAt = &badRate, time.Now().Add(time.Second)
						scheduler.accounts[0] = &bad
					}
				}
				concurrency := service.NewConcurrencyService(concurrencyCache)
				upstream := &termConcurrencyRouterUpstream{mode: mode}
				svc := service.NewGatewayService(nil, &fakeGroupRepo{group: key.Group}, nil, nil, nil, nil, nil, nil, cfg, service.NewSchedulerSnapshotService(scheduler, nil, nil, nil, nil), concurrency, service.NewBillingService(cfg, nil), service.NewRateLimitService(termConcurrencyRouterAccountRepo{}, nil, cfg, nil, nil), cache, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
				h := &GatewayHandler{gatewayService: svc, billingCacheService: cache, cfg: cfg, maxAccountSwitches: 2, concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatClaude, time.Second)}
				var userAcquires, userReleases atomic.Int32
				h.concurrencyHelper.claudeConcurrency = termConcurrencyProviderFunc(func(ctx context.Context, _ int64, _ time.Duration, _ func() error) (*service.UserScopeLease, error) {
					userAcquires.Add(1)
					if mode == "lease_lost_before_selection" {
						causeCtx, cancel := context.WithCancelCause(ctx)
						cancel(service.ErrTermConcurrencyUnavailable)
						ctx = causeCtx
					}
					return service.NewUserScopeLease(ctx, func() { userReleases.Add(1) }), nil
				})
				upstream.before = func() {
					if mode == "pre_send_switch" {
						require.EqualValues(t, 2, concurrencyCache.accounts.Load())
						require.EqualValues(t, 1, concurrencyCache.releases.Load())
						require.EqualValues(t, 52, billing.snapshot.AccountID)
					} else {
						require.EqualValues(t, 1, concurrencyCache.accounts.Load())
						require.Zero(t, concurrencyCache.releases.Load())
					}
					require.Zero(t, userReleases.Load())
					require.NotNil(t, billing.snapshot)
				}
				panics := 0
				router := gin.New()
				router.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, recovered any) {
					panics++
					require.Equal(t, "synthetic handler forward dependency panic", recovered)
					c.AbortWithStatus(500)
				}))
				router.POST(route.path, func(c *gin.Context) {
					c.Set(string(middleware.ContextKeyAPIKey), key)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 5})
					c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, key.Group))
					route.invoke(h, c)
				})
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, route.path, bytes.NewBufferString(route.body))
				if mode == "cancel_held" {
					parent, cancel := context.WithCancel(context.Background())
					defer cancel()
					finish, done := make(chan struct{}), make(chan struct{})
					upstream.entered, upstream.cancelled, upstream.finish = make(chan struct{}), make(chan struct{}), finish
					defer func() {
						select {
						case <-finish:
						default:
							close(finish)
						}
					}()
					go func() { defer close(done); router.ServeHTTP(response, request.WithContext(parent)) }()
					select {
					case <-upstream.entered:
					case <-time.After(3 * time.Second):
						t.Fatal("actual handler did not reach the admitted sender")
					}
					cancel()
					select {
					case <-upstream.cancelled:
					case <-time.After(3 * time.Second):
						t.Fatal("handler cancellation did not reach the selected sender")
					}
					require.Zero(t, userReleases.Load(), "cancelled local Forward still owns the user lease")
					require.Zero(t, concurrencyCache.releases.Load(), "cancelled local Forward still owns the account slot")
					close(finish)
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Fatal("handler did not finish after its sender returned")
					}
				} else {
					router.ServeHTTP(response, request)
				}
				require.EqualValues(t, 1, userReleases.Load(), response.Body.String())
				require.EqualValues(t, 1, userAcquires.Load())
				if mode == "lease_lost_before_selection" {
					require.Equal(t, 503, response.Code, response.Body.String())
					require.Contains(t, response.Body.String(), "TERM_CONCURRENCY_UNAVAILABLE")
					require.Zero(t, scheduler.snapshotCalls.Load())
					require.Zero(t, upstream.calls.Load())
					require.Zero(t, concurrencyCache.accounts.Load())
					return
				}
				if mode == "pre_send_switch" {
					require.Zero(t, panics)
					require.EqualValues(t, 1, upstream.calls.Load(), "the first vetoed account never reached the sender")
					require.Equal(t, []string{"acquire:51", "release:51", "acquire:52", "release:52"}, concurrencyCache.events)
					require.EqualValues(t, 1, billing.noCharges.Load())
					require.NotEqual(t, 200, response.Code)
					return
				}
				require.EqualValues(t, 1, upstream.calls.Load(), "another same-group account is available, but an admitted attempt cannot fail over")
				require.EqualValues(t, 1, concurrencyCache.accounts.Load())
				require.EqualValues(t, 1, concurrencyCache.releases.Load(), "the actual handler must release once on normal or panic unwind")
				if mode == "cancel_held" {
					require.Zero(t, panics)
					require.Zero(t, billing.noCharges.Load())
					require.Len(t, billing.unknown, 1)
				} else if mode == "panic" {
					require.Equal(t, 1, panics)
					require.Equal(t, 500, response.Code)
					require.Zero(t, billing.noCharges.Load())
					require.Len(t, billing.unknown, 1)
				} else {
					require.Zero(t, panics)
					require.EqualValues(t, 1, billing.noCharges.Load())
					require.NotEqual(t, 200, response.Code)
				}
			})
		}
	}
}
