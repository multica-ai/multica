// Package themestore reads the community theme registry and serves its
// themes to the app.
//
// The registry publishes an index.json that its CI writes: every theme with
// the one release that passed the checks, the SHA-256 of that release's
// theme.zip, and screenshots rendered by the same CI. The store only reads
// that file, the zips it names and the screenshots next to it, so it never
// calls the GitHub API and the app only installs files the checks passed.
//
// Everything is cached in memory. The index is refreshed after IndexTTL and
// the previous copy is served while the registry cannot be reached. Packages
// and screenshots are keyed by theme version, so a cached copy never goes
// stale; a size-bounded LRU keeps memory flat.
package themestore

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// DefaultIndexURL is the official registry, github.com/multica-ai/multica-themes.
const DefaultIndexURL = "https://raw.githubusercontent.com/multica-ai/multica-themes/main/index.json"

const (
	// MaxPackageBytes matches THEME_PACKAGE_MAX_BYTES in packages/core/appearance.
	MaxPackageBytes    = 1 << 20
	maxIndexBytes      = 2 << 20
	maxScreenshotBytes = 2 << 20
	maxThemes          = 500
	defaultIndexTTL    = 10 * time.Minute
	// After a failed refresh the stale index is served for this long before
	// the registry is asked again.
	retryAfterFailure = time.Minute
	defaultCacheBytes = 48 << 20
)

var (
	ErrNotFound = errors.New("theme not found")
	ErrUpstream = errors.New("theme registry unavailable")

	themeID      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	repoName     = regexp.MustCompile(`^[A-Za-z0-9-]{1,39}/[A-Za-z0-9._-]{1,100}$`)
	versionText  = regexp.MustCompile(`^[0-9A-Za-z.+-]{1,20}$`)
	sha256Hex    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	relativeFile = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*\.(png|jpg|jpeg|webp)$`)
)

// Modes a theme can support and have screenshots for.
var Modes = []string{"light", "dark"}

// Theme is one entry of the registry index.
type Theme struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Author      string            `json:"author"`
	Repo        string            `json:"repo"`
	Description string            `json:"description,omitempty"`
	Modes       []string          `json:"modes"`
	Version     string            `json:"version"`
	Tag         string            `json:"tag"`
	Package     Package           `json:"package"`
	Screenshots map[string]string `json:"screenshots,omitempty"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
}

// Package is the release asset the registry checked.
type Package struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type index struct {
	Themes []json.RawMessage `json:"themes"`
}

// Config configures a Store. Zero values select the defaults.
type Config struct {
	IndexURL   string
	HTTPClient *http.Client
	IndexTTL   time.Duration
	// CacheBytes bounds the packages and screenshots kept in memory.
	CacheBytes int64
	Now        func() time.Time
}

// Store serves the registry with caching. It is safe for concurrent use.
type Store struct {
	indexURL *url.URL
	client   *http.Client
	ttl      time.Duration
	now      func() time.Time
	group    singleflight.Group

	mu        sync.Mutex
	themes    []Theme
	fetchedAt time.Time
	failedAt  time.Time

	files *lru
}

