//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompareCustomReleaseVersions(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		want            int
	}{
		{"2.9.6", "2.9.6.1", -1},
		{"v2.9.6.1", "v2.9.6.2", -1},
		{"2.9.6.10", "2.9.6.9", 1},
		{"2.9.6.1", "2.9.6", 1},
		{"2.9.6.1", "2.9.7", -1},
		{"2.9.6", "2.9.6.0", 0},
		{"0.2.7.8", "2.9.6.1", -1},
	} {
		t.Run(tc.current+"->"+tc.latest, func(t *testing.T) { require.Equal(t, tc.want, compareVersions(tc.current, tc.latest)) })
	}
}

func TestUpdateServiceCustomReleaseSourceAndRevision(t *testing.T) {
	client := &updateServiceGitHubClientStub{release: &GitHubRelease{TagName: "v2.9.6.1", Name: "Sub2API 2.9.6.1"}}
	s := NewUpdateService(&updateServiceCacheStub{}, client, "2.9.6", "release")
	info, err := s.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.Equal(t, "2.9.6.1", info.LatestVersion)
	require.Equal(t, "h614626370-del/sub2api", client.latestRepo)
}
