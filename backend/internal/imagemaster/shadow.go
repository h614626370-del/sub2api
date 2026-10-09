package imagemaster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imagepolicy"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// A shadow has its own billing identity. Only IDs, never credentials, are saved.
type ShadowConfig struct {
	Enabled  bool  `json:"enabled"`
	GroupID  int64 `json:"group_id"`
	APIKeyID int64 `json:"api_key_id"`
}

func ShadowDefaults() ShadowConfig { return ShadowConfig{} }
func (c ShadowConfig) Validate() error {
	if c.GroupID < 0 || c.APIKeyID < 0 ||
		(c.Enabled && (c.GroupID == 0 || c.APIKeyID == 0)) {
		return errors.New("invalid shadow settings")
	}
	return nil
}

type ShadowResult struct {
	Outcome    string `json:"outcome"`
	Code       string `json:"code,omitempty"`
	Status     int    `json:"status"`
	Images     int    `json:"images"`
	DurationMS int64  `json:"duration_ms"`
}
type ShadowRecord struct {
	ID             string       `json:"id"`
	RequestID      string       `json:"request_id"`
	StartedAt      int64        `json:"started_at"`
	UserID         int64        `json:"user_id"`
	SourceGroupID  int64        `json:"source_group_id"`
	GroupID        int64        `json:"group_id"`
	APIKeyID       int64        `json:"api_key_id"`
	RequestedModel string       `json:"requested_model"`
	Model          string       `json:"model"`
	Route          string       `json:"route"`
	Original       ShadowResult `json:"original"`
	Test           ShadowResult `json:"test"`
}
type ShadowStatus struct {
	Config       ShadowConfig   `json:"config"`
	Items        []ShadowRecord `json:"items"`
	Active       int            `json:"active"`
	LargeSkipped int64          `json:"large_skipped"`
	StorageError bool           `json:"storage_error"`
}
type shadowStore struct {
	mu             sync.Mutex
	writeMu        sync.Mutex
	path           string
	rows           map[string]*ShadowRecord
	active         int
	large          int64
	storageError   bool
	ctx            context.Context
	cancel         context.CancelFunc
	validateTarget func(context.Context, ShadowConfig) error
}

func newShadowStore(dir string) *shadowStore {
	ctx, cancel := context.WithCancel(context.Background())
	s := &shadowStore{path: filepath.Join(dir, "shadow.json"), rows: map[string]*ShadowRecord{}, ctx: ctx, cancel: cancel}
	data, err := os.ReadFile(s.path)
	if err == nil {
		var rows []ShadowRecord
		if json.Unmarshal(data, &rows) != nil {
			s.storageError = true
		} else {
			for _, r := range rows {
				if time.Since(time.UnixMilli(r.StartedAt)) >= retention {
					continue
				}
				if r.Original.Outcome == "running" {
					r.Original = ShadowResult{Outcome: "unknown", Code: "process_restarted"}
				}
				if r.Test.Outcome == "running" {
					r.Test = ShadowResult{Outcome: "unknown", Code: "process_restarted"}
				}
				row := r
				s.rows[r.ID] = &row
			}
		}
	} else if !os.IsNotExist(err) {
		s.storageError = true
	}
	s.trimHistoryLocked()
	return s
}
func (s *shadowStore) close() { s.cancel(); s.persist() }
func (s *shadowStore) snapshot() ShadowStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]ShadowRecord, 0, len(s.rows))
	for id, r := range s.rows {
		if r.Original.Outcome != "running" && r.Test.Outcome != "running" && time.Since(time.UnixMilli(r.StartedAt)) >= retention {
			delete(s.rows, id)
			continue
		}
		items = append(items, *r)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt > items[j].StartedAt })
	return ShadowStatus{Items: items, Active: s.active, LargeSkipped: s.large, StorageError: s.storageError}
}
func (s *shadowStore) persist() {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := atomicJSON(s.path, s.snapshot().Items)
	s.mu.Lock()
	s.storageError = err != nil
	s.mu.Unlock()
}
func (m *Manager) ShadowSnapshot() ShadowStatus {
	v := m.shadow.snapshot()
	v.Config = m.Config().Shadow
	return v
}

func (m *Manager) SaveShadowConfig(cfg ShadowConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.config
	next.Shadow = cfg
	if err := atomicJSON(filepath.Join(m.dir, "config.json"), next); err != nil {
		return err
	}
	m.config = next
	return nil
}

