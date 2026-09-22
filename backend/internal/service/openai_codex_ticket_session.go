package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// A session owns exactly one transport connection. Implementations must reject
// reconnects, follow no redirects, and cancel pending work on Close.
type OpenAICodexTicketSession interface {
	Do(*http.Request) (*http.Response, error)
	Close()
}

var ErrCodexTicketConnectionLost = errors.New("ticket session connection lost; reconnect is not reuse")

type OpenAICodexTicketSessionFactory interface {
	NewCodexTicketSession(proxyURL string) (OpenAICodexTicketSession, error)
}

type codexTicketSession struct {
	client    OpenAICodexTicketSession
	id        string
	accountID int64
	model     string
	proxyURL  string
	createdAt time.Time
	nextProbe atomic.Int64
	// Only the account/model singleflight owner accesses ticket and successes.
	ticket    *openAICodexTicket
	successes int
	failures  int
	ipChecked bool
	egressIP  string
	ipSource  string
}

// Query only the original origin through the retained transport. No credentials,
// cookies, redirects, or replacement connections are used for diagnostics.
func (session *codexTicketSession) lookupEgressIP(ctx context.Context) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	endpoint, err := url.Parse(chatgptCodexURL)
	if err != nil {
		return "", true
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = "/cdn-cgi/trace", "", "", ""
	var reused atomic.Bool
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { reused.Store(info.Reused) },
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", true
	}
	resp, err := session.client.Do(req)
	if err != nil || resp == nil {
		return "", false
	}
	defer resp.Body.Close()
	const limit = 16 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(body) > limit {
		return "", false
	}
	alive := !resp.Close
	if resp.StatusCode != http.StatusOK || !reused.Load() {
		return "", alive
	}
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && key == "ip" {
			if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil {
				return ip.String(), alive
			}
		}
	}
	return "", alive
}

func codexTicketConnectionMaxAge(cfg config.OpenAICodexTicketConfig) time.Duration {
	seconds := cfg.ConnectionMaxAgeSeconds
	if seconds <= 0 {
		seconds = 300
	}
	return time.Duration(min(seconds, 3600)) * time.Second
}

func (s *OpenAIGatewayService) codexTicketSessionForProbe(accountID int64, model, proxyURL string, cfg config.OpenAICodexTicketConfig) (*codexTicketSession, error) {
	key := openAICodexTicketKey(accountID, model)
	if raw, ok := s.openaiCodexTicketSessions.Load(key); ok {
		session := raw.(*codexTicketSession)
		if cfg.ReuseConnection && cfg.CookieEnabled && session.proxyURL == proxyURL &&
			time.Since(session.createdAt) < codexTicketConnectionMaxAge(cfg) && session.ticket.usable(time.Now(), cfg) &&
			len(session.ticket.liveCookies(time.Now(), cfg)) == 2 {
			return session, nil
		}
		s.releaseCodexTicketSession(key, session)
	}
	if !cfg.ReuseConnection {
		return nil, nil
	}
	if !cfg.CookieEnabled {
		return nil, errors.New("ticket connection reuse requires cookie capture")
	}
	factory, ok := s.httpUpstream.(OpenAICodexTicketSessionFactory)
	if !ok {
		return nil, errors.New("ticket connection reuse is not supported by this transport")
	}
	client, err := factory.NewCodexTicketSession(proxyURL)
	if err != nil {
		return nil, err
	}
	session := &codexTicketSession{
		client: client, id: uuid.NewString(), accountID: accountID, model: model,
		proxyURL: proxyURL, createdAt: time.Now(),
	}
	s.openaiCodexTicketSessions.Store(key, session)
	return session, nil
}

func (s *OpenAIGatewayService) releaseCodexTicketSession(key string, session *codexTicketSession) {
	s.openaiCodexTicketSessions.CompareAndDelete(key, session)
	session.client.Close()
}

func (s *OpenAIGatewayService) closeCodexTicketSessions(accountID *int64) {
	s.openaiCodexTicketSessions.Range(func(key, value any) bool {
		session := value.(*codexTicketSession)
		if accountID == nil || session.accountID == *accountID {
			s.releaseCodexTicketSession(key.(string), session)
		}
		return true
	})
}

func (s *OpenAIGatewayService) pruneCodexTicketSessions(ctx context.Context, accounts []Account, cfg config.OpenAICodexTicketConfig) {
	if !cfg.ReuseConnection || !cfg.CookieEnabled {
		s.closeCodexTicketSessions(nil)
		return
	}
	eligible := make(map[int64]bool, len(accounts))
	for i := range accounts {
		if isOpenAICodexTicketHarvestAccount(&accounts[i]) {
			eligible[accounts[i].ID] = true
		}
	}
	models := make(map[string]bool, len(cfg.Models))
	for _, model := range cfg.Models {
		models[strings.TrimSpace(model)] = true
	}
	proxyURL := s.openAICodexTicketHarvestProxyURLContext(ctx)
	s.openaiCodexTicketSessions.Range(func(key, value any) bool {
		session := value.(*codexTicketSession)
		if !cfg.ReuseConnection || !cfg.CookieEnabled || !eligible[session.accountID] ||
			!models[session.model] || proxyURL != session.proxyURL || time.Since(session.createdAt) >= codexTicketConnectionMaxAge(cfg) {
			s.releaseCodexTicketSession(key.(string), session)
		}
		return true
	})
}

func (s *OpenAIGatewayService) codexTicketSessionDue(accountID int64, model string, now time.Time) bool {
	if raw, ok := s.openaiCodexTicketSessions.Load(openAICodexTicketKey(accountID, model)); ok {
		return now.UnixNano() >= raw.(*codexTicketSession).nextProbe.Load()
	}
	return false
}

func codexTicketResponseModel(body []byte) string {
	// Require a completed response, not just a model on a partial stream event.
	if gjson.ValidBytes(body) {
		if gjson.GetBytes(body, "status").String() != "completed" {
			return ""
		}
		return gjson.GetBytes(body, "model").String()
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		event := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if gjson.Get(event, "type").String() == "response.completed" {
			return gjson.Get(event, "response.model").String()
		}
	}
	return ""
}
