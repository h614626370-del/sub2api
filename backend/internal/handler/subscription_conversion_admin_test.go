package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionConversionRequiresAdmin(t *testing.T) {
	h := &SubscriptionHandler{}
	for _, tc := range []struct {
		name   string
		role   string
		status int
	}{
		{"anonymous", "", http.StatusUnauthorized},
		{"user", service.RoleUser, http.StatusForbidden},
		{"observer", service.RoleObserver, http.StatusForbidden},
	} {
		for name, handle := range map[string]gin.HandlerFunc{
			"settings": h.ConversionSettings(nil),
			"save":     h.SaveConversionSettings(nil),
			"overview": h.AdminConversionOverview(nil),
			"convert":  h.AdminConvertToBalance,
		} {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"enabled":true}`))
				c.Request.Header.Set("Content-Type", "application/json")
				if tc.role != "" {
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
					c.Set(string(middleware.ContextKeyUserRole), tc.role)
				}
				handle(c)
				require.Equal(t, tc.status, w.Code)
			})
		}
	}
}

func TestAdminConversionRejectsInvalidTargetAndConfirmation(t *testing.T) {
	h := &SubscriptionHandler{}
	for _, tc := range []struct {
		name, userID, subscriptionID, body string
		overview                           bool
	}{
		{"missing user", "", "1", `{}`, false},
		{"invalid user", "abc", "1", `{}`, false},
		{"negative user", "-1", "1", `{}`, false},
		{"invalid subscription", "42", "0", `{}`, false},
		{"missing quote", "42", "1", `{}`, false},
		{"short quote", "42", "1", `{"quote":"abc"}`, false},
		{"invalid overview user", "0", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
			c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
			c.Params = gin.Params{{Key: "user_id", Value: tc.userID}, {Key: "id", Value: tc.subscriptionID}}
			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			if tc.overview {
				h.AdminConversionOverview(nil)(c)
			} else {
				h.AdminConvertToBalance(c)
			}
			require.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestAdminConversionUsesTargetUserNotOperatorOrBody(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	svc := service.NewSubscriptionService(nil, nil, nil, client, &config.Config{})
	defer svc.Stop()
	h := NewSubscriptionHandler(svc)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT value FROM settings").
		WithArgs(service.SettingKeySubscriptionConversion).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(`{"enabled":true}`))
	mock.ExpectQuery("SELECT id FROM users").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
	c.Params = gin.Params{{Key: "user_id", Value: "42"}, {Key: "id", Value: "9"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"quote":"`+strings.Repeat("a", 64)+`","user_id":7,"admin_id":99,"amount_usd":"9999"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.AdminConvertToBalance(c)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "CONVERSION_UNAVAILABLE")
	require.NoError(t, mock.ExpectationsWereMet())
}
