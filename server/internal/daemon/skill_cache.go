package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

type SkillBundleCache struct {
	root  string
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewSkillBundleCache(root string) *SkillBundleCache {
	return &SkillBundleCache{root: root, locks: make(map[string]*sync.Mutex)}
}

func (c *SkillBundleCache) Load(workspaceID string, ref SkillRefData) (SkillData, bool) {
	if c == nil || c.root == "" {
		return SkillData{}, false
	}
	keyPath := c.bundlePath(workspaceID, ref)
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return SkillData{}, false
	}
	var bundle SkillData
	if err := json.Unmarshal(data, &bundle); err != nil || !validateSkillBundle(ref, bundle) {
		_ = os.Remove(keyPath)
		return SkillData{}, false
	}
	return bundle, true
}

func (c *SkillBundleCache) Store(workspaceID string, bundle SkillData) error {
	if c == nil || c.root == "" {
		return nil
	}
	ref := SkillRefData{ID: bundle.ID, Source: bundle.Source, Hash: bundle.Hash}
	dir := filepath.Dir(c.bundlePath(workspaceID, ref))
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
	if err := os.WriteFile(filepath.Join(tmp, "bundle.json"), data, 0o644); err != nil {
		return err
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
	return filepath.Join(
		c.root,
		safeCacheSegment(workspaceID),
		safeCacheSegment(ref.Source),
		safeCacheSegment(ref.ID),
		safeCacheSegment(ref.Hash),
		"bundle.json",
	)
}

// loadLocalSkillBundle looks for a pre-built bundle in dir. It tries two layouts:
//
//  1. exact-hash layout (same as the on-disk skill cache):
//     <dir>/<workspace>/<source>/<id>/<hash>/bundle.json
//  2. local-maintenance layout (no hash in path):
//     <dir>/<workspace>/<source>/<id>/bundle.json
//
// The exact-hash layout lets operators copy a working skill cache verbatim.
// The local-maintenance layout lets operators maintain a skill without needing
// to rename directories when the content (and therefore hash) changes; the
// bundle is accepted if it is self-consistent.
func loadLocalSkillBundle(dir, workspaceID string, ref SkillRefData) (SkillData, bool) {
	if dir == "" {
		return SkillData{}, false
	}
	base := filepath.Join(
		dir,
		safeCacheSegment(workspaceID),
		safeCacheSegment(ref.Source),
		safeCacheSegment(ref.ID),
	)

	// Try exact-hash layout first.
	exactPath := filepath.Join(base, safeCacheSegment(ref.Hash), "bundle.json")
	if data, err := os.ReadFile(exactPath); err == nil {
		var bundle SkillData
		if err := json.Unmarshal(data, &bundle); err == nil && validateSkillBundle(ref, bundle) {
			return bundle, true
		}
	}

	// Fall back to local-maintenance layout: any self-consistent bundle for this
	// source+id wins. This allows operators to pin a skill locally without
	// tracking its hash in the directory name.
	if data, err := os.ReadFile(filepath.Join(base, "bundle.json")); err == nil {
		var bundle SkillData
		if err := json.Unmarshal(data, &bundle); err == nil && bundle.Source == ref.Source && bundle.ID == ref.ID {
			selfRef := skillRefFromBundle(bundle)
			if validateSkillBundle(selfRef, bundle) {
				return bundle, true
			}
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return SkillData{}, false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(base, entry.Name(), "bundle.json"))
		if err != nil {
			continue
		}
		var bundle SkillData
		if err := json.Unmarshal(data, &bundle); err != nil {
			continue
		}
		if bundle.Source != ref.Source || bundle.ID != ref.ID {
			continue
		}
		// Validate self-consistency using the bundle's own derived ref, not the
		// possibly-stale requested hash.
		selfRef := skillRefFromBundle(bundle)
		if validateSkillBundle(selfRef, bundle) {
			return bundle, true
		}
	}
	return SkillData{}, false
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
