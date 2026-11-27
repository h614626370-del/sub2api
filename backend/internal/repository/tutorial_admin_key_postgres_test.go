//go:build unit

package repository_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/tutorial"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type tutorialKeyUsers struct{ service.UserRepository }

func (tutorialKeyUsers) GetByID(_ context.Context, id int64) (*service.User, error) {
	return &service.User{ID: id, Role: service.RoleAdmin, Status: service.StatusActive}, nil
}

func (tutorialKeyUsers) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func TestScopedAdminKeyPostgresTutorialMaintenance(t *testing.T) {
	db := repository.ScopedAdminKeyTestDB(t)
	for _, file := range []string{"383_tutorial_center.sql", "395_tutorial_routes_and_asset_cleanup.sql"} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", file))
		require.NoError(t, err)
		_, err = db.Exec(string(migration))
		require.NoError(t, err)
	}
	t.Setenv("DATA_DIR", t.TempDir())
	settings := service.NewSettingService(nil, nil)
	settings.SetScopedAdminKeyRepository(repository.NewScopedAdminKeyRepository(db))
	users := service.NewUserService(tutorialKeyUsers{}, nil, nil, nil)
	tutorials := tutorial.NewHandler(db)
	router := gin.New()
	admin := router.Group("/api/v1/admin", gin.HandlerFunc(middleware.NewAdminAuthMiddleware(nil, users, settings, nil)))
	const base = "/tutorials"
	tutorials.RegisterAdmin(admin.Group(base))
	tutorials.RegisterPublic(router.Group("/api/v1/public/tutorials"))
	for _, path := range []string{"/groups", "/settings/admin-api-keys/tutorials"} {
		admin.GET(path, func(c *gin.Context) { t.Error("tutorial key reached a forbidden handler") })
	}

	ctx := context.Background()
	_, unrelated, err := settings.CreateScopedAdminKey(ctx, service.ScopedAdminKeyInput{Name: "Unrelated key", Enabled: true}, 9)
	require.NoError(t, err)
	input := service.ScopedAdminKeyInput{Name: "Tutorial editor", Enabled: true,
		Policy: adminaccess.Policy{GlobalPermissions: []string{adminaccess.TutorialManagementPermission}}}
	key, item, err := settings.CreateScopedAdminKey(ctx, input, 9)
	require.NoError(t, err)
	keys, err := settings.ListTutorialAdminKeys(ctx)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, item.ID, keys[0].ID)
	require.ErrorIs(t, settings.RevokeTutorialAdminKey(ctx, unrelated.ID), service.ErrScopedAdminKeyNotFound)
	input.Policy.GlobalPermissions = append(input.Policy.GlobalPermissions, "users.read")
	_, err = settings.UpdateScopedAdminKey(ctx, item.ID, input)
	require.Error(t, err)

	call := func(method, path, contentType, body string, status int, data any) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if strings.HasPrefix(path, "/admin") {
			req.Header.Set("x-api-key", key)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		require.Equal(t, status, res.Code, "%s %s: %s", method, path, res.Body.String())
		if data != nil {
			require.NoError(t, json.Unmarshal(res.Body.Bytes(), &struct {
				Data any `json:"data"`
			}{Data: data}))
		}
		return res
	}
	api := func(method, path, body string, status int, data any) {
		t.Helper()
		call(method, "/admin"+base+path, "application/json", body, status, data)
	}
	api("GET", "", "", http.StatusOK, nil)
	var category tutorial.Category
	api("POST", "/categories", `{"name":"Getting started","sort_order":10}`, http.StatusOK, &category)
	categoryPath := fmt.Sprintf("/categories/%d", category.ID)
	api("PUT", categoryPath, `{"name":"Setup","sort_order":20}`, http.StatusOK, &category)
	api("GET", "/categories", "", http.StatusOK, nil)

	var article tutorial.Article
	api("POST", "", fmt.Sprintf(`{"title":"First guide","content":"Draft","category_id":%d,"visibility":"public"}`, category.ID), http.StatusOK, &article)
	path := fmt.Sprintf("/%d", article.ID)
	publicPath := "/public/tutorials" + path
	call("GET", publicPath, "", "", http.StatusNotFound, nil)

	var imageBytes, formBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	form := multipart.NewWriter(&formBytes)
	file, err := form.CreateFormFile("file", "setup.png")
	require.NoError(t, err)
	_, err = file.Write(imageBytes.Bytes())
	require.NoError(t, err)
	require.NoError(t, form.Close())
	var asset struct{ ID, Reference string }
	call("POST", "/admin"+base+path+"/assets", form.FormDataContentType(), formBytes.String(), http.StatusOK, &asset)
	require.NotEmpty(t, asset.Reference)
	api("GET", path+"/assets/"+asset.ID, "", http.StatusOK, nil)

	update := fmt.Sprintf(`{"title":"First guide","content":"![Setup](%s)","category_id":%d,"visibility":"public","version":%d}`, asset.Reference, category.ID, article.Version)
	api("PUT", path, update, http.StatusOK, &article)
	api("PUT", path, update, http.StatusConflict, nil)
	api("GET", path, "", http.StatusOK, &article)
	api("POST", path+"/publish", fmt.Sprintf(`{"version":%d}`, article.Version), http.StatusOK, &article)
	call("GET", publicPath, "", "", http.StatusOK, nil)
	call("GET", publicPath+"/assets/"+asset.ID, "", "", http.StatusOK, nil)
	api("POST", path+"/unpublish", fmt.Sprintf(`{"version":%d}`, article.Version), http.StatusOK, &article)
	call("GET", publicPath, "", "", http.StatusNotFound, nil)
	api("DELETE", path, fmt.Sprintf(`{"version":%d}`, article.Version), http.StatusOK, nil)
	api("DELETE", categoryPath, "", http.StatusOK, nil)
	for _, path := range []string{"/groups", "/settings/admin-api-keys/tutorials"} {
		call("GET", "/admin"+path, "", "", http.StatusForbidden, nil)
	}
	require.NoError(t, settings.RevokeTutorialAdminKey(ctx, item.ID))
	api("GET", "", "", http.StatusUnauthorized, nil)
}
