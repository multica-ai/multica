package storage

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const defaultUploadDir = "./uploads"

type LocalStorage struct {
	dir string
}

func NewLocalStorageFromEnv() *LocalStorage {
	dir := strings.TrimSpace(os.Getenv("UPLOAD_DIR"))
	if dir == "" {
		dir = defaultUploadDir
	}

	absDir, err := filepath.Abs(dir)
	if err == nil {
		dir = absDir
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("failed to initialize local upload dir", "dir", dir, "error", err)
		return nil
	}

	slog.Info("local storage initialized", "dir", dir)
	return &LocalStorage{dir: dir}
}

func (s *LocalStorage) Dir() string {
	return s.dir
}

func (s *LocalStorage) FileHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filePath, err := s.pathForKey(r.URL.Path)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}

		http.ServeFile(w, r, filePath)
	})
}

func (s *LocalStorage) Upload(_ context.Context, key string, data []byte, _ string, _ string) error {
	filePath, err := s.pathForKey(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("create local upload dir: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return fmt.Errorf("write local upload file: %w", err)
	}
	return nil
}

func (s *LocalStorage) PublicURL(r *http.Request, key string) string {
	return requestBaseURL(r) + path.Join("/files", key)
}

func (s *LocalStorage) KeyFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Path != "" {
		return strings.TrimPrefix(parsed.Path, "/files/")
	}
	return strings.TrimPrefix(rawURL, "/files/")
}

func (s *LocalStorage) Delete(_ context.Context, key string) {
	filePath, err := s.pathForKey(key)
	if err != nil {
		slog.Error("local delete rejected invalid key", "key", key, "error", err)
		return
	}
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		slog.Error("local file delete failed", "key", key, "error", err)
	}
}

func (s *LocalStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.Delete(ctx, key)
	}
}

func (s *LocalStorage) pathForKey(key string) (string, error) {
	for _, segment := range strings.Split(strings.TrimPrefix(key, "/"), "/") {
		if segment == ".." {
			return "", fmt.Errorf("invalid key path")
		}
	}

	cleanKey := strings.TrimPrefix(path.Clean("/"+key), "/")
	if cleanKey == "" || cleanKey == "." {
		return "", fmt.Errorf("empty key")
	}

	fullPath := filepath.Join(s.dir, filepath.FromSlash(cleanKey))
	relPath, err := filepath.Rel(s.dir, fullPath)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid key path")
	}
	return fullPath, nil
}

func requestBaseURL(r *http.Request) string {
	scheme := forwardedScheme(r)
	host := forwardedHost(r)
	return scheme + "://" + host
}

func forwardedScheme(r *http.Request) string {
	if forwarded := r.Header.Get("Forwarded"); forwarded != "" {
		parts := strings.Split(strings.Split(forwarded, ",")[0], ";")
		for _, part := range parts {
			kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(kv) == 2 && strings.EqualFold(kv[0], "proto") && kv[1] != "" {
				return strings.Trim(kv[1], "\"")
			}
		}
	}
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func forwardedHost(r *http.Request) string {
	if forwarded := r.Header.Get("Forwarded"); forwarded != "" {
		parts := strings.Split(strings.Split(forwarded, ",")[0], ";")
		for _, part := range parts {
			kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(kv) == 2 && strings.EqualFold(kv[0], "host") && kv[1] != "" {
				return strings.Trim(kv[1], "\"")
			}
		}
	}
	if host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); host != "" {
		return host
	}
	return r.Host
}
