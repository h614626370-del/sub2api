package reauthruntime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleaseVersionPatternSupportsCustomRevision(t *testing.T) {
	for _, version := range []string{"2.9.6", "2.10.0", "2.9.7-rc.1", "2.9.6.1", "2.9.6.10", "2.9.6.1-rc.1"} {
		require.True(t, releaseVersionPattern.MatchString(version), version)
	}
	for _, version := range []string{"dev", "2.9", "2.9.6.1.2", "../../other", "2.10.0.x", "2.10.0.-1", "2.10.0.1/path"} {
		require.False(t, releaseVersionPattern.MatchString(version), version)
	}
}
