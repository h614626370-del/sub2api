package handler

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBPSFailureDoesNotAppendFallback(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/responses", nil)
			if stream {
				c.Header("Content-Type", "text/event-stream")
				_, _ = c.Writer.WriteString(": heartbeat\n\n")
			}
			before := c.Writer.Size()
			err := errors.New("BPS failure")
			service.WriteOpenAIBPSError(c, err)
			original := rec.Body.String()
			require.True(t, openAIForwardErrorAlreadyCommunicated(c, before, err))
			h := &OpenAIGatewayHandler{}
			require.False(t, h.ensureForwardErrorResponse(c, stream))
			service.WriteOpenAIBPSError(c, err)
			require.Equal(t, original, rec.Body.String())
			if stream {
				require.Contains(t, original, "event: response.failed")
			} else {
				require.JSONEq(t, `{"error":{"type":"server_error","code":"bps_upstream_error","message":"BPS upstream request failed"}}`, original)
			}
		})
	}
}
