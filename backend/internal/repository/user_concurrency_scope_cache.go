package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const userScopeRedisOperationTimeout = 2 * time.Second

func (c *concurrencyCache) GetLegacyUserConcurrencyBounded(ctx context.Context, userID int64) (int, error) {
	if c == nil || c.rdb == nil || userID <= 0 {
		return 0, service.ErrTermConcurrencyUnavailable
	}
	client := c.scopedRedisClient()
	if client == nil {
		return 0, service.ErrTermConcurrencyUnavailable
	}
	opCtx, cancel := context.WithTimeout(ctx, userScopeRedisOperationTimeout)
	defer cancel()
	return getCountScript.Run(opCtx, client, []string{userSlotKey(userID), liveUserSlotKey(userID)}, c.slotTTLSeconds).Int()
}

var userScopeScript = redis.NewScript(`
redis.replicate_commands()
local tm = redis.call('TIME')
local now = tonumber(tm[1])*1000 + math.floor(tonumber(tm[2])/1000)
local active, waiting, released = KEYS[1], KEYS[2], KEYS[3]
redis.call('ZREMRANGEBYSCORE', active, '-inf', now)
redis.call('ZREMRANGEBYSCORE', waiting, '-inf', now)
redis.call('ZREMRANGEBYSCORE', released, '-inf', now)
local op, member, ticket, kind = ARGV[1], ARGV[2], ARGV[3], ARGV[4]
local limit, ttl = tonumber(ARGV[5]), tonumber(ARGV[6])
local status, expiry = 1, 0
local cancelledKind=kind
if op == 'acquire' or op == 'promote' then cancelledKind='active' end
if op == 'enqueue' then cancelledKind='waiting' end
if op ~= 'release' and op ~= 'stats' and redis.call('ZSCORE',released,cancelledKind..':'..member) then status=2
elseif op == 'acquire' then
  local old = redis.call('ZSCORE', active, member)
  if old then expiry=tonumber(old)
  elseif redis.call('ZCARD',active) >= limit then status=0
  else expiry=now+ttl; redis.call('ZADD',active,expiry,member) end
elseif op == 'enqueue' then
  local old = redis.call('ZSCORE', waiting, member)
  if old then expiry=tonumber(old)
  elseif redis.call('ZCARD',waiting) >= limit then status=0
  else expiry=now+ttl; redis.call('ZADD',waiting,expiry,member) end
elseif op == 'promote' then
  local old = redis.call('ZSCORE',active,member)
  if old then expiry=tonumber(old)
  elseif redis.call('ZSCORE',released,'waiting:'..ticket) or not redis.call('ZSCORE',waiting,ticket) then status=2
  elseif redis.call('ZCARD',active) >= limit then status=0
  else redis.call('ZREM',waiting,ticket); expiry=now+ttl; redis.call('ZADD',active,expiry,member) end
elseif op == 'renew' then
  local target = active; if kind == 'waiting' then target=waiting end
  local old = redis.call('ZSCORE',target,member)
  if not old then status=2
  else expiry=math.max(tonumber(old),now+ttl); redis.call('ZADD',target,expiry,member) end
elseif op == 'release' then
  local target = active; if kind == 'waiting' then target=waiting end
  if redis.call('ZREM',target,member) == 0 then status=2 end
  redis.call('ZADD',released,now+math.max(ttl,60000),kind..':'..member)
end
-- A later short member must never shorten the lifetime of an existing lease.
for _,key in ipairs(KEYS) do
  local last = redis.call('ZREVRANGE',key,0,0,'WITHSCORES')
  if #last == 0 then redis.call('DEL',key)
  else redis.call('PEXPIREAT',key,math.ceil(tonumber(last[2]))+1000) end
end
return {status,redis.call('ZCARD',active),redis.call('ZCARD',waiting),now,expiry}
`)

func userScopeKeys(scope service.UserConcurrencyScope, userID int64) ([]string, error) {
	if scope != service.UserConcurrencyClaudeSubscription || userID <= 0 {
		return nil, service.ErrTermConcurrencyUnavailable
	}
	base := fmt.Sprintf("concurrency:scope:{claude_subscription:user:%d}", userID)
	return []string{base + ":active", base + ":waiting", base + ":released"}, nil
}

func (c *concurrencyCache) scopedRedisClient() *redis.Client {
	c.scopeClientMu.Lock()
	defer c.scopeClientMu.Unlock()
	if c.scopeClientClosed {
		return nil
	}
	if c.scopeClient == nil {
		options := *c.rdb.Options()
		// Options contains client-owned mutable notification processors. Never
		// register the new client's handlers against the old client's processor.
		options.PushNotificationProcessor = nil
		options.MaintNotificationsConfig = nil
		options.ClientSideCache = nil
		options.ClientSideCacheConfig = nil
		if options.TLSConfig != nil {
			options.TLSConfig = options.TLSConfig.Clone()
		}
		options.ContextTimeoutEnabled = true
		options.MaxRetries = -1
		options.DialTimeout, options.ReadTimeout, options.WriteTimeout, options.PoolTimeout = userScopeRedisOperationTimeout, userScopeRedisOperationTimeout, userScopeRedisOperationTimeout, userScopeRedisOperationTimeout
		options.MinIdleConns = 0
		// The project's original dialer is go-redis's default closure over its
		// original Options. Rebuild it after setting the bounded timeout, retaining
		// Network, endpoint, TLS and all authentication options above.
		options.Dialer = redis.NewDialer(&options)
		c.scopeClient = redis.NewClient(&options)
	}
	return c.scopeClient
}

