// copy_files.go physically copies file blobs so a workspace copy is
// self-contained.
//
// A normal copy shares the underlying stored blob: the copied artifact /
// attachment row keeps the source workspace's file URL, so the copy and the
// original point at the same physical file. That is cheap, but it means the
// source workspace cannot be deleted without the copies losing their files.
// MaterializeCopiedFiles gives every shared blob its own physical copy under the
// target workspace and rewrites the row's URL, so the target owns all of its
// files and the source can be safely deleted.
//
// CEREBRO-PATCH(workspace-copy): TECH-3766 — materialize file blobs on copy.
package workspacecopy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// FilesMaterializedResult reports how many shared file blobs were given their
// own physical copy in the target workspace.
type FilesMaterializedResult struct {
	ArtifactFiles int64 `json:"artifact_files_materialized,omitempty"`
	Attachments   int64 `json:"attachments_materialized,omitempty"`
	FilesSkipped  int64 `json:"files_skipped,omitempty"`
}

// fileRow is one materializable row: an artifact or attachment whose blob may
// still live under another workspace.
type fileRow struct {
	id          pgtype.UUID
	url         string
	contentType string // "" for artifacts — derived from the extension
	filename    string // "" for artifacts — derived from the blob key
}

// MaterializeCopiedFiles walks every artifact and attachment in the target
// workspace whose stored blob still lives under a different workspace and gives
// each its own physical copy under the target, rewriting the row's URL to the
// new blob. After this pass the target workspace owns all of its file blobs, so
// the source workspace of a copy can be deleted without the copies losing their
// files.
//
// The pass is idempotent: once a row's blob lives under the target workspace it
// is skipped, so re-running never duplicates. Blob I/O happens outside any
// transaction; each URL rewrite is its own short statement. A blob that cannot
// be resolved or read is logged and skipped (best-effort) rather than failing
// the whole pass — the row keeps its shared URL and the file still works as long
// as the source exists.
func (s *Store) MaterializeCopiedFiles(ctx context.Context, targetWorkspace pgtype.UUID) (FilesMaterializedResult, error) {
	var res FilesMaterializedResult
	if s.storage == nil {
		return res, fmt.Errorf("materialize files: storage not configured")
	}
	targetID := uuid.UUID(targetWorkspace.Bytes).String()

	// Collect both row sets first; we cannot stream blob I/O while a query
	// holds the pooled connection.
	var artifacts, attachments []fileRow

	artRows, err := s.pool.Query(ctx, `
		SELECT id, file_url FROM artifact
		WHERE workspace_id = $1 AND file_url IS NOT NULL AND file_url <> ''`,
		targetWorkspace)
	if err != nil {
		return res, fmt.Errorf("list artifact files: %w", err)
	}
	for artRows.Next() {
		var r fileRow
		if err := artRows.Scan(&r.id, &r.url); err != nil {
			artRows.Close()
			return res, fmt.Errorf("scan artifact file: %w", err)
		}
		artifacts = append(artifacts, r)
	}
	artRows.Close()
	if err := artRows.Err(); err != nil {
		return res, fmt.Errorf("iterate artifact files: %w", err)
	}

	attRows, err := s.pool.Query(ctx, `
		SELECT id, url, content_type, filename FROM attachment
		WHERE workspace_id = $1 AND url <> ''`,
		targetWorkspace)
	if err != nil {
		return res, fmt.Errorf("list attachment files: %w", err)
	}
	for attRows.Next() {
		var r fileRow
		if err := attRows.Scan(&r.id, &r.url, &r.contentType, &r.filename); err != nil {
			attRows.Close()
			return res, fmt.Errorf("scan attachment file: %w", err)
		}
		attachments = append(attachments, r)
	}
	attRows.Close()
	if err := attRows.Err(); err != nil {
		return res, fmt.Errorf("iterate attachment files: %w", err)
	}

	for _, r := range artifacts {
		newURL, ok := s.materializeBlob(ctx, r, targetID, "artifacts")
		if !ok {
			res.FilesSkipped++
			continue
		}
		if _, err := s.pool.Exec(ctx, `UPDATE artifact SET file_url = $1 WHERE id = $2`, newURL, r.id); err != nil {
			return res, fmt.Errorf("update artifact file_url: %w", err)
		}
		res.ArtifactFiles++
	}

	for _, r := range attachments {
		newURL, ok := s.materializeBlob(ctx, r, targetID, "attachments")
		if !ok {
			res.FilesSkipped++
			continue
		}
		if _, err := s.pool.Exec(ctx, `UPDATE attachment SET url = $1 WHERE id = $2`, newURL, r.id); err != nil {
			return res, fmt.Errorf("update attachment url: %w", err)
		}
		res.Attachments++
	}

	return res, nil
}

// materializeBlob copies r's blob to a new key under the target workspace and
// returns the new URL. It returns ok=false (skip) when the blob already lives
// under the target workspace, cannot be resolved to a storage key, or cannot be
// read — in those cases the row keeps its existing URL.
func (s *Store) materializeBlob(ctx context.Context, r fileRow, targetID, subdir string) (string, bool) {
	key := s.storage.KeyFromURL(r.url)
	if key == "" {
		slog.Warn("materialize: cannot resolve storage key, skipping", "url", r.url)
		return "", false
	}
	// Already owned by the target workspace — every blob key embeds
	// workspaces/<workspace-id>/, so a key containing the target id is already
	// local. This is what makes the pass idempotent.
	if strings.Contains(key, targetID) {
		return "", false
	}

	rc, err := s.storage.GetReader(ctx, key)
	if err != nil {
		slog.Warn("materialize: source blob unreadable, skipping", "key", key, "error", err)
		return "", false
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		slog.Warn("materialize: reading source blob failed, skipping", "key", key, "error", err)
		return "", false
	}

	ext := path.Ext(key)
	newID, err := uuid.NewV7()
	if err != nil {
		slog.Warn("materialize: uuid generation failed, skipping", "error", err)
		return "", false
	}
	newKey := "workspaces/" + targetID + "/" + subdir + "/" + newID.String() + ext

	contentType := r.contentType
	if contentType == "" {
		contentType = mime.TypeByExtension(ext)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	filename := r.filename
	if filename == "" {
		filename = path.Base(key)
	}

	newURL, err := s.storage.Upload(ctx, newKey, data, contentType, filename)
	if err != nil {
		slog.Warn("materialize: uploading copy failed, skipping", "key", newKey, "error", err)
		return "", false
	}
	return newURL, true
}
