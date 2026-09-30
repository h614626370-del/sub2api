package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/requestcapture"
	"github.com/stretchr/testify/require"
)

type bps403CaptureStore struct {
	requestcapture.Store
	mu      sync.Mutex
	records [][]byte
}

func (s *bps403CaptureStore) Tasks(context.Context, string, int, int) ([]requestcapture.Task, error) {
	return nil, nil
}
func (s *bps403CaptureStore) SaveTask(context.Context, *requestcapture.Task) error { return nil }
func (s *bps403CaptureStore) SaveRecord(_ context.Context, r *requestcapture.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(r)
	if err == nil {
		s.records = append(s.records, data)
	}
	return err
}

type bps403CaptureSettingsRepo struct{ fakeSettingRepo }

func (s *bps403CaptureSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := map[string]string{}
	for _, key := range keys {
		values[key] = s.vals[key]
	}
	return values, nil
}

func TestBPS403ForwardAutomaticallyCapturesOriginalAndActualRequest(t *testing.T) {
	for _, session := range []string{"known-session", ""} {
		t.Run("session="+session, func(t *testing.T) {
			store := &bps403CaptureStore{}
			dir := t.TempDir()
			manager, err := requestcapture.New(store, dir, requestcapture.Config{BPS403Enabled: true, QuotaMiB: 16, RetentionDays: 7})
			require.NoError(t, err)
			t.Cleanup(manager.Close)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 403, Header: http.Header{"X-Request-Id": {"bps-request-id"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"basispoints_model_access_changed"}}`))}}
			svc := openAIClientToolsTestService(upstream)
			cache := &comboCacheAndStore{}
			svc.cache = cache
			svc.settingService = NewSettingService(&bps403CaptureSettingsRepo{fakeSettingRepo{vals: map[string]string{SettingKeyBPS403CaptureEnabled: "true", SettingKeyBPS403SessionBlockEnabled: "true"}}}, nil)
			svc.settingService.requestCapture = manager
			c, original := newCyberBlockTestCtx(map[string]string{"session_id": session}, `{"model":"gpt-5.6-sol","input":"original full conversation","client_metadata":{"local_only":"preserve-original-marker"}}`)
			ctx := context.WithValue(requestcapture.WithDeferredBody(c.Request.Context()), ctxkey.ClientRequestID, "gateway-request-id")
			requestcapture.RememberDeferredBody(ctx, original)
			c.Request = c.Request.WithContext(ctx)
			c.Set("api_key", &APIKey{ID: 7, UserID: 8})
			// Simulate rewriting before forwarding; the original remains request-scoped.
			forwarded := []byte(`{"model":"gpt-5.6-sol","input":"original full conversation"}`)
			_, err = svc.forwardExcelBPS(ctx, c, excelAccount(), forwarded, time.Now())
			require.Error(t, err)
			require.Equal(t, http.StatusForbidden, c.Writer.Status())
			require.Equal(t, "/basispoints/api/responses", upstream.lastReq.URL.Path)
			require.Equal(t, session != "", svc.FindBPS403SessionBlockedForIdentity(ctx, ResolveCyberSessionIdentity(7, c, original)) != "")
			// A repeated observer for the same logical request must not duplicate it.
			svc.observeBPS403Response(c, excelAccount(), upstream.lastReq, upstream.resp)
			require.Eventually(t, func() bool { return manager.Stats().ActiveRequests == 0 }, 5*time.Second, 10*time.Millisecond)
			store.mu.Lock()
			records := append([][]byte(nil), store.records...)
			store.mu.Unlock()
			require.Len(t, records, 1)
			var record requestcapture.Record
			require.NoError(t, json.Unmarshal(records[0], &record))
			require.Equal(t, "gateway-request-id", record.RequestID)
			require.Equal(t, "bps-request-id", record.Attempts[0].UpstreamRequestID)
			require.False(t, record.Partial, record.Reason)
			require.Len(t, record.Parts, 2)
			for _, part := range record.Parts {
				saved, err := os.ReadFile(filepath.Join(dir, record.TaskID, record.ID, part.Name))
				require.NoError(t, err)
				if part.Stage == "client_request" {
					require.JSONEq(t, string(original), string(saved))
				}
				if part.Stage == "upstream_request" {
					require.JSONEq(t, string(upstream.lastBody), string(saved))
				}
			}
		})
	}
}
