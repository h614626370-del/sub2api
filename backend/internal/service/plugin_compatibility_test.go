package service

import (
	"testing"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluatePluginCompatibility(t *testing.T) {
	manifest := testPluginManifest(nil)
	host := PluginHostInfo{Version: "0.1.179", BuildType: "release"}

	result := EvaluatePluginCompatibility(manifest, host)
	require.True(t, result.Compatible)
	assert.True(t, result.Tested)
	assert.Equal(t, "compatible", result.Status)

	manifest.Requires.TestedSub2APIVersions = []string{"0.1.178"}
	result = EvaluatePluginCompatibility(manifest, host)
	require.True(t, result.Compatible)
	assert.False(t, result.Tested)
	assert.Equal(t, "untested", result.Status)

	manifest.Requires.Sub2API = ">=0.2.0 <0.3.0"
	result = EvaluatePluginCompatibility(manifest, host)
	assert.False(t, result.Compatible)
	assert.Equal(t, "incompatible", result.Status)
}

func TestEvaluatePluginCompatibilityRejectsProtocolMismatch(t *testing.T) {
	manifest := testPluginManifest(nil)
	manifest.Requires.PluginProtocol = pluginv1.ProtocolVersion + 1

	result := EvaluatePluginCompatibility(manifest, PluginHostInfo{Version: "0.1.179"})

	assert.False(t, result.Compatible)
	assert.Equal(t, "incompatible", result.Status)
}

func TestMatchesSemverRange(t *testing.T) {
	assert.True(t, matchesSemverRange("0.1.179", ">=0.1.170, <0.2.0"))
	assert.True(t, matchesSemverRange("v1.2.3", "=1.2.3"))
	assert.False(t, matchesSemverRange("0.1.169", ">=0.1.170 <0.2.0"))
	assert.False(t, matchesSemverRange("dev", ">=0.1.0"))
	assert.False(t, matchesSemverRange("0.1.179", "^0.1.0"))
}

func TestPluginCustomReleaseCompatibility(t *testing.T) {
	manifest := testPluginManifest(nil)
	manifest.Requires.Sub2API = ">=0.2.8 <0.3.0"
	manifest.Requires.TestedSub2APIVersions = []string{"0.2.8"}
	host := PluginHostInfo{Version: "0.2.8.5", BuildType: "release"}
	result := EvaluatePluginCompatibility(manifest, host)
	require.True(t, result.Compatible)
	assert.False(t, result.Tested)
	assert.Equal(t, "untested", result.Status)
	assert.Equal(t, "0.2.8.5", result.CurrentSub2API)
	manifest.Requires.TestedSub2APIVersions = []string{"0.2.8.5"}
	assert.True(t, EvaluatePluginCompatibility(manifest, host).Tested)
	manifest.Requires.TestedSub2APIVersions = []string{"0.2.8.6"}
	assert.False(t, EvaluatePluginCompatibility(manifest, host).Tested)
	manifest.Requires.PluginProtocol++
	assert.False(t, EvaluatePluginCompatibility(manifest, host).Compatible)
}

func TestCustomPluginHostVersionRanges(t *testing.T) {
	for _, tc := range []struct {
		version, expression string
		want                bool
	}{
		{"0.2.8.5", ">=0.2.8 <0.3.0", true},
		{" v0.2.8.10 ", "=0.2.8", true},
		{"0.2.8.0", ">=0.2.8", true},
		{"0.2.8.99", ">=0.2.9", false},
		{"0.3.0.1", "<0.3.0", false},
		{"0.2.8.5", ">=0.2.8.1", false},
		{"0.2.8-beta.1", ">=0.2.8", false},
		{"0.2.8+build.5", ">=0.2.8", true},
		{"0.2.8.x", ">=0.2.8", false},
		{"0.2.8.01", ">=0.2.8", false},
		{"0.2.8.-1", ">=0.2.8", false},
		{"0.2.8.1.2", ">=0.2.8", false},
		{"0.2.8.", ">=0.2.8", false},
	} {
		t.Run(tc.version+"_"+tc.expression, func(t *testing.T) {
			assert.Equal(t, tc.want, matchesSemverRange(tc.version, tc.expression))
		})
	}
	assert.Equal(t, "v0.2.8+custom.5", normalizePluginHostVersion("0.2.8.5"))
}