func (m *Manager) SetShadowTargetValidator(validate func(context.Context, ShadowConfig) error) {
	m.shadow.mu.Lock()
	m.shadow.validateTarget = validate
	m.shadow.mu.Unlock()
}
func (m *Manager) ValidateShadowTarget(ctx context.Context, cfg ShadowConfig) error {
	if !cfg.Enabled {
		return nil
	}
	m.shadow.mu.Lock()
	validate := m.shadow.validateTarget
	m.shadow.mu.Unlock()
	if validate == nil {
		return errors.New("shadow gateway unavailable")
	}
	return validate(ctx, cfg)
}
func (m *Manager) ShadowBodySkipped() { m.shadow.mu.Lock(); m.shadow.large++; m.shadow.mu.Unlock() }

// ShadowJob never owns the original request/context/writer. Its buffers are
// bounded and admission never waits for a worker or performs disk/network IO.
type ShadowJob struct {
	s        *shadowStore
	id       string
	start    time.Time
	original chan ShadowResult
}

const ShadowBodyLimit = 16 << 20
const ShadowObservationLimit = 64 << 20

func (m *Manager) BeginShadow(raw []byte, who Identity, sourceGroup int64, requestID string, dispatch Dispatch) *ShadowJob {
	return m.BeginShadowWithConfig(raw, who, sourceGroup, requestID, m.Config(), dispatch)
}

// Capture configuration once at admission, so concurrent settings changes
// cannot attribute an old target dispatch to a newly selected group.
func (m *Manager) BeginShadowWithConfig(raw []byte, who Identity, sourceGroup int64, requestID string, cfg Config, dispatch Dispatch) *ShadowJob {
	s := m.shadow
	if !cfg.Shadow.Enabled {
		return nil
	}
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return nil
	}
	if len(raw) > ShadowBodyLimit {
		s.large++
		s.mu.Unlock()
		return nil
	}
	id := uuid.NewString()
	start := time.Now()
	requestedModel := gjson.GetBytes(raw, "model").String()
	if !modelName.MatchString(requestedModel) {
		requestedModel = "invalid_model"
	}
	s.rows[id] = &ShadowRecord{ID: id, RequestID: requestID, StartedAt: start.UnixMilli(), UserID: who.UserID,
		SourceGroupID: sourceGroup, GroupID: cfg.Shadow.GroupID, APIKeyID: cfg.Shadow.APIKeyID,
		RequestedModel: requestedModel, Route: "responses",
		Original: ShadowResult{Outcome: "running"}, Test: ShadowResult{Outcome: "running"}}
	s.active++
	s.mu.Unlock()
	j := &ShadowJob{s: s, id: id, start: start, original: make(chan ShadowResult, 1)}
	// raw is exclusively read; the route restores the original body separately.
	go j.run(raw, cfg, dispatch)
	return j
}

func (j *ShadowJob) run(raw []byte, cfg Config, dispatch Dispatch) {
	ctx, cancel := context.WithTimeout(j.s.ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	result := ShadowResult{Outcome: "failed", Code: "shadow_execution_failed"}
	defer func() {
		if recover() != nil {
			result = ShadowResult{Outcome: "failed", Code: "shadow_execution_failed"}
		}
		result.DurationMS = time.Since(j.start).Milliseconds()
		j.s.mu.Lock()
		if r := j.s.rows[j.id]; r != nil {
			r.Test = result
		}
		j.s.mu.Unlock()
		var original ShadowResult
		select {
		case original = <-j.original:
		case <-j.s.ctx.Done():
			original = ShadowResult{Outcome: "unknown", Code: "observation_timeout", DurationMS: time.Since(j.start).Milliseconds()}
		}
		j.s.mu.Lock()
		if r := j.s.rows[j.id]; r != nil {
			r.Original = original
		}
		j.s.active--
		j.s.trimHistoryLocked()
		j.s.mu.Unlock()
		j.s.persist()
	}()
	p, err := Prepare(raw)
	if err != nil {
		_, code := errorInfo(err)
		result = ShadowResult{Outcome: "unsupported", Code: code}
		return
	}
	j.s.mu.Lock()
	r := j.s.rows[j.id]
	r.Model = p.Model
	r.Route = p.Route
	j.s.mu.Unlock()
	payload, contentType, err := p.encode(ctx, cfg.MaxBodyMiB<<20, nil)
	if err != nil {
		_, code := errorInfo(err)
		result.Code = code
		return
	}
	path := "/v1/images/generations"
	if p.Route == "images-edits" {
		path = "/v1/images/edits"
	}
	req, err := http.NewRequestWithContext(imagepolicy.WithDirectOnly(ctx), http.MethodPost, path, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Request-ID", "shadow-"+j.id)
	w := &captureWriter{header: http.Header{}, body: boundedBuffer{limit: cfg.MaxResponseMiB << 20}, cancel: cancel}
	dispatch(w, req)
	result.Status = w.status
	if ctx.Err() != nil {
		result.Code = "shadow_timeout_or_canceled"
		return
	}
	if w.err != nil {
		result.Code = "response_too_large"
		return
	}
	if w.status < 200 || w.status >= 300 {
		result.Code = "gateway_http_" + strconv.Itoa(w.status)
		return
	}
	_, count, err := responseSnapshot(w.body.Bytes(), p, j.id)
	if err != nil {
		_, result.Code = errorInfo(err)
		return
	}
	result.Outcome = "completed"
	result.Code = ""
	result.Images = count
}

// History capacity is never an admission limit. Running pairs remain visible;
// retain only the most recent 2,000 completed pairs after they finish.
func (s *shadowStore) trimHistoryLocked() {
	if len(s.rows) <= 2000 {
		return
	}
	finished := make([]*ShadowRecord, 0, len(s.rows))
	for _, r := range s.rows {
		if r.Original.Outcome != "running" && r.Test.Outcome != "running" {
			finished = append(finished, r)
		}
	}
	if len(finished) <= 2000 {
		return
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].StartedAt > finished[j].StartedAt })
	for _, r := range finished[2000:] {
		delete(s.rows, r.ID)
	}
}

