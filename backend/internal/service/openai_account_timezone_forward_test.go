//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountTimezoneForwardHTTPAndSSE(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			s := newAstraOAuthSetup(t, passthrough)
			s.account.Extra[accountTimezoneOverrideKey] = "Europe/London"
			s.svc.settingService = NewSettingService(&timezoneSettingsRepo{&codexPolicyMigrationRepoStub{values: map[string]string{SettingKeyOpenAIOAuthDefaultTimezone: "Asia/Tokyo"}}}, s.svc.cfg)
			s.upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(codexCompletedSSE(`{"id":"resp_test","model":"gpt-5.6-sol","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)))}
			streamJSON := "false"
			if stream {
				streamJSON = "true"
			}
			body := []byte(`{"model":"gpt-5.6-sol","stream":` + streamJSON + `,"instructions":"test","input":[{"role":"user","internal_chat_message_metadata_passthrough":{"content_item_kinds":["environments.environment_context"]},"content":[{"type":"input_text","text":"<environment_context><timezone>UTC</timezone></environment_context>"}]}],"tools":[{"type":"web_search"}]}`)
			_, err := s.svc.Forward(context.Background(), s.c, s.account, body)
			require.NoError(t, err)
			require.NotNil(t, s.upstream.lastReq)
			wire, err := s.upstream.lastReq.GetBody()
			require.NoError(t, err)
			got, err := io.ReadAll(wire)
			require.NoError(t, err)
			require.NoError(t, wire.Close())
			require.Contains(t, string(got), "Asia/Tokyo")
			require.Equal(t, "Asia/Tokyo", gjson.GetBytes(got, "tools.0.user_location.timezone").String())
		}
	}
}
