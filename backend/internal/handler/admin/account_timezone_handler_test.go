package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type timezoneAdminStub struct {
	service.AdminService
	service.AccountTimezoneManager
	called   bool
	override string
}

func (s *timezoneAdminStub) SetAccountTimezone(_ context.Context, _ int64, value string) (*service.AccountTimezoneState, error) {
	s.called, s.override = true, value
	return &service.AccountTimezoneState{Override: value}, nil
}

func TestAccountTimezoneHandlerRejectsLegacyOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		id, body string
		status   int
		called   bool
	}{
		{"1", `{}`, 400, false}, {"1", `{"override":null}`, 400, false},
		{"0", `{"override":"UTC"}`, 400, false}, {"bad", `{"override":"UTC"}`, 400, false},
		{"1", `{"override":""}`, 400, false}, {"1", `{"override":"Asia/Tokyo"}`, 400, false},
	} {
		s := &timezoneAdminStub{}
		h := &AccountHandler{adminService: s}
		r := gin.New()
		r.PUT("/:id/timezone", h.SetTimezone)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("PUT", "/"+tc.id+"/timezone", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, tc.status, w.Code, tc.body)
		require.Equal(t, tc.called, s.called, tc.body)
		require.Contains(t, w.Body.String(), "ACCOUNT_TIMEZONE_OVERRIDE_UNSUPPORTED")
	}
}
