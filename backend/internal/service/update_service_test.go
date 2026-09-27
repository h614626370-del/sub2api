//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data map[string]string
}

func (s *updateServiceCacheStub) GetUpdateInfo(_ context.Context, scope string) (string, error) {
	if s.data == nil || s.data[scope] == "" {
		return "", errors.New("cache miss")
	}
	return s.data[scope], nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, scope, data string, _ time.Duration) error {
	if s.data == nil {
		s.data = make(map[string]string)
	}
	s.data[scope] = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release        *GitHubRelease
	releasesByRepo map[string]*GitHubRelease
	recentReleases []*GitHubRelease
	recentErr      error
	latestRepos    []string
	recentRepos    []string
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	s.latestRepos = append(s.latestRepos, repo)
	if s.releasesByRepo != nil {
		return s.releasesByRepo[repo], nil
	}
	return s.release, nil
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(_ context.Context, repo string, _ int) ([]*GitHubRelease, error) {
	s.recentRepos = append(s.recentRepos, repo)
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	github := &updateServiceGitHubClientStub{
		release: &GitHubRelease{
			TagName: "v0.1.132",
			Name:    "v0.1.132",
		},
	}
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		github,
		"0.1.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
	require.Equal(t, []string{customGitHubRepo}, github.latestRepos)
}

func TestUpdateServiceUsesSeparateOfficialAndCustomSources(t *testing.T) {
	cache := &updateServiceCacheStub{}
	github := &updateServiceGitHubClientStub{releasesByRepo: map[string]*GitHubRelease{
		officialGitHubRepo: {TagName: "v0.2.8", Name: "Official"},
		customGitHubRepo: {
			TagName: "v0.2.7",
			Name:    "Custom",
			Assets: []GitHubAsset{{
				Name:               "sub2api_0.2.7_linux_amd64.tar.gz",
				APIURL:             "https://api.github.com/repos/h614626370-del/sub2api/releases/assets/1",
				BrowserDownloadURL: "https://github.com/h614626370-del/sub2api/releases/download/v0.2.7/sub2api_0.2.7_linux_amd64.tar.gz",
			}},
		},
	}}
	svc := NewUpdateService(cache, github, "0.2.7", "release")

	official, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.True(t, official.HasUpdate)
	require.Equal(t, "0.2.8", official.LatestVersion)

	custom, err := svc.CheckCustomUpdate(context.Background(), false)
	require.NoError(t, err)
	require.False(t, custom.HasUpdate)
	require.Equal(t, "0.2.7", custom.LatestVersion)
	require.Equal(t, "https://api.github.com/repos/h614626370-del/sub2api/releases/assets/1", custom.ReleaseInfo.Assets[0].DownloadURL)

	require.Equal(t, []string{officialGitHubRepo, customGitHubRepo}, github.latestRepos)
	require.NotEqual(t, cache.data[officialCacheScope], cache.data[customCacheScope])
}

func TestCompareVersionsSupportsCustomRevisionSegment(t *testing.T) {
	tests := []struct {
		name           string
		current        string
		latest         string
		wantComparison int
	}{
		{name: "newer custom revision", current: "0.2.7.1", latest: "0.2.7.2", wantComparison: -1},
		{name: "older custom revision", current: "v0.2.7.3", latest: "v0.2.7.2", wantComparison: 1},
		{name: "official version wins", current: "0.2.7.99", latest: "0.2.8", wantComparison: -1},
		{name: "missing custom revision equals zero", current: "0.2.7", latest: "0.2.7.0", wantComparison: 0},
		{name: "upgrade to next official baseline zero revision", current: "v0.2.7.8", latest: "v0.2.8.0", wantComparison: -1},
		{name: "official baseline equals custom zero revision", current: "v0.2.8", latest: "v0.2.8.0", wantComparison: 0},
		{name: "upgrade from custom zero revision", current: "v0.2.8.0", latest: "v0.2.8.1", wantComparison: -1},
		{name: "multi digit custom revision", current: "0.2.7.9", latest: "0.2.7.10", wantComparison: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantComparison, compareVersions(tt.current, tt.latest))
		})
	}
}

func TestReleaseAssetDownloadURLFallsBackToBrowserURL(t *testing.T) {
	asset := GitHubAsset{BrowserDownloadURL: "https://github.com/test/repo/releases/download/v1/asset"}
	require.Equal(t, asset.BrowserDownloadURL, releaseAssetDownloadURL(asset))
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.1.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.1.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.144", versions[1].Version)
	require.Equal(t, "0.1.143", versions[2].Version)
	require.Equal(t, []string{customGitHubRepo}, svc.githubClient.(*updateServiceGitHubClientStub).recentRepos)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.145", versions[1].Version)
	require.Equal(t, "0.1.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.148"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148"},
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
		{TagName: "v0.1.144"},
		{TagName: "v0.1.143"},
		{TagName: "v0.1.142"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.1.147",  // current version
		"v0.1.147", // current version with prefix
		"0.1.148",  // newer than current
		"0.1.142",  // older than the 3 most recent
		"9.9.9",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}
