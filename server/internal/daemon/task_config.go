package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

const taskConfigSourceType = "control_plane_managed"

type taskConfigRef struct {
	Provider    string `json:"provider"`
	ProviderRef string `json:"provider_ref"`
	Version     string `json:"version"`
	Path        string `json:"path"`
	Mode        uint32 `json:"mode"`
	Repo        string `json:"repo,omitempty"`
	Target      string `json:"target,omitempty"`
	Account     string `json:"account,omitempty"`
	Region      string `json:"region,omitempty"`
}

type taskConfigMaterialization struct {
	TaskID     string
	SourceType string
	EnvRoot    string
	WorkDir    string
	Path       string
	TempPath   string
	Mode       os.FileMode
	Selectors  TaskConfigSelectors
	Digest     [sha256.Size]byte
}

func validateTaskConfigRef(ref taskConfigRef) error {
	if ref.Provider != "aws_secrets_manager" || strings.TrimSpace(ref.ProviderRef) == "" || strings.TrimSpace(ref.Version) == "" {
		return errors.New("task_config: invalid provider binding")
	}
	if ref.Mode != 0o600 || !safeTaskConfigRelativePath(ref.Path) {
		return errors.New("task_config: invalid destination")
	}
	if strings.TrimSpace(ref.Repo) == "" || strings.TrimSpace(ref.Target) == "" || strings.TrimSpace(ref.Account) == "" || strings.TrimSpace(ref.Region) == "" {
		return errors.New("task_config: incomplete selector tuple")
	}
	return nil
}

func safeTaskConfigRelativePath(path string) bool {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if path == "" || strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return false
	}
	if len(path) >= 3 && isWindowsDrive(path[0], path[1], path[2]) {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return filepath.Clean(path) == filepath.FromSlash(path)
}

func isWindowsDrive(a, b, c byte) bool {
	return ((a >= 'a' && a <= 'z') || (a >= 'A' && a <= 'Z')) && b == ':' && (c == '/' || c == '\\')
}

func materializeTaskConfig(ctx context.Context, taskID, envRoot, workDir string, ref taskConfigRef, resolve func(context.Context) ([]byte, error)) (*taskConfigMaterialization, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, errors.New("task_config: task identity is required")
	}
	if err := validateTaskConfigRef(ref); err != nil {
		return nil, err
	}
	if resolve == nil {
		return nil, errors.New("task_config: provider unavailable")
	}
	content, err := resolve(ctx)
	if err != nil {
		return nil, errors.New("task_config: provider resolve failed")
	}
	if len(content) == 0 {
		return nil, errors.New("task_config: provider returned empty value")
	}
	defer clear(content)

	target, err := validateTaskConfigTarget(workDir, ref.Path)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, errors.New("task_config: create destination parent failed")
	}
	if err := validateExistingParents(workDir, ref.Path); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(target); err == nil {
		return nil, errors.New("task_config: destination already exists")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, errors.New("task_config: destination cannot be inspected")
	}
	tempPath := filepath.Join(parent, "."+filepath.Base(target)+".multica-"+taskID+".tmp")
	if _, err := os.Lstat(tempPath); err == nil {
		return nil, errors.New("task_config: temporary destination already exists")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, errors.New("task_config: temporary destination cannot be inspected")
	}
	if err := execenv.RegisterSidecarFiles(envRoot, target, tempPath); err != nil {
		return nil, errors.New("task_config: register cleanup intent failed")
	}
	if err := execenv.RegisterTaskConfigIntent(envRoot, taskID, workDir, target, tempPath); err != nil {
		_ = execenv.CleanupSidecarFiles(envRoot, target, tempPath)
		return nil, errors.New("task_config: register cleanup intent failed")
	}
	registered := true
	cleanOnError := true
	defer func() {
		if registered && cleanOnError {
			paths, _ := execenv.CleanupTaskConfigIntent(envRoot)
			if len(paths) > 0 {
				_ = execenv.CleanupSidecarFiles(envRoot, paths...)
			} else {
				_ = execenv.CleanupSidecarFiles(envRoot, target, tempPath)
			}
		}
	}()

	f, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, errors.New("task_config: create temporary destination failed")
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, errors.New("task_config: set destination mode failed")
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return nil, errors.New("task_config: write destination failed")
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, errors.New("task_config: sync destination failed")
	}
	if err := f.Close(); err != nil {
		return nil, errors.New("task_config: close destination failed")
	}
	if err := validateExistingParents(workDir, ref.Path); err != nil {
		return nil, err
	}
	// Link is the portable no-replace publish primitive available here: unlike
	// Rename it cannot clobber a target that appeared after the initial probe.
	if err := os.Link(tempPath, target); err != nil {
		return nil, errors.New("task_config: publish destination failed")
	}
	if err := os.Remove(tempPath); err != nil {
		return nil, errors.New("task_config: remove temporary destination failed")
	}
	cleanOnError = false
	return &taskConfigMaterialization{
		TaskID: taskID, SourceType: taskConfigSourceType, EnvRoot: envRoot, WorkDir: workDir,
		Path: target, TempPath: tempPath, Mode: 0o600,
		Selectors: TaskConfigSelectors{Repo: ref.Repo, Target: ref.Target, Account: ref.Account, Region: ref.Region},
		Digest:    sha256.Sum256(content),
	}, nil
}

