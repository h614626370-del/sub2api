//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompareReleaseVersionsSupportsCustomRevision(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		want            int
	}{
		{"2.9.6", "2.9.7", -1},
		{"v2.9.6", "v2.10.0", -1},
		{"2.10.0", "2.9.9", 1},
		{"2.9.6", "2.9.6", 0},
		{"2.9.6", "2.9.6.1", -1},
		{"2.9.6.10", "2.9.6.9", 1},
		{"2.10.0", "2.10.0.0", 0},
		{"2.10.0.0", "v2.10.0", 0},
		{"2.10.0.9", "2.10.0.10", -1},
		{"v2.10.0.1", "v2.10.0", 1},
		{"2.10.0.99", "2.10.1", -1},
		{"2.10.0.1-rc.1", "2.10.0.2", -1},
		{"0.2.7.8", "2.9.6", -1},
	} {
		t.Run(tc.current+"->"+tc.latest, func(t *testing.T) { require.Equal(t, tc.want, compareVersions(tc.current, tc.latest)) })
	}
}

func TestUpdateServiceDualSourcesKeepCachesIsolated(t *testing.T) {
	ctx := context.Background()
	cache := &updateServiceCacheStub{}
	client := &updateServiceGitHubClientStub{releasesByRepo: map[string]*GitHubRelease{
		upstreamGitHubRepo: {TagName: "v2.11.0", HTMLURL: "https://github.com/ranxi2001/sub2api/releases/tag/v2.11.0", Assets: []GitHubAsset{{Name: "upstream.tar.gz"}}},
		githubRepo:         {TagName: "v2.10.0.2", HTMLURL: "https://github.com/h614626370-del/sub2api/releases/tag/v2.10.0.2", Assets: []GitHubAsset{{Name: "custom.tar.gz"}}},
	}}
	s := NewUpdateService(cache, client, "2.10.0.1", "release")
	upstream, err := s.CheckUpstreamUpdate(ctx, false)
	require.NoError(t, err)
	custom, err := s.CheckUpdate(ctx, false)
	require.NoError(t, err)
	require.True(t, upstream.HasUpdate)
	require.True(t, custom.HasUpdate)
	require.Equal(t, "2.11.0", upstream.LatestVersion)
	require.Equal(t, "2.10.0.2", custom.LatestVersion)
	require.Len(t, cache.data, 2)
	upstreamCached, err := s.CheckUpstreamUpdate(ctx, false)
	require.NoError(t, err)
	customCached, err := s.CheckUpdate(ctx, false)
	require.NoError(t, err)
	require.True(t, upstreamCached.Cached)
	require.True(t, customCached.Cached)
	require.Equal(t, upstream.ReleaseInfo, upstreamCached.ReleaseInfo)
	require.Equal(t, custom.ReleaseInfo, customCached.ReleaseInfo)
	require.Len(t, client.latestCalls, 2)

	client.errorsByRepo = map[string]error{upstreamGitHubRepo: errors.New("upstream unavailable")}
	stale, err := s.CheckUpstreamUpdate(ctx, true)
	require.NoError(t, err)
	require.Contains(t, stale.Warning, "upstream unavailable")
	require.Equal(t, "2.11.0", stale.LatestVersion)
	custom, err = s.CheckUpdate(ctx, true)
	require.NoError(t, err)
	require.Empty(t, custom.Warning)
	require.Equal(t, "2.10.0.2", custom.LatestVersion)

	client.errorsByRepo[githubRepo] = errors.New("custom unavailable")
	require.ErrorContains(t, s.PerformUpdate(ctx), "could not verify the latest custom release")
}

func TestUpdateServiceSourceFailureDoesNotReuseOtherRepository(t *testing.T) {
	ctx := context.Background()
	client := &updateServiceGitHubClientStub{
		releasesByRepo: map[string]*GitHubRelease{upstreamGitHubRepo: {TagName: "v9.0.0"}},
		errorsByRepo:   map[string]error{githubRepo: errors.New("custom unavailable")},
	}
	s := NewUpdateService(&updateServiceCacheStub{}, client, "2.10.0.1", "release")
	_, err := s.CheckUpstreamUpdate(ctx, false)
	require.NoError(t, err)
	custom, err := s.CheckUpdate(ctx, false)
	require.NoError(t, err)
	require.False(t, custom.HasUpdate)
	require.Nil(t, custom.ReleaseInfo)
	require.Contains(t, custom.Warning, "custom unavailable")
	require.Equal(t, "2.10.0.1", custom.LatestVersion)
}

func TestUpdateServiceUpstreamSameBaseDoesNotSuggestDowngrade(t *testing.T) {
	client := &updateServiceGitHubClientStub{release: &GitHubRelease{TagName: "v2.10.0"}}
	s := NewUpdateService(&updateServiceCacheStub{}, client, "2.10.0.3", "release")
	info, err := s.CheckUpstreamUpdate(context.Background(), true)
	require.NoError(t, err)
	require.False(t, info.HasUpdate)
	require.Equal(t, upstreamGitHubRepo, client.latestRepo)
}

func TestUpdateServiceFourPartRollbackUsesCustomRepository(t *testing.T) {
	client := &updateServiceGitHubClientStub{recentReleases: []*GitHubRelease{
		{TagName: "v2.10.0.10"}, {TagName: "v2.10.0.9"}, {TagName: "v2.10.0.2"},
		{TagName: "v2.10.0"}, {TagName: "v2.10.1"}, {TagName: "v2.10.0.8-rc.1", Prerelease: true},
	}}
	s := NewUpdateService(&updateServiceCacheStub{}, client, "2.10.0.10", "release")
	versions, err := s.ListRollbackVersions(context.Background())
	require.NoError(t, err)
	require.Equal(t, githubRepo, client.recentRepo)
	require.Len(t, versions, 3)
	require.Equal(t, []string{"2.10.0.9", "2.10.0.2", "2.10.0"},
		[]string{versions[0].Version, versions[1].Version, versions[2].Version})
	require.ErrorIs(t, s.RollbackToVersion(context.Background(), "2.10.0.10"), ErrRollbackVersionNotAllowed)
	require.ErrorContains(t, s.RollbackToVersion(context.Background(), "v2.10.0.9"), "no compatible release")
}

func TestUpdateServiceRejectsMissingOrUnstableRelease(t *testing.T) {
	for _, release := range []*GitHubRelease{nil, {}, {TagName: "v2.11.0", Draft: true}, {TagName: "v2.11.0-rc.1", Prerelease: true}} {
		s := NewUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{release: release}, "2.10.0", "release")
		info, err := s.CheckUpdate(context.Background(), true)
		require.NoError(t, err)
		require.NotEmpty(t, info.Warning)
		require.False(t, info.HasUpdate)
	}
}

func TestUpdateServiceCustomReleaseSource(t *testing.T) {
	client := &updateServiceGitHubClientStub{release: &GitHubRelease{TagName: "v2.9.7", Name: "Sub2API 2.9.7"}}
	s := NewUpdateService(&updateServiceCacheStub{}, client, "2.9.6", "release")
	info, err := s.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.Equal(t, "2.9.7", info.LatestVersion)
	require.Equal(t, "h614626370-del/sub2api", client.latestRepo)
}
