// Package imagepolicy carries the internal, request-scoped image-only policy.
// It must only be restored from authenticated internal routing, never user headers.
package imagepolicy

import "context"

type directOnlyKey struct{}

const DirectRequiredCode = "image_direct_required"

func WithDirectOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, directOnlyKey{}, true)
}

func DirectOnly(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(directOnlyKey{}).(bool)
	return value
}

func IsImageEndpoint(path string) bool {
	return path == "/v1/images/generations" || path == "/v1/images/edits"
}