func validateTaskConfigTarget(workDir, rel string) (string, error) {
	if workDir == "" || !safeTaskConfigRelativePath(rel) {
		return "", errors.New("task_config: invalid destination")
	}
	root, err := filepath.Abs(workDir)
	if err != nil {
		return "", errors.New("task_config: invalid workdir")
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	relTarget, err := filepath.Rel(root, target)
	if err != nil || relTarget == ".." || strings.HasPrefix(relTarget, ".."+string(filepath.Separator)) || filepath.IsAbs(relTarget) {
		return "", errors.New("task_config: destination escapes workdir")
	}
	return target, nil
}

func validateExistingParents(workDir, rel string) error {
	root, err := filepath.Abs(workDir)
	if err != nil {
		return errors.New("task_config: invalid workdir")
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	cur := root
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("task_config: destination parent is unsafe")
		}
	}
	return nil
}

func preflightTaskConfig(taskID string, m *taskConfigMaterialization, ref taskConfigRef) error {
	if m == nil || taskID == "" || m.TaskID != taskID || m.SourceType != taskConfigSourceType || m.Mode != 0o600 {
		return errors.New("task_config_preflight_failed")
	}
	if err := validateTaskConfigRef(ref); err != nil {
		return errors.New("task_config_preflight_failed")
	}
	wantSelectors := TaskConfigSelectors{Repo: ref.Repo, Target: ref.Target, Account: ref.Account, Region: ref.Region}
	if m.Selectors != wantSelectors || !execenv.SidecarFileRegistered(m.EnvRoot, m.Path) {
		return errors.New("task_config_preflight_failed")
	}
	target, err := validateTaskConfigTarget(m.WorkDir, ref.Path)
	if err != nil || target != m.Path {
		return errors.New("task_config_preflight_failed")
	}
	info, err := os.Lstat(m.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("task_config_preflight_failed")
	}
	if _, err := os.Lstat(m.TempPath); !errors.Is(err, fs.ErrNotExist) {
		return errors.New("task_config_preflight_failed")
	}
	content, err := os.ReadFile(m.Path)
	if err != nil {
		return errors.New("task_config_preflight_failed")
	}
	digest := sha256.Sum256(content)
	clear(content)
	if digest != m.Digest {
		return errors.New("task_config_preflight_failed")
	}
	return nil
}

func cleanupTaskConfig(m *taskConfigMaterialization) error {
	if m == nil {
		return nil
	}
	paths, firstErr := execenv.CleanupTaskConfigIntent(m.EnvRoot)
	if len(paths) > 0 {
		if err := execenv.CleanupSidecarFiles(m.EnvRoot, paths...); firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (d *Daemon) materializeTaskConfigForTask(ctx context.Context, task Task, env *execenv.Environment) (*taskConfigMaterialization, error) {
	if env == nil || len(task.ProjectResources) == 0 {
		return nil, nil
	}
	resource, ref, found, err := taskConfigResource(task)
	if !found {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("task_config: invalid binding")
	}
	m, err := materializeTaskConfig(ctx, task.ID, env.RootDir, env.WorkDir, ref, func(resolveCtx context.Context) ([]byte, error) {
		selectors := TaskConfigSelectors{}
		if task.TaskConfigSelectors != nil {
			selectors = *task.TaskConfigSelectors
		}
		return d.client.ResolveTaskConfig(resolveCtx, task.RuntimeID, task.ID, resource.ID, ref, selectors)
	})
	if m != nil {
		if task.TaskConfigSelectors != nil {
			m.Selectors = *task.TaskConfigSelectors
		}
	}
	return m, err
}

func taskConfigResource(task Task) (*ProjectResourceData, taskConfigRef, bool, error) {
	var resource *ProjectResourceData
	for i := range task.ProjectResources {
		if task.ProjectResources[i].ResourceType != "task_config" {
			continue
		}
		if resource != nil {
			return nil, taskConfigRef{}, true, errors.New("task_config: multiple bindings")
		}
		resource = &task.ProjectResources[i]
	}
	if resource == nil {
		return nil, taskConfigRef{}, false, nil
	}
	var ref taskConfigRef
	if err := jsonUnmarshalNoSecret(resource.ResourceRef, &ref); err != nil {
		return resource, taskConfigRef{}, true, err
	}
	return resource, ref, true, nil
}

// jsonUnmarshalNoSecret keeps the materializer's error surface independent of
// the provider reference contents; standard json.Unmarshal errors can echo
// malformed input in future versions.
func jsonUnmarshalNoSecret(data []byte, dst any) error {
	if len(data) == 0 {
		return errors.New("empty")
	}
	return json.Unmarshal(data, dst)
}
