package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

type SkillBundleCache struct {
	root      string
	rename    func(string, string) error
	removeAll func(string) error
	mu        sync.Mutex
	locks     map[string]*sync.Mutex
}

func NewSkillBundleCache(root string) *SkillBundleCache {
	return &SkillBundleCache{
		root:      root,
		rename:    os.Rename,
		removeAll: os.RemoveAll,
		locks:     make(map[string]*sync.Mutex),
	}
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

// Store publishes a bundle under its immutable content key.
//
// Audited for the shared workspaces root (two daemon processes serving one
// backend share .skill-cache, GH #8280). Concurrent stores for the same key from
// different processes are benign and stay unlocked on purpose:
//
//   - the key is the content hash, so two writers publish identical bytes and
//     whichever rename wins leaves the same content authoritative;
//   - the bundle is written into a temp directory and published with one rename,
//     so a reader sees the old complete file, the new complete file, or ENOENT —
//     never a partial one (the temp directory is never a key any reader looks
//     up);
//   - the loser's rename can remove the winner's fresh directory before retrying,
//     and Load validates the JSON and drops anything unparseable, so the worst
//     outcome of an interleaving is a cache miss and a re-download, not a wrong
//     or corrupt bundle.
//
// So this needs no scope-level lock: the correctness property (never serve
// invalid content) holds without one, and adding a claim would only remove a
// benign re-download.
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
	if err := c.removeAll(dir); err != nil {
		return err
	}
	if err := c.rename(tmp, dir); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		if err := c.removeAll(dir); err != nil {
			return err
		}
		if err := c.rename(tmp, dir); err != nil {
			return err
		}
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
