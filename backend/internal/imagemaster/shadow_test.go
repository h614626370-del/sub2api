package imagemaster

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imagepolicy"
	"github.com/stretchr/testify/require"
)

const originalImage = `{"id":"resp_test","status":"completed","output":[{"type":"image_generation_call","status":"completed","result":"image"}]}`

func shadowManager(t *testing.T) *Manager {
	m := testManager(t)
	cfg := m.Config()
	cfg.Enabled = false
	cfg.Shadow = ShadowConfig{Enabled: true, GroupID: 9, APIKeyID: 90}
	require.NoError(t, m.SaveConfig(cfg))
	return m
}
func shadowFinished(t *testing.T, m *Manager) ShadowRecord {
	t.Helper()
	require.Eventually(t, func() bool { s := m.ShadowSnapshot(); return s.Active == 0 && len(s.Items) == 1 }, 3*time.Second, 10*time.Millisecond)
	return m.ShadowSnapshot().Items[0]
}
func TestShadowOriginalRequiresImageAndTerminalResult(t *testing.T) {
	for _, tc := range []struct {
		name, body, content, outcome string
		status                       int
		overflow, disconnected       bool
	}{
		{"json", originalImage, "application/json", "completed", 200, false, false},
		{"sse", "data: {\"type\":\"response.completed\",\"response\":" + originalImage + "}\r\n\r\ndata: [DONE]\n\n", "text/event-stream", "completed", 200, false, false},
		{"text only", `{"status":"completed","output":[]}`, "application/json", "no_image", 200, false, false},
		{"stream failed", "data: {\"type\":\"response.failed\"}\n\n", "text/event-stream", "failed", 200, false, false},
		{"incomplete", `{"status":"incomplete"}`, "application/json", "failed", 200, false, false},
		{"done only", "data: [DONE]\n\n", "text/event-stream", "unknown", 200, false, false},
		{"item then empty terminal", "data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"status\":\"completed\",\"result\":\"image\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n", "text/event-stream", "completed", 200, false, false},
		{"multiline sse", "data: {\"type\":\"response.completed\",\n" + "data: \"response\":" + originalImage + "}\n\n", "text/event-stream", "completed", 200, false, false},
		{"http error", originalImage, "application/json", "failed", 429, false, false},
		{"limit", originalImage, "application/json", "unknown", 200, true, false},
		{"disconnect", originalImage, "application/json", "disconnected", 200, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ObserveOriginal([]byte(tc.body), tc.content, tc.status, tc.overflow, tc.disconnected)
			require.Equal(t, tc.outcome, r.Outcome)
		})
	}
}
func TestShadowAsyncIsolationAdmissionAndMetadata(t *testing.T) {
	m := shadowManager(t)
	entered, release := make(chan struct{}), make(chan struct{})
	job := m.BeginShadow(textRequest(true), Identity{UserID: 7, APIKeyID: 8}, 2, "original-id", func(w http.ResponseWriter, r *http.Request) {
		require.True(t, imagepolicy.DirectOnly(r.Context()))
		require.Empty(t, r.Header.Get("Authorization"))
		require.Equal(t, "/v1/images/generations", r.URL.Path)
		require.NotEqual(t, "original-id", r.Header.Get("X-Request-ID"))
		payload, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, string(payload), "private prompt")
		close(entered)
		<-release
		successful(w, r)
	})
	require.NotNil(t, job)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	job.CompleteOriginal([]byte(originalImage), "application/json", 200, false, false)
	close(release)
	row := shadowFinished(t, m)
	require.Equal(t, "completed", row.Test.Outcome)
	require.Equal(t, "completed", row.Original.Outcome)
	require.EqualValues(t, 90, row.APIKeyID)
	require.EqualValues(t, 7, row.UserID)
	m.shadow.persist()
	data, err := os.ReadFile(filepath.Join(m.dir, "shadow.json"))
	require.NoError(t, err)
	require.NotContains(t, string(data), "private prompt")
	require.NotContains(t, string(data), "b64_json")
	require.Empty(t, m.Snapshot().Items)
}
func TestShadowEditsPreservePromptAndImage(t *testing.T) {
	m := shadowManager(t)
	job := m.BeginShadow(editRequest(t, testImage(t)), Identity{}, 2, "", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/images/edits", r.URL.Path)
		require.NoError(t, r.ParseMultipartForm(1<<20))
		defer func() { require.NoError(t, r.MultipartForm.RemoveAll()) }()
		require.Equal(t, "edit image", r.FormValue("prompt"))
		require.Equal(t, "1024x1024", r.FormValue("size"))
		require.Len(t, r.MultipartForm.File["image"], 1)
		successful(w, r)
	})
	job.CompleteOriginal([]byte(originalImage), "application/json", 200, false, false)
	require.Equal(t, "images-edits", shadowFinished(t, m).Route)
}
func TestShadowUnsupportedTimeoutAndPanicDoNotAffectOriginal(t *testing.T) {
	for _, kind := range []string{"unsupported", "timeout", "panic", "http_failure"} {
		t.Run(kind, func(t *testing.T) {
			m := shadowManager(t)
			cfg := m.Config()
			cfg.TimeoutSeconds = 1
			require.NoError(t, m.SaveConfig(cfg))
			raw := textRequest(false)
			if kind == "unsupported" {
				raw = []byte(strings.Replace(string(raw), `"input":`, `"previous_response_id":"hidden","input":`, 1))
			}
			job := m.BeginShadow(raw, Identity{}, 2, "", func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "unsupported":
					t.Error("unsupported dispatch")
				case "timeout":
					<-r.Context().Done()
				case "panic":
					panic("synthetic")
				default:
					w.WriteHeader(429)
				}
			})
			job.CompleteOriginal([]byte(originalImage), "application/json", 200, false, false)
			row := shadowFinished(t, m)
			require.Equal(t, "completed", row.Original.Outcome)
			if kind == "unsupported" {
				require.Equal(t, "unsupported", row.Test.Outcome)
			} else {
				require.Equal(t, "failed", row.Test.Outcome)
			}
		})
	}
}
func TestShadowReloadMarksInterruptedUnknown(t *testing.T) {
	dir := t.TempDir()
	rows := []ShadowRecord{{ID: "test", StartedAt: time.Now().UnixMilli(), Original: ShadowResult{Outcome: "running"}, Test: ShadowResult{Outcome: "running"}}}
	require.NoError(t, atomicJSON(filepath.Join(dir, "shadow.json"), rows))
	s := newShadowStore(dir)
	defer s.close()
	require.Equal(t, "unknown", s.snapshot().Items[0].Original.Outcome)
	require.Equal(t, "unknown", s.snapshot().Items[0].Test.Outcome)
}
func TestShadowSettingsRemainSeparate(t *testing.T) {
	m := shadowManager(t)
	before := m.Config()
	require.NoError(t, m.SaveShadowConfig(ShadowDefaults()))
	require.Equal(t, before.Enabled, m.Config().Enabled)
	require.Equal(t, before.TimeoutSeconds, m.Config().TimeoutSeconds)
	_, err := m.SaveMainConfig(before)
	require.NoError(t, err)
	require.False(t, m.Config().Shadow.Enabled)
	require.Error(t, (ShadowConfig{Enabled: true}).Validate())
	require.Error(t, m.ValidateShadowTarget(context.Background(), before.Shadow))
	m.SetShadowTargetValidator(func(context.Context, ShadowConfig) error { return nil })
	require.NoError(t, m.ValidateShadowTarget(context.Background(), before.Shadow))
	data, err := json.Marshal(m.ShadowSnapshot())
	require.NoError(t, err)
	require.NotContains(t, string(data), "Authorization")
}

