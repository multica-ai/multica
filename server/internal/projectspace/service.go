package projectspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultRoot        = "./data/project-spaces"
	DefaultStagingRoot = "./data/project-imports"
	MaxImportFiles     = 5000
	MaxImportBytes     = int64(5 << 30)
	MaxFileBytes       = int64(100 << 20)
)

var reservedTopLevel = map[string]struct{}{
	".ai":      {},
	".git":     {},
	".multica": {},
}

type Service struct {
	root        string
	stagingRoot string
	customRoot  bool
	available   bool
	startupErr  error
}

type Status struct {
	Available  bool `json:"available"`
	Configured bool `json:"configured"`
}

type Entry struct {
	Name         string    `json:"name"`
	RelativePath string    `json:"relative_path"`
	Kind         string    `json:"kind"`
	SizeBytes    int64     `json:"size_bytes"`
	ModifiedAt   time.Time `json:"modified_at"`
}

func ResolveRoot(raw, fallback string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = fallback
	}
	if raw == "" {
		return "", errors.New("project space root is empty")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve project space root: %w", err)
	}
	return filepath.Clean(abs), nil
}

func NewFromEnv() *Service {
	rawRoot := strings.TrimSpace(os.Getenv("MULTICA_PROJECT_SPACE_ROOT"))
	root, rootErr := ResolveRoot(rawRoot, DefaultRoot)
	staging, stagingErr := ResolveRoot(os.Getenv("MULTICA_PROJECT_IMPORT_STAGING_ROOT"), DefaultStagingRoot)
	svc := &Service{
		root:        root,
		stagingRoot: staging,
		customRoot:  rawRoot != "",
	}
	if rootErr != nil {
		svc.startupErr = rootErr
		return svc
	}
	if stagingErr != nil {
		svc.startupErr = stagingErr
		return svc
	}
	if err := svc.preflight(); err != nil {
		svc.startupErr = err
		return svc
	}
	svc.available = true
	return svc
}

func NewForTest(root, stagingRoot string) (*Service, error) {
	resolvedRoot, err := ResolveRoot(root, DefaultRoot)
	if err != nil {
		return nil, err
	}
	resolvedStaging, err := ResolveRoot(stagingRoot, DefaultStagingRoot)
	if err != nil {
		return nil, err
	}
	svc := &Service{root: resolvedRoot, stagingRoot: resolvedStaging, customRoot: root != ""}
	if err := svc.preflight(); err != nil {
		return nil, err
	}
	svc.available = true
	return svc, nil
}

func (s *Service) Status() Status {
	if s == nil {
		return Status{}
	}
	return Status{Available: s.available, Configured: s.customRoot}
}

func (s *Service) Err() error {
	if s == nil {
		return errors.New("project space is not configured")
	}
	return s.startupErr
}

func (s *Service) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *Service) StagingRoot() string {
	if s == nil {
		return ""
	}
	return s.stagingRoot
}

func (s *Service) preflight() error {
	if err := os.MkdirAll(s.root, 0o750); err != nil {
		return fmt.Errorf("create project space root: %w", err)
	}
	if err := os.MkdirAll(s.stagingRoot, 0o700); err != nil {
		return fmt.Errorf("create project import staging root: %w", err)
	}

	probeDir := filepath.Join(s.root, ".multica-health")
	if err := os.MkdirAll(probeDir, 0o750); err != nil {
		return fmt.Errorf("create project space health directory: %w", err)
	}
	probe, err := os.CreateTemp(probeDir, ".probe-*")
	if err != nil {
		return fmt.Errorf("open project space health probe: %w", err)
	}
	oldPath := probe.Name()
	newPath := oldPath + ".ready"
	defer func() {
		_ = os.Remove(oldPath)
		_ = os.Remove(newPath)
	}()
	if _, err := probe.WriteString("multica-project-space"); err != nil {
		_ = probe.Close()
		return fmt.Errorf("write project space health probe: %w", err)
	}
	if err := probe.Close(); err != nil {
		return fmt.Errorf("close project space health probe: %w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename project space health probe: %w", err)
	}
	if _, err := os.ReadFile(newPath); err != nil {
		return fmt.Errorf("read project space health probe: %w", err)
	}
	return nil
}

func (s *Service) ProjectDir(workspaceID, projectID string) (string, error) {
	if s == nil || !s.available {
		return "", errors.New("project space unavailable")
	}
	if !safeID(workspaceID) || !safeID(projectID) {
		return "", errors.New("invalid project space identity")
	}
	root := filepath.Join(s.root, "workspaces", workspaceID, "projects", projectID)
	if err := ensureWithin(s.root, root); err != nil {
		return "", err
	}
	return root, nil
}

