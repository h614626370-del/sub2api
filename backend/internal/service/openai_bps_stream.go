package service

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// Count bytes read, including comments and unfinished SSE lines. A downstream
// heartbeat must never extend this deadline.
type bpsIdleBody struct {
	io.ReadCloser
	mu       sync.Mutex
	lastRead time.Time
	idle     time.Duration
	timer    *time.Timer
	stopped  bool
	timedOut bool
}

func bpsWatchUpstream(body io.ReadCloser, idle time.Duration) *bpsIdleBody {
	b := &bpsIdleBody{ReadCloser: body, idle: idle, lastRead: time.Now()}
	if idle > 0 {
		b.mu.Lock()
		b.timer = time.AfterFunc(idle, b.check)
		b.mu.Unlock()
	}
	return b
}

func (b *bpsIdleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.mu.Lock()
		b.lastRead = time.Now()
		b.mu.Unlock()
	}
	return n, err
}

func (b *bpsIdleBody) check() {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	if remaining := b.idle - time.Since(b.lastRead); remaining > 0 {
		b.timer.Reset(remaining)
		b.mu.Unlock()
		return
	}
	b.timedOut, b.stopped = true, true
	b.mu.Unlock()
	_ = b.ReadCloser.Close()
}

func (b *bpsIdleBody) stop() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopped = true
	if b.timer != nil {
		b.timer.Stop()
	}
	return b.timedOut
}

// Only the caller consumes events and writes keepalives. The reader can block
// on upstream data without blocking downstream liveness or racing Gin writes.
func readBPSEventsWithKeepalive(
	ctx context.Context,
	resp *http.Response,
	maxSize int,
	interval time.Duration,
	consume func(string, map[string]any) error,
	keepalive func() error,
) error {
	type event struct {
		typ     string
		payload map[string]any
		err     error
		done    bool
	}
	events := make(chan event, 1)
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		send := func(e event) error {
			select {
			case events <- e:
				return nil
			case <-stop:
				return context.Canceled
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		err := readBPSEvents(resp, maxSize, func(typ string, payload map[string]any) error {
			return send(event{typ: typ, payload: payload})
		})
		_ = send(event{done: true, err: err})
	}()
	defer func() {
		close(stop)
		_ = resp.Body.Close()
		<-finished
	}()

	var ticks <-chan time.Time
	if interval > 0 && keepalive != nil {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		ticks = ticker.C
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-events:
			if e.done {
				return e.err
			}
			if err := consume(e.typ, e.payload); err != nil {
				return err
			}
		case <-ticks:
			if err := keepalive(); err != nil {
				return err
			}
		}
	}
}
