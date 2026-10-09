package imagemaster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imagepolicy"
	"github.com/google/uuid"
)

type Identity struct{ UserID, APIKeyID int64 }

// Dispatch is an in-process gateway. It must run the normal target-model,
// group, scheduling and billing gates, and must never route back into Serve.
type Dispatch func(http.ResponseWriter, *http.Request)

type captureWriter struct {
	header http.Header
	status int
	body   boundedBuffer
	err    error
	cancel context.CancelFunc
}

func (w *captureWriter) Header() http.Header { return w.header }
func (w *captureWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *captureWriter) Write(p []byte) (int, error) {
	w.WriteHeader(200)
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.body.Write(p)
	if err != nil {
		w.err = fail(502, "gateway_response_too_large")
		w.cancel()
	}
	return n, err
}
func (w *captureWriter) Flush() { w.WriteHeader(200) }

func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

func (m *Manager) Serve(w http.ResponseWriter, req *http.Request, raw []byte, who Identity, dispatch Dispatch) {
	m.serve(w, req, raw, who, dispatch, nil)
}

func (m *Manager) serve(w http.ResponseWriter, req *http.Request, raw []byte, who Identity, dispatch Dispatch, loader imageLoader) {
	cfg := m.Config()
	id := uuid.NewString()
	w.Header().Set("X-Bridge-Request-Id", id)
	if len(raw) > cfg.MaxBodyMiB<<20 {
		m.rejected(id, who, nil, "request_too_large")
		_ = writeJSON(w, 413, map[string]any{"error": errorBody("request_too_large")})
		return
	}
	plan, err := Prepare(raw)
	if err != nil {
		status, code := errorInfo(err)
		m.rejected(id, who, nil, code)
		_ = writeJSON(w, status, map[string]any{"error": errorBody(code)})
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	start := time.Now().UnixMilli()
	m.start(Record{ID: id, StartedAt: start, Outcome: "running",
		Route: plan.Route, Model: plan.Model, RequestedModel: plan.RequestedModel, Stream: plan.Stream,
		UserID: who.UserID, APIKeyID: who.APIKeyID, SourceImages: len(plan.images), Stages: []Stage{{"received", start}}}, cancel)
	outcome, code := "failed", "image_master_failed"
	defer func() { m.finish(id, outcome, code) }()
	if cfg.RawRequestLogging {
		m.saveRaw(id, req.Header, raw)
	}
	events := &eventWriter{w: w}
	defer func() { _ = http.NewResponseController(w).SetWriteDeadline(time.Time{}) }()
	if plan.Stream {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(200)
		if _, err = fmt.Fprint(w, ": image master connected\n\n"); err != nil {
			outcome, code = "client_disconnected", "client_disconnected"
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	type result struct {
		snapshot map[string]any
		images   int
		err      error
	}
	done := make(chan result, 1)
	go func() {
		var result result
		defer func() {
			if recover() != nil {
				result.err = fail(502, "gateway_execution_failed")
			}
			done <- result
		}()
		if ctx.Err() != nil {
			result.err = ctx.Err()
			return
		}
		m.track(id, "source_preparation", nil)
		payload, contentType, err := plan.encode(ctx, cfg.MaxBodyMiB<<20, loader)
		if err != nil {
			result.err = err
			return
		}
		m.track(id, "gateway", nil)
		internal := req.Clone(imagepolicy.WithDirectOnly(ctx))
		internal.URL.Path, internal.URL.RawPath, internal.URL.RawQuery = "/v1/images/generations", "", ""
		if plan.Route == "images-edits" {
			internal.URL.Path = "/v1/images/edits"
		}
		internal.RequestURI = internal.URL.RequestURI()
		internal.Body = http.NoBody
		if len(payload) > 0 {
			internal.Body = readCloser{bytes.NewReader(payload)}
		}
		internal.GetBody = nil
		internal.ContentLength = int64(len(payload))
		internal.TransferEncoding = nil
		internal.Header.Set("Content-Type", contentType)
		internal.Header.Set("Content-Length", strconv.Itoa(len(payload)))
		internal.Header.Del("Content-Encoding")
		internal.Header.Del("Accept-Encoding")
		internal.Header.Set("Accept", "application/json")
		captured := &captureWriter{header: http.Header{}, body: boundedBuffer{limit: cfg.MaxResponseMiB << 20}, cancel: cancel}
		dispatch(captured, internal)
		m.track(id, "gateway_complete", func(r *Record) {
			r.GatewayStatus = captured.status
			r.GatewayRequestID = captured.header.Get("X-Request-ID")
			if r.GatewayRequestID == "" {
				r.GatewayRequestID = req.Header.Get("X-Request-ID")
			}
		})
		if captured.err != nil {
			result.err = captured.err
			return
		}
		if ctx.Err() != nil {
			result.err = ctx.Err()
			return
		}
		if captured.status < 200 || captured.status >= 300 {
			status := captured.status
			if status < 400 {
				status = 502
			}
			code := "gateway_http_" + strconv.Itoa(status)
			if isDirectRequiredError(captured.body.Bytes()) {
				code = imagepolicy.DirectRequiredCode
			}
			result.err = fail(status, code)
			return
		}
		if strings.Contains(captured.header.Get("Content-Type"), "text/event-stream") {
			result.err = fail(502, "unexpected_gateway_stream")
			return
		}
		result.snapshot, result.images, result.err = responseSnapshot(captured.body.Bytes(), plan, id)
	}()
	ticker := time.NewTicker(time.Duration(cfg.HeartbeatSeconds) * time.Second)
	defer ticker.Stop()
	var completed result
	waiting := true
	for waiting {
		select {
		case completed = <-done:
			waiting = false
		case <-ctx.Done():
			// Wait for the canceled gateway and downloads before returning.
			// Neither Gin context nor request bodies may outlive the handler.
			completed = <-done
			if completed.err == nil {
				completed.err = ctx.Err()
			}
			waiting = false
		case <-ticker.C:
			if plan.Stream {
				_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(15 * time.Second))
				_, err = fmt.Fprint(w, ": keep-alive\n\n")
				if err != nil {
					cancel()
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				m.track(id, "", func(r *Record) { r.Heartbeats++ })
			}
		}
	}
	if completed.err != nil {
		status, reason := errorInfo(completed.err)
		code = reason
		m.mu.Lock()
		operatorCancelled := m.records[id].ErrorCode == "operator_cancelled"
		m.mu.Unlock()
		if operatorCancelled {
			outcome, code = "canceled", "operator_cancelled"
		} else if req.Context().Err() != nil {
			outcome, code = "client_disconnected", "client_disconnected"
		}
		if plan.Stream {
			_ = events.emit("response.failed", map[string]any{"response": map[string]any{
				"id": "resp_" + id, "object": "response", "status": "failed", "output": []any{}, "error": errorBody(code)}})
			if cfg.DoneSentinel {
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}
		} else {
			_ = writeJSON(w, status, map[string]any{"error": errorBody(code)})
		}
		return
	}
	m.track(id, "delivery", func(r *Record) { r.ImageCount = completed.images })
	if deadline, ok := ctx.Deadline(); ok {
		_ = http.NewResponseController(w).SetWriteDeadline(deadline)
	}
	if plan.Stream {
		if err = events.replay(completed.snapshot); err != nil {
			outcome, code = "client_disconnected", "client_disconnected"
			return
		}
		if cfg.DoneSentinel {
			if _, err = fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
				outcome, code = "client_disconnected", "client_disconnected"
				return
			}
		}
	} else {
		if err = writeJSON(w, 200, completed.snapshot); err != nil {
			outcome, code = "client_disconnected", "client_disconnected"
			return
		}
	}
	outcome, code = completed.snapshot["status"].(string), ""
}

type readCloser struct{ *bytes.Reader }

func (readCloser) Close() error { return nil }
