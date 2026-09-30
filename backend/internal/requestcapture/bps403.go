package requestcapture

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// CaptureBPS403 creates a one-request diagnostic task after a confirmed upstream
// rejection. It does not turn on manual capture or collect successful traffic.
// The regular streaming filter, quotas, partial markers, private files, admin
// export and retention policy apply equally to these automatically saved tasks.
func (m *Manager) CaptureBPS403(ctx context.Context, meta Meta, accountID int64, original, upstream []byte, requestHeaders, responseHeaders http.Header) (string, error) {
	if m == nil {
		return "", ErrDisabled
	}
	m.adminMu.Lock()
	defer m.adminMu.Unlock()
	m.mu.Lock()
	if !m.config.BPS403Enabled || m.stopping.Load() {
		m.mu.Unlock()
		return "", ErrDisabled
	}
	if m.unhealthy.Load() || len(m.sessions) >= MaxSessions || m.used.Load() >= m.config.QuotaMiB<<20 || !m.reserve(64<<10) {
		m.admissionSkipped.Add(1)
		m.mu.Unlock()
		return "", ErrCapacity
	}
	m.mu.Unlock()
	now := time.Now().UTC()
	task := Task{ID: uuid.NewString(), InstanceID: m.instance, TargetType: "bps403", TargetID: accountID, TargetName: "BPS responses HTTP 403", CreatedAt: now, ExpiresAt: now.Add(time.Minute), Status: "running"}
	if err := m.store.SaveTask(ctx, &task); err != nil {
		m.buffer.Add(-(64 << 10))
		return "", err
	}
	m.mu.Lock()
	rt := &runtimeTask{task: task}
	rt.active.Store(true)
	m.tasks[task.ID] = rt
	if !m.config.BPS403Enabled || m.stopping.Load() {
		m.stopLocked(rt, "feature_disabled")
		m.buffer.Add(-(64 << 10))
		m.mu.Unlock()
		m.signal()
		return "", ErrDisabled
	}
	meta.RequestID = bounded(meta.RequestID, 128)
	if meta.RequestID == "" {
		meta.RequestID = uuid.NewString()
	}
	meta.ClientRequestID = bounded(meta.ClientRequestID, 128)
	meta.Path = bounded(SafeURL(meta.Path), 2048)
	meta.Method = bounded(meta.Method, 16)
	rt.refs = 1
	s := &Session{m: m, meta: meta, candidates: []*runtimeTask{rt}, matched: map[string]bool{task.ID: true}, streams: map[*Stream]struct{}{}, records: map[string]*recordState{}, finishedTargets: map[string]bool{}, usage: map[string]int64{}}
	m.sessions[s] = struct{}{}
	m.mu.Unlock()
	s.MarkError("bps_upstream_403")
	if len(original) == 0 {
		s.MarkPartial("client_input_unavailable")
	} else {
		s.ClientRequest(original, "application/json", nil)
	}
	attempt := s.BeginAttempt(accountID)
	s.AttemptResponse(attempt, http.StatusForbidden, responseHeaders, nil)
	part := s.NewStream("upstream_request", attempt, 0, "application/json", requestHeaders)
	part.part.URL = "/basispoints/api/responses"
	_, _ = part.Write(upstream)
	_ = part.Close()
	s.Finish(http.StatusForbidden)
	return task.ID, nil
}
