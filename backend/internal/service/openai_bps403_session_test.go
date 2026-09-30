package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func bps403TestService() (*OpenAIGatewayService, *comboCacheAndStore) {
	cache := &comboCacheAndStore{}
	s := &OpenAIGatewayService{cache: cache, settingService: &SettingService{settingRepo: &fakeSettingRepo{vals: map[string]string{SettingKeyBPS403SessionBlockEnabled: "true", SettingKeyBPS403SessionBlockTTLSeconds: "60", SettingKeyCyberSessionBlockEnabled: "true"}}}}
	return s, cache
}

func TestBPS403BlocksOnlyExactEndpointAndStatus(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		status     int
		blocked    bool
	}{
		{"responses forbidden", "/basispoints/api/responses", 403, true},
		{"attachment forbidden", "/basispoints/api/attachments", 403, false},
		{"native forbidden", "/backend-api/codex/responses", 403, false},
		{"unauthorized", "/basispoints/api/responses", 401, false},
		{"rate limited", "/basispoints/api/responses", 429, false},
		{"success", "/basispoints/api/responses", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := bps403TestService()
			s.httpUpstream = &httpUpstreamRecorder{resp: &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"basispoints_model_access_changed"}}`))}}
			c, body := newCyberBlockTestCtx(map[string]string{"session_id": "session-A"}, `{"model":"gpt-5.4","input":"plain"}`)
			c.Set("api_key", &APIKey{ID: 7, UserID: 8})
			RememberBPS403RequestBody(c, body)
			req, err := http.NewRequest(http.MethodPost, "https://bps.openai.com"+tc.path, strings.NewReader(`{}`))
			require.NoError(t, err)
			resp, err := s.doBPS403ObservedRequest(c, excelAccount(), req, "")
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			identity := ResolveCyberSessionIdentity(7, c, body)
			require.True(t, identity.Resolved())
			require.Equal(t, tc.blocked, s.FindBPS403SessionBlockedForIdentity(context.Background(), identity) != "")
			require.Empty(t, s.FindCyberSessionBlockedForIdentity(context.Background(), identity), "BPS 403 must not poison cyber policy keys")
			require.Empty(t, s.FindBPS403SessionBlockedForIdentity(context.Background(), ResolveCyberSessionIdentity(99, c, body)))
			c.Request.Header.Set("session_id", "session-B")
			require.Empty(t, s.FindBPS403SessionBlockedForIdentity(context.Background(), ResolveCyberSessionIdentity(7, c, body)))
		})
	}
}

func TestBPS403MissingIdentityAndDisabledNeverBlock(t *testing.T) {
	s, cache := bps403TestService()
	c, body := newCyberBlockTestCtx(nil, `{"input":"no session","prompt_cache_key":"not-a-session"}`)
	c.Set("api_key", &APIKey{ID: 7})
	RememberBPS403RequestBody(c, body)
	req, err := http.NewRequest(http.MethodPost, "https://bps.openai.com/basispoints/api/responses", strings.NewReader(`{}`))
	require.NoError(t, err)
	s.observeBPS403Response(c, excelAccount(), req, &http.Response{StatusCode: 403})
	require.Empty(t, cache.store.blocked)
	c.Request.Header.Set("session_id", "one")
	s.settingService.bps403RuntimeCache.Store(&bps403Runtime{expiresAt: time.Now().Add(time.Hour).UnixNano(), ttl: time.Minute})
	s.observeBPS403Response(c, excelAccount(), req, &http.Response{StatusCode: 403})
	require.Empty(t, cache.store.blocked)
}

func TestBPS403RuntimeDefaultsAndBounds(t *testing.T) {
	for _, ttl := range []string{"", "0", "-1", "604801", "9223372036854775807"} {
		s := &SettingService{settingRepo: &fakeSettingRepo{vals: map[string]string{SettingKeyBPS403SessionBlockTTLSeconds: ttl}}}
		v := s.getBPS403Runtime(context.Background())
		require.False(t, v.enabled)
		require.False(t, v.capture)
		require.Equal(t, time.Hour, v.ttl)
	}
}
