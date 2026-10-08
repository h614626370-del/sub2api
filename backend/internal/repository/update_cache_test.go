package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestUpdateCacheRepositoryIsolation(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewUpdateCache(client)
	ctx := context.Background()
	require.NoError(t, client.Set(ctx, updateCacheKey, "legacy unscoped data", time.Hour).Err())
	_, err := cache.GetUpdateInfo(ctx, "ranxi2001/sub2api")
	require.ErrorIs(t, err, redis.Nil)
	require.NoError(t, cache.SetUpdateInfo(ctx, "ranxi2001/sub2api", "2.11.0", time.Minute))
	require.NoError(t, cache.SetUpdateInfo(ctx, "h614626370-del/sub2api", "2.10.0.1", time.Hour))
	upstream, err := cache.GetUpdateInfo(ctx, "ranxi2001/sub2api")
	require.NoError(t, err)
	require.Equal(t, "2.11.0", upstream)
	custom, err := cache.GetUpdateInfo(ctx, "h614626370-del/sub2api")
	require.NoError(t, err)
	require.Equal(t, "2.10.0.1", custom)
	server.FastForward(2 * time.Minute)
	_, err = cache.GetUpdateInfo(ctx, "ranxi2001/sub2api")
	require.ErrorIs(t, err, redis.Nil)
	custom, err = cache.GetUpdateInfo(ctx, "h614626370-del/sub2api")
	require.NoError(t, err)
	require.Equal(t, "2.10.0.1", custom)
}
