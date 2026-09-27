package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAlignAccountTimezoneEnvironmentAndSearch(t *testing.T) {
	body := []byte(`{"input":[{"role":"user","internal_chat_message_metadata_passthrough":{"content_item_kinds":["environments.environment_context","user.text"]},"content":[{"type":"input_text","text":"<environment_context><current_date>2020-01-01</current_date><timezone>UTC</timezone></environment_context>"},{"type":"input_text","text":"keep 2020-01-01 UTC"}]}],"tools":[{"type":"web_search_preview"}],"message":"<timezone>UTC</timezone>"}`)
	got := alignAccountTimezone(body, "America/Los_Angeles", time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC))
	require.Contains(t, string(got), "<current_date>2026-09-21</current_date>")
	require.Contains(t, string(got), "<timezone>America/Los_Angeles</timezone>")
	require.Contains(t, string(got), "keep 2020-01-01 UTC")
	require.Contains(t, string(got), `"timezone":"America/Los_Angeles"`)
	require.Contains(t, string(got), `"message":"<timezone>UTC</timezone>"`)
}

func TestAccountTimezoneDSTAndPreservation(t *testing.T) {
	for _, tc := range []struct{ at, want string }{
		{"2026-01-22T07:30:00Z", "2026-01-21"},
		{"2026-07-22T07:30:00Z", "2026-07-22"},
	} {
		now, err := time.Parse(time.RFC3339, tc.at)
		require.NoError(t, err)
		body := []byte(`{"input":[{"role":"user","internal_chat_message_metadata_passthrough":{"content_item_kinds":["environments.environment_context","user.text"],"create_time":1789293131.82212345},"content":[{"type":"input_text","text":"<environment_context><cwd>/a&amp;b</cwd><current_date/><timezone>UTC</timezone><nested><timezone>UTC</timezone></nested></environment_context>"},{"type":"input_text","text":"<environment_context><timezone>UTC</timezone></environment_context>"}]}],"tools":[{"type":"web_search","user_location":{"type":"approximate","country":"US","city":"Boston"}},{"type":"function","name":"timezone"}]}`)
		original := string(body)
		got := alignAccountTimezone(body, "America/Los_Angeles", now)
		require.True(t, json.Valid(got))
		env := gjson.GetBytes(got, "input.0.content.0.text").String()
		require.Contains(t, env, "<current_date>"+tc.want+"</current_date>")
		require.Contains(t, env, "<nested><timezone>UTC</timezone></nested>")
		require.Contains(t, env, "<cwd>/a&amp;b</cwd>")
		require.Equal(t, "<environment_context><timezone>America/Los_Angeles</timezone></environment_context>", gjson.GetBytes(got, "input.0.content.1.text").String())
		require.Contains(t, string(got), "1789293131.82212345")
		require.Equal(t, "Boston", gjson.GetBytes(got, "tools.0.user_location.city").String())
		require.False(t, gjson.GetBytes(got, "tools.1.user_location").Exists())
		require.Equal(t, original, string(body))
	}
}

type timezoneManagerStub struct {
	AccountTimezoneManager
	state    *AccountTimezoneState
	detected *AccountTimezoneState
	err      error
	calls    int
}

func (m *timezoneManagerStub) GetAccountTimezone(context.Context, int64) (*AccountTimezoneState, error) {
	return m.state, nil
}
func (m *timezoneManagerStub) DetectAccountTimezone(context.Context, int64, bool) (*AccountTimezoneState, error) {
	m.calls++
	return m.detected, m.err
}

