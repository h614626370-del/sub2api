package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexSessionStub struct {
	do     func(*http.Request) (*http.Response, error)
	doGET  func(*http.Request) (*http.Response, error)
	closed atomic.Bool
	calls  atomic.Int64
}

func (s *codexSessionStub) Do(req *http.Request) (*http.Response, error) {
	if s.closed.Load() {
		return nil, errors.New("closed")
	}
	if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.GotConn != nil {
		trace.GotConn(httptrace.GotConnInfo{Reused: s.calls.Add(1) > 1})
	}
	if req.Method == http.MethodGet {
		if s.doGET != nil {
			return s.doGET(req)
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	return s.do(req)
}

type codexSessionAuditSink struct {
	OpenAICodexTicketAuditRepository
	items chan *OpenAICodexTicketAudit
}

func (s *codexSessionAuditSink) Insert(_ context.Context, item *OpenAICodexTicketAudit) error {
	s.items <- item
	return nil
}

func nextSessionAudit(t *testing.T, sink *codexSessionAuditSink) *OpenAICodexTicketAudit {
	t.Helper()
	select {
	case item := <-sink.items:
		return item
	case <-time.After(3 * time.Second):
		t.Fatal("audit not recorded")
		return nil
	}
}

func TestCodexTicketSessionValidatesOldTicketAndPreservesCookieDeadlines(t *testing.T) {
	calls := 0
	upstream := &codexSessionFactoryStub{do: func(*http.Request) (*http.Response, error) {
		calls++
		resp := codexSessionResponse(fakeCodexTicketState(292), "gpt-6-astra")
		if calls > 1 {
			resp.Header.Del(openAICodexTurnStateHeader)
			resp.Header.Del("Set-Cookie")
		}
		resp.Header.Set("x-egress-ip", "203.0.113.20")
		return resp, nil
	}}
	svc := ticketTestService(t, codexSessionConfig(), upstream)
	defer svc.StopOpenAICodexTicketHarvester()
	sink := &codexSessionAuditSink{items: make(chan *OpenAICodexTicketAudit, 10)}
	svc.codexTicketAuditRepo = sink
	account := ticketTestAccount(41)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	first := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
	a := nextSessionAudit(t, sink)
	require.Equal(t, "success", a.Outcome)
	require.Equal(t, "false", a.ResponseHeaders["x-sub2api-connection-reused"])
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	second := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
	b := nextSessionAudit(t, sink)
	require.Equal(t, "validated", b.Outcome)
	require.Equal(t, "true", b.ResponseHeaders["x-sub2api-connection-reused"])
	require.Equal(t, "true", b.ResponseHeaders["x-sub2api-connection-retained"])
	require.Equal(t, "2", b.ResponseHeaders["x-sub2api-connection-successes"])
	require.Equal(t, a.ResponseHeaders["x-sub2api-connection-session"], b.ResponseHeaders["x-sub2api-connection-session"])
	require.Equal(t, first.ExpiresAt, second.ExpiresAt)
	require.Equal(t, first.CapturedAt, second.CapturedAt)
	require.Equal(t, first.Cookies, second.Cookies)
	require.Equal(t, "203.0.113.20", b.EgressIP)
	require.Len(t, upstream.sessions, 1)
	require.False(t, upstream.sessions[0].closed.Load())
}

func TestCodexTicketSessionSemanticMissRechecksOnceWithoutFailureIP(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "312", true: "model"}[mismatch], func(t *testing.T) {
			calls := 0
			upstream := &codexSessionFactoryStub{do: func(*http.Request) (*http.Response, error) {
				calls++
				state, model := fakeCodexTicketState(292), "gpt-6-astra"
				if calls > 1 {
					if mismatch {
						model = "other"
					} else {
						state = fakeCodexTicketState(312)
					}
				}
				resp := codexSessionResponse(state, model)
				resp.Header.Set("x-egress-ip", "203.0.113.21")
				return resp, nil
			}}
			svc := ticketTestService(t, codexSessionConfig(), upstream)
			defer svc.StopOpenAICodexTicketHarvester()
			sink := &codexSessionAuditSink{items: make(chan *OpenAICodexTicketAudit, 10)}
			svc.codexTicketAuditRepo = sink
			account := ticketTestAccount(41)
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			nextSessionAudit(t, sink)
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			a := nextSessionAudit(t, sink)
			require.Empty(t, a.EgressIP)
			require.Empty(t, a.ResponseHeaders["x-sub2api-egress-ip-source"])
			require.Equal(t, "true", a.ResponseHeaders["x-sub2api-connection-retained"])
			require.False(t, upstream.sessions[0].closed.Load())
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			b := nextSessionAudit(t, sink)
			require.Equal(t, "false", b.ResponseHeaders["x-sub2api-connection-retained"])
			require.True(t, upstream.sessions[0].closed.Load())
			require.NotNil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
			require.Len(t, upstream.sessions, 1)
		})
	}
}

