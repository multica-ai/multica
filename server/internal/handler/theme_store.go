package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/themestore"
)

// ThemeStoreTheme is a registry theme as the app sees it. Download URLs and
// screenshot paths stay on the server: the app fetches both through the
// routes below, so the web CSP stays closed and GitHub is never called from
// the browser.
type ThemeStoreTheme struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Author      string   `json:"author"`
	Repo        string   `json:"repo"`
	Description string   `json:"description,omitempty"`
	Modes       []string `json:"modes"`
	Version     string   `json:"version"`
	Size        int64    `json:"size"`
	Screenshots []string `json:"screenshots"`
	// ScreenshotRevisions names each mode's current screenshot without its
	// registry path. The registry can re-render a screenshot without a new
	// release, so the app caches a screenshot by revision, not by version.
	ScreenshotRevisions map[string]string `json:"screenshot_revisions"`
	UpdatedAt           string            `json:"updated_at,omitempty"`
	// Downloads counts installs and updates served by this server.
	Downloads int64 `json:"downloads"`
}

func (h *Handler) themeStore(w http.ResponseWriter) *themestore.Store {
	if h.ThemeStore == nil {
		writeError(w, http.StatusNotFound, "the theme store is turned off on this server")
	}
	return h.ThemeStore
}

func writeThemeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, themestore.ErrNotFound) {
		writeError(w, http.StatusNotFound, "theme not found")
		return
	}
	slog.WarnContext(r.Context(), "theme store: registry request failed", "error", err)
	writeError(w, http.StatusBadGateway, "the theme registry could not be reached")
}

// ListStoreThemes returns the themes of the community registry.
func (h *Handler) ListStoreThemes(w http.ResponseWriter, r *http.Request) {
	store := h.themeStore(w)
	if store == nil {
		return
	}
	themes, err := store.Themes(r.Context())
	if err != nil {
		writeThemeStoreError(w, r, err)
		return
	}
	downloads := h.themeStoreDownloads(r)
	out := make([]ThemeStoreTheme, 0, len(themes))
	for _, t := range themes {
		shots := []string{}
		revisions := map[string]string{}
		for _, mode := range themestore.Modes {
			if file, ok := t.Screenshots[mode]; ok {
				shots = append(shots, mode)
				sum := sha256.Sum256([]byte(file))
				revisions[mode] = hex.EncodeToString(sum[:6])
			}
		}
		out = append(out, ThemeStoreTheme{
			ID:                  t.ID,
			Name:                t.Name,
			Author:              t.Author,
			Repo:                t.Repo,
			Description:         t.Description,
			Modes:               t.Modes,
			Version:             t.Version,
			Size:                t.Package.Size,
			Screenshots:         shots,
			ScreenshotRevisions: revisions,
			UpdatedAt:           t.UpdatedAt,
			Downloads:           downloads[t.ID],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"themes": out})
}

// GetStoreThemePackage returns the theme's checked theme.zip. The app reads
// it with the same parser and CSS sanitizer as an imported theme.
func (h *Handler) GetStoreThemePackage(w http.ResponseWriter, r *http.Request) {
	store := h.themeStore(w)
	if store == nil {
		return
	}
	theme, data, err := store.Package(r.Context(), chi.URLParam(r, "themeId"))
	if err != nil {
		writeThemeStoreError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Theme-Version", theme.Version)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := w.Write(data); err != nil {
		return
	}
	if h.Queries != nil {
		if err := h.Queries.AddThemeStoreDownload(r.Context(), theme.ID); err != nil {
			slog.WarnContext(r.Context(), "theme store: count download", "theme", theme.ID, "error", err)
		}
	}
}

// themeStoreDownloads is the download count of every theme served so far. A
// failed read leaves the counts out rather than failing the list.
func (h *Handler) themeStoreDownloads(r *http.Request) map[string]int64 {
	counts := map[string]int64{}
	if h.Queries == nil {
		return counts
	}
	rows, err := h.Queries.ListThemeStoreDownloads(r.Context())
	if err != nil {
		slog.WarnContext(r.Context(), "theme store: read download counts", "error", err)
		return counts
	}
	for _, row := range rows {
		counts[row.ThemeID] = row.Downloads
	}
	return counts
}

// GetStoreThemeScreenshot returns the screenshot the registry CI rendered
// for a theme in light or dark mode.
func (h *Handler) GetStoreThemeScreenshot(w http.ResponseWriter, r *http.Request) {
	store := h.themeStore(w)
	if store == nil {
		return
	}
	data, contentType, err := store.Screenshot(r.Context(), chi.URLParam(r, "themeId"), chi.URLParam(r, "mode"))
	if err != nil {
		writeThemeStoreError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}
