//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type routeSourceSettingsRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r routeSourceSettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func (r routeSourceSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := map[string]string{}
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

type routeSourceTargetReader struct{}

func (routeSourceTargetReader) GetByID(_ context.Context, id int64) (*service.Group, error) {
	return &service.Group{ID: id, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSpecial}, nil
}

type routeSourceAccountRepo struct {
	openAIImagesFailoverAccountRepo
}

func (r routeSourceAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, id int64, platform string) ([]service.Account, error) {
	var result []service.Account
	for _, account := range r.accounts {
		if account.Platform == platform {
			for _, groupID := range account.GroupIDs {
				if groupID == id {
					result = append(result, account)
					break
				}
			}
		}
	}
	return result, nil
}

func newRouteSourcesHandler(t *testing.T, upstream service.HTTPUpstream) *OpenAIGatewayHandler {
	t.Helper()
	handler := newOpenAIResponsesFailoverTestHandler(t, upstream)
	repo := routeSourceAccountRepo{openAIImagesFailoverAccountRepo{accounts: []service.Account{
		{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{10, 30}, Credentials: map[string]any{"access_token": "test-normal"}},
		{ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{20}, Credentials: map[string]any{"access_token": "test-special"}},
	}}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	settings := service.NewSettingService(routeSourceSettingsRepo{values: map[string]string{
		service.SettingKeyOpenAIAstraGroupID: "20", service.SettingKeyOpenAIAstraSourceGroupIDs: "[10]",
	}}, cfg)
	settings.SetDefaultSubscriptionGroupReader(routeSourceTargetReader{})
	// Keep the existing fixture's no-op billing dependencies, but use standard
	// scheduling so the test verifies original versus dedicated group pools.
	handler.gatewayService = service.NewOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil,
		nil, nil, upstream, nil, nil, nil, nil, nil, nil, settings, nil,
	)
	return handler
}

func TestOpenAIRouteSourcesHTTPAndStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, sourceID := range []int64{10, 30} {
		for _, stream := range []bool{false, true} {
			name := "ordinary"
			if sourceID == 10 {
				name = "selected"
			}
			if stream {
				name += "_stream"
			}
			t.Run(name, func(t *testing.T) {
				body := `{"model":"gpt-6-astra","input":"hello","stream":false}`
				response := astra200()
				if stream {
					body = `{"model":"gpt-6-astra","input":"hello","stream":true}`
					response.Header.Set("Content-Type", "text/event-stream")
					response.Body = io.NopCloser(strings.NewReader(
						"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
							"data: {\"type\":\"response.completed\",\"response\":" + astraProSuccessResponse + "}\n\n",
					))
				}
				upstream := newAstraProCapturedUpstream(response)
				handler := newRouteSourcesHandler(t, upstream)
				c, rec := newAstraProFailoverContext(t, body)
				key, ok := middleware2.GetAPIKeyFromContext(c)
				require.True(t, ok)
				key.GroupID, key.Group.ID = &sourceID, sourceID
				handler.Responses(c)
				_, accounts, _ := upstream.snapshot()
				want := int64(1)
				if sourceID == 10 {
					want = 2
				}
				require.Equal(t, []int64{want}, accounts)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Equal(t, sourceID, *key.GroupID)
				require.Equal(t, sourceID, key.Group.ID)
			})
		}
	}
}

func TestOpenAIRouteSourcesInputTokens(t *testing.T) {
	for _, native := range []bool{true, false} {
		for _, sourceID := range []int64{10, 30} {
			upstream := newAstraProCapturedUpstream(&http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"object":"response.input_tokens","input_tokens":7}`)),
			})
			handler := newRouteSourcesHandler(t, upstream)
			body := `{"model":"gpt-6-astra","input":"hello"}`
			if !native {
				body = `{"model":"gpt-6-astra","messages":[{"role":"user","content":"hello"}]}`
			}
			c, rec := newAstraProFailoverContext(t, body)
			key, ok := middleware2.GetAPIKeyFromContext(c)
			require.True(t, ok)
			key.GroupID, key.Group.ID = &sourceID, sourceID
			if native {
				c.Request.URL.Path = "/v1/responses/input_tokens"
				handler.ResponsesInputTokens(c)
			} else {
				key.Group.AllowMessagesDispatch = true
				c.Request.URL.Path = "/v1/messages/count_tokens"
				handler.CountTokens(c)
			}
			_, accounts, _ := upstream.snapshot()
			want := int64(1)
			if sourceID == 10 {
				want = 2
			}
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, []int64{want}, accounts)
		}
	}
}

func TestOpenAIRouteSourcesRetryCannotEscapePool(t *testing.T) {
	upstream := newAstraProCapturedUpstream(astra403(), astra200())
	handler := newRouteSourcesHandler(t, upstream)
	c, rec := newAstraProFailoverContext(t, astraProRequestBody)
	key, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	sourceID := int64(10)
	key.GroupID, key.Group.ID = &sourceID, sourceID
	handler.Responses(c)
	_, accounts, _ := upstream.snapshot()
	require.Equal(t, []int64{2}, accounts)
	require.NotEqual(t, http.StatusOK, rec.Code)
}