func TestCodexTicketSessionCookieDeletionAndServerCloseReleaseConnection(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		t.Run(map[bool]string{false: "close", true: "delete-cookie"}[deletion], func(t *testing.T) {
			calls := 0
			upstream := &codexSessionFactoryStub{do: func(*http.Request) (*http.Response, error) {
				calls++
				resp := codexSessionResponse(fakeCodexTicketState(292), "gpt-6-astra")
				if calls > 1 {
					if deletion {
						resp.Header.Del("Set-Cookie")
						resp.Header.Add("Set-Cookie", "__cflb=; Max-Age=0; Path=/")
					} else {
						resp.Close = true
					}
				}
				return resp, nil
			}}
			svc := ticketTestService(t, codexSessionConfig(), upstream)
			account := ticketTestAccount(41)
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.True(t, upstream.sessions[0].closed.Load())
			if deletion {
				require.Len(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").liveCookies(time.Now(), svc.openAICodexTicketConfig()), 1)
			}
		})
	}
}

func TestCodexTicketSessionLifetimePolicy(t *testing.T) {
	cfg := codexSessionConfig()
	policy := codexTicketPolicyFromConfig(cfg)
	require.Equal(t, 5*time.Minute, codexTicketConnectionMaxAge(policy.Apply(cfg)))
	policy.ConnectionMaxAgeSeconds = 600
	require.NoError(t, policy.Validate())
	require.Equal(t, 10*time.Minute, codexTicketConnectionMaxAge(policy.Apply(cfg)))
	policy.ConnectionMaxAgeSeconds = 3601
	require.Error(t, policy.Validate())
	policy.ConnectionMaxAgeSeconds = 29
	require.Error(t, policy.Validate())
}

func TestCodexTicketSessionQueriesIPOnlyOnceAfterSuccessOnSameConnection(t *testing.T) {
	gets, posts := 0, 0
	upstream := &codexSessionFactoryStub{
		do: func(*http.Request) (*http.Response, error) {
			posts++
			length := 292
			if posts == 1 {
				length = 312
			}
			return codexSessionResponse(fakeCodexTicketState(length), "gpt-6-astra"), nil
		},
		doGET: func(req *http.Request) (*http.Response, error) {
			gets++
			require.Equal(t, "https://chatgpt.com/cdn-cgi/trace", req.URL.String())
			require.Empty(t, req.Header.Get("Authorization"))
			require.Empty(t, req.Header.Get("Cookie"))
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ip=203.0.113.22\n"))}, nil
		},
	}
	svc := ticketTestService(t, codexSessionConfig(), upstream)
	defer svc.StopOpenAICodexTicketHarvester()
	sink := &codexSessionAuditSink{items: make(chan *OpenAICodexTicketAudit, 10)}
	svc.codexTicketAuditRepo = sink
	account := ticketTestAccount(41)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Empty(t, nextSessionAudit(t, sink).EgressIP)
	require.Zero(t, gets)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	a := nextSessionAudit(t, sink)
	require.Equal(t, "203.0.113.22", a.EgressIP)
	require.Equal(t, "same_connection_trace", a.ResponseHeaders["x-sub2api-egress-ip-source"])
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	b := nextSessionAudit(t, sink)
	require.Equal(t, a.EgressIP, b.EgressIP)
	require.Equal(t, a.ResponseHeaders["x-sub2api-connection-session"], b.ResponseHeaders["x-sub2api-connection-session"])
	require.Equal(t, "true", b.ResponseHeaders["x-sub2api-connection-reused"])
	require.Equal(t, 1, gets)
}

func TestCodexTicketSessionIPRejectsNewConnectionAndInvalidResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reused bool
		status int
		body   string
	}{
		{"new-connection", false, 200, "ip=203.0.113.22\n"},
		{"invalid-ip", true, 200, "ip=not-an-ip\n"},
		{"redirect", true, 302, "ip=203.0.113.22\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &codexSessionStub{doGET: func(req *http.Request) (*http.Response, error) {
				httptrace.ContextClientTrace(req.Context()).GotConn(httptrace.GotConnInfo{Reused: tc.reused})
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}}
			session := &codexTicketSession{client: client}
			ip, alive := session.lookupEgressIP(context.Background())
			require.Empty(t, ip)
			require.True(t, alive)
		})
	}
}
func (s *codexSessionStub) Close() { s.closed.Store(true) }

type codexSessionFactoryStub struct {
	HTTPUpstream
	sessions []*codexSessionStub
	do       func(*http.Request) (*http.Response, error)
	doGET    func(*http.Request) (*http.Response, error)
}

func (f *codexSessionFactoryStub) NewCodexTicketSession(string) (OpenAICodexTicketSession, error) {
	s := &codexSessionStub{do: f.do, doGET: f.doGET}
	f.sessions = append(f.sessions, s)
	return s, nil
}

func codexSessionResponse(state, model string) *http.Response {
	resp := codexTicketResponse()
	resp.Header.Set(openAICodexTurnStateHeader, state)
	resp.Header.Add("Set-Cookie", "__cflb=test-cf; Path=/; Max-Age=240; Secure")
	resp.Header.Add("Set-Cookie", "__oailb=test-oai; Path=/; Max-Age=240; Secure")
	resp.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.completed","response":{"model":` + jsonString(model) + `}}` + "\n\n"))
	return resp
}