func TestAccountTimezoneGatewayProxyBindingAndRetry(t *testing.T) {
	p := &Proxy{Host: "old.example", Protocol: "http", Port: 80}
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Proxy: p}
	body := []byte(`{"tools":[{"type":"web_search"}]}`)
	m := &timezoneManagerStub{state: &AccountTimezoneState{Source: "none", HasProxy: true, ProxyFingerprint: timezoneProxyFingerprint(p)}, err: errors.New("offline")}
	s := &OpenAIGatewayService{accountTimezoneManager: m}
	require.Equal(t, body, s.applyAccountTimezone(context.Background(), a, body))
	require.Equal(t, body, s.applyAccountTimezone(context.Background(), a, body))
	require.Equal(t, 1, m.calls)
	// Legacy manual values no longer override the client.
	m.state = &AccountTimezoneState{Source: "manual", Timezone: "Asia/Tokyo"}
	require.Equal(t, body, s.applyAccountTimezone(context.Background(), a, body))
	// No proxy on the active connection must not inherit a newly assigned proxy.
	m.state = &AccountTimezoneState{Source: "proxy", Timezone: "Asia/Tokyo", ProxyFingerprint: timezoneProxyFingerprint(p)}
	a.Proxy = nil
	require.Equal(t, body, s.applyAccountTimezone(context.Background(), a, body))
	// The proxy can change while detection is running; validate the result again.
	a.Proxy = p
	m.state = &AccountTimezoneState{Source: "none", HasProxy: true, ProxyFingerprint: timezoneProxyFingerprint(p)}
	m.err = nil
	m.detected = &AccountTimezoneState{Source: "proxy", Timezone: "Asia/Tokyo", ProxyFingerprint: "changed"}
	s = &OpenAIGatewayService{accountTimezoneManager: m}
	require.Equal(t, body, s.applyAccountTimezone(context.Background(), a, body))
	a.Platform = PlatformAnthropic
	m.state = &AccountTimezoneState{Source: "manual", Timezone: "Asia/Tokyo"}
	require.Equal(t, body, s.applyAccountTimezone(context.Background(), a, body))
}

func TestAccountTimezoneGatewayGlobalFallbackAndProxyPriority(t *testing.T) {
	ctx := context.Background()
	p := &Proxy{Host: "proxy.example", Protocol: "http", Port: 80}
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Proxy: p}
	body := []byte(`{"tools":[{"type":"web_search","user_location":{"timezone":"UTC","city":"Boston"}}]}`)
	repo := &codexPolicyMigrationRepoStub{values: map[string]string{SettingKeyOpenAIOAuthDefaultTimezone: "Europe/London"}}
	settings := NewSettingService(repo, nil)
	fingerprint := timezoneProxyFingerprint(p)
	m := &timezoneManagerStub{
		state: &AccountTimezoneState{Source: "global", Timezone: "Europe/London", HasProxy: true, ProxyFingerprint: fingerprint},
		err:   errors.New("offline"),
	}
	s := &OpenAIGatewayService{accountTimezoneManager: m, settingService: settings}
	zone := func() string {
		return gjson.GetBytes(s.applyAccountTimezone(ctx, a, body), "tools.0.user_location.timezone").String()
	}
	require.Equal(t, "Europe/London", zone())
	require.Equal(t, "Europe/London", zone())
	require.Equal(t, 1, m.calls, "global fallback must not suppress the first detection; failures back off")
	repo.values[SettingKeyOpenAIOAuthDefaultTimezone] = "Asia/Shanghai"
	settings.invalidateOpenAIOAuthTimezoneCache()
	require.Equal(t, "Asia/Shanghai", zone(), "a changed default takes effect during backoff")
	s.accountTimezoneFailures.Store("1:"+fingerprint, time.Now().Add(-time.Second))
	m.err = nil
	m.detected = &AccountTimezoneState{Source: "proxy", Timezone: "America/Los_Angeles", HasProxy: true, ProxyFingerprint: fingerprint}
	require.Equal(t, "America/Los_Angeles", zone())
	require.Equal(t, 2, m.calls)
	m.state = m.detected
	require.Equal(t, "America/Los_Angeles", zone())
	require.Equal(t, 2, m.calls, "valid proxy results do not probe again")
	// Both proxy reassignment and removal on a long-lived connection use the default.
	a.Proxy = &Proxy{Host: "another.example", Protocol: "http", Port: 80}
	require.Equal(t, "Asia/Shanghai", zone())
	a.Proxy = nil
	require.Equal(t, "Asia/Shanghai", zone())
	m.state = &AccountTimezoneState{Source: "global", Timezone: "Asia/Shanghai"}
	require.Equal(t, "Asia/Shanghai", zone())
	require.Equal(t, 2, m.calls)
	repo.values[SettingKeyOpenAIOAuthDefaultTimezone] = ""
	settings.invalidateOpenAIOAuthTimezoneCache()
	require.Equal(t, "UTC", zone())
	// The active proxy must also match a result returned by a slow detection.
	repo.values[SettingKeyOpenAIOAuthDefaultTimezone] = "Europe/London"
	settings.invalidateOpenAIOAuthTimezoneCache()
	a.Proxy = p
	m.state = &AccountTimezoneState{Source: "global", HasProxy: true, ProxyFingerprint: fingerprint}
	m.detected = &AccountTimezoneState{Source: "proxy", Timezone: "Asia/Tokyo", ProxyFingerprint: "changed"}
	require.Equal(t, "Europe/London", zone())
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeSetupToken} {
		a.Type = kind
		require.Equal(t, body, s.applyAccountTimezone(ctx, a, body))
	}
	a.Type, a.Platform = AccountTypeOAuth, PlatformAnthropic
	require.Equal(t, body, s.applyAccountTimezone(ctx, a, body))
}

