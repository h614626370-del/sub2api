package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const updateCacheKeyPrefix = "update:latest:"

type updateCache struct {
	rdb *redis.Client
}

func NewUpdateCache(rdb *redis.Client) service.UpdateCache {
	return &updateCache{rdb: rdb}
}

func updateCacheKey(scope string) string {
	return updateCacheKeyPrefix + scope
}

func (c *updateCache) GetUpdateInfo(ctx context.Context, scope string) (string, error) {
	return c.rdb.Get(ctx, updateCacheKey(scope)).Result()
}

func (c *updateCache) SetUpdateInfo(ctx context.Context, scope, data string, ttl time.Duration) error {
	return c.rdb.Set(ctx, updateCacheKey(scope), data, ttl).Err()
}
