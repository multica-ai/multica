package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type LocalStorage struct {
	uploadDir string
	baseURL   string
}

// metaSuffix is the on-disk extension for the sidecar JSON file that
// captures an upload's original filename and sniffed content type. The
// sidecar exists so ServeFile can set Content-Disposition the way S3's
// PutObject path already does, instead of letting the browser fall back
// to the storage-key basename for the download filename.
const metaSuffix = ".meta.json"

type localMeta struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

// NewLocalStorageFromEnv creates a LocalStorage from environment variables.
// Returns nil if upload directory cannot be created.
//
// Environment variables:
//   - LOCAL_UPLOAD_DIR (default: "./data/uploads")
//   - LOCAL_UPLOAD_BASE_URL (optional, e.g., "http://localhost:8080")
func NewLocalStorageFromEnv() *LocalStorage {
	uploadDir := os.Getenv("LOCAL_UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = "./data/uploads"
	}

	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		slog.Error("failed to create upload directory", "dir", uploadDir, "error", err)
		return nil
	}

	baseURL := strings.TrimSuffix(os.Getenv("LOCAL_UPLOAD_BASE_URL"), "/")

	slog.Info("local storage initialized", "dir", uploadDir, "baseURL", baseURL)
	return &LocalStorage{
		uploadDir: uploadDir,
		baseURL:   baseURL,
	}
}

func (s *LocalStorage) CdnDomain() string {
	if s.baseURL == "" {
		return ""
	}
	u, err := url.Parse(s.baseURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (s *LocalStorage) KeyFromURL(rawURL string) string {
	if s.baseURL != "" && strings.HasPrefix(rawURL, s.baseURL) {
		rawURL = strings.TrimPrefix(rawURL, s.baseURL)
	}

	prefix := "/uploads/"
	if idx := strings.Index(rawURL, prefix); idx >= 0 {
		return rawURL[idx+len(prefix):]
	}
	if i := strings.LastIndex(rawURL, "/"); i >= 0 {
		return rawURL[i+1:]
	}
	return rawURL
}

func (s *LocalStorage) Delete(ctx context.Context, key string) {
	if key == "" {
		return
	}
	filePath := filepath.Join(s.uploadDir, key)
	if err := os.Remove(filePath); err != nil {
		if !os.IsNotExist(err) {
			slog.Error("local storage Delete failed", "key", key, "error", err)
		}
	}
	if err := os.Remove(filePath + metaSuffix); err != nil && !os.IsNotExist(err) {
		slog.Error("local storage meta Delete failed", "key", key, "error", err)
	}
}

func (s *LocalStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.Delete(ctx, key)
	}
}

func (s *LocalStorage) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	dest := filepath.Join(s.uploadDir, key)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", fmt.Errorf("local storage MkdirAll: %w", err)
	}
	if err := os.WriteFile(dest, data, 0644); err != nil {
		return "", fmt.Errorf("local storage WriteFile: %w", err)
	}
	// Best-effort sidecar so ServeFile can restore the original filename in
	// Content-Disposition. A failure here is logged but does not fail the
	// upload — the file is still usable, just without the human-readable
	// download name. Skip when there's no filename to preserve: a sidecar
	// without a filename is dead weight, since ServeFile only reads it for
	// that field.
	if filename != "" {
		body, _ := json.Marshal(localMeta{Filename: filename, ContentType: contentType})
		if err := os.WriteFile(dest+metaSuffix, body, 0644); err != nil {
			slog.Error("local storage meta write failed", "key", key, "error", err)
		}
	}

	if s.baseURL != "" {
		return fmt.Sprintf("%s/uploads/%s", s.baseURL, key), nil
	}
	return fmt.Sprintf("/uploads/%s", key), nil
}

func (s *LocalStorage) GetFilePath(key string) string {
	return filepath.Join(s.uploadDir, key)
}

func (s *LocalStorage) ServeFile(w http.ResponseWriter, r *http.Request, filename string) {
	// The sidecar is an implementation detail of the local backend; refuse
	// to serve it directly so /uploads/<key>.meta.json doesn't become a
	// stable read API. Comes before any disk work so a path-traversal
	// attempt at a .meta.json sibling can't trigger an out-of-tree read.
	if strings.HasSuffix(filename, metaSuffix) {
		http.NotFound(w, r)
		return
	}

	filePath := filepath.Join(s.uploadDir, filename)
	// filepath.Join cleans the path but doesn't enforce containment, so a
	// caller passing "../etc/passwd" lands outside uploadDir. http.ServeFile
	// rejects such requests on r.URL.Path, but readLocalMeta runs first —
	// without this guard a crafted path could trigger a stray disk read on
	// an arbitrary <some-path>.meta.json before the 400 lands.
	if !isUnder(s.uploadDir, filePath) {
		http.NotFound(w, r)
		return
	}
	slog.Info("serving file", "filename", filename, "filepath", filePath)

	// Mirror the S3 Upload path: when sidecar metadata exists for this key,
	// set Content-Disposition with the original uploaded filename. Without
	// it, browsers download the file under the storage-key basename (the
	// UUID + extension) instead of the human-readable name the uploader
	// chose. Uploads from before the sidecar landed have no .meta.json on
	// disk and fall through to the existing behavior.
	if meta, ok := readLocalMeta(filePath); ok && meta.Filename != "" {
		safe := sanitizeFilename(meta.Filename)
		disposition := "attachment"
		if isInlineContentType(meta.ContentType) {
			disposition = "inline"
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, safe))
	}

	// Use http.ServeFile which has built-in path traversal protection
	// It sanitizes the path and prevents access outside the directory
	http.ServeFile(w, r, filePath)
}

// isUnder reports whether target resolves to a path inside dir (or equal to
// it). Both inputs are passed through filepath.Clean so trailing slashes and
// "." segments don't fool the comparison.
func isUnder(dir, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func readLocalMeta(filePath string) (localMeta, bool) {
	body, err := os.ReadFile(filePath + metaSuffix)
	if err != nil {
		return localMeta{}, false
	}
	var meta localMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return localMeta{}, false
	}
	return meta, true
}

func (s *LocalStorage) UploadFromReader(ctx context.Context, key string, reader io.Reader, contentType string, filename string) (string, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("local storage ReadAll: %w", err)
	}

	return s.Upload(ctx, key, data, contentType, filename)
}