// New returns a Store for the registry at cfg.IndexURL.
func New(cfg Config) (*Store, error) {
	raw := cfg.IndexURL
	if raw == "" {
		raw = DefaultIndexURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("theme registry URL %q must be an absolute http(s) URL", raw)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	ttl := cfg.IndexTTL
	if ttl <= 0 {
		ttl = defaultIndexTTL
	}
	cacheBytes := cfg.CacheBytes
	if cacheBytes <= 0 {
		cacheBytes = defaultCacheBytes
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Store{indexURL: u, client: client, ttl: ttl, now: now, files: newLRU(cacheBytes)}, nil
}

// Themes lists the registry's themes.
func (s *Store) Themes(ctx context.Context) ([]Theme, error) {
	s.mu.Lock()
	themes, fetchedAt, failedAt := s.themes, s.fetchedAt, s.failedAt
	s.mu.Unlock()
	now := s.now()
	if themes != nil && now.Sub(fetchedAt) < s.ttl {
		return themes, nil
	}
	if themes != nil && now.Sub(failedAt) < retryAfterFailure {
		return themes, nil
	}
	v, err, _ := s.group.Do("index", func() (any, error) {
		fresh, err := s.fetchIndex(context.WithoutCancel(ctx))
		s.mu.Lock()
		defer s.mu.Unlock()
		if err != nil {
			s.failedAt = s.now()
			return nil, err
		}
		s.themes, s.fetchedAt = fresh, s.now()
		return fresh, nil
	})
	if err != nil {
		if themes != nil {
			return themes, nil
		}
		return nil, err
	}
	return v.([]Theme), nil
}

// Theme finds one theme of the registry by id.
func (s *Store) Theme(ctx context.Context, id string) (Theme, error) {
	themes, err := s.Themes(ctx)
	if err != nil {
		return Theme{}, err
	}
	for _, theme := range themes {
		if theme.ID == id {
			return theme, nil
		}
	}
	return Theme{}, ErrNotFound
}

// Package returns the theme's checked release: theme.zip, verified against
// the size and SHA-256 in the index.
func (s *Store) Package(ctx context.Context, id string) (Theme, []byte, error) {
	theme, err := s.Theme(ctx, id)
	if err != nil {
		return Theme{}, nil, err
	}
	key := "package\x00" + theme.ID + "\x00" + theme.Package.SHA256
	data, err := s.cached(ctx, key, func(ctx context.Context) ([]byte, error) {
		u, err := url.Parse(theme.Package.URL)
		if err != nil {
			return nil, err
		}
		data, err := s.get(ctx, u, MaxPackageBytes)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if int64(len(data)) != theme.Package.Size || hex.EncodeToString(sum[:]) != theme.Package.SHA256 {
			return nil, fmt.Errorf("%w: %s does not match the checked release", ErrUpstream, theme.Package.URL)
		}
		return data, nil
	})
	return theme, data, err
}

// Screenshot returns the screenshot of the theme in a mode, and its content type.
func (s *Store) Screenshot(ctx context.Context, id, mode string) ([]byte, string, error) {
	theme, err := s.Theme(ctx, id)
	if err != nil {
		return nil, "", err
	}
	file, ok := theme.Screenshots[mode]
	if !ok {
		return nil, "", ErrNotFound
	}
	u := s.indexURL.ResolveReference(&url.URL{Path: file})
	key := "screenshot\x00" + theme.ID + "\x00" + theme.Version + "\x00" + u.String()
	data, err := s.cached(ctx, key, func(ctx context.Context) ([]byte, error) {
		return s.get(ctx, u, maxScreenshotBytes)
	})
	if err != nil {
		return nil, "", err
	}
	return data, imageType(file), nil
}

func (s *Store) cached(ctx context.Context, key string, load func(context.Context) ([]byte, error)) ([]byte, error) {
	if data, ok := s.files.get(key); ok {
		return data, nil
	}
	v, err, _ := s.group.Do(key, func() (any, error) {
		data, err := load(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		s.files.put(key, data)
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

func (s *Store) fetchIndex(ctx context.Context) ([]Theme, error) {
	body, err := s.get(ctx, s.indexURL, maxIndexBytes)
	if err != nil {
		return nil, err
	}
	var idx index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("%w: index.json: %v", ErrUpstream, err)
	}
	themes := make([]Theme, 0, len(idx.Themes))
	seen := map[string]bool{}
	for _, raw := range idx.Themes {
		if len(themes) == maxThemes {
			break
		}
		var theme Theme
		if json.Unmarshal(raw, &theme) != nil {
			continue
		}
		if clean, ok := validTheme(theme); ok && !seen[clean.ID] {
			seen[clean.ID] = true
			themes = append(themes, clean)
		}
	}
	return themes, nil
}

// validTheme keeps an entry only when every field the app relies on is
// well-formed. The index comes from the registry's own CI, but it is still
// read as untrusted input.
func validTheme(t Theme) (Theme, bool) {
	t.Name = strings.TrimSpace(t.Name)
	t.Author = strings.TrimSpace(t.Author)
	t.Description = strings.TrimSpace(t.Description)
	if !themeID.MatchString(t.ID) || !repoName.MatchString(t.Repo) ||
		t.Name == "" || len(t.Name) > 60 || t.Author == "" || len(t.Author) > 60 || len(t.Description) > 300 ||
		!versionText.MatchString(t.Version) || !versionText.MatchString(t.Tag) ||
		!sha256Hex.MatchString(t.Package.SHA256) || t.Package.Size <= 0 || t.Package.Size > MaxPackageBytes {
		return Theme{}, false
	}
	if u, err := url.Parse(t.Package.URL); err != nil || u.Scheme != "https" || u.Host == "" {
		return Theme{}, false
	}
	modes := []string{}
	for _, mode := range Modes {
		for _, m := range t.Modes {
			if m == mode {
				modes = append(modes, mode)
				break
			}
		}
	}
	if len(modes) == 0 {
		return Theme{}, false
	}
	t.Modes = modes
	shots := map[string]string{}
	for _, mode := range Modes {
		file := t.Screenshots[mode]
		if relativeFile.MatchString(file) && path.Clean(file) == file && !strings.HasPrefix(file, "..") {
			shots[mode] = file
		}
	}
	t.Screenshots = shots
	return t, true
}

func (s *Store) get(ctx context.Context, u *url.URL, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "multica-theme-store")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: GET %s: %s", ErrUpstream, u.Redacted(), res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrUpstream, u.Redacted(), limit)
	}
	return data, nil
}

func imageType(file string) string {
	switch strings.ToLower(path.Ext(file)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

// lru keeps the most recently used entries up to a total size.
type lru struct {
	mu    sync.Mutex
	max   int64
	size  int64
	order *list.List
	items map[string]*list.Element
}

type lruEntry struct {
	key  string
	data []byte
}

func newLRU(max int64) *lru {
	return &lru{max: max, order: list.New(), items: map[string]*list.Element{}}
}

func (c *lru) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*lruEntry).data, true
}

func (c *lru) put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if int64(len(data)) > c.max {
		return
	}
	if el, ok := c.items[key]; ok {
		c.size -= int64(len(el.Value.(*lruEntry).data))
		c.order.Remove(el)
	}
	c.items[key] = c.order.PushFront(&lruEntry{key: key, data: data})
	c.size += int64(len(data))
	for c.size > c.max {
		last := c.order.Back()
		entry := last.Value.(*lruEntry)
		c.order.Remove(last)
		delete(c.items, entry.key)
		c.size -= int64(len(entry.data))
	}
}
