package admin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBPS403Settings(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	r := doUpdateSettings(t, h, map[string]any{"site_name": "defaults"}, nil)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.False(t, gjson.Get(r.Body.String(), "data.bps403_session_block_enabled").Bool())
	require.False(t, gjson.Get(r.Body.String(), "data.bps403_capture_enabled").Bool())
	require.EqualValues(t, 3600, gjson.Get(r.Body.String(), "data.bps403_session_block_ttl_seconds").Int())
	for _, ttl := range []any{0, -1, 604801, 1.5, "invalid"} {
		r = doUpdateSettings(t, h, map[string]any{"bps403_session_block_ttl_seconds": ttl}, nil)
		require.Equal(t, 400, r.Code, r.Body.String())
	}
	r = doUpdateSettings(t, h, map[string]any{"bps403_session_block_enabled": true, "bps403_capture_enabled": true, "bps403_session_block_ttl_seconds": 120}, nil)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.Equal(t, "120", repo.values[service.SettingKeyBPS403SessionBlockTTLSeconds])
	r = doUpdateSettings(t, h, map[string]any{"site_name": "preserve BPS settings"}, nil)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.True(t, gjson.Get(r.Body.String(), "data.bps403_session_block_enabled").Bool())
	require.True(t, gjson.Get(r.Body.String(), "data.bps403_capture_enabled").Bool())
	require.EqualValues(t, 120, gjson.Get(r.Body.String(), "data.bps403_session_block_ttl_seconds").Int())
	require.False(t, gjson.Get(r.Body.String(), "data.request_capture_enabled").Bool())
	r = doUpdateSettings(t, h, map[string]any{"bps403_session_block_enabled": false}, nil)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.False(t, gjson.Get(r.Body.String(), "data.bps403_session_block_enabled").Bool())
	require.True(t, gjson.Get(r.Body.String(), "data.bps403_capture_enabled").Bool())
	public, err := h.settingService.GetPublicSettings(context.Background())
	require.NoError(t, err)
	raw, err := json.Marshal(public)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "bps403")
}
