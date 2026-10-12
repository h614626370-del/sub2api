package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPaymentDashboardRejectsInvalidRangesBeforeQuery(t *testing.T) {
	for _, query := range []string{"days=367", "days=abc", "start_date=2026-01-01", "start_date=2026-02-01&end_date=2026-01-01", "start_date=2026-01-01&end_date=2026-01-31&days=30"} {
		t.Run(query, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/dashboard?"+query, nil)
			NewPaymentHandler(nil, nil).GetDashboard(c)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}
