package tutorial

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

func TestImageValidation(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if mime, err := validateImage(buf.Bytes()); err != nil || mime != "image/png" {
		t.Fatalf("valid PNG rejected: %v", err)
	}
	for _, raw := range [][]byte{[]byte(`<svg onload="alert(1)"></svg>`), []byte(`<html>not an image</html>`), buf.Bytes()[:20]} {
		if _, err := validateImage(raw); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
}
func TestAssetReferences(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef"
	if !referencesAsset("![screenshot](tutorial-asset:"+key+")", key) {
		t.Fatal("missing reference")
	}
	if referencesAsset("tutorial-asset:"+key+"ab", key) {
		t.Fatal("partial reference accepted")
	}
	if assetIDPattern.MatchString("../../etc/passwd") {
		t.Fatal("path accepted")
	}
}

// The opt-in database test creates a fresh schema, independent of application data.
func TestTutorialLifecycle(t *testing.T) {
	dsn := os.Getenv("TUTORIAL_TEST_DSN")
	if dsn == "" {
		t.Skip("TUTORIAL_TEST_DSN is not configured")
	}
	root, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	schema := fmt.Sprintf("tutorial_test_%d", time.Now().UnixNano())
	if _, err = root.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := root.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); e != nil {
			t.Error(e)
		}
	}()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`CREATE TABLE users(id BIGINT PRIMARY KEY);INSERT INTO users VALUES(1)`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/383_tutorial_center.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}
	ctx := context.Background()
	category, err := s.SaveCategory(ctx, 0, Category{Name: "快速开始", SortOrder: 2})
	if err != nil {
		t.Fatal(err)
	}
	in := Content{Title: "Published title", Summary: "Published summary", Content: "## Original body", CategoryID: &category.ID}
	a, err := s.Create(ctx, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, a.ID, MemberAccess); err != ErrNotFound {
		t.Fatal("draft was readable")
	}
	list, total, err := s.List(ctx, MemberAccess, "", "", 0, 1, 20)
	if err != nil || total != 0 || len(list) != 0 {
		t.Fatal("draft leaked to list")
	}
	a, err = s.Change(ctx, a.ID, a.Version, "publish", Content{})
	if err != nil {
		t.Fatal(err)
	}
	if a.HasChanges || !a.Published {
		t.Fatal("bad publication state")
	}
	oldVersion := a.Version
	in.Title = "Secret draft title"
	in.Summary = "Private draft summary"
	in.Content = "Draft body"
	a, err = s.Change(ctx, a.ID, a.Version, "save", in)
	if err != nil {
		t.Fatal(err)
	}
	if !a.HasChanges {
		t.Fatal("unpublished changes not tracked")
	}
	public, err := s.Get(ctx, a.ID, MemberAccess)
	if err != nil {
		t.Fatal(err)
	}
	if public.Title != "Published title" || public.Content.Content != "## Original body" || public.HasChanges {
		t.Fatal("unpublished changes leaked")
	}
	_, total, err = s.List(ctx, MemberAccess, "Secret", "", 0, 1, 20)
	if err != nil || total != 0 {
		t.Fatal("draft search leaked")
	}
	list, total, err = s.List(ctx, MemberAccess, "ORIGINAL BODY", "", category.ID, 1, 20)
	if err != nil || total != 1 || len(list) != 1 || list[0].ID != a.ID || list[0].Content.Content != "" {
		t.Fatalf("published body search returned unexpected results: total=%d, err=%v", total, err)
	}
	_, total, err = s.List(ctx, MemberAccess, "Draft body", "", 0, 1, 20)
	if err != nil || total != 0 {
		t.Fatal("draft body leaked to member search")
	}
	list, total, err = s.List(ctx, AdminAccess, "Draft body", "published", 0, 1, 20)
	if err != nil || total != 1 || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("draft body did not match admin search: total=%d, err=%v", total, err)
	}
	if _, err = s.Change(ctx, a.ID, oldVersion, "save", in); err != ErrConflict {
		t.Fatal("stale editor overwrote draft")
	}
	if _, err = s.Change(ctx, a.ID, oldVersion, "unpublish", Content{}); err != ErrConflict {
		t.Fatal("stale action accepted")
	}
	if err = s.Neighbors(ctx, public, MemberAccess); err != nil || public.PreviousID != nil || public.NextID != nil {
		t.Fatalf("bad neighbors: %v", err)
	}
	// Published and draft-only images must remain distinct.
	key := "0123456789abcdef0123456789abcdef"
	in.Content = "正文关键词\n\n![image](tutorial-asset:" + key + ")"
	a, err = s.Change(ctx, a.ID, a.Version, "save", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Change(ctx, a.ID, a.Version, "publish", Content{}); err != ErrInvalid {
		t.Fatal("nonexistent image accepted")
	}
	if _, err = db.Exec(`INSERT INTO tutorial_assets(id,tutorial_id,mime,size) VALUES($1,$2,'image/png',10)`, key, a.ID); err != nil {
		t.Fatal(err)
	}
	a, err = s.Change(ctx, a.ID, a.Version, "publish", Content{})
	if err != nil {
		t.Fatal(err)
	}
	// A missing legacy visibility field must never grant anonymous access.
	if _, err = db.Exec(`UPDATE tutorials SET published=published-'visibility' WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, a.ID, PublicAccess); err != ErrNotFound {
		t.Fatal("login-only article leaked to public detail")
	}
	publicItems, publicTotal, err := s.List(ctx, PublicAccess, "", "", 0, 1, 20)
	if err != nil || publicTotal != 0 || len(publicItems) != 0 {
		t.Fatal("login-only article leaked to public list")
	}
	publicCategories, err := s.Categories(ctx, PublicAccess)
	if err != nil || len(publicCategories) != 0 {
		t.Fatal("private category leaked")
	}
	privateArticle, err := s.Create(ctx, 1, Content{Title: "Private neighbor", Content: "Login-only keyword", CategoryID: &category.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Change(ctx, privateArticle.ID, privateArticle.Version, "publish", Content{}); err != nil {
		t.Fatal(err)
	}
	in.Visibility = "public"
	a, err = s.Change(ctx, a.ID, a.Version, "save", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, a.ID, PublicAccess); err != ErrNotFound {
		t.Fatal("unpublished access change took effect")
	}
	a, err = s.Change(ctx, a.ID, a.Version, "publish", Content{})
	if err != nil {
		t.Fatal(err)
	}
	publicArticle, err := s.Get(ctx, a.ID, PublicAccess)
	if err != nil || publicArticle.Visibility != "public" {
		t.Fatal("published public article is unavailable")
	}
	if err = s.Neighbors(ctx, publicArticle, PublicAccess); err != nil || publicArticle.PreviousID != nil || publicArticle.NextID != nil {
		t.Fatal("private neighbor ID leaked")
	}
	_, publicTotal, err = s.List(ctx, PublicAccess, "Private neighbor", "", 0, 1, 20)
	if err != nil || publicTotal != 0 {
		t.Fatal("private search leaked")
	}
	_, publicTotal, err = s.List(ctx, PublicAccess, "Login-only keyword", "", 0, 1, 20)
	if err != nil || publicTotal != 0 {
		t.Fatal("login-only body leaked to public search")
	}
	publicItems, publicTotal, err = s.List(ctx, PublicAccess, "正文关键词", "", 0, 1, 20)
	if err != nil || publicTotal != 1 || len(publicItems) != 1 || publicItems[0].ID != a.ID || publicItems[0].Content.Content != "" {
		t.Fatalf("public body search returned unexpected results: total=%d, err=%v", publicTotal, err)
	}
	publicCategories, err = s.Categories(ctx, PublicAccess)
	if err != nil || len(publicCategories) != 1 || publicCategories[0].ID != category.ID {
		t.Fatal("public category is unavailable")
	}
	// Exercise the actual anonymous image handler with a local asset file.
	assetDir := t.TempDir()
	if err = os.WriteFile(assetDir+"/"+key, []byte("test image"), 0600); err != nil {
		t.Fatal(err)
	}
	h := &Handler{store: s, assetDir: assetDir}
	router := gin.New()
	h.RegisterPublic(router.Group("/public/tutorials"))
	readAsset := func() int {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", fmt.Sprintf("/public/tutorials/%d/assets/%s", a.ID, key), nil))
		if recorder.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("public image may be cached after access revocation")
		}
		return recorder.Code
	}
	if readAsset() != 200 {
		t.Fatal("public image unavailable")
	}
	in.Visibility = "login"
	a, err = s.Change(ctx, a.ID, a.Version, "save", in)
	if err != nil {
		t.Fatal(err)
	}
	if readAsset() != 200 {
		t.Fatal("draft restriction changed published access")
	}
	a, err = s.Change(ctx, a.ID, a.Version, "publish", Content{})
	if err != nil {
		t.Fatal(err)
	}
	if readAsset() != 404 {
		t.Fatal("image remained public after restriction")
	}
	if _, err = s.Get(ctx, a.ID, PublicAccess); err != ErrNotFound {
		t.Fatal("restricted article remained public")
	}
	_, publicTotal, err = s.List(ctx, PublicAccess, "正文关键词", "", 0, 1, 20)
	if err != nil || publicTotal != 0 {
		t.Fatal("restricted body remained searchable by the public")
	}
	if err = s.DeleteCategory(ctx, category.ID); err != nil {
		t.Fatal(err)
	}
	a, err = s.Get(ctx, a.ID, AdminAccess)
	if err != nil {
		t.Fatal(err)
	}
	public, err = s.Get(ctx, a.ID, MemberAccess)
	if err != nil || a.CategoryID != nil || public.CategoryID != nil {
		t.Fatal("category removal did not reclassify")
	}
	if a.HasChanges {
		t.Fatal("category-only change created spurious draft")
	}
	a, err = s.Change(ctx, a.ID, a.Version, "unpublish", Content{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, a.ID, MemberAccess); err != ErrNotFound {
		t.Fatal("unpublished article readable")
	}
	a, err = s.Change(ctx, a.ID, a.Version, "delete", Content{})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.DeletedAssets) != 1 || a.DeletedAssets[0] != key {
		t.Fatal("deleted asset cleanup missing")
	}
	var assets int
	if err = db.QueryRow(`SELECT count(*) FROM tutorial_assets`).Scan(&assets); err != nil || assets != 0 {
		t.Fatal("orphan asset metadata")
	}
}

func TestContentLimits(t *testing.T) {
	inValidVisibility := Content{Visibility: "everyone"}
	if validateContent(&inValidVisibility, false) == nil {
		t.Fatal("invalid visibility accepted")
	}
	in := Content{Title: strings.Repeat("字", 161)}
	if validateContent(&in, false) == nil {
		t.Fatal("long title accepted")
	}
	in = Content{}
	if validateContent(&in, false) != nil {
		t.Fatal("empty draft rejected")
	}
	if validateContent(&in, true) == nil {
		t.Fatal("empty publication accepted")
	}
}
