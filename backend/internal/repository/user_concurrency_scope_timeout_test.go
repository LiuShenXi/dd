//go:build unit

package repository

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestClaudeSubscriptionConcurrencyBlackholeTransportHasWholeOperationDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var accepted sync.WaitGroup
	var socketsMu sync.Mutex
	var sockets []net.Conn
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			socketsMu.Lock()
			sockets = append(sockets, connection)
			socketsMu.Unlock()
			go func() { defer accepted.Done(); defer connection.Close(); _, _ = io.Copy(io.Discard, connection) }()
		}
	}()
	original := redis.NewClient(&redis.Options{Addr: listener.Addr().String(), ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, PoolTimeout: 30 * time.Second, MaxRetries: 20})
	cache := NewConcurrencyCache(original, 15, 30).(*concurrencyCache)
	defer func() {
		_ = cache.CloseScopeCache()
		_ = original.Close()
		_ = listener.Close()
		<-acceptDone
		socketsMu.Lock()
		for _, socket := range sockets {
			_ = socket.Close()
		}
		socketsMu.Unlock()
		accepted.Wait()
	}()
	errors := make(chan error, 2)
	started := time.Now()
	go func() {
		_, err := cache.GetScopeStats(context.Background(), service.UserConcurrencyClaudeSubscription, 7)
		errors <- err
	}()
	go func() { _, err := cache.GetLegacyUserConcurrencyBounded(context.Background(), 7); errors <- err }()
	for range 2 {
		select {
		case err := <-errors:
			require.Error(t, err)
		case <-time.After(4 * time.Second):
			t.Fatal("bounded projection waited on the legacy 30s client")
		}
	}
	require.Less(t, time.Since(started), 4*time.Second, "handshake/network/retry time is bounded for the entire operation, below heartbeat15s")
	require.Equal(t, 30*time.Second, original.Options().ReadTimeout)
	require.Equal(t, 20, original.Options().MaxRetries)
	require.False(t, original.Options().ContextTimeoutEnabled)
	require.True(t, cache.scopedRedisClient().Options().ContextTimeoutEnabled)
	require.Zero(t, cache.scopedRedisClient().Options().MaxRetries)
}
