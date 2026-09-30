package requestcapture

import (
	"bytes"
	"context"
	"sync"
)

type deferredBodyKey struct{}
type deferredBody struct {
	mu   sync.Mutex
	body []byte
}

// Keep an immutable request-scoped copy only while incident capture is enabled.
// Nothing is written to disk unless a later upstream response qualifies.
func WithDeferredBody(ctx context.Context) context.Context {
	return context.WithValue(ctx, deferredBodyKey{}, &deferredBody{})
}
func RememberDeferredBody(ctx context.Context, body []byte) {
	if holder, ok := ctx.Value(deferredBodyKey{}).(*deferredBody); ok {
		holder.mu.Lock()
		defer holder.mu.Unlock()
		if holder.body == nil {
			holder.body = bytes.Clone(body)
		}
	}
}
func DeferredBody(ctx context.Context) []byte {
	if holder, ok := ctx.Value(deferredBodyKey{}).(*deferredBody); ok {
		holder.mu.Lock()
		defer holder.mu.Unlock()
		return holder.body
	}
	return nil
}
