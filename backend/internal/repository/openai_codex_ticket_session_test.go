package repository

import (
	"context"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketSessionReusesConnectionAndRejectsReconnect(t *testing.T) {
	var connections atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	transport := &http.Transport{DialContext: newUpstreamDialer().DialContext, MaxConnsPerHost: 1}
	guardCodexTicketSessionDial(transport)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	for i := 0; i < 3; i++ {
		var got, reused bool
		ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
			got, reused = true, info.Reused
		}})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.True(t, got)
		require.Equal(t, i > 0, reused)
	}
	require.Equal(t, int64(1), connections.Load())
	transport.CloseIdleConnections()
	_, err := client.Post(server.URL, "application/json", nil)
	require.ErrorContains(t, err, "reconnect is not reuse")
	require.Equal(t, int64(1), connections.Load())
}

func TestCodexTicketSessionRetainsOneHTTPSProxyTunnel(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	var tunnels atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		remote, err := net.DialTimeout("tcp", r.Host, time.Second)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer func() { _ = remote.Close() }()
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking unavailable", http.StatusInternalServerError)
			return
		}
		client, _, err := hijacker.Hijack()
		if err != nil {
			return
		}
		defer func() { _ = client.Close() }()
		tunnels.Add(1)
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = io.Copy(remote, client)
			_ = remote.Close()
		}()
		_, _ = io.Copy(client, remote)
		_ = client.Close()
		<-done
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	transport, err := buildUpstreamTransport(defaultPoolSettings(nil), proxyURL, upstreamProtocolModeOpenAIH1)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	upstreamTransport, ok := upstream.Client().Transport.(*http.Transport)
	require.True(t, ok)
	transport.TLSClientConfig = upstreamTransport.TLSClientConfig.Clone()
	transport.TLSClientConfig.RootCAs = roots
	transport.TLSClientConfig.InsecureSkipVerify = false
	guardCodexTicketSessionDial(transport)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	for i := 0; i < 3; i++ {
		resp, err := client.Post(upstream.URL, "application/json", nil)
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	require.Equal(t, int64(1), tunnels.Load())
	transport.CloseIdleConnections()
	_, err = client.Post(upstream.URL, "application/json", nil)
	require.ErrorContains(t, err, "reconnect is not reuse")
	require.Equal(t, int64(1), tunnels.Load())
}

func TestCodexTicketSessionCloseCancelsActiveRequest(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	transport := &http.Transport{DialContext: newUpstreamDialer().DialContext}
	session := &codexTicketHTTPSession{
		client: &http.Client{Transport: transport}, transport: transport,
		ctx: ctx, cancel: cancel, validate: func(*http.Request) error { return nil },
	}
	defer session.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL, nil)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := session.Do(req)
		done <- err
	}()
	<-started
	session.Close()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("session close did not cancel its request")
	}
}