func TestAlignTimezoneEnvironmentRejectsMalformedOrUnrelatedXML(t *testing.T) {
	for _, text := range []string{"<environment_context><timezone>UTC</timezone>", "<environment_context><foo>UTC</foo></environment_context>", "<other><timezone>UTC</timezone></other>"} {
		require.Equal(t, text, alignTimezoneEnvironment(text, "2026-01-01", "Asia/Tokyo"))
	}
}

func TestAccountTimezoneGatewayEnvironmentWithoutMetadata(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	repo := &codexPolicyMigrationRepoStub{values: map[string]string{SettingKeyOpenAIOAuthDefaultTimezone: "Asia/Tokyo"}}
	s := &OpenAIGatewayService{settingService: NewSettingService(repo, nil)}
	for _, tc := range []struct {
		name, body, path string
	}{
		{"array", `{"input":[{"role":"user","content":[{"type":"input_text","text":"<environment_context><timezone>UTC</timezone></environment_context>"}]}]}`, "input.0.content.0.text"},
		{"string", `{"input":[{"role":"user","content":"<environment_context><timezone>UTC</timezone></environment_context>"}]}`, "input.0.content"},
		{"escaped", `{"input":[{"role":"user","content":"\u003cenvironment_context\u003e<timezone>UTC</timezone></environment_context>"}]}`, "input.0.content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			require.True(t, hasAccountTimezoneFields(body))
			got := s.applyAccountTimezone(context.Background(), account, body)
			require.Equal(t, "<environment_context><timezone>Asia/Tokyo</timezone></environment_context>", gjson.GetBytes(got, tc.path).String())
		})
	}
}

func TestAccountTimezonePreservesNonEnvironmentMessages(t *testing.T) {
	for _, body := range []string{
		`{"input":[{"role":"system","content":"<environment_context><timezone>UTC</timezone></environment_context>"}]}`,
		`{"input":[{"role":"assistant","content":[{"type":"input_text","text":"<environment_context><timezone>UTC</timezone></environment_context>"}]}]}`,
		`{"input":[{"role":"user","content":"Example: <environment_context><timezone>UTC</timezone></environment_context>"}]}`,
		`{"input":[{"role":"user","content":"<environment_context><timezone>UTC</timezone></environment_context> trailing text"}]}`,
		`{"input":[{"role":"user","content":"<environment_context><timezone>UTC</timezone>"}]}`,
		`{"input":[{"role":"user","content":[{"type":"input_image","text":"<environment_context><timezone>UTC</timezone></environment_context>"}]}]}`,
		`{"input":[{"role":"user","content":"<environment_context><cwd>/app</cwd></environment_context>"}]}`,
	} {
		require.Equal(t, body, string(alignAccountTimezone([]byte(body), "Asia/Tokyo", time.Now())))
	}
}

func TestAlignTimezoneEnvironmentRejectsUnsafeXML(t *testing.T) {
	for _, text := range []string{
		"<environment_context><!DOCTYPE timezone><timezone>UTC</timezone></environment_context>",
		"<environment_context><?timezone UTC?><timezone>UTC</timezone></environment_context>",
		"<environment_context><timezone>&custom;</timezone></environment_context>",
	} {
		require.Equal(t, text, alignTimezoneEnvironment(text, "2026-01-01", "Asia/Tokyo"))
	}
}
