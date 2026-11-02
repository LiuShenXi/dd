//go:build unit

package admin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeSubscriptionConcurrencyAdminInputPresenceAndRange(t *testing.T) {
	for _, raw := range []string{`{}`, `{"claude_subscription_concurrency":1}`, `{"claude_subscription_concurrency":2147483647}`} {
		var in UpdateUserRequest
		require.NoError(t, json.Unmarshal([]byte(raw), &in))
		var create CreateUserRequest
		require.NoError(t, json.Unmarshal([]byte(raw), &create))
		if raw == `{}` {
			require.Nil(t, in.ClaudeSubscriptionConcurrency.value)
		} else {
			require.NotNil(t, in.ClaudeSubscriptionConcurrency.value)
		}
	}
	for _, raw := range []string{`null`, `0`, `-1`, `1.5`, `"2"`, `true`, `2147483648`, `999999999999999999999999`} {
		t.Run(raw, func(t *testing.T) {
			payload := []byte(`{"claude_subscription_concurrency":` + raw + `}`)
			var in UpdateUserRequest
			require.Error(t, json.Unmarshal(payload, &in))
			var create CreateUserRequest
			require.Error(t, json.Unmarshal(payload, &create))
		})
	}
}
