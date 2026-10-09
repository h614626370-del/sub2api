package imagemaster

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imagepolicy"
	"github.com/stretchr/testify/require"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(m.Close)
	cfg := m.Config()
	cfg.Enabled = true
	require.NoError(t, m.SaveConfig(cfg))
	return m
}

func textRequest(stream bool) []byte {
	body, _ := json.Marshal(map[string]any{"model": "gpt-5.5", "stream": stream, "background": false,
		"input": "private prompt", "tools": []any{map[string]any{"type": "image_generation", "model": "gpt-image-2", "partial_images": 2}}})
	return body
}

func successful(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", "gateway-test-id")
	_, _ = io.WriteString(w, `{"data":[{"b64_json":"dGVzdA=="}],"usage":{"total_tokens":42}}`)
}

func serve(t *testing.T, m *Manager, raw []byte, call Dispatch) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer synthetic-client-key")
	w := httptest.NewRecorder()
	m.Serve(w, req, raw, Identity{UserID: 7, APIKeyID: 9}, call)
	return w
}

func testImage(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 2, 2))))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

func editRequest(t *testing.T, imageURLs ...string) []byte {
	t.Helper()
	content := []any{map[string]any{"type": "input_text", "text": "edit image"}}
	for _, url := range imageURLs {
		content = append(content, map[string]any{"type": "input_image", "image_url": url})
	}
	raw, err := json.Marshal(map[string]any{"model": "gpt-5.5", "stream": true,
		"tools": []any{map[string]any{"type": "image_generation", "model": "gpt-image-2", "size": "1024x1024"}},
		"input": []any{map[string]any{"role": "user", "content": content}}})
	require.NoError(t, err)
	return raw
}

func TestImageMasterResponsesRewriteAndEvents(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(map[bool]string{true: "stream", false: "json"}[stream], func(t *testing.T) {
			m := testManager(t)
			var calls int
			w := serve(t, m, textRequest(stream), func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "/v1/images/generations", r.URL.Path)
				require.True(t, imagepolicy.DirectOnly(r.Context()))
				require.Equal(t, "Bearer synthetic-client-key", r.Header.Get("Authorization"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "gpt-image-2", body["model"])
				require.Equal(t, "private prompt", body["prompt"])
				require.Equal(t, float64(1), body["n"])
				require.Equal(t, "b64_json", body["response_format"])
				require.Equal(t, false, body["stream"])
				require.NotContains(t, body, "background")
				require.NotContains(t, body, "tools")
				require.NotContains(t, body, "partial_images")
				successful(w, r)
			})
			require.Equal(t, 1, calls)
			require.Equal(t, 200, w.Code)
			require.Contains(t, w.Body.String(), `"total_tokens":42`)
			if stream {
				require.Contains(t, w.Body.String(), "event: response.completed")
				require.Contains(t, w.Body.String(), `"id":"ig_`+w.Header().Get("X-Bridge-Request-Id")+`"`)
				require.Equal(t, 1, strings.Count(w.Body.String(), "event: response.completed"))
				require.NotContains(t, w.Body.String(), "partial_image")
			} else {
				require.NotContains(t, w.Body.String(), "event:")
			}
			s := m.Snapshot()
			require.Zero(t, s.Active)
			require.Len(t, s.Items, 1)
			require.Equal(t, "completed", s.Items[0].Outcome)
			require.Equal(t, "images-generations", s.Items[0].Route)
			require.Equal(t, "gpt-image-2", s.Items[0].Model)
			require.Equal(t, "gpt-5.5", s.Items[0].RequestedModel)
			require.EqualValues(t, 7, s.Items[0].UserID)
			require.Equal(t, "gateway-test-id", s.Items[0].GatewayRequestID)
			stored, err := os.ReadFile(filepath.Join(m.dir, "requests.json"))
			require.NoError(t, err)
			require.NotContains(t, string(stored), "synthetic-client-key")
			require.NotContains(t, string(stored), "private prompt")
		})
	}
}

func firstImageTool(t *testing.T, input map[string]any) map[string]any {
	t.Helper()
	return objectAt(t, input, "tools", 0)
}

func objectAt(t *testing.T, input map[string]any, field string, index int) map[string]any {
	t.Helper()
	items, ok := input[field].([]any)
	require.True(t, ok)
	require.Greater(t, len(items), index)
	item, ok := items[index].(map[string]any)
	require.True(t, ok)
	return item
}

