package repository

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type codexTicketHTTPSession struct {
	client    *http.Client
	transport *http.Transport
	ctx       context.Context
	cancel    context.CancelFunc
	validate  func(*http.Request) error
}

func (s *httpUpstreamService) NewCodexTicketSession(proxyURL string) (service.OpenAICodexTicketSession, error) {
	if err := service.ValidateOpenAICodexTicketHarvestProxyURL(proxyURL); err != nil || proxyURL == "" {
		return nil, errors.New("ticket session requires a valid proxy")
	}
	_, proxy, err := normalizeProxyURL(proxyURL)
	if err != nil {
		return nil, errors.New("invalid ticket session proxy")
	}
	settings := defaultPoolSettings(s.cfg)
	settings.maxIdleConns, settings.maxIdleConnsPerHost, settings.maxConnsPerHost = 1, 1, 1
	settings.idleConnTimeout = time.Minute
	transport, err := buildUpstreamTransport(settings, proxy, upstreamProtocolModeOpenAIH1)
	if err != nil {
		return nil, errors.New("cannot create ticket session transport")
	}
	guardCodexTicketSessionDial(transport)
	ctx, cancel := context.WithCancel(context.Background())
	return &codexTicketHTTPSession{
		client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		transport: transport, ctx: ctx, cancel: cancel, validate: s.validateRequestHost,
	}, nil
}

func guardCodexTicketSessionDial(transport *http.Transport) {
	dial := transport.DialContext
	var dialed atomic.Bool
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !dialed.CompareAndSwap(false, true) {
			return nil, service.ErrCodexTicketConnectionLost
		}
		return dial(ctx, network, addr)
	}
}

func (s *codexTicketHTTPSession) Do(req *http.Request) (*http.Response, error) {
	if err := s.validate(req); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(s.ctx, cancel)
	if s.ctx.Err() != nil {
		cancel()
	}
	resp, err := s.client.Do(req.Clone(ctx))
	if err != nil {
		stop()
		cancel()
		return nil, err
	}
	decompressResponseBody(resp)
	resp.Body = wrapTrackedBody(resp.Body, func() {
		stop()
		cancel()
	})
	return resp, nil
}

func (s *codexTicketHTTPSession) Close() {
	s.cancel()
	s.transport.CloseIdleConnections()
}
