package tutorial

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/lib/pq"
)

func imageReferences(markdown string) map[string]bool {
	refs := make(map[string]bool)
	source := []byte(markdown)
	_, _ = rewritePackageImages(tutorialPackageMarkdown.Parse(source), source, func(destination string) (string, error) {
		if len(destination) == len(assetScheme)+32 && destination[:len(assetScheme)] == assetScheme {
			if key := destination[len(assetScheme):]; assetIDPattern.MatchString(key) {
				refs[key] = true
			}
		}
		return "", nil
	})
	return refs
}

func queueUnusedAssets(ctx context.Context, tx *sql.Tx, id int64, keep map[string]bool) error {
	keys := make([]string, 0, len(keep))
	for key := range keep {
		keys = append(keys, key)
	}
	_, err := tx.ExecContext(ctx, `WITH removed AS (
 DELETE FROM tutorial_assets WHERE tutorial_id=$1 AND NOT (id=ANY($2::varchar[])) RETURNING id
) INSERT INTO tutorial_asset_garbage(id) SELECT id FROM removed ON CONFLICT DO NOTHING`, id, pq.Array(keys))
	return err
}

// File removal follows the database commit. The queue also covers interrupted or failed removals.
func (h *Handler) collectAssetGarbage() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := h.store.db.QueryContext(ctx, `SELECT id FROM tutorial_asset_garbage`)
	if err != nil {
		log.Printf("Tutorial asset cleanup: %v", err)
		return
	}
	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			break
		}
		keys = append(keys, key)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		log.Printf("Tutorial asset cleanup: %v", err)
		return
	}
	removed := make([]string, 0, len(keys))
	for _, key := range keys {
		if !assetIDPattern.MatchString(key) {
			continue
		}
		if err := os.Remove(filepath.Join(h.assetDir, key)); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("Could not remove tutorial image %s: %v", key, err)
			continue
		}
		removed = append(removed, key)
	}
	if len(removed) != 0 {
		if _, err := h.store.db.ExecContext(ctx, `DELETE FROM tutorial_asset_garbage WHERE id=ANY($1::varchar[])`, pq.Array(removed)); err != nil {
			log.Printf("Tutorial asset cleanup: %v", err)
		}
	}
}
