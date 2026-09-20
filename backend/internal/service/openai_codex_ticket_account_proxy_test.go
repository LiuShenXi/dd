package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func accountBoundProxy(protocol, host string, port int) *Proxy {
	return &Proxy{ID: 9, Name: "test-bound", Protocol: protocol, Host: host, Port: port, Status: StatusActive}
}

func TestCodexTicketAccountBoundProxyUsedForHarvest(t *testing.T) {
	svc, repo, upstream := sentinelFixture(t)
	repo.account.ProxyID = new(int64)
	*repo.account.ProxyID = 9
	repo.account.Proxy = accountBoundProxy("socks5", "172.18.0.1", 10802)
	ticket, err := svc.collectOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.NoError(t, err)
	require.NotNil(t, ticket)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "socks5://172.18.0.1:10802", upstream.lastProxyURL)
}

func TestCodexTicketUnboundAccountUsesGlobalHarvestProxy(t *testing.T) {
	svc, repo, upstream := sentinelFixture(t)
	repo.account.ProxyID = nil
	repo.account.Proxy = nil
	ticket, err := svc.collectOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.NoError(t, err)
	require.NotNil(t, ticket)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "http://harvest.invalid", upstream.lastProxyURL)
}

func TestCodexTicketInvalidBoundProxyFallsBackToGlobal(t *testing.T) {
	svc, repo, upstream := sentinelFixture(t)
	repo.account.ProxyID = new(int64)
	*repo.account.ProxyID = 9
	repo.account.Proxy = accountBoundProxy("ftp", "172.18.0.1", 21)
	ticket, err := svc.collectOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.NoError(t, err)
	require.NotNil(t, ticket)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "http://harvest.invalid", upstream.lastProxyURL)
}

func TestCodexTicketBoundProxyWithoutCredentialsKeepsAuth(t *testing.T) {
	svc, repo, upstream := sentinelFixture(t)
	repo.account.ProxyID = new(int64)
	*repo.account.ProxyID = 9
	repo.account.Proxy = &Proxy{ID: 9, Name: "auth-bound", Protocol: "socks5", Host: "172.18.0.1", Port: 10802, Username: "u", Password: "p", Status: StatusActive}
	ticket, err := svc.collectOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.NoError(t, err)
	require.NotNil(t, ticket)
	require.Equal(t, "socks5://u:p@172.18.0.1:10802", upstream.lastProxyURL)
}

func TestCodexTicketEmptyGlobalSettingBlocksOnlyUnboundAccounts(t *testing.T) {
	svc, repo, upstream := sentinelFixture(t)
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = ""
	repo.account.ProxyID = new(int64)
	*repo.account.ProxyID = 9
	repo.account.Proxy = accountBoundProxy("socks5", "172.18.0.1", 10802)
	ticket, err := svc.collectOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.NoError(t, err)
	require.NotNil(t, ticket)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "socks5://172.18.0.1:10802", upstream.lastProxyURL)

	unbound := ticketTestAccount(3)
	unbound.Status = StatusActive
	unbound.Schedulable = true
	unbound.Extra = map[string]any{}
	unbound.Credentials = map[string]any{"access_token": "tok", "chatgpt_account_id": "acc-1"}
	ticket, err = svc.collectOpenAICodexTicket(context.Background(), unbound, "gpt-6-astra")
	require.Error(t, err)
	require.Nil(t, ticket)
	require.Len(t, upstream.requests, 1)
}