func codexSessionConfig() config.OpenAICodexTicketConfig {
	return config.OpenAICodexTicketConfig{
		Enabled: true, ReuseConnection: true, CookieEnabled: true, CookieTTLSeconds: 240,
		TTLSeconds: 3600, HarvestProxyURL: "http://proxy.example.com:8080",
		Models: []string{"gpt-6-astra"},
	}
}

func TestCodexTicketSessionRetainsContextWithoutRenewingEchoedTicket(t *testing.T) {
	var requests []*http.Request
	upstream := &codexSessionFactoryStub{do: func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req)
		return codexSessionResponse(fakeCodexTicketState(292), "gpt-6-astra"), nil
	}}
	svc := ticketTestService(t, codexSessionConfig(), upstream)
	defer svc.StopOpenAICodexTicketHarvester()
	account := ticketTestAccount(41)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	first := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
	require.NotNil(t, first)
	require.Len(t, upstream.sessions, 1)
	require.False(t, upstream.sessions[0].closed.Load())
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	second := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
	require.Len(t, upstream.sessions, 1)
	require.Equal(t, first.ExpiresAt, second.ExpiresAt)
	require.Equal(t, first.CapturedAt, second.CapturedAt)
	require.Len(t, requests, 2)
	require.False(t, requests[0].Close)
	require.Equal(t, requests[0].Header.Get("session_id"), requests[1].Header.Get("session_id"))
	require.Equal(t, first.State, requests[1].Header.Get(openAICodexTurnStateHeader))
	require.Len(t, requests[1].Cookies(), 2)
	require.False(t, svc.codexTicketSessionDue(account.ID, "gpt-6-astra", time.Now()))
	require.True(t, svc.codexTicketSessionDue(account.ID, "gpt-6-astra", time.Now().Add(time.Minute)))
}

func TestCodexTicketSessionRejectsFailedCandidates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state string
		model string
		body  string
	}{
		{"wrong_length", fakeCodexTicketState(312), "gpt-6-astra", ""},
		{"wrong_model", fakeCodexTicketState(292), "gpt-5.6-luna", ""},
		{"incomplete", fakeCodexTicketState(292), "gpt-6-astra", `data: {"type":"response.created","response":{"model":"gpt-6-astra"}}`},
		{"oversized", fakeCodexTicketState(292), "gpt-6-astra", strings.Repeat("x", (1<<20)+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &codexSessionFactoryStub{do: func(*http.Request) (*http.Response, error) {
				resp := codexSessionResponse(tc.state, tc.model)
				if tc.body != "" {
					resp.Body = io.NopCloser(strings.NewReader(tc.body))
				}
				return resp, nil
			}}
			svc := ticketTestService(t, codexSessionConfig(), upstream)
			account := ticketTestAccount(41)
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
			require.True(t, upstream.sessions[0].closed.Load())
			_, ok := svc.openaiCodexTicketSessions.Load(openAICodexTicketKey(account.ID, "gpt-6-astra"))
			require.False(t, ok)
		})
	}
}

func TestCodexTicketSessionDiscardDuringProbeCannotRestoreTicket(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	upstream := &codexSessionFactoryStub{do: func(*http.Request) (*http.Response, error) {
		close(started)
		<-finish
		return codexSessionResponse(fakeCodexTicketState(292), "gpt-6-astra"), nil
	}}
	svc := ticketTestService(t, codexSessionConfig(), upstream)
	account := ticketTestAccount(41)
	svc.accountRepo = &codexTicketDiscardRepo{account: account}
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	}()
	<-started
	_, err := svc.DiscardOpenAICodexTickets(context.Background(), &account.ID)
	require.NoError(t, err)
	close(finish)
	<-done
	require.True(t, upstream.sessions[0].closed.Load())
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
}

func TestCodexTicketSessionIsolationAndCleanup(t *testing.T) {
	upstream := &codexSessionFactoryStub{do: func(*http.Request) (*http.Response, error) {
		return codexSessionResponse(fakeCodexTicketState(292), "gpt-6-astra"), nil
	}}
	cfg := codexSessionConfig()
	svc := ticketTestService(t, cfg, upstream)
	a, b := ticketTestAccount(41), ticketTestAccount(42)
	svc.probeOnceOpenAICodexTicket(context.Background(), a, "gpt-6-astra")
	svc.probeOnceOpenAICodexTicket(context.Background(), b, "gpt-6-astra")
	require.Len(t, upstream.sessions, 2)
	a.Schedulable = false
	svc.pruneCodexTicketSessions(context.Background(), []Account{*a, *b}, cfg)
	require.True(t, upstream.sessions[0].closed.Load())
	require.False(t, upstream.sessions[1].closed.Load())
	cfg.ReuseConnection = false
	svc.pruneCodexTicketSessions(context.Background(), []Account{*b}, cfg)
	require.True(t, upstream.sessions[1].closed.Load())
}
