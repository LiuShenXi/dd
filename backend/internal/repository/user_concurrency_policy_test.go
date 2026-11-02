//go:build unit

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestClaudeSubscriptionConcurrencyNilPolicyDatabaseFailsClosed(t *testing.T) {
	var nilRepo *claudeConcurrencyPolicyRepository
	for _, repo := range []*claudeConcurrencyPolicyRepository{nilRepo, {}} {
		called := false
		err := repo.WithClaudeConcurrencyLimit(context.Background(), 1, 3, func(context.Context, int) error { called = true; return nil })
		require.ErrorIs(t, err, service.ErrTermConcurrencyUnavailable)
		require.False(t, called)
	}
}

func TestClaudeSubscriptionConcurrencyUserCreatePreservesNilNoOp(t *testing.T) {
	r := &userRepository{}
	require.NotPanics(t, func() { require.NoError(t, r.Create(context.Background(), nil)) })
	require.NotPanics(t, func() { require.NoError(t, r.CreateWithEmailAliasGuard(context.Background(), nil)) })
}
