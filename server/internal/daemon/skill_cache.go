package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

type SkillBundleCache struct {
	root       string
	legacyRoot  string
	mu         sync.Mutex
	locks      map[string]*sync.Mutex
}

func NewSkillBundleCache(root string) *SkillBundleCache {
	cache := &SkillBundleCache{root: root, locks: make(map[string]*sync.Mutex)}
	if filepath.Base(root) == "v2" {
		cache.legacyRoot = filepath.Join(filepath.Dir(root), "v1")
	}
	return cache
}

func (c *SkillBundleCache) Load(workspaceID string, ref SkillRefData) (SkillData, bool) {
	if c == nil || c.root == "" {
		return SkillData{}, false
	}
	if bundle, ok := c.loadFromRoot(c.root, workspaceID, ref); ok {
		return bundle, true
	}
	if c.legacyRoot != "" {
		if bundle, ok := c.loadFromRoot(c.legacyRoot, workspaceID, ref); ok {
			_ = c.Store(workspaceID, bundle)
			return bundle, true
		}
	}
	return SkillData{}, false
}

func (c *SkillBundleCache) Store(workspaceID string, bundle SkillData) error {
	if c == nil || c.root == "" {
		return nil
	}
	ref := SkillRefData{ID: bundle.ID, Source: bundle.Source, Hash: bundle.Hash}
	dir := c.bundleDir(workspaceID, ref)
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".bundle-*")
	if err != nil {
		if mkErr := os.MkdirAll(filepath.Dir(dir), 0o755); mkErr != nil {
			return mkErr
		}
		tmp, err = os.MkdirTemp(filepath.Dir(dir), ".bundle-*")
		if err != nil {
			return err
		}
	}
	defer os.RemoveAll(tmp)

	data, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, ".skill-bundle.json"), data, 0o644); err != nil {
		return err
	}
	body := execenv.EnsureSkillFrontmatter(bundle.Content, sanitizeSkillName(bundle.Name), bundle.Description)
	if err := os.WriteFile(filepath.Join(tmp, "SKILL.md"), []byte(body), 0o644); err != nil {
		return err
	}
	for _, file := range bundle.Files {
		target := filepath.Join(tmp, file.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(file.Content), 0o644); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return err
	}
	return nil
}

func (c *SkillBundleCache) WithRefLock(workspaceID string, ref SkillRefData, fn func() error) error {
	if c == nil {
		return fn()
	}
	key := workspaceID + "\x00" + ref.Source + "\x00" + ref.ID + "\x00" + ref.Hash
	lock := c.lockForKey(key)
	lock.Lock()
	defer lock.Unlock()
	return fn()
}

func (c *SkillBundleCache) lockForKey(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if lock := c.locks[key]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	c.locks[key] = lock
	return lock
}

func (c *SkillBundleCache) bundlePath(workspaceID string, ref SkillRefData) string {
	return filepath.Join(c.bundleDir(workspaceID, ref), ".skill-bundle.json")
}

func (c *SkillBundleCache) bundleDir(workspaceID string, ref SkillRefData) string {
	return bundleDirForRoot(c.root, workspaceID, ref)
}

func bundleDirForRoot(root, workspaceID string, ref SkillRefData) string {
	return filepath.Join(
		root,
		safeCacheSegment(workspaceID),
		safeCacheSegment(ref.Source),
		safeCacheSegment(ref.ID),
		safeCacheSegment(ref.Hash),
	)
}

func (c *SkillBundleCache) loadFromRoot(root, workspaceID string, ref SkillRefData) (SkillData, bool) {
	dir := bundleDirForRoot(root, workspaceID, ref)
	data, err := os.ReadFile(filepath.Join(dir, ".skill-bundle.json"))
	if err != nil {
		return SkillData{}, false
	}
	var bundle SkillData
	if err := json.Unmarshal(data, &bundle); err != nil || !validateSkillBundle(ref, bundle) {
		_ = os.RemoveAll(dir)
		return SkillData{}, false
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		_ = os.RemoveAll(dir)
		return SkillData{}, false
	}
	for _, file := range bundle.Files {
		if _, err := os.Stat(filepath.Join(dir, file.Path)); err != nil {
			_ = os.RemoveAll(dir)
			return SkillData{}, false
		}
	}
	return bundle, true
}

func validateSkillBundle(ref SkillRefData, bundle SkillData) bool {
	if bundle.ID != ref.ID || bundle.Source != ref.Source || bundle.Hash != ref.Hash {
		return false
	}
	if len(bundle.Files) != ref.FileCount {
		return false
	}
	files := make([]skillbundle.File, 0, len(bundle.Files))
	for _, file := range bundle.Files {
		if !safeSkillFilePath(file.Path) {
			return false
		}
		files = append(files, skillbundle.File{Path: file.Path, Content: file.Content})
	}
	manifest := skillbundle.BuildManifest(skillbundle.Skill{
		ID:          bundle.ID,
		Source:      bundle.Source,
		Name:        bundle.Name,
		Description: bundle.Description,
		Content:     bundle.Content,
		Files:       files,
	})
	if manifest.Hash != ref.Hash {
		return false
	}
	if ref.SizeBytes > 0 && manifest.SizeBytes != ref.SizeBytes {
		return false
	}
	return true
}

func safeSkillFilePath(p string) bool {
	if p == "" || strings.Contains(p, "\x00") || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	clean := path.Clean(p)
	if clean == "." || clean != p || strings.HasPrefix(clean, "../") || clean == ".." {
		return false
	}
	return true
}

func safeCacheSegment(s string) string {
	var b strings.Builder
	if s == "" {
		return "_"
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "." || out == ".." {
		return fmt.Sprintf("_%s", out)
	}
	return out
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

func sanitizeSkillName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlphaNum.ReplaceAllString(s, "-")
	out := strings.Trim(s, "-")
	if out == "" {
		return "skill"
	}
	return out
}