func TestImageMasterDirectEditsAndMask(t *testing.T) {
	m := testManager(t)
	raw := editRequest(t, testImage(t), testImage(t))
	var input map[string]any
	require.NoError(t, json.Unmarshal(raw, &input))
	firstImageTool(t, input)["input_image_mask"] = map[string]any{"image_url": testImage(t)}
	raw, _ = json.Marshal(input)
	w := serve(t, m, raw, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/images/edits", r.URL.Path)
		require.True(t, imagepolicy.DirectOnly(r.Context()))
		require.NoError(t, r.ParseMultipartForm(4<<20))
		defer func() { require.NoError(t, r.MultipartForm.RemoveAll()) }()
		require.Equal(t, "gpt-image-2", r.FormValue("model"))
		require.Equal(t, "1", r.FormValue("n"))
		require.Equal(t, "b64_json", r.FormValue("response_format"))
		require.NotEqual(t, "true", r.FormValue("stream"))
		require.Len(t, r.MultipartForm.File["image"], 2)
		require.Len(t, r.MultipartForm.File["mask"], 1)
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"dGVzdA=="}],"usage":{"total_tokens":7}}`)
	})
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "event: response.completed")
	require.Contains(t, w.Body.String(), `"total_tokens":7`)
	require.Equal(t, 2, m.Snapshot().Items[0].SourceImages)
}

func TestImageMasterUnsupportedInputsNeverDispatch(t *testing.T) {
	m := testManager(t)
	base := editRequest(t, testImage(t))
	for _, change := range []func(map[string]any){
		func(v map[string]any) { v["previous_response_id"] = "resp_prior" },
		func(v map[string]any) { firstImageTool(t, v)["n"] = 2 },
		func(v map[string]any) { firstImageTool(t, v)["model"] = "text-model" },
		func(v map[string]any) { v["stream"] = "true" },
		func(v map[string]any) { v["background"] = true },
		func(v map[string]any) { v["instructions"] = 123 },
		func(v map[string]any) { objectAt(t, v, "input", 0)["role"] = "assistant" },
	} {
		var v map[string]any
		require.NoError(t, json.Unmarshal(base, &v))
		change(v)
		raw, _ := json.Marshal(v)
		w := serve(t, m, raw, func(http.ResponseWriter, *http.Request) { t.Error("unexpected dispatch") })
		require.Equal(t, 400, w.Code)
	}
	w := serve(t, m, editRequest(t, "http://example.test/a.png"), func(http.ResponseWriter, *http.Request) { t.Error("unexpected dispatch") })
	require.Equal(t, 400, w.Code)
}

func TestImageMasterRemoteFailureCancelsSiblings(t *testing.T) {
	m := testManager(t)
	raw := editRequest(t, "https://example.test/one.png", "https://example.test/two.png")
	ready := make(chan struct{})
	canceled := make(chan struct{})
	loader := func(ctx context.Context, url string) (imageInput, error) {
		if strings.Contains(url, "one") {
			<-ready
			return imageInput{}, fail(400, "image_download_failed")
		}
		close(ready)
		<-ctx.Done()
		close(canceled)
		return imageInput{}, ctx.Err()
	}
	w := httptest.NewRecorder()
	m.serve(w, httptest.NewRequest("POST", "/v1/responses", nil), raw, Identity{},
		func(http.ResponseWriter, *http.Request) { t.Error("unexpected dispatch") }, loader)
	<-canceled
	require.Contains(t, w.Body.String(), "response.failed")
	require.Zero(t, m.Snapshot().Active)
}

func TestImageMasterPublicImageAddresses(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "::1", "::ffff:8.8.8.8", "fc00::1", "2001:db8::1"} {
		require.False(t, publicIP(netip.MustParseAddr(value)), value)
	}
	require.True(t, publicIP(netip.MustParseAddr("8.8.8.8")))
	for _, value := range []string{"http://example.com/a", "file:///etc/passwd", "https://user:pass@example.com/a"} {
		_, err := remoteURL(value)
		require.Error(t, err)
	}
	_, err := downloadImage(context.Background(), "https://127.0.0.1/a")
	require.Error(t, err)
}

func TestImageMasterConcurrentRequestsGoStraightToGateway(t *testing.T) {
	m := testManager(t)
	var calls atomic.Int32
	gate := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(gate) })
	dispatch := func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-gate
		successful(w, r)
	}
	var wg sync.WaitGroup
	defer func() { unblock(); wg.Wait() }()
	statuses := make(chan int, 30)
	for i := 0; i < 30; i++ {
		wg.Go(func() {
			w := serve(t, m, textRequest(false), dispatch)
			statuses <- w.Code
		})
	}
	require.Eventually(t, func() bool { return calls.Load() == 30 }, 5*time.Second, time.Millisecond)
	require.Equal(t, 30, m.Snapshot().Active)
	for _, row := range m.Snapshot().Items {
		require.Equal(t, "running", row.Outcome)
		require.Equal(t, "received", row.Stages[0].Name)
	}
	unblock()
	wg.Wait()
	require.EqualValues(t, 30, calls.Load())
	for i := 0; i < 30; i++ {
		require.Equal(t, 200, <-statuses)
	}
	require.Zero(t, m.Snapshot().Active)
}

func TestImageMasterCancelDoesNotBlockNewRequests(t *testing.T) {
	m := testManager(t)
	started := make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- serve(t, m, textRequest(true), func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
		})
	}()
	<-started
	w := serve(t, m, textRequest(false), successful)
	require.Equal(t, 200, w.Code)
	var row Record
	for _, item := range m.Snapshot().Items {
		if item.FinishedAt == 0 {
			row = item
		}
	}
	require.NotEmpty(t, row.ID)
	require.Error(t, m.Delete(row.ID))
	require.True(t, m.Cancel(row.ID))
	require.Contains(t, (<-done).Body.String(), "operator_cancelled")
	var canceled bool
	for _, item := range m.Snapshot().Items {
		if item.ID == row.ID {
			canceled = item.Outcome == "canceled"
		}
	}
	require.True(t, canceled)
	require.Zero(t, m.Snapshot().Active)
	require.Equal(t, 200, serve(t, m, textRequest(false), successful).Code)
}

func TestImageMasterGatewayLimitIsStillEnforced(t *testing.T) {
	m := testManager(t)
	w := serve(t, m, textRequest(false), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	require.Equal(t, 429, w.Code)
	require.Contains(t, w.Body.String(), "gateway_http_429")
	require.Zero(t, m.Snapshot().Active)
}

func TestImageMasterIgnoresRetiredLocalControls(t *testing.T) {
	dir := t.TempDir()
	cfg, err := json.Marshal(Defaults())
	require.NoError(t, err)
	var legacy map[string]any
	require.NoError(t, json.Unmarshal(cfg, &legacy))
	legacy["max_concurrent"], legacy["max_queue"], legacy["paused"] = 1, 0, true
	legacy["control_model"], legacy["direct_edits"] = "invalid/retired/model", false
	legacy["enabled"] = true
	require.NoError(t, atomicJSON(filepath.Join(dir, "config.json"), legacy))
	m, err := New(dir)
	require.NoError(t, err)
	defer m.Close()
	expected := Defaults()
	expected.Enabled = true
	require.Equal(t, expected, m.Config())
	require.NoError(t, m.SaveConfig(m.Config()))
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	require.NoError(t, err)
	require.NotContains(t, string(data), "max_concurrent")
	require.NotContains(t, string(data), "max_queue")
	require.NotContains(t, string(data), "paused")
	require.NotContains(t, string(data), "control_model")
	require.NotContains(t, string(data), "direct_edits")
	status, err := json.Marshal(m.Snapshot())
	require.NoError(t, err)
	require.NotContains(t, string(status), "queued")
	require.NotContains(t, string(status), "paused")
}

func TestImageMasterHistoryCapacityDoesNotLimitActiveRequests(t *testing.T) {
	m := testManager(t)
	m.mu.Lock()
	for i := 0; i < maxRecords; i++ {
		id := time.UnixMilli(int64(i)).Format(time.RFC3339Nano)
		m.records[id] = &Record{ID: id, StartedAt: time.Now().UnixMilli(), Outcome: "running"}
	}
	m.mu.Unlock()
	m.start(Record{ID: "extra", StartedAt: time.Now().UnixMilli(), Outcome: "running"}, func() {})
	require.Len(t, m.Snapshot().Items, maxRecords+1)
	require.Equal(t, 1, m.Snapshot().Active)
	m.finish("extra", "completed", "")
	require.Len(t, m.Snapshot().Items, maxRecords)
	require.Zero(t, m.Snapshot().Active)
}

func TestImageMasterHeartbeatArrivesBeforeResult(t *testing.T) {
	m := testManager(t)
	cfg := m.Config()
	cfg.HeartbeatSeconds = 1
	require.NoError(t, m.SaveConfig(cfg))
	gate := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.Serve(w, r, textRequest(true), Identity{}, func(w http.ResponseWriter, r *http.Request) { <-gate; successful(w, r) })
	}))
	defer server.Close()
	res, err := http.Get(server.URL)
	require.NoError(t, err)
	defer func() { require.NoError(t, res.Body.Close()) }()
	reader := bufio.NewReader(res.Body)
	require.Eventually(t, func() bool {
		line, err := reader.ReadString('\n')
		return err == nil && strings.HasPrefix(line, ": keep-alive")
	}, 3*time.Second, time.Millisecond)
	close(gate)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(body), "response.completed")
}

func TestImageMasterTimeoutAndFailuresNeverRetry(t *testing.T) {
	for _, code := range []string{"timeout", "http", "empty", "sse", "oversize"} {
		t.Run(code, func(t *testing.T) {
			m := testManager(t)
			cfg := m.Config()
			cfg.TimeoutSeconds = 1
			cfg.MaxResponseMiB = 1
			require.NoError(t, m.SaveConfig(cfg))
			calls := 0
			w := serve(t, m, textRequest(true), func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch code {
				case "timeout":
					<-r.Context().Done()
				case "http":
					w.WriteHeader(503)
					_, _ = io.WriteString(w, "not json")
				case "empty":
					_, _ = io.WriteString(w, `{"id":"resp","status":"completed","output":[]}`)
				case "sse":
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {}")
				case "oversize":
					_, _ = io.WriteString(w, strings.Repeat("x", 2<<20))
				}
			})
			require.Equal(t, 1, calls)
			require.Contains(t, w.Body.String(), "response.failed")
			require.NotContains(t, w.Body.String(), "event: response.completed")
			require.Zero(t, m.Snapshot().Active)
		})
	}
}

func TestImageMasterSettingsRawRetentionAndPersistence(t *testing.T) {
	m := testManager(t)
	cfg := m.Config()
	cfg.RawRequestLogging = true
	require.NoError(t, m.SaveConfig(cfg))
	w := serve(t, m, textRequest(false), successful)
	id := w.Header().Get("X-Bridge-Request-Id")
	raw, err := m.Raw(id)
	require.NoError(t, err)
	require.Contains(t, string(raw), "synthetic-client-key")
	reopened, err := New(m.dir)
	require.NoError(t, err)
	defer reopened.Close()
	require.True(t, reopened.Config().Enabled)
	require.Len(t, reopened.Snapshot().Items, 1)
	_, err = m.Raw("../config")
	require.Error(t, err)
	m.mu.Lock()
	m.records[id].StartedAt = time.Now().Add(-13 * time.Hour).UnixMilli()
	m.lastPrune = time.Time{}
	m.mu.Unlock()
	require.Empty(t, m.Snapshot().Items)
	_, err = os.Stat(filepath.Join(m.dir, "raw", id+".json"))
	require.True(t, os.IsNotExist(err))
	cfg.TimeoutSeconds = 0
	require.Error(t, m.SaveConfig(cfg))
	require.NotZero(t, m.Config().TimeoutSeconds)
}

func TestImageMasterEditsAlwaysUseImageEndpoint(t *testing.T) {
	p, err := Prepare(editRequest(t, testImage(t)))
	require.NoError(t, err)
	require.Equal(t, "images-edits", p.Route)
}

func TestImageMasterRawBudgetIncludesHeadersAndEscaping(t *testing.T) {
	headers := map[string][]string{"Authorization": {"Bearer synthetic-key"}}
	body := []byte("{\"input\":\"<>&\"}\n")
	data, err := encodeRawRequest(headers, body, 1024)
	require.NoError(t, err)
	_, err = encodeRawRequest(headers, body, int64(len(data)))
	require.NoError(t, err)
	_, err = encodeRawRequest(headers, body, int64(len(data)-1))
	require.Error(t, err)
	_, err = encodeRawRequest(headers, body, -1)
	require.Error(t, err)
	var decoded struct {
		Headers map[string][]string `json:"headers"`
		Body    string              `json:"body"`
	}
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, string(body), decoded.Body)
	require.Equal(t, headers, decoded.Headers)
}

func TestImageMasterRawCleanupFailureRemainsVisibleAndRetries(t *testing.T) {
	m := testManager(t)
	orphan := filepath.Join(m.dir, "raw", "orphan.json")
	require.NoError(t, os.Mkdir(orphan, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(orphan, "blocked"), []byte("test"), 0600))
	m.mu.Lock()
	m.lastPrune = time.Time{}
	m.pruneLocked()
	m.persistLocked()
	m.mu.Unlock()
	require.True(t, m.Snapshot().StorageError)
	require.Error(t, m.Delete(""))
	require.NoError(t, os.Remove(filepath.Join(orphan, "blocked")))
	require.NoError(t, m.Delete(""))
	require.False(t, m.Snapshot().StorageError)
}

func TestImageMasterIncompleteAndReasoningReplay(t *testing.T) {
	p := &Plan{Route: "responses"}
	raw := []byte(`{"id":"resp_x","status":"incomplete","output":[{"id":"r","type":"reasoning","summary":[{"type":"summary_text","text":"summary"}]},{"id":"m","type":"message","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"total_tokens":3}}`)
	snapshot, _, err := responseSnapshot(raw, p, "test")
	require.NoError(t, err)
	w := httptest.NewRecorder()
	require.NoError(t, (&eventWriter{w: w}).replay(snapshot))
	require.Contains(t, w.Body.String(), "event: response.incomplete")
	require.Contains(t, w.Body.String(), "response.reasoning_summary_text.done")
	require.Contains(t, w.Body.String(), "response.output_text.done")
}
