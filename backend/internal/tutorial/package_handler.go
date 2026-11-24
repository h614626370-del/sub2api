package tutorial

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

func (h *Handler) importPackage(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Authentication required")
		return
	}
	var id, version int64
	if c.Param("id") != "" {
		id, ok = idParam(c, "id")
		if !ok {
			return
		}
		version, _ = strconv.ParseInt(c.Query("version"), 10, 64)
		if version < 1 {
			response.BadRequest(c, "Version is required")
			return
		}
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPackageBytes+(1<<20))
	err := c.Request.ParseMultipartForm(1 << 20)
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			respond(c, nil, packageError("ZIP_SIZE", "Tutorial ZIP exceeds 52 MiB", ""))
		} else {
			respond(c, nil, packageError("ZIP_REQUIRED", "Upload one tutorial ZIP file", ""))
		}
		return
	}
	if c.Request.MultipartForm == nil || len(c.Request.MultipartForm.File) != 1 || len(c.Request.MultipartForm.File["file"]) != 1 {
		respond(c, nil, packageError("ZIP_REQUIRED", "Upload one tutorial ZIP file", ""))
		return
	}
	header := c.Request.MultipartForm.File["file"][0]
	file, err := header.Open()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	defer func() { _ = file.Close() }()
	pkg, err := readTutorialPackage(file, header.Size)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	article, err := h.saveImportedPackage(c.Request.Context(), pkg, subject.UserID, id, version)
	if err == nil {
		h.collectAssetGarbage()
	}
	respond(c, article, err)
}

