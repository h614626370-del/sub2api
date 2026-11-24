package tutorial

import (
	"archive/zip"
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
	"strconv"
	"strings"
	"testing"

	apperrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const testPackageMarkdown = "---\ntitle: Quick start\nsummary: A summary\ncategory: Imported category\ncategory_sort_order: 8\nvisibility: public\npinned: true\nsort_order: -5\n---\n" +
	"## Keep **formatting**\n\n![one](<./images/a b.png> \"Original title\")\n\n![two][shot]\n\n[shot]: images/a%20b.png\n\n" +
	"```md\n![ignored](images/absent.png)\n```\n\n<img alt=\"screen\" src=\"images/a%20b.png\">\n"

func tutorialTestPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 3, 3))))
	return out.Bytes()
}

func makeTutorialTestZip(t *testing.T, markdown string, images map[string][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	if markdown != "" {
		entry, err := archive.Create("tutorial.md")
		require.NoError(t, err)
		_, err = entry.Write([]byte(markdown))
		require.NoError(t, err)
	}
	for name, raw := range images {
		entry, err := archive.Create(name)
		require.NoError(t, err)
		_, err = entry.Write(raw)
		require.NoError(t, err)
	}
	require.NoError(t, archive.Close())
	return out.Bytes()
}

func TestReadTutorialPackage(t *testing.T) {
	img := tutorialTestPNG(t)
	archive := makeTutorialTestZip(t, testPackageMarkdown, map[string][]byte{"images/a b.png": img})
	pkg, err := readTutorialPackage(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)
	require.Equal(t, "Quick start", pkg.content.Title)
	require.Equal(t, "A summary", pkg.content.Summary)
	require.Equal(t, "public", pkg.content.Visibility)
	require.True(t, pkg.content.Pinned)
	require.Equal(t, -5, pkg.content.SortOrder)
	id := pkg.images["images/a b.png"].id
	require.Equal(t, 3, strings.Count(pkg.content.Content, assetScheme+id))
	require.Contains(t, pkg.content.Content, "## Keep **formatting**")
	require.Contains(t, pkg.content.Content, `"Original title"`)
	require.Contains(t, pkg.content.Content, "![ignored](images/absent.png)")

	archive = makeTutorialTestZip(t, "# **Fallback** title\n\nBody", nil)
	pkg, err = readTutorialPackage(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)
	require.Equal(t, "Fallback title", pkg.content.Title)
	require.Equal(t, "login", pkg.content.Visibility)
}

func TestReadTutorialPackageGBKNames(t *testing.T) {
	const title = "Codex CLI \u914d\u7f6e\u6559\u7a0b"
	const imagePath = "images/\u622a\u56fe.png"
	png := tutorialTestPNG(t)
	for _, duplicate := range []bool{false, true} {
		var raw bytes.Buffer
		archive := zip.NewWriter(&raw)
		for _, file := range []struct {
			name string
			data []byte
		}{
			{title + ".md", []byte("![Preview](./" + imagePath + ")")},
			{imagePath, png},
		} {
			name, err := simplifiedchinese.GBK.NewEncoder().String(file.name)
			require.NoError(t, err)
			entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, NonUTF8: true})
			require.NoError(t, err)
			_, err = entry.Write(file.data)
			require.NoError(t, err)
		}
		if duplicate {
			entry, err := archive.Create(imagePath)
			require.NoError(t, err)
			_, err = entry.Write(png)
			require.NoError(t, err)
		}
		require.NoError(t, archive.Close())
		pkg, err := readTutorialPackage(bytes.NewReader(raw.Bytes()), int64(raw.Len()))
		if duplicate {
			require.Error(t, err)
			require.Equal(t, "TUTORIAL_PACKAGE_PATH_INVALID", apperrors.FromError(err).Reason)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, title, pkg.content.Title)
		require.Contains(t, pkg.images, imagePath)
		require.Contains(t, pkg.content.Content, assetScheme+pkg.images[imagePath].id)
	}
}

