//go:build integration

package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type UpdateCacheSuite struct {
	IntegrationRedisSuite
	cache *updateCache
}

func (s *UpdateCacheSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.cache = NewUpdateCache(s.rdb).(*updateCache)
}

func (s *UpdateCacheSuite) TestGetUpdateInfo_Missing() {
	_, err := s.cache.GetUpdateInfo(s.ctx, "official")
	require.True(s.T(), errors.Is(err, redis.Nil), "expected redis.Nil for missing update info")
}

func (s *UpdateCacheSuite) TestSetAndGetUpdateInfo() {
	updateTTL := 5 * time.Minute
	require.NoError(s.T(), s.cache.SetUpdateInfo(s.ctx, "official", "v1.2.3", updateTTL), "SetUpdateInfo")

	info, err := s.cache.GetUpdateInfo(s.ctx, "official")
	require.NoError(s.T(), err, "GetUpdateInfo")
	require.Equal(s.T(), "v1.2.3", info, "update info mismatch")
}

func (s *UpdateCacheSuite) TestSetUpdateInfo_TTL() {
	updateTTL := 5 * time.Minute
	require.NoError(s.T(), s.cache.SetUpdateInfo(s.ctx, "official", "v1.2.3", updateTTL))

	ttl, err := s.rdb.TTL(s.ctx, updateCacheKey("official")).Result()
	require.NoError(s.T(), err, "TTL updateCacheKey")
	s.AssertTTLWithin(ttl, 1*time.Second, updateTTL)
}

func (s *UpdateCacheSuite) TestSetUpdateInfo_Overwrite() {
	require.NoError(s.T(), s.cache.SetUpdateInfo(s.ctx, "official", "v1.0.0", 5*time.Minute))
	require.NoError(s.T(), s.cache.SetUpdateInfo(s.ctx, "official", "v2.0.0", 5*time.Minute))

	info, err := s.cache.GetUpdateInfo(s.ctx, "official")
	require.NoError(s.T(), err)
	require.Equal(s.T(), "v2.0.0", info, "expected overwritten value")
}

func (s *UpdateCacheSuite) TestSetUpdateInfo_ZeroTTL() {
	// TTL=0 means persist forever (no expiry) in Redis SET command
	require.NoError(s.T(), s.cache.SetUpdateInfo(s.ctx, "official", "v0.0.0", 0))

	info, err := s.cache.GetUpdateInfo(s.ctx, "official")
	require.NoError(s.T(), err)
	require.Equal(s.T(), "v0.0.0", info)

	ttl, err := s.rdb.TTL(s.ctx, updateCacheKey("official")).Result()
	require.NoError(s.T(), err)
	// TTL=-1 means no expiry, TTL=-2 means key doesn't exist
	require.Equal(s.T(), time.Duration(-1), ttl, "expected TTL=-1 for key with no expiry")
}

func (s *UpdateCacheSuite) TestScopesAreIsolated() {
	require.NoError(s.T(), s.cache.SetUpdateInfo(s.ctx, "official", "v0.2.8", 5*time.Minute))
	require.NoError(s.T(), s.cache.SetUpdateInfo(s.ctx, "custom", "v0.2.7", 5*time.Minute))

	official, err := s.cache.GetUpdateInfo(s.ctx, "official")
	require.NoError(s.T(), err)
	custom, err := s.cache.GetUpdateInfo(s.ctx, "custom")
	require.NoError(s.T(), err)
	require.Equal(s.T(), "v0.2.8", official)
	require.Equal(s.T(), "v0.2.7", custom)
}

func TestUpdateCacheSuite(t *testing.T) {
	suite.Run(t, new(UpdateCacheSuite))
}