func (s *Service) EnsureProject(workspaceID, projectID string) (string, error) {
	root, err := s.ProjectDir(workspaceID, projectID)
	if err != nil {
		return "", err
	}
	for _, rel := range []string{
		"inbox/uploads",
		"knowledge",
		"artifacts",
		".ai",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o750); err != nil {
			return "", fmt.Errorf("create project space layout: %w", err)
		}
	}
	indexPath := filepath.Join(root, "index.md")
	if _, err := os.Stat(indexPath); errors.Is(err, fs.ErrNotExist) {
		body := []byte("# Project knowledge\n\nCanonical uploads live under `inbox/uploads/`.\n")
		if err := os.WriteFile(indexPath, body, 0o640); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("create project index: %w", err)
		}
	}
	return root, nil
}

func NormalizeRelativePath(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || strings.HasPrefix(raw, "/") || filepath.IsAbs(raw) || filepath.VolumeName(raw) != "" {
		return "", errors.New("path must be relative")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f || r == 0 {
			return "", errors.New("path contains control characters")
		}
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path escapes project space")
	}
	parts := strings.Split(clean, "/")
	if _, reserved := reservedTopLevel[strings.ToLower(parts[0])]; reserved {
		return "", errors.New("path uses a reserved directory")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("path contains an invalid segment")
		}
	}
	if len(clean) > 1024 {
		return "", errors.New("path is too long")
	}
	return clean, nil
}

func NormalizeBatchName(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "upload"
	}
	normalized, err := NormalizeRelativePath(raw)
	if err != nil {
		return "", err
	}
	if strings.Contains(normalized, "/") {
		return "", errors.New("batch name must be a single path segment")
	}
	return normalized, nil
}

func (s *Service) ResolveProjectPath(workspaceID, projectID, relative string) (string, error) {
	root, err := s.EnsureProject(workspaceID, projectID)
	if err != nil {
		return "", err
	}
	rel, err := NormalizeRelativePath(relative)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	if err := ensureWithin(root, target); err != nil {
		return "", err
	}
	if err := rejectSymlinkPath(root, target); err != nil {
		return "", err
	}
	return target, nil
}

func (s *Service) ImportTarget(workspaceID, projectID, date, batchName, relative string) (string, string, error) {
	batch, err := NormalizeBatchName(batchName)
	if err != nil {
		return "", "", err
	}
	rel, err := NormalizeRelativePath(relative)
	if err != nil {
		return "", "", err
	}
	finalRel := path.Join("inbox/uploads", date, batch, rel)
	target, err := s.ResolveProjectPath(workspaceID, projectID, finalRel)
	return finalRel, target, err
}

func (s *Service) ImportStagingPath(importID, fileID string) (string, error) {
	if !safeID(importID) || !safeID(fileID) {
		return "", errors.New("invalid import identity")
	}
	dir := filepath.Join(s.stagingRoot, importID)
	if err := ensureWithin(s.stagingRoot, dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create import staging directory: %w", err)
	}
	return filepath.Join(dir, fileID+".upload"), nil
}

func (s *Service) List(workspaceID, projectID, relative string) ([]Entry, error) {
	root, err := s.EnsureProject(workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	dir := root
	prefix := ""
	if strings.TrimSpace(relative) != "" && relative != "." {
		prefix, err = NormalizeRelativePath(relative)
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(root, filepath.FromSlash(prefix))
	}
	if err := ensureWithin(root, dir); err != nil {
		return nil, err
	}
	if err := rejectSymlinkPath(root, dir); err != nil {
		return nil, err
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(item.Name(), ".") {
			continue
		}
		if item.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		}
		entries = append(entries, Entry{
			Name:         item.Name(),
			RelativePath: path.Join(prefix, item.Name()),
			Kind:         kind,
			SizeBytes:    info.Size(),
			ModifiedAt:   info.ModTime().UTC(),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "directory"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func HashFile(filename string) (string, int64, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func CopyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(dst)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func ConflictPath(target string) (string, bool, error) {
	if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
		return target, false, nil
	} else if err != nil {
		return "", false, err
	}
	ext := filepath.Ext(target)
	base := strings.TrimSuffix(target, ext)
	for i := 2; i <= 9999; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(candidate); errors.Is(err, fs.ErrNotExist) {
			return candidate, true, nil
		} else if err != nil {
			return "", false, err
		}
	}
	return "", false, errors.New("too many conflicting files")
}

func safeID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func ensureWithin(root, target string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("path escapes configured root")
	}
	return nil
}

// rejectSymlinkPath prevents an existing symlink below root from redirecting
// a read or write outside the project space. Missing suffixes are allowed
// because upload finalization creates them after this check.
func rejectSymlinkPath(root, target string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("path escapes project space")
	}
	current := rootAbs
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, fs.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("project space paths may not contain symlinks")
		}
	}
	return nil
}
