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

func TestAlignAccountTimezoneOnlyMarkedContextAndSearch(t *testing.T) {
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
		require.Equal(t, gjson.Get(original, "input.0.content.1.text").String(), gjson.GetBytes(got, "input.0.content.1.text").String())
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
	a := &Account{ID: 1, Platform: PlatformOpenAI, Proxy: p}
	body := []byte(`{"tools":[{"type":"web_search"}]}`)
	m := &timezoneManagerStub{state: &AccountTimezoneState{Source: "none", HasProxy: true, ProxyFingerprint: timezoneProxyFingerprint(p)}, err: errors.New("offline")}
	s := &OpenAIGatewayService{accountTimezoneManager: m}
	require.Equal(t, body, s.applyAccountTimezone(context.Background(), a, body))
	require.Equal(t, body, s.applyAccountTimezone(context.Background(), a, body))
	require.Equal(t, 1, m.calls)
	// A saved manual override takes effect even while automatic retries back off.
	m.state = &AccountTimezoneState{Source: "manual", Timezone: "Asia/Tokyo"}
	require.Equal(t, "Asia/Tokyo", gjson.GetBytes(s.applyAccountTimezone(context.Background(), a, body), "tools.0.user_location.timezone").String())
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

func TestAlignTimezoneEnvironmentRejectsMalformedOrUnmarkedXML(t *testing.T) {
	for _, text := range []string{"<environment_context><timezone>UTC</timezone>", "<environment_context><foo>UTC</foo></environment_context>", "<other><timezone>UTC</timezone></other>"} {
		require.Equal(t, text, alignTimezoneEnvironment(text, "2026-01-01", "Asia/Tokyo"))
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
