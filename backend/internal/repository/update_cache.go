package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const updateCacheKey = "update:latest"

type updateCache struct {
	rdb *redis.Client
}

func NewUpdateCache(rdb *redis.Client) service.UpdateCache {
	return &updateCache{rdb: rdb}
}

func (c *updateCache) GetUpdateInfo(ctx context.Context, repo string) (string, error) {
	return c.rdb.Get(ctx, updateCacheKey+":"+repo).Result()
}

func (c *updateCache) SetUpdateInfo(ctx context.Context, repo, data string, ttl time.Duration) error {
	return c.rdb.Set(ctx, updateCacheKey+":"+repo, data, ttl).Err()
}