// CompleteOriginal is nonblocking and receives only a bounded copy. The route
// has already delivered these exact bytes to the user's writer.
func (j *ShadowJob) CompleteOriginal(body []byte, contentType string, status int, overflow, disconnected bool) {
	duration := time.Since(j.start).Milliseconds()
	go func() {
		result := ObserveOriginal(body, contentType, status, overflow, disconnected)
		result.DurationMS = duration
		select {
		case j.original <- result:
		default:
		}
	}()
}

// A 200 or [DONE] is not proof of image generation. Require a terminal
// Responses result with a nonempty completed image_generation_call.
func ObserveOriginal(body []byte, contentType string, status int, overflow, disconnected bool) ShadowResult {
	r := ShadowResult{Outcome: "unknown", Status: status}
	if disconnected {
		r.Outcome = "disconnected"
		r.Code = "client_disconnected"
		return r
	}
	if status < 200 || status >= 300 {
		r.Outcome = "failed"
		r.Code = "http_" + strconv.Itoa(status)
		return r
	}
	if overflow {
		r.Code = "observation_limit"
		return r
	}
	completedItems := map[string]struct{}{}
	inspect := func(raw []byte) {
		if !gjson.ValidBytes(raw) {
			return
		}
		v := gjson.ParseBytes(raw)
		kind := v.Get("type").String()
		if kind == "response.output_item.done" {
			item := v.Get("item")
			if item.Get("type").String() == "image_generation_call" && item.Get("status").String() == "completed" && item.Get("result").String() != "" {
				id := item.Get("id").String()
				if id == "" {
					id = v.Get("output_index").Raw
				}
				completedItems[id] = struct{}{}
			}
			return
		}
		if kind == "error" || kind == "response.failed" || kind == "response.incomplete" {
			r.Outcome = "failed"
			r.Code = "response_failed"
			return
		}
		if kind == "response.completed" {
			v = v.Get("response")
		}
		state := v.Get("status").String()
		if state == "failed" || state == "incomplete" || (v.Get("error").Exists() && v.Get("error").Type != gjson.Null) {
			r.Outcome = "failed"
			r.Code = "response_failed"
			return
		}
		if state != "completed" {
			return
		}
		count := 0
		v.Get("output").ForEach(func(_, item gjson.Result) bool {
			if item.Get("type").String() == "image_generation_call" && item.Get("status").String() == "completed" && item.Get("result").String() != "" {
				count++
			}
			return true
		})
		if len(completedItems) > count {
			count = len(completedItems)
		}
		r.Images = count
		r.Outcome = "completed"
		r.Code = ""
		if count == 0 {
			r.Outcome = "no_image"
			r.Code = "image_missing"
		}
	}
	if bytes.Contains([]byte(contentType), []byte("text/event-stream")) {
		var event []byte
		for _, line := range bytes.Split(body, []byte("\n")) {
			line = bytes.TrimSuffix(line, []byte("\r"))
			if len(line) == 0 {
				if len(event) > 0 {
					inspect(event)
					event = event[:0]
				}
				continue
			}
			if bytes.HasPrefix(line, []byte("data:")) {
				if len(event) > 0 {
					event = append(event, '\n')
				}
				event = append(event, bytes.TrimSpace(line[5:])...)
			}
		}
		if len(event) > 0 {
			inspect(event)
		}
	} else {
		inspect(body)
	}
	return r
}
