package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type bpsPipeUpstream struct {
	HTTPUpstream
	body io.ReadCloser
}

func (u *bpsPipeUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       u.body,
	}, nil
}

// Snapshots are copied by the writer goroutine; tests never read its buffer
// while the gateway is still writing.
type bpsFlushRecorder struct {
	*httptest.ResponseRecorder
	snapshots chan string
}

func (w *bpsFlushRecorder) Flush() {
	w.ResponseRecorder.Flush()
	w.snapshots <- w.Body.String()
}

func bpsAwaitSnapshot(t *testing.T, snapshots <-chan string, contains string) string {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case snapshot := <-snapshots:
			if strings.Contains(snapshot, contains) {
				return snapshot
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %q", contains)
			return ""
		}
	}
}

func TestBPSStreamingKeepalivePreservesBufferedTools(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "function", true: "custom"}[custom], func(t *testing.T) {
			s, a := bpsFixture()
			s.cfg = &config.Config{Gateway: config.GatewayConfig{StreamKeepaliveInterval: 1}}
			reader, writer := io.Pipe()
			s.httpUpstream = &bpsPipeUpstream{body: reader}
			ctx, cancel := context.WithCancel(context.Background())
			c, _ := bpsContext(1, "/responses")
			rec := &bpsFlushRecorder{ResponseRecorder: httptest.NewRecorder(), snapshots: make(chan string, 64)}
			c, _ = gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/responses", nil).WithContext(ctx)
			tool := bpsFunction("exec")
			native := bpsNative("buffered-call", "exec", map[string]any{"command": "pwd"})
			wantEvent := "response.function_call_arguments.done"
			if custom {
				tool = map[string]any{"type": "custom", "name": "apply_patch"}
				native["arguments"] = bpsJSON(map[string]any{
					"summary": "codex2api.custom/apply_patch",
					"code":    "*** Begin Patch\n*** End Patch",
				})
				wantEvent = "response.custom_tool_call_input.done"
			}
			body := []byte(bpsJSON(map[string]any{
				"model": "gpt-6-astra", "input": "hi", "stream": true, "tools": []any{tool},
			}))
			done := make(chan error, 1)
			exited := make(chan struct{})
			go func() {
				defer close(exited)
				_, err := s.Forward(ctx, c, a, body)
				done <- err
			}()
			t.Cleanup(func() {
				cancel()
				_ = writer.Close()
				select {
				case <-exited:
				case <-time.After(5 * time.Second):
					t.Error("gateway did not stop")
				}
			})
			_, err := io.WriteString(writer, bpsEvent("response.created", map[string]any{
				"response": map[string]any{"id": "resp_test", "status": "in_progress"},
			})+bpsEvent("response.output_item.added", map[string]any{"item": native, "output_index": 0}))
			require.NoError(t, err)
			// Leave a large tool event unfinished: parser progress must not be
			// required for a downstream heartbeat.
			_, err = io.WriteString(writer, "event: response.function_call_arguments.delta\ndata: {\"delta\":\"private")
			require.NoError(t, err)
			snapshot := bpsAwaitSnapshot(t, rec.snapshots, "event: response.in_progress")
			require.Contains(t, snapshot, `"id":"resp_test"`)
			require.NotContains(t, snapshot, "run_officejs")
			require.NotContains(t, snapshot, "private")
			require.NotContains(t, snapshot, "response.output_item.added")
			require.NotContains(t, snapshot, "response.completed")
			_, err = io.WriteString(writer, "\"}\n\n"+bpsEvent("response.completed", map[string]any{"response": bpsResponse(native)}))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("gateway did not finish")
			}
			final := rec.Body.String()
			require.Contains(t, final, wantEvent)
			require.Equal(t, 1, strings.Count(final, "event: response.completed\n"))
			require.NotContains(t, final, "run_officejs")
			require.NotContains(t, final, "private")
			require.Less(t, strings.Index(final, "event: response.in_progress"), strings.Index(final, wantEvent))

			expectedSequence := 0
			resp := &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(final))}
			require.NoError(t, readBPSEvents(resp, 0, func(_ string, payload map[string]any) error {
				sequence, err := bpsTestValue[json.Number](t, payload["sequence_number"]).Int64()
				require.NoError(t, err)
				require.Equal(t, int64(expectedSequence), sequence)
				expectedSequence++
				return nil
			}))
		})
	}
}

func TestBPSKeepaliveReaderStopsOnCancelAndWriteError(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "write error", true: "cancel"}[cancelRequest], func(t *testing.T) {
			reader, writer := io.Pipe()
			defer writer.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			want := errors.New("downstream write failed")
			calls := 0
			resp := &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}
			err := readBPSEventsWithKeepalive(ctx, resp, 0, time.Millisecond, func(string, map[string]any) error {
				return nil
			}, func() error {
				calls++
				if cancelRequest {
					cancel()
					return nil
				}
				return want
			})
			require.Equal(t, 1, calls)
			if cancelRequest {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, want)
			}
			_, err = writer.Write([]byte("closed"))
			require.ErrorIs(t, err, io.ErrClosedPipe)
		})
	}
}

func TestBPSKeepaliveDoesNotHideUpstreamTimeout(t *testing.T) {
	for _, sendCreated := range []bool{false, true} {
		t.Run(map[bool]string{false: "before created", true: "after created"}[sendCreated], func(t *testing.T) {
			s, a := bpsFixture()
			s.cfg = &config.Config{Gateway: config.GatewayConfig{
				StreamKeepaliveInterval:   1,
				StreamDataIntervalTimeout: 2,
			}}
			reader, writer := io.Pipe()
			s.httpUpstream = &bpsPipeUpstream{body: reader}
			c, rec := bpsContext(1, "/responses")
			ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			sent := make(chan struct{})
			go func() {
				defer close(sent)
				if sendCreated {
					_, _ = io.WriteString(writer, bpsEvent("response.created", map[string]any{
						"response": map[string]any{"id": "resp_timeout", "status": "in_progress"},
					}))
				}
			}()
			defer func() { _ = writer.Close(); <-sent }()
			_, err := s.Forward(ctx, c, a, bpsRequestBody("hi", true))
			require.ErrorContains(t, err, "BPS upstream stopped sending data")
			require.NotContains(t, rec.Body.String(), "response.completed")
			if sendCreated {
				require.Contains(t, rec.Body.String(), "event: response.in_progress")
				require.Contains(t, rec.Body.String(), "event: response.failed")
			} else {
				require.NotContains(t, rec.Body.String(), "response.in_progress")
				require.NotContains(t, rec.Body.String(), "response.created")
				require.Equal(t, http.StatusGatewayTimeout, rec.Code)
			}
		})
	}
}

func TestBPSKeepaliveReaderDisabledAndMalformed(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		wantError               bool
	}{
		{"unary", "application/json", bpsJSON(bpsResponse(bpsText("ok"))), false},
		{"malformed", "text/event-stream", "data: not-json\n\n", true},
		{"empty", "text/event-stream", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{"Content-Type": []string{tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body))}
			err := readBPSEventsWithKeepalive(context.Background(), resp, 0, 0, func(string, map[string]any) error {
				time.Sleep(5 * time.Millisecond)
				return nil
			}, func() error {
				t.Fatal("disabled keepalive was called")
				return nil
			})
			if tc.wantError {
				require.ErrorContains(t, err, "malformed")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
