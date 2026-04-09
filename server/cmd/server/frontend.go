package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var workspaceRoutePrefixes = []string{
	"/login",
	"/issues",
	"/board",
	"/inbox",
	"/my-issues",
	"/agents",
	"/runtimes",
	"/skills",
	"/settings",
}

var workspaceDevPrefixes = []string{
	"/@vite",
	"/@react-refresh",
	"/@fs",
	"/src/",
	"/assets/",
	"/node_modules/.vite",
}

func newFrontendHandler() http.Handler {
	marketingProxy := newOptionalProxy(os.Getenv("MARKETING_SITE_ORIGIN"))
	workspaceProxy := newOptionalProxy(os.Getenv("WORKSPACE_SITE_ORIGIN"))
	workspaceDist := strings.TrimSpace(os.Getenv("WORKSPACE_DIST_DIR"))
	if workspaceDist == "" {
		workspaceDist = filepath.Clean(filepath.Join("..", "apps", "workspace", "dist"))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if workspaceProxy != nil && isWorkspaceDevPath(path) {
			workspaceProxy.ServeHTTP(w, r)
			return
		}

		if isWorkspaceRoute(path) {
			if workspaceProxy != nil {
				workspaceProxy.ServeHTTP(w, r)
				return
			}
			if serveWorkspaceAssetOrIndex(workspaceDist, w, r) {
				return
			}
		}

		if serveWorkspaceAsset(workspaceDist, w, r) {
			return
		}

		if marketingProxy != nil {
			marketingProxy.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	})
}

func isWorkspaceRoute(path string) bool {
	for _, prefix := range workspaceRoutePrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func isWorkspaceDevPath(path string) bool {
	if isWorkspaceRoute(path) || path == "/" {
		return true
	}
	for _, prefix := range workspaceDevPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func newOptionalProxy(rawURL string) http.Handler {
	target := strings.TrimSpace(rawURL)
	if target == "" {
		return nil
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return nil
	}

	return httputil.NewSingleHostReverseProxy(parsed)
}

func serveWorkspaceAssetOrIndex(distDir string, w http.ResponseWriter, r *http.Request) bool {
	if serveWorkspaceAsset(distDir, w, r) {
		return true
	}

	indexPath := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return false
	}

	http.ServeFile(w, r, indexPath)
	return true
}

func serveWorkspaceAsset(distDir string, w http.ResponseWriter, r *http.Request) bool {
	if distDir == "" {
		return false
	}

	cleanPath := filepath.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	if cleanPath == "/" {
		return false
	}

	fullPath := filepath.Join(distDir, strings.TrimPrefix(cleanPath, "/"))
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return false
	}

	http.ServeFile(w, r, fullPath)
	return true
}