func TestShadowEveryRequestReachesGatewayWithRetiredSettings(t *testing.T) {
	dir := t.TempDir()
	cfg := Defaults()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	var saved map[string]any
	require.NoError(t, json.Unmarshal(raw, &saved))
	saved["shadow"] = map[string]any{"enabled": true, "group_id": 9, "api_key_id": 90, "sample_percent": 1, "concurrency": 1}
	require.NoError(t, atomicJSON(filepath.Join(dir, "config.json"), saved))
	m, err := New(dir)
	require.NoError(t, err)
	t.Cleanup(m.Close)
	entered, release := make(chan struct{}, 8), make(chan struct{})
	defer close(release)
	for i := 0; i < 8; i++ {
		job := m.BeginShadow(textRequest(false), Identity{}, 2, "", func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-release; successful(w, r) })
		require.NotNil(t, job)
		job.CompleteOriginal([]byte(originalImage), "application/json", 200, false, false)
	}
	for i := 0; i < 8; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("shadow must dispatch every eligible request without a local concurrency cap")
		}
	}
	require.Equal(t, 8, m.ShadowSnapshot().Active)
	require.NoError(t, m.SaveShadowConfig(m.Config().Shadow))
	raw, err = os.ReadFile(filepath.Join(dir, "config.json"))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "sample_percent")
	require.NotContains(t, string(raw), "concurrency")
}

func TestShadowHistoryCapacityDoesNotLimitRequests(t *testing.T) {
	m := shadowManager(t)
	m.shadow.mu.Lock()
	for i := 0; i < 2000; i++ {
		id := strconv.Itoa(i)
		m.shadow.rows[id] = &ShadowRecord{ID: id, StartedAt: time.Now().UnixMilli(), Original: ShadowResult{Outcome: "running"}, Test: ShadowResult{Outcome: "running"}}
	}
	m.shadow.mu.Unlock()
	dispatched := make(chan struct{}, 1)
	job := m.BeginShadow(textRequest(false), Identity{}, 2, "", func(w http.ResponseWriter, r *http.Request) { dispatched <- struct{}{}; successful(w, r) })
	require.NotNil(t, job)
	job.CompleteOriginal([]byte(originalImage), "application/json", 200, false, false)
	select {
	case <-dispatched:
	case <-time.After(time.Second):
		t.Fatal("history must not block gateway dispatch")
	}
	require.Eventually(t, func() bool { return m.ShadowSnapshot().Active == 0 }, time.Second, 10*time.Millisecond)
	require.Len(t, m.ShadowSnapshot().Items, 2001)
	m.shadow.mu.Lock()
	for _, r := range m.shadow.rows {
		r.Original.Outcome = "completed"
		r.Test.Outcome = "completed"
	}
	m.shadow.trimHistoryLocked()
	require.Len(t, m.shadow.rows, 2000)
	m.shadow.mu.Unlock()
}
