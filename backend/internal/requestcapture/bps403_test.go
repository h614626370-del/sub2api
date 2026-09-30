package requestcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBPS403CaptureWithoutManualTasksPreservesFullPlaintext(t *testing.T) {
	m, err := New(newMemoryStore(), t.TempDir(), Config{BPS403Enabled: true, QuotaMiB: 1024, RetentionDays: 7})
	require.NoError(t, err)
	t.Cleanup(m.Close)
	text := strings.Repeat("完整对话与工具结果", 60000) + "END-OF-PLAINTEXT"
	body, err := json.Marshal(map[string]any{"model": "test", "input": text, "authorization": "private-body-secret"})
	require.NoError(t, err)
	upstream, err := json.Marshal(map[string]any{"instructions": "actual BPS bridge instructions", "input": text, "access_token": "private-upstream-secret"})
	require.NoError(t, err)
	id, err := m.CaptureBPS403(context.Background(), Meta{APIKeyID: 1, SessionHash: "hash", UserID: 9, Method: "POST", Path: "/v1/responses"}, 42, body, upstream, http.Header{"Authorization": {"Bearer private-header-secret"}}, http.Header{"X-Request-Id": {"upstream-403"}})
	require.NoError(t, err)
	require.Nil(t, m.Begin(Meta{UserID: 9}), "automatic capture must not enable manual traffic capture")
	drain(t, m)
	task, err := m.Task(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "bps403", task.TargetType)
	require.NotEqual(t, "running", task.Status)
	records, err := m.Records(context.Background(), id, "", true, 20, 0)
	require.NoError(t, err)
	require.Len(t, records, 1)
	r := records[0]
	require.Equal(t, 403, r.Status)
	require.False(t, r.Partial, r.Reason)
	require.Equal(t, "bps_upstream_403", r.ErrorCode)
	require.Equal(t, int64(1), r.APIKeyID)
	require.Equal(t, "upstream-403", r.Attempts[0].UpstreamRequestID)
	require.Len(t, r.Parts, 2)
	for _, part := range r.Parts {
		if part.Stage == "upstream_request" {
			require.Equal(t, "/basispoints/api/responses", part.URL)
		}
	}
	require.False(t, m.Stats().ManualEnabled)
	all := partsText(t, m, r)
	require.Equal(t, 2, strings.Count(all, "END-OF-PLAINTEXT"))
	require.Contains(t, all, text)
	require.Contains(t, all, "actual BPS bridge instructions")
	require.NotContains(t, all, "private-body-secret")
	require.NotContains(t, all, "private-upstream-secret")
	require.NotContains(t, all, "private-header-secret")
	encoded, err := json.Marshal(r)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "END-OF-PLAINTEXT", "database metadata must not contain plaintext payloads")
	var archive bytes.Buffer
	require.NoError(t, m.Export(context.Background(), &archive, id, ""))
	require.Positive(t, archive.Len(), "an automatic task must finalize and become exportable")
}

func TestBPS403CaptureHonorsIndependentSwitchAndQuota(t *testing.T) {
	m, _ := testManager(t)
	_, err := m.CaptureBPS403(context.Background(), Meta{}, 1, []byte(`{}`), []byte(`{}`), nil, nil)
	require.ErrorIs(t, err, ErrDisabled)
	m.ApplyConfig(Config{BPS403Enabled: true, QuotaMiB: 1, RetentionDays: 7})
	m.used.Store(1 << 20)
	_, err = m.CaptureBPS403(context.Background(), Meta{}, 1, []byte(`{}`), []byte(`{}`), nil, nil)
	require.ErrorIs(t, err, ErrCapacity)
	require.EqualValues(t, 1, m.Stats().AdmissionSkipped)
}

func TestBPS403DeferredBodyKeepsOriginalBeforeRewrite(t *testing.T) {
	ctx := WithDeferredBody(context.Background())
	body := []byte(`{"input":"original"}`)
	RememberDeferredBody(ctx, body)
	body[10] = 'X'
	RememberDeferredBody(ctx, []byte(`{"input":"changed"}`))
	require.Equal(t, `{"input":"original"}`, string(DeferredBody(ctx)))
	require.Nil(t, DeferredBody(context.Background()))
}
