package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketCookiesCaptureScopeAndLifetime(t *testing.T) {
	now := time.Now().UTC()
	endpoint, err := url.Parse(chatgptCodexURL)
	require.NoError(t, err)
	response := &http.Response{Header: http.Header{"Set-Cookie": {
		"__cflb=route-a; Domain=.chatgpt.com; Path=/; HttpOnly; Secure; Max-Age=60",
		"__oailb=route-b; Path=/; HttpOnly; Max-Age=600",
		"auth_token=do-not-store; Path=/",
	}}}
	got := captureCodexTicketCookies(response, endpoint, now, 240)
	require.Len(t, got, 2)
	require.Equal(t, now.Add(60*time.Second), got[0].ExpiresAt)
	require.Equal(t, now.Add(240*time.Second), got[1].ExpiresAt)
	for _, raw := range []string{"__cflb=x; Domain=evil.example", "__cflb=x; Path=/unrelated", "__cflb=x; Max-Age=-1", "__cflb=x; Expires=Wed, 01 Jan 2020 00:00:00 GMT"} {
		require.Empty(t, captureCodexTicketCookies(&http.Response{Header: http.Header{"Set-Cookie": {raw}}}, endpoint, now, 240))
	}
	response.Header.Add("Set-Cookie", "__cflb=deleted; Max-Age=-1")
	require.Len(t, captureCodexTicketCookies(response, endpoint, now, 240), 1)
}

func TestCodexTicketCookiesHarvestInjectPersistAndIsolate(t *testing.T) {
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, CookieEnabled: true, CookieRequired: true, CookieTTLSeconds: 240, TTLSeconds: 240, RefreshBeforeSeconds: 30, HarvestProxyURL: "http://probe.example"}
	headers := http.Header{"Set-Cookie": {"__cflb=route-a; Path=/; HttpOnly", "__oailb=route-b; Path=/; Max-Age=120", "auth_token=private; Path=/"}}
	headers.Set(openAICodexTurnStateHeader, fakeCodexTicketState(292))
	headers.Set("x-egress-ip", "203.0.113.41")
	upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(""))}}}
	svc := ticketTestService(t, cfg, upstream)
	account := ticketTestAccount(41)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
	require.NotNil(t, ticket)
	require.Len(t, ticket.Cookies, 2)
	h := http.Header{"Cookie": {"unrelated=keep; __cflb=old"}}
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
	require.Contains(t, h.Get("Cookie"), "__cflb=route-a")
	require.Contains(t, h.Get("Cookie"), "__oailb=route-b")
	require.Contains(t, h.Get("Cookie"), "unrelated=keep")
	require.NotContains(t, h.Get("Cookie"), "private")
	require.NotContains(t, h.Get("Cookie"), "old")
	require.ErrorIs(t, svc.applyOpenAICodexTicket(context.Background(), ticketTestAccount(42), "gpt-6-astra", http.Header{}), ErrOpenAICodexTicketUnavailable)
	require.ErrorIs(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-5.6-sol", http.Header{}), ErrOpenAICodexTicketUnavailable)
	raw, err := json.Marshal(ticket)
	require.NoError(t, err)
	var stored any
	require.NoError(t, json.Unmarshal(raw, &stored))
	restored := parseOpenAICodexTicketFromAny(account.ID, "gpt-6-astra", stored)
	require.Len(t, restored.Cookies, 2)
	require.Equal(t, "203.0.113.41", restored.EgressIP)
	require.Equal(t, "upstream_header", restored.EgressIPSource)
	account.Extra = map[string]any{openAICodexTicketExtraKey("gpt-6-astra"): stored}
	require.Empty(t, RedactOpenAICodexTicketExtra(account.Extra))
	statuses := OpenAICodexTicketStatuses(account, cfg, time.Now().Add(2*time.Hour))
	require.False(t, statuses[0].Ready)
	require.Equal(t, "203.0.113.41", statuses[0].LastSuccessIP)
	require.NotNil(t, statuses[0].LastSuccessAt)
	policy := codexTicketPolicyFromConfig(cfg)
	require.False(t, ticket.policyNeedsRefresh(time.Now(), policy.Apply(cfg)))
	require.True(t, ticket.policyNeedsRefresh(ticket.CapturedAt.Add(100*time.Second), policy.Apply(cfg)))
	policy.TTLSeconds = 10
	require.False(t, ticket.usable(ticket.CapturedAt.Add(11*time.Second), policy.Apply(cfg)))
	policy.TTLSeconds = 240
	policy.CookieTTLSeconds = 10
	require.False(t, ticket.usable(ticket.CapturedAt.Add(11*time.Second), policy.Apply(cfg)))
	svc.cfg.Gateway.OpenAICodexTicket.CookieEnabled = false
	svc.cfg.Gateway.OpenAICodexTicket.CookieRequired = false
	h = http.Header{}
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
	require.Empty(t, h.Get("Cookie"))
}

func TestCodexTicketCookiesStrictModeBlocksWithoutCookiesEvenWhenFailOpen(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, CookieEnabled: true, CookieRequired: true, FailClosed: false}, nil)
	account := ticketTestAccount(41)
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{State: fakeCodexTicketState(292), Length: 292, Model: "gpt-6-astra", CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
	require.True(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
	require.ErrorIs(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", http.Header{}), ErrOpenAICodexTicketUnavailable)
}

func TestCodexTicketCookiesSeparateWebSocketConnections(t *testing.T) {
	account := ticketTestAccount(1)
	empty := normalizeOpenAIWSHandshakeCompatibility(account, http.Header{})
	a := normalizeOpenAIWSHandshakeCompatibility(account, http.Header{"Cookie": {"__cflb=a"}})
	b := normalizeOpenAIWSHandshakeCompatibility(account, http.Header{"Cookie": {"__cflb=b"}})
	require.NotEqual(t, empty, a)
	require.NotEqual(t, a, b)
	require.Equal(t, a, normalizeOpenAIWSHandshakeCompatibility(account, http.Header{"Cookie": {"__cflb=a"}}))
}

func TestCodexTicketCookieRefreshUsesUpstreamLifetime(t *testing.T) {
	now := time.Now()
	cfg := config.OpenAICodexTicketConfig{TTLSeconds: 3600, TargetLength: 292, CookieEnabled: true, CookieTTLSeconds: 240, RefreshBeforeSeconds: 600}
	ticket := &openAICodexTicket{State: fakeCodexTicketState(292), Length: 292, CapturedAt: now, ExpiresAt: now.Add(time.Hour), Cookies: []openAICodexTicketCookie{
		{Name: "__cflb", Value: "test", CapturedAt: now, ExpiresAt: now.Add(40 * time.Second)},
		{Name: "__oailb", Value: "test", CapturedAt: now, ExpiresAt: now.Add(240 * time.Second)},
	}}
	require.False(t, ticket.policyNeedsRefresh(now, cfg))
	require.False(t, ticket.policyNeedsRefresh(now.Add(29*time.Second), cfg))
	require.True(t, ticket.policyNeedsRefresh(now.Add(30*time.Second), cfg))
	cfg.RefreshBeforeSeconds = 0
	require.False(t, ticket.policyNeedsRefresh(now.Add(39*time.Second), cfg))
	require.True(t, ticket.policyNeedsRefresh(now.Add(40*time.Second), cfg))
}
