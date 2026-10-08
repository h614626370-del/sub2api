package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imagepolicy"
	"github.com/stretchr/testify/require"
)

func TestImagesDirectOnlyDoesNotFallbackOnUnavailableEndpoint(t *testing.T) {
	for _, endpoint := range []string{"/v1/images/generations", "/v1/images/edits"} {
		for _, status := range []int{404, 405} {
			t.Run(fmt.Sprintf("%s-%d", endpoint, status), func(t *testing.T) {
				calls := 0
				upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					calls++
					require.True(t, imagepolicy.DirectOnly(req.Context()))
					require.Equal(t, "/backend-api/codex"+strings.TrimPrefix(endpoint, "/v1"), req.URL.Path)
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"not supported"}}`))}, nil
				}}
				body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
				c, rec := newOpenAIImagesTestContext(t, body)
				svc := newOpenAIImagesTestService(upstream)
				parsed, err := svc.ParseOpenAIImagesRequest(c, body)
				require.NoError(t, err)
				parsed.Endpoint = endpoint
				if parsed.IsEdits() {
					parsed.InputImageURLs = []string{"data:image/png;base64,aGVsbG8="}
				}
				result, err := svc.ForwardImages(imagepolicy.WithDirectOnly(context.Background()), c, directImagesTestAccount(), body, parsed, "")
				require.Nil(t, result)
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Equal(t, OpenAIImagesDirectRequiredReason, failover.Reason)
				require.False(t, failover.RetryableOnSameAccount)
				require.True(t, failover.ShouldRetryNextAccount())
				require.Equal(t, 1, calls)
				require.Empty(t, rec.Body.String())
			})
		}
	}
}

func TestImagesDirectOnlyRejectsUnsupportedMappedModelBeforeUpstream(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		for _, channelModel := range []string{"", "gpt-image-1", "text-only-model"} {
			t.Run(accountType+channelModel, func(t *testing.T) {
				calls := 0
				upstream := &codexModelsHTTPUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
					calls++
					return nil, fmt.Errorf("must not call upstream")
				}}
				body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
				c, _ := newOpenAIImagesTestContext(t, body)
				svc := newOpenAIImagesTestService(upstream)
				parsed, err := svc.ParseOpenAIImagesRequest(c, body)
				require.NoError(t, err)
				account := directImagesTestAccount()
				account.Type = accountType
				if channelModel == "" {
					account.Credentials["model_mapping"] = map[string]any{"gpt-image-2": "gpt-image-1"}
				}
				_, err = svc.ForwardImages(imagepolicy.WithDirectOnly(context.Background()), c, account, body, parsed, channelModel)
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Equal(t, OpenAIImagesDirectRequiredReason, failover.Reason)
				require.Zero(t, calls)
			})
		}
	}
}

func TestImagesDirectOnlyRejectsInvalidAccountMapping(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	svc := newOpenAIImagesTestService(&httpUpstreamRecorder{})
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	account := directImagesTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"gpt-image-2": "text-only-model"}
	_, err = svc.ForwardImages(imagepolicy.WithDirectOnly(context.Background()), c, account, body, parsed, "")
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, OpenAIImagesDirectRequiredReason, failover.Reason)
	require.True(t, failover.ShouldRetryNextAccount())
}

func TestImagesDirectOnlyKeepsSuccessfulDirectPathAndUsage(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(imagepolicy.WithDirectOnly(context.Background()), c, directImagesTestAccount(), body, parsed, "")
	require.NoError(t, err)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 20, result.Usage.ImageOutputTokens)
	require.Equal(t, "/backend-api/codex/images/generations", upstream.lastReq.URL.Path)
}

func TestImagesDirectOnlyAPIKeyUsesImageEndpoint(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","stream":false,"n":1,"response_format":"b64_json"}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	account := &Account{ID: 6, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "synthetic-key", "base_url": "https://image-upstream.example/v1"}}
	result, err := svc.ForwardImages(imagepolicy.WithDirectOnly(context.Background()), c, account, body, parsed, "")
	require.NoError(t, err)
	require.Equal(t, 1, result.ImageCount)
	require.True(t, imagepolicy.DirectOnly(upstream.lastReq.Context()))
	require.Equal(t, "https://image-upstream.example/v1/images/generations", upstream.lastReq.URL.String())
}