func (c *concurrencyCache) CloseScopeCache() error {
	if c == nil {
		return nil
	}
	c.scopeClientMu.Lock()
	c.scopeClientClosed = true
	client := c.scopeClient
	c.scopeClientMu.Unlock()
	if client != nil {
		return client.Close()
	}
	return nil
}

func (c *concurrencyCache) runUserScope(ctx context.Context, op string, in service.UserScopeOperation) (service.UserScopeResult, error) {
	bad := service.UserScopeResult{Status: service.UserScopeUnavailable}
	keys, err := userScopeKeys(in.Scope, in.UserID)
	if err != nil || c == nil || c.rdb == nil {
		return bad, service.ErrTermConcurrencyUnavailable
	}
	if op != "stats" {
		if _, err = uuid.Parse(in.MemberID); err != nil {
			return bad, service.ErrTermConcurrencyUnavailable
		}
		if op == "promote" {
			if _, err = uuid.Parse(in.TicketID); err != nil {
				return bad, service.ErrTermConcurrencyUnavailable
			}
		}
		if (op == "acquire" || op == "enqueue" || op == "promote") && !service.ValidClaudeSubscriptionConcurrency(in.Limit) {
			return bad, service.ErrTermConcurrencyNotConfigured
		}
		if (op == "renew" || op == "release") && in.Kind != service.UserScopeActive && in.Kind != service.UserScopeWaiting {
			return bad, service.ErrTermConcurrencyUnavailable
		}
		if op != "release" && (in.LeaseTTL < time.Millisecond || in.LeaseTTL > 24*time.Hour) {
			return bad, service.ErrTermConcurrencyUnavailable
		}
	}
	opCtx, cancel := context.WithTimeout(ctx, userScopeRedisOperationTimeout)
	defer cancel()
	client := c.scopedRedisClient()
	if client == nil {
		return bad, service.ErrTermConcurrencyUnavailable
	}
	values, err := userScopeScript.Run(opCtx, client, keys, op, in.MemberID, in.TicketID, string(in.Kind), in.Limit, in.LeaseTTL.Milliseconds()).Int64Slice()
	if err != nil {
		return bad, fmt.Errorf("%w: %v", service.ErrTermConcurrencyUnavailable, err)
	}
	if len(values) != 5 {
		return bad, service.ErrTermConcurrencyUnavailable
	}
	status := service.UserScopeAcquired
	if values[0] == 0 {
		status = service.UserScopeFull
	} else if values[0] == 2 {
		status = service.UserScopeMissing
	} else if values[0] != 1 {
		return bad, service.ErrTermConcurrencyUnavailable
	}
	out := service.UserScopeResult{Status: status, Active: int(values[1]), Waiting: int(values[2]), ServerTime: time.UnixMilli(values[3]).UTC()}
	if values[4] > 0 {
		out.ExpiresAt = time.UnixMilli(values[4]).UTC()
	}
	return out, nil
}

func (c *concurrencyCache) TryAcquireScope(ctx context.Context, in service.UserScopeOperation) (service.UserScopeResult, error) {
	return c.runUserScope(ctx, "acquire", in)
}
func (c *concurrencyCache) TryEnqueueScope(ctx context.Context, in service.UserScopeOperation) (service.UserScopeResult, error) {
	return c.runUserScope(ctx, "enqueue", in)
}
func (c *concurrencyCache) PromoteScope(ctx context.Context, in service.UserScopeOperation) (service.UserScopeResult, error) {
	return c.runUserScope(ctx, "promote", in)
}
func (c *concurrencyCache) RenewScope(ctx context.Context, in service.UserScopeOperation) (service.UserScopeResult, error) {
	return c.runUserScope(ctx, "renew", in)
}
func (c *concurrencyCache) ReleaseScope(ctx context.Context, in service.UserScopeOperation) (service.UserScopeResult, error) {
	return c.runUserScope(ctx, "release", in)
}
func (c *concurrencyCache) GetScopeStats(ctx context.Context, scope service.UserConcurrencyScope, userID int64) (service.UserScopeStats, error) {
	out, err := c.runUserScope(ctx, "stats", service.UserScopeOperation{Scope: scope, UserID: userID})
	return service.UserScopeStats{Active: out.Active, Waiting: out.Waiting, ServerTime: out.ServerTime}, err
}
