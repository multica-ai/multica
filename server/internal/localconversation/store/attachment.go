package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// MaxAttachmentBytes bounds one attachment file.
const MaxAttachmentBytes = 32 << 20

const tempPrefix = ".tmp-"

var mediaExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// NewAttachment is an attachment file already written, committed by a Batch.
type NewAttachment struct {
	ID             string
	ScopeID        int64
	ConversationID string
	// Name is the file name under FilesDir.
	Name string
	// FileName is the name the user gave the file, for uploads.
	FileName  string
	Size      int64
	MediaType string
	CreatedAt time.Time
}

// FilesDir holds attachment files, next to the database.
func (s *Store) FilesDir() string { return filepath.Join(filepath.Dir(s.path), "files") }

// WriteAttachmentFile writes data as a private file under FilesDir: to a
// temporary name, synced, then renamed into place, so a crash never leaves
// a partial file under an attachment name. The attachment becomes visible
// only when a Batch commits it; a file whose batch never commits is removed
// by RemoveUnreferencedFiles.
func (s *Store) WriteAttachmentFile(scopeID int64, conversationID, mediaType string, data []byte, now time.Time) (NewAttachment, error) {
	ext, ok := mediaExtensions[mediaType]
	if !ok {
		return NewAttachment{}, fmt.Errorf("local conversation store: unsupported attachment type %q", mediaType)
	}
	if len(data) == 0 || len(data) > MaxAttachmentBytes {
		return NewAttachment{}, fmt.Errorf("local conversation store: attachment size %d outside 1..%d", len(data), MaxAttachmentBytes)
	}
	id, name, size, err := s.writeFile(bytes.NewReader(data), ext, MaxAttachmentBytes)
	if err != nil {
		return NewAttachment{}, err
	}
	return NewAttachment{ID: id, ScopeID: scopeID, ConversationID: conversationID, Name: name, Size: size, MediaType: mediaType, CreatedAt: now}, nil
}

// MaxUploadBytes bounds one file a user attaches to a message.
const MaxUploadBytes = 25 << 20

// ErrUploadTooLarge rejects an upload over MaxUploadBytes.
var ErrUploadTooLarge = errors.New("local conversation store: the file exceeds 25 MB")

// ErrAttachmentConflict rejects a client-chosen id another scope already uses.
var ErrAttachmentConflict = errors.New("local conversation store: the attachment id is in use")