func TestTutorialPackageInvalid(t *testing.T) {
	img := tutorialTestPNG(t)
	for _, tc := range []struct {
		name, markdown, code, filename string
		images                         map[string][]byte
	}{
		{name: "missing markdown", code: "LAYOUT"},
		{name: "missing image", markdown: "![missing](./images/missing.png)", code: "IMAGE_MISSING", filename: "images/missing.png"},
		{name: "bad format", markdown: "# Example", images: map[string][]byte{"images/bad.svg": img}, code: "IMAGE_FORMAT", filename: "images/bad.svg"},
		{name: "mismatch", markdown: "# Example", images: map[string][]byte{"images/bad.jpg": img}, code: "IMAGE_FORMAT", filename: "images/bad.jpg"},
		{name: "image over limit", markdown: "# Example", images: map[string][]byte{"images/large.png": make([]byte, maxAssetBytes+1)}, code: "IMAGE_SIZE", filename: "images/large.png"},
		{name: "unexpected traversal", markdown: "# Example", images: map[string][]byte{"images/../bad.png": img}, code: "PATH_INVALID", filename: "images/../bad.png"},
		{name: "encoded traversal", markdown: "![unsafe](images/%2e%2e/bad.png)", code: "IMAGE_PATH", filename: "images/%2e%2e/bad.png"},
		{name: "invalid metadata", markdown: "---\nvisibility: [\n---\nhello", code: "METADATA"},
		{name: "invalid body", markdown: "---\ntitle: Sample\n---\n \n", code: "CONTENT_INVALID"},
		{name: "ambiguous names", markdown: "# Example", images: map[string][]byte{"images/a.png": img, "images/A.png": img}, code: "PATH_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := makeTutorialTestZip(t, tc.markdown, tc.images)
			_, err := readTutorialPackage(bytes.NewReader(raw), int64(len(raw)))
			require.Error(t, err)
			detail := apperrors.FromError(err)
			require.Equal(t, "TUTORIAL_PACKAGE_"+tc.code, detail.Reason)
			if tc.filename != "" {
				require.Equal(t, tc.filename, detail.Metadata["file"])
			}
		})
	}
}