func (h *Handler) saveImportedPackage(ctx context.Context, pkg *tutorialPackage, author, id, version int64) (*Article, error) {
	tx, err := h.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(8348201)`); err != nil {
		return nil, err
	}
	in := pkg.content
	refs := imageReferences(in.Content)
	if id != 0 {
		var current int64
		var published []byte
		var visibility string
		err = tx.QueryRowContext(ctx, `SELECT version,COALESCE(NULLIF(route_slug,''),draft->>'slug',''),published,
 COALESCE(draft->>'visibility','login') FROM tutorials WHERE id=$1 FOR UPDATE`, id).Scan(&current, &in.Slug, &published, &visibility)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if current != version {
			return nil, ErrConflict
		}
		if pkg.metadata.Visibility == "" {
			in.Visibility = visibility
		}
		if len(published) != 0 {
			var live Content
			if err := json.Unmarshal(published, &live); err != nil {
				return nil, err
			}
			for key := range imageReferences(live.Content) {
				refs[key] = true
			}
		}
		if err := queueUnusedAssets(ctx, tx, id, refs); err != nil {
			return nil, err
		}
	}
	if name := pkg.metadata.Category; name != "" {
		var categoryID int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM tutorial_categories WHERE name=$1`, name).Scan(&categoryID)
		if errors.Is(err, sql.ErrNoRows) {
			err = tx.QueryRowContext(ctx, `INSERT INTO tutorial_categories(name,sort_order) VALUES($1,$2) RETURNING id`, name, pkg.metadata.CategorySortOrder).Scan(&categoryID)
		}
		if err != nil {
			return nil, err
		}
		in.CategoryID = &categoryID
	}
	draft, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var row *sql.Row
	if id == 0 {
		row = tx.QueryRowContext(ctx, `INSERT INTO tutorials(draft,author_id) VALUES($1,$2) RETURNING `+articleColumns, string(draft), author)
	} else {
		row = tx.QueryRowContext(ctx, `UPDATE tutorials SET draft=$2,version=version+1,updated_at=NOW() WHERE id=$1 RETURNING `+articleColumns, id, string(draft))
	}
	article, err := scanArticle(row, AdminAccess)
	if err != nil {
		return nil, err
	}
	var written []string
	committed := false
	defer func() {
		if !committed {
			for _, filename := range written {
				_ = os.Remove(filename)
			}
		}
	}()
	for _, asset := range pkg.images {
		if !refs[asset.id] {
			continue
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		filename, err := h.writeAsset(asset.id, asset.data)
		if err != nil {
			return nil, err
		}
		written = append(written, filename)
		if _, err = tx.ExecContext(ctx, `INSERT INTO tutorial_assets(id,tutorial_id,mime,size) VALUES($1,$2,$3,$4)`, asset.id, article.ID, asset.mime, len(asset.data)); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return article, nil
}

func (h *Handler) exportPackage(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	data, err := h.buildTutorialPackage(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="tutorial-%d.zip"`, id))
	c.Data(http.StatusOK, "application/zip", data)
}

func (h *Handler) buildTutorialPackage(ctx context.Context, id int64) ([]byte, error) {
	tx, err := h.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Keep the draft and its files available until the archive has been built.
	article, err := scanArticle(tx.QueryRowContext(ctx, `SELECT `+articleColumns+` FROM tutorials WHERE id=$1 FOR SHARE`, id), AdminAccess)
	if err != nil {
		return nil, err
	}
	if err := validateContent(&article.Content, true); err != nil {
		return nil, packageError("CONTENT_INVALID", "Invalid tutorial title, summary, body, visibility or sort order", "")
	}
	meta := packageMetadata{
		Title: article.Title, Summary: article.Summary, Visibility: article.Visibility,
		Pinned: article.Pinned, SortOrder: article.SortOrder,
	}
	if article.CategoryID != nil {
		err = tx.QueryRowContext(ctx, `SELECT name,sort_order FROM tutorial_categories WHERE id=$1`, *article.CategoryID).Scan(&meta.Category, &meta.CategorySortOrder)
		if err != nil {
			return nil, err
		}
	}
	source := []byte(article.Content.Content)
	referenced := imageReferences(article.Content.Content)
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	if _, err := archive.Create("images/"); err != nil {
		return nil, err
	}
	exported := make(map[string]string)
	total := int64(0)
	rows, err := tx.QueryContext(ctx, `SELECT id,mime,size FROM tutorial_assets WHERE tutorial_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key, mime string
		var size int64
		if err := rows.Scan(&key, &mime, &size); err != nil {
			return nil, err
		}
		if !referenced[key] {
			continue
		}
		if !assetIDPattern.MatchString(key) {
			return nil, packageError("IMAGE_PATH", "Invalid stored tutorial asset ID", truncatePath(key))
		}
		ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif"}[mime]
		if ext == "" {
			return nil, packageError("IMAGE_FORMAT", "Image must be a PNG, JPEG or GIF", key)
		}
		if len(exported) >= maxAssetCount || size > maxAssetTotal-total {
			return nil, packageError("IMAGE_QUOTA", "Tutorial image quota exceeded (200 images / 50 MiB)", "")
		}
		name := "images/" + key + ext
		if err := addPackageImage(archive, filepath.Join(h.assetDir, key), name, size, mime); err != nil {
			return nil, err
		}
		total += size
		exported[key] = name
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	body, err := rewritePackageImages(tutorialPackageMarkdown.Parse(source), source, func(destination string) (string, error) {
		key, assetRef := strings.CutPrefix(destination, assetScheme)
		if !assetRef {
			name, err := localPackageImage(destination)
			if name != "" {
				return "", packageError("IMAGE_MISSING", "Draft references an image that is not stored as a tutorial asset", name)
			}
			return "", err
		}
		if name, ok := exported[key]; ok {
			return name, nil
		}
		if !assetIDPattern.MatchString(key) {
			return "", packageError("IMAGE_PATH", "Invalid tutorial asset reference", truncatePath(destination))
		}
		return "", packageError("IMAGE_MISSING", "Tutorial image record is missing", key)
	})
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, packageError("MARKDOWN_SIZE", "Markdown body exceeds 1 MiB after exporting image references", "")
	}
	header, err := yaml.Marshal(meta)
	if err != nil {
		return nil, err
	}
	markdown, err := archive.Create("tutorial.md")
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(markdown, "---\n%s---\n%s", header, body); err != nil {
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return data.Bytes(), tx.Commit()
}

func addPackageImage(archive *zip.Writer, filename, name string, size int64, mime string) error {
	if size <= 0 || size > maxAssetBytes {
		return packageError("IMAGE_SIZE", "Invalid stored image size or image exceeds 5 MiB", name)
	}
	file, err := os.Open(filename)
	if errors.Is(err, os.ErrNotExist) {
		return packageError("IMAGE_MISSING", "Stored tutorial image file is missing", name)
	}
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, size+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) != size {
		return packageError("IMAGE_SIZE", "Stored image size does not match its record", name)
	}
	if actual, err := validateImage(raw); err != nil || actual != mime {
		return packageError("IMAGE_FORMAT", "Invalid stored image format or dimensions", name)
	}
	entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err != nil {
		return err
	}
	_, err = entry.Write(raw)
	return err
}