// StoreUpload saves a file the user attaches to a message and commits it at
// once, so a later send can reference it. A client that chose id can repeat the
// upload safely; identical content shares one file on disk.
func (s *Store) StoreUpload(ctx context.Context, scopeID int64, id, fileName, mediaType string, src io.Reader, now time.Time) (sqlitedb.Attachment, error) {
	if a, found, err := s.existingUpload(ctx, scopeID, id); found || err != nil {
		return a, err
	}
	ext, ok := mediaExtensions[mediaType]
	if !ok {
		if e := strings.ToLower(filepath.Ext(fileName)); safeExtension.MatchString(e) {
			ext = e
		}
	}
	hash := sha256.New()
	fileID, name, size, err := s.writeFile(io.TeeReader(src, hash), ext, MaxUploadBytes)
	if err != nil {
		return sqlitedb.Attachment{}, err
	}
	if id == "" {
		id = fileID
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	stored := filepath.Join(s.FilesDir(), name)
	if same, found := s.uploadWithHash(ctx, scopeID, sum); found && same.Path != name {
		_ = os.Remove(stored)
		name = same.Path
		stored = ""
	}
	a := sqlitedb.Attachment{ID: id, ScopeID: scopeID, Path: name, FileName: fileName, Size: size, MediaType: mediaType, Sha256: sum, CreatedAt: formatTime(now)}
	if err := s.insertUpload(ctx, a); err != nil {
		if stored != "" {
			_ = os.Remove(stored)
		}
		return sqlitedb.Attachment{}, err
	}
	return a, nil
}

// LinkUpload records an attachment for content this scope already stored, so
// the file is not transferred again. ErrNotFound means it has to be uploaded.
func (s *Store) LinkUpload(ctx context.Context, scopeID int64, id, sum, fileName, mediaType string, now time.Time) (sqlitedb.Attachment, error) {
	if a, found, err := s.existingUpload(ctx, scopeID, id); found || err != nil {
		return a, err
	}
	same, found := s.uploadWithHash(ctx, scopeID, sum)
	if !found {
		return sqlitedb.Attachment{}, fmt.Errorf("%w: content %s", ErrNotFound, sum)
	}
	a := sqlitedb.Attachment{ID: id, ScopeID: scopeID, Path: same.Path, FileName: fileName, Size: same.Size, MediaType: mediaType, Sha256: sum, CreatedAt: formatTime(now)}
	if err := s.insertUpload(ctx, a); err != nil {
		return sqlitedb.Attachment{}, err
	}
	return a, nil
}

// DiscardUpload removes an upload no message or queued prompt uses. It is a
// no-op for anything else, because the caller only says the draft dropped it.
func (s *Store) DiscardUpload(ctx context.Context, scopeID int64, id string) error {
	a, err := sqlitedb.New(s.read).GetAttachment(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (a.ScopeID != scopeID || a.ConversationID != "")) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.removeIfUnused(ctx, a)
	return err
}

// RemoveAbandonedUploads deletes uploads older than minAge that no message or
// queued prompt uses. It returns the number removed.
func (s *Store) RemoveAbandonedUploads(ctx context.Context, minAge time.Duration, now time.Time) (int, error) {
	uploads, err := sqlitedb.New(s.read).ListUploadsCreatedBefore(ctx, formatTime(now.Add(-minAge)))
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, a := range uploads {
		gone, err := s.removeIfUnused(ctx, a)
		if err != nil {
			return removed, err
		}
		if gone {
			removed++
		}
	}
	return removed, nil
}

func (s *Store) removeIfUnused(ctx context.Context, a sqlitedb.Attachment) (bool, error) {
	// sqlc cannot parse json_each, so this query is written by hand.
	var used bool
	err := s.read.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM message, json_each(message.attachment_ids) WHERE json_each.value = ?1)
		OR EXISTS (SELECT 1 FROM prompt_queue_item, json_each(prompt_queue_item.item, '$.attachmentIds') WHERE json_each.value = ?1)`, a.ID).Scan(&used)
	if err != nil || used {
		return false, err
	}
	if err := sqlitedb.New(s.write).DeleteAttachment(ctx, a.ID); err != nil {
		return false, err
	}
	// Other attachments may share the file.
	sharing, err := sqlitedb.New(s.read).CountAttachmentsAtPath(ctx, a.Path)
	if err != nil {
		return false, err
	}
	if sharing == 0 {
		_ = os.Remove(filepath.Join(s.FilesDir(), a.Path))
	}
	return true, nil
}

func (s *Store) existingUpload(ctx context.Context, scopeID int64, id string) (sqlitedb.Attachment, bool, error) {
	if id == "" {
		return sqlitedb.Attachment{}, false, nil
	}
	a, err := sqlitedb.New(s.read).GetAttachment(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return sqlitedb.Attachment{}, false, nil
	}
	if err != nil {
		return sqlitedb.Attachment{}, false, err
	}
	if a.ScopeID != scopeID {
		return sqlitedb.Attachment{}, false, ErrAttachmentConflict
	}
	return a, true, nil
}

// uploadWithHash finds this scope's stored content, skipping a file that is gone.
func (s *Store) uploadWithHash(ctx context.Context, scopeID int64, sum string) (sqlitedb.Attachment, bool) {
	a, err := sqlitedb.New(s.read).FindUploadByHash(ctx, sqlitedb.FindUploadByHashParams{ScopeID: scopeID, Sha256: sum})
	if err != nil {
		return sqlitedb.Attachment{}, false
	}
	if _, err := os.Stat(filepath.Join(s.FilesDir(), a.Path)); err != nil {
		return sqlitedb.Attachment{}, false
	}
	return a, true
}

func (s *Store) insertUpload(ctx context.Context, a sqlitedb.Attachment) error {
	return sqlitedb.New(s.write).InsertAttachment(ctx, sqlitedb.InsertAttachmentParams{
		ID: a.ID, ScopeID: a.ScopeID, Path: a.Path, FileName: a.FileName, Size: a.Size, MediaType: a.MediaType, Sha256: a.Sha256, CreatedAt: a.CreatedAt,
	})
}

var safeExtension = regexp.MustCompile(`^\.[a-z0-9]{1,16}$`)

// writeFile copies src to a private file under FilesDir: to a temporary
// name, synced, then renamed into place, so a crash never leaves a partial
// file under an attachment name. Empty files and files over limit fail.
func (s *Store) writeFile(src io.Reader, ext string, limit int64) (id, name string, size int64, err error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", "", 0, err
	}
	id = hex.EncodeToString(buf)
	dir := s.FilesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", 0, err
	}
	tmp, err := os.CreateTemp(dir, tempPrefix+"*")
	if err != nil {
		return "", "", 0, err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", "", 0, err
	}
	size, err = io.Copy(tmp, io.LimitReader(src, limit+1))
	if err == nil && size > limit {
		err = ErrUploadTooLarge
	}
	if err == nil && size == 0 {
		err = errors.New("local conversation store: the file is empty")
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", "", 0, err
	}
	name = id + ext
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		return "", "", 0, err
	}
	return id, name, size, nil
}

// AddPreviewRoot lets the scope preview files under a folder the user chose.
func (s *Store) AddPreviewRoot(ctx context.Context, scopeID int64, path string, now time.Time) error {
	return sqlitedb.New(s.write).InsertPreviewRoot(ctx, sqlitedb.InsertPreviewRootParams{ScopeID: scopeID, Path: path, CreatedAt: formatTime(now)})
}

// PreviewRoots lists the folders the scope may preview files under.
func (s *Store) PreviewRoots(ctx context.Context, scopeID int64) ([]string, error) {
	return sqlitedb.New(s.read).ListPreviewRoots(ctx, scopeID)
}

// Attachment returns a committed attachment and its file path. scopeID must
// match: an attachment is never served across accounts or workspaces.
func (s *Store) Attachment(ctx context.Context, scopeID int64, id string) (sqlitedb.Attachment, string, error) {
	a, err := sqlitedb.New(s.read).GetAttachment(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && a.ScopeID != scopeID) {
		return sqlitedb.Attachment{}, "", fmt.Errorf("%w: attachment %s", ErrNotFound, id)
	}
	if err != nil {
		return sqlitedb.Attachment{}, "", err
	}
	return a, filepath.Join(s.FilesDir(), a.Path), nil
}

// RemoveUnreferencedFiles deletes files under FilesDir that no committed
// attachment references and that are older than minAge, so files of batches
// still being committed are kept. It returns the number removed.
func (s *Store) RemoveUnreferencedFiles(ctx context.Context, minAge time.Duration, now time.Time) (int, error) {
	entries, err := os.ReadDir(s.FilesDir())
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	q := sqlitedb.New(s.read)
	removed := 0
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < minAge {
			continue
		}
		if !strings.HasPrefix(e.Name(), tempPrefix) {
			n, err := q.CountAttachmentsAtPath(ctx, e.Name())
			if err != nil {
				return removed, err
			}
			if n > 0 {
				continue
			}
		}
		if err := os.Remove(filepath.Join(s.FilesDir(), e.Name())); err == nil {
			removed++
		}
	}
	return removed, nil
}
