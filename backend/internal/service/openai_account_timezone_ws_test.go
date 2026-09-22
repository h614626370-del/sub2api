package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountTimezoneWebSocketWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIWSExecutionScopeTestConfig()
	capture := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_tz","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)}}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: capture, handshake: http.Header{}})
	defer pool.Close()
	s := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool, openaiWSStateStore: NewOpenAIWSStateStore(nil)}
	a := &Account{ID: 455, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{"responses_websockets_v2_enabled": true, accountTimezoneOverrideKey: "Asia/Tokyo"}}
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		c.Set("api_key", &APIKey{ID: 21})
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, first, err := conn.Read(ctx)
		if err != nil {
			done <- err
			return
		}
		done <- s.ProxyResponsesWebSocketFromClient(ctx, c, conn, a, "test", first, nil)
	}))
	defer server.Close()
	client := dialOpenAIWSExecutionScopeClient(t, server.URL, "tz")
	defer func() { _ = client.CloseNow() }()
	writeOpenAIWSExecutionScopeRequest(t, client, `{"type":"response.create","model":"gpt-5.1","store":false,"input":[{"role":"user","internal_chat_message_metadata_passthrough":{"content_item_kinds":["environments.environment_context"]},"content":[{"type":"input_text","text":"<environment_context><timezone>UTC</timezone></environment_context>"}]}],"tools":[{"type":"web_search"}]}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := client.Read(ctx)
	require.NoError(t, err)
	_ = client.Close(coderws.StatusNormalClosure, "done")
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	capture.mu.Lock()
	wire, err := json.Marshal(capture.lastWrite)
	capture.mu.Unlock()
	require.NoError(t, err)
	require.Contains(t, gjson.GetBytes(wire, "input.0.content.0.text").String(), "Asia/Tokyo")
	require.Equal(t, "Asia/Tokyo", gjson.GetBytes(wire, "tools.0.user_location.timezone").String())
}