func TestTutorialPackageLifecycle(t *testing.T) {
	db := tutorialTestDB(t)
	ctx := context.Background()
	h := &Handler{store: &Store{db: db}, assetDir: t.TempDir()}
	router := gin.New()
	router.Use(h.RedirectPublicTutorial)
	router.GET("/tutorials/:key", func(c *gin.Context) { c.Status(http.StatusOK) })
	secured := router.Group("/admin")
	secured.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
	})
	h.RegisterAdmin(secured)
	h.RegisterPublic(router.Group("/public"))

	img := tutorialTestPNG(t)
	raw := makeTutorialTestZip(t, testPackageMarkdown, map[string][]byte{"images/a b.png": img, "images/unused.png": img})
	importRequest := func(url string, raw []byte) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, err := form.CreateFormFile("file", "tutorial.zip")
		require.NoError(t, err)
		_, err = part.Write(raw)
		require.NoError(t, err)
		require.NoError(t, form.Close())
		request := httptest.NewRequest(http.MethodPost, url, &body)
		request.Header.Set("Content-Type", form.FormDataContentType())
		out := httptest.NewRecorder()
		router.ServeHTTP(out, request)
		return out
	}
	get := func(url string) *httptest.ResponseRecorder {
		t.Helper()
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest(http.MethodGet, url, nil))
		return out
	}
	importedArticle := func(out *httptest.ResponseRecorder) Article {
		t.Helper()
		require.Equal(t, http.StatusOK, out.Code, out.Body.String())
		var result struct{ Data Article }
		require.NoError(t, json.Unmarshal(out.Body.Bytes(), &result))
		return result.Data
	}
	bad := makeTutorialTestZip(t, "![missing](images/missing.png)", nil)
	out := importRequest("/admin/import", bad)
	require.Equal(t, http.StatusBadRequest, out.Code)
	require.Contains(t, out.Body.String(), `"file":"images/missing.png"`)
	files, err := os.ReadDir(h.assetDir)
	require.NoError(t, err)
	require.Empty(t, files)

	a := importedArticle(importRequest("/admin/import", raw))
	require.False(t, a.Published)
	require.NotNil(t, a.CategoryID)
	assets := imageReferences(a.Content.Content)
	require.Len(t, assets, 1)
	var oldAsset string
	for key := range assets {
		oldAsset = key
	}
	id := strconv.FormatInt(a.ID, 10)
	require.Equal(t, http.StatusNotFound, get("/public/"+id+"/assets/"+oldAsset).Code)
	files, err = os.ReadDir(h.assetDir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	out = get("/admin/" + id + "/export")
	require.Equal(t, http.StatusOK, out.Code)
	require.Equal(t, "application/zip", out.Header().Get("Content-Type"))
	require.Equal(t, `attachment; filename="tutorial-`+id+`.zip"`, out.Header().Get("Content-Disposition"))
	exported := bytes.Clone(out.Body.Bytes())
	pkg, err := readTutorialPackage(bytes.NewReader(exported), int64(len(exported)))
	require.NoError(t, err)
	require.Len(t, pkg.images, 1)
	require.Equal(t, "Imported category", pkg.metadata.Category)
	require.Equal(t, 8, pkg.metadata.CategorySortOrder)
	copy := importedArticle(importRequest("/admin/import", exported))
	require.Equal(t, a.CategoryID, copy.CategoryID)
	require.Equal(t, a.Title, copy.Title)
	require.Equal(t, a.Summary, copy.Summary)
	require.Equal(t, a.Visibility, copy.Visibility)
	require.Equal(t, a.Pinned, copy.Pinned)
	require.Equal(t, a.SortOrder, copy.SortOrder)
	require.NotEqual(t, a.Content.Content, copy.Content.Content)
	require.False(t, copy.Published)

	a.Slug = "quick-start"
	draft, err := h.store.Change(ctx, a.ID, a.Version, "save", a.Content)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, get("/public/quick-start").Code)
	require.Equal(t, http.StatusOK, get("/tutorials/"+id).Code)
	_, err = h.store.Create(ctx, 1, a.Content)
	require.ErrorIs(t, err, ErrSlugUsed)
	published, err := h.store.Change(ctx, a.ID, draft.Version, "publish", Content{})
	require.NoError(t, err)
	redirect := get("/tutorials/" + id + "?source=test")
	require.Equal(t, http.StatusMovedPermanently, redirect.Code)
	require.Equal(t, "/tutorials/quick-start?source=test", redirect.Header().Get("Location"))
	require.Equal(t, a.ID, importedArticle(get("/public/quick-start")).ID)
	require.Equal(t, http.StatusOK, get("/public/"+id+"/assets/"+oldAsset).Code)
	renamed := a.Content
	renamed.Slug = "different-address"
	_, err = h.store.Change(ctx, a.ID, published.Version, "save", renamed)
	require.ErrorIs(t, err, ErrSlugLock)
	updatedZip := makeTutorialTestZip(t, strings.ReplaceAll(strings.ReplaceAll(testPackageMarkdown, "Quick start", "Replacement"), "visibility: public", "visibility: login"), map[string][]byte{"images/a b.png": img})
	stale := importRequest("/admin/"+id+"/import?version="+strconv.FormatInt(a.Version, 10), updatedZip)
	require.Equal(t, http.StatusConflict, stale.Code)
	updated := importedArticle(importRequest("/admin/"+id+"/import?version="+strconv.FormatInt(published.Version, 10), updatedZip))
	require.True(t, updated.HasChanges)
	require.Equal(t, a.ID, updated.ID)
	require.Equal(t, "quick-start", updated.Slug)
	require.Equal(t, "Replacement", updated.Title)
	require.Equal(t, "login", updated.Visibility)
	view := importedArticle(get("/public/" + id))
	require.Equal(t, "Quick start", view.Title)
	var newAsset string
	for key := range imageReferences(updated.Content.Content) {
		newAsset = key
	}
	require.Equal(t, http.StatusOK, get("/admin/"+id+"/assets/"+newAsset).Code)
	require.Equal(t, http.StatusNotFound, get("/public/"+id+"/assets/"+newAsset).Code)
	require.Equal(t, http.StatusNotFound, get("/public/"+id+"/export").Code)
	require.Equal(t, http.StatusOK, get("/public/"+id+"/assets/"+oldAsset).Code)

	require.NoError(t, os.Remove(filepath.Join(h.assetDir, newAsset)))
	out = get("/admin/" + id + "/export")
	require.Equal(t, http.StatusBadRequest, out.Code)
	require.Contains(t, out.Body.String(), "TUTORIAL_PACKAGE_IMAGE_MISSING")
	// A second import discards the superseded draft file and keeps the live file.
	require.NoError(t, os.WriteFile(filepath.Join(h.assetDir, newAsset), img, 0600))
	finalZip := makeTutorialTestZip(t, "# Final version\n\n![new](images/new.png)", map[string][]byte{"images/new.png": img})
	updated = importedArticle(importRequest("/admin/"+id+"/import?version="+strconv.FormatInt(updated.Version, 10), finalZip))
	require.Equal(t, "login", updated.Visibility)
	require.NoFileExists(t, filepath.Join(h.assetDir, newAsset))
	require.FileExists(t, filepath.Join(h.assetDir, oldAsset))
	request := httptest.NewRequest(http.MethodPost, "/admin/"+id+"/publish", strings.NewReader(fmt.Sprintf(`{"version":%d}`, updated.Version)))
	request.Header.Set("Content-Type", "application/json")
	out = httptest.NewRecorder()
	router.ServeHTTP(out, request)
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	require.NoFileExists(t, filepath.Join(h.assetDir, oldAsset))
	require.Equal(t, http.StatusNotFound, get("/public/quick-start").Code)
	require.Equal(t, http.StatusOK, get("/tutorials/"+id).Code)
	var countForArticle int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM tutorial_assets WHERE tutorial_id=$1`, a.ID).Scan(&countForArticle))
	require.Equal(t, 1, countForArticle)

	// Force a filesystem failure after the category and draft have been written in the transaction.
	blocked := filepath.Join(h.assetDir, "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0600))
	h.assetDir = blocked
	withCategory := bytes.ReplaceAll([]byte(testPackageMarkdown), []byte("Imported category"), []byte("Must roll back"))
	failingZip := makeTutorialTestZip(t, string(withCategory), map[string][]byte{"images/a b.png": img})
	out = importRequest("/admin/import", failingZip)
	require.Equal(t, http.StatusInternalServerError, out.Code)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM tutorials WHERE id IN ($1,$2) OR draft->>'title'='Quick start'`, a.ID, copy.ID).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM tutorial_categories WHERE name='Must roll back'`).Scan(&count))
	require.Zero(t, count)
}
