// Package imagemaster adapts synchronous image results to Responses clients.
// It owns transport and diagnostics only; the site's gateway owns authorization,
// account selection, upstream calls and billing.
package imagemaster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

const retention = 12 * time.Hour
const maxRecords = 10000
const maxRawBytes = 256 << 20

type Config struct {
	Shadow            ShadowConfig `json:"shadow"`
	Enabled           bool         `json:"enabled"`
	TimeoutSeconds    int          `json:"timeout_seconds"`
	HeartbeatSeconds  int          `json:"heartbeat_seconds"`
	MaxBodyMiB        int          `json:"max_body_mib"`
	MaxResponseMiB    int          `json:"max_response_mib"`
	DoneSentinel      bool         `json:"done_sentinel"`
	RawRequestLogging bool         `json:"raw_request_logging"`
}

func Defaults() Config {
	return Config{Shadow: ShadowDefaults(), TimeoutSeconds: 900, HeartbeatSeconds: 5, MaxBodyMiB: 128, MaxResponseMiB: 64}
}

var modelName = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,128}$`)

func (c Config) Validate() error {
	if err := c.Shadow.Validate(); err != nil {
		return err
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 3600 ||
		c.HeartbeatSeconds < 1 || c.HeartbeatSeconds > 60 || c.MaxBodyMiB < 1 || c.MaxBodyMiB > 128 ||
		c.MaxResponseMiB < 1 || c.MaxResponseMiB > 128 {
		return errors.New("invalid image master configuration")
	}
	return nil
}

type Stage struct {
	Name string `json:"name"`
	At   int64  `json:"at"`
}

type Record struct {
	ID               string  `json:"id"`
	StartedAt        int64   `json:"started_at"`
	FinishedAt       int64   `json:"finished_at,omitempty"`
	Outcome          string  `json:"outcome"`
	Route            string  `json:"route"`
	RequestedModel   string  `json:"requested_model"`
	Model            string  `json:"model"`
	UserID           int64   `json:"user_id"`
	APIKeyID         int64   `json:"api_key_id"`
	SourceImages     int     `json:"source_images"`
	ImageCount       int     `json:"image_count"`
	Stream           bool    `json:"stream"`
	Heartbeats       int     `json:"heartbeats"`
	DurationMS       int64   `json:"duration_ms"`
	GatewayStatus    int     `json:"gateway_status,omitempty"`
	GatewayRequestID string  `json:"gateway_request_id,omitempty"`
	ErrorCode        string  `json:"error_code,omitempty"`
	RawSaved         bool    `json:"raw_saved"`
	Stages           []Stage `json:"stages"`
}

type Status struct {
	Config       Config   `json:"config"`
	Active       int      `json:"active"`
	StorageError bool     `json:"storage_error"`
	Items        []Record `json:"items"`
}

type Manager struct {
	shadow          *shadowStore
	mu              sync.Mutex
	dir             string
	config          Config
	records         map[string]*Record
	cancels         map[string]context.CancelFunc
	storageError    bool
	rawStorageError bool
	lastPrune       time.Time
	stop            chan struct{}
	stopOnce        sync.Once
	// Persisted configuration is local to this application instance, like its
	// request recordings. No credentials or network destination are configured.
}

func New(dir string) (*Manager, error) {
	if err := os.MkdirAll(filepath.Join(dir, "raw"), 0700); err != nil {
		return nil, err
	}
	m := &Manager{dir: dir, config: Defaults(), records: map[string]*Record{}, cancels: map[string]context.CancelFunc{}, stop: make(chan struct{})}
	if b, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		if err = json.Unmarshal(b, &m.config); err != nil {
			return nil, err
		}
		if err = m.config.Validate(); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if b, err := os.ReadFile(filepath.Join(dir, "requests.json")); err == nil {
		var rows []Record
		if err = json.Unmarshal(b, &rows); err != nil {
			return nil, err
		}
		for i := range rows {
			if time.Since(time.UnixMilli(rows[i].StartedAt)) < retention {
				row := rows[i]
				m.records[row.ID] = &row
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	m.pruneLocked()
	m.shadow = newShadowStore(dir)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-ticker.C:
				m.shadow.persist()
				m.mu.Lock()
				m.pruneLocked()
				m.persistLocked()
				m.mu.Unlock()
			}
		}
	}()
	return m, nil
}

func (m *Manager) Close() {
	m.shadow.close()
	m.stopOnce.Do(func() { close(m.stop) })
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cancel := range m.cancels {
		cancel()
	}
}

func atomicJSON(path string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return atomicBytes(path, b)
}

func atomicBytes(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".image-master-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config
}

func (m *Manager) SaveConfig(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := atomicJSON(filepath.Join(m.dir, "config.json"), c); err != nil {
		return err
	}
	m.config = c
	return nil
}

// Main and shadow forms may be saved concurrently. Merge under the same lock
// as persistence so a stale main form cannot revert the independent test target.
func (m *Manager) SaveMainConfig(c Config) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.Shadow = m.config.Shadow
	if err := c.Validate(); err != nil {
		return c, err
	}
	if err := atomicJSON(filepath.Join(m.dir, "config.json"), c); err != nil {
		return c, err
	}
	m.config = c
	return c, nil
}

func (m *Manager) Snapshot() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	rows := make([]Record, 0, len(m.records))
	for _, r := range m.records {
		copy := *r
		copy.Stages = append([]Stage(nil), r.Stages...)
		rows = append(rows, copy)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].StartedAt > rows[j].StartedAt })
	return Status{Config: m.config, Active: len(m.cancels),
		StorageError: m.storageError || m.rawStorageError, Items: rows}
}

func (m *Manager) track(id, phase string, update func(*Record)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.records[id]; r != nil {
		if phase != "" {
			r.Stages = append(r.Stages, Stage{phase, time.Now().UnixMilli()})
		}
		if update != nil {
			update(r)
		}
	}
}

func (m *Manager) finish(id, outcome, code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cancels, id)
	if r := m.records[id]; r != nil {
		r.Outcome, r.ErrorCode = outcome, code
		r.FinishedAt = time.Now().UnixMilli()
		r.DurationMS = r.FinishedAt - r.StartedAt
		r.Stages = append(r.Stages, Stage{outcome, r.FinishedAt})
	}
	for len(m.records) > maxRecords && m.makeRecordRoomLocked() {
	}
	m.pruneLocked()
	m.persistLocked()
}

func (m *Manager) rejected(id string, who Identity, p *Plan, code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	if !m.makeRecordRoomLocked() {
		return
	}
	now := time.Now().UnixMilli()
	r := &Record{ID: id, StartedAt: now, FinishedAt: now, Outcome: "failed", Route: "responses",
		UserID: who.UserID, APIKeyID: who.APIKeyID, ErrorCode: code, Stages: []Stage{{"failed", now}}}
	if p != nil {
		r.Route, r.Model, r.RequestedModel, r.Stream, r.SourceImages = p.Route, p.Model, p.RequestedModel, p.Stream, len(p.images)
	}
	m.records[id] = r
	m.persistLocked()
}

func (m *Manager) makeRecordRoomLocked() bool {
	if len(m.records) < maxRecords {
		return true
	}
	var oldest *Record
	for _, row := range m.records {
		if row.FinishedAt != 0 && (oldest == nil || row.StartedAt < oldest.StartedAt) {
			oldest = row
		}
	}
	if oldest == nil {
		return false
	}
	delete(m.records, oldest.ID)
	if err := os.Remove(filepath.Join(m.dir, "raw", oldest.ID+".json")); err != nil && !os.IsNotExist(err) {
		m.rawStorageError = true
	}
	return true
}

func (m *Manager) persistLocked() {
	rows := make([]Record, 0, len(m.records))
	for _, r := range m.records {
		if r.FinishedAt != 0 {
			rows = append(rows, *r)
		}
	}
	m.storageError = atomicJSON(filepath.Join(m.dir, "requests.json"), rows) != nil
}

func (m *Manager) pruneLocked() {
	if time.Since(m.lastPrune) < time.Minute {
		return
	}
	m.lastPrune = time.Now()
	for id, r := range m.records {
		if r.FinishedAt != 0 && time.Since(time.UnixMilli(r.StartedAt)) >= retention {
			delete(m.records, id)
			if err := os.Remove(filepath.Join(m.dir, "raw", id+".json")); err != nil && !os.IsNotExist(err) {
				m.rawStorageError = true
			}
		}
	}
	// Remove expired raw data and orphaned files left by an interrupted process.
	files, err := os.ReadDir(filepath.Join(m.dir, "raw"))
	if err != nil {
		m.rawStorageError = true
		return
	}
	for _, f := range files {
		id := f.Name()
		if len(id) > 5 {
			id = id[:len(id)-5]
		}
		if m.records[id] == nil {
			if err := os.Remove(filepath.Join(m.dir, "raw", f.Name())); err != nil && !os.IsNotExist(err) {
				m.rawStorageError = true
			}
		}
	}
}

func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel := m.cancels[id]; cancel != nil {
		m.records[id].ErrorCode = "operator_cancelled"
		cancel()
		return true
	}
	return false
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id != "" && m.cancels[id] != nil {
		return errors.New("cancel the active request before deleting it")
	}
	for key, r := range m.records {
		if (id == "" || key == id) && r.FinishedAt != 0 {
			if err := os.Remove(filepath.Join(m.dir, "raw", key+".json")); err != nil && !os.IsNotExist(err) {
				m.rawStorageError = true
				return err
			}
			delete(m.records, key)
		}
	}
	m.persistLocked()
	if m.storageError {
		return errors.New("could not persist request history")
	}
	m.rawStorageError = false
	m.lastPrune = time.Time{}
	m.pruneLocked()
	if m.rawStorageError {
		return errors.New("could not remove orphaned raw requests")
	}
	return nil
}

func (m *Manager) Raw(id string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.records[id]; r == nil || !r.RawSaved {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(m.dir, "raw", id+".json"))
}

func (m *Manager) saveRaw(id string, headers map[string][]string, body []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var size int64
	files, err := os.ReadDir(filepath.Join(m.dir, "raw"))
	if err != nil {
		m.rawStorageError = true
		return
	}
	for _, f := range files {
		info, err := f.Info()
		if err != nil {
			m.rawStorageError = true
			return
		}
		size += info.Size()
	}
	data, err := encodeRawRequest(headers, body, maxRawBytes-size)
	if err != nil {
		m.rawStorageError = true
		return
	}
	err = atomicBytes(filepath.Join(m.dir, "raw", id+".json"), data)
	if err != nil {
		m.rawStorageError = true
		return
	}
	m.rawStorageError = false
	if r := m.records[id]; r != nil {
		r.RawSaved = true
	}
}

func encodeRawRequest(headers map[string][]string, body []byte, budget int64) ([]byte, error) {
	if int64(len(body)) > budget {
		return nil, errors.New("raw request storage limit")
	}
	data, err := json.Marshal(struct {
		Headers map[string][]string `json:"headers"`
		Body    string              `json:"body"`
	}{headers, string(body)})
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > budget {
		return nil, errors.New("raw request storage limit")
	}
	return data, nil
}

func (m *Manager) start(r Record, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	// History capacity must not act as a second concurrency limit. Preserve
	// active records until they finish; only the gateway controls admission.
	m.makeRecordRoomLocked()
	m.records[r.ID], m.cancels[r.ID] = &r, cancel
}

type bridgeError struct {
	status int
	code   string
}

func (e *bridgeError) Error() string     { return e.code }
func fail(status int, code string) error { return &bridgeError{status, code} }
func errorInfo(err error) (int, string) {
	var e *bridgeError
	if errors.As(err, &e) {
		return e.status, e.code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return 504, "image_master_timeout"
	}
	if errors.Is(err, context.Canceled) {
		return 499, "image_master_cancelled"
	}
	return 502, "image_master_failed"
}

func errorBody(code string) map[string]any {
	return map[string]any{"type": "image_generation_error", "code": code,
		"message": fmt.Sprintf("Image generation failed (%s)", code)}
}
