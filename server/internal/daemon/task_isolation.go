package daemon

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/multica-ai/multica/server/internal/taskauth"
	"github.com/multica-ai/multica/server/pkg/agent"
)

const maxTaskShebangBytes = 4096

func buildTaskIsolationPolicy(params taskIsolationParams) (agent.TaskIsolationPolicy, string, error) {
	if params.Environment == nil {
		return agent.TaskIsolationPolicy{}, "", fmt.Errorf("task environment is required")
	}
	if params.LookupPath == nil {
		params.LookupPath = exec.LookPath
	}

	envRoot, err := existingTaskDirectory("environment root", params.Environment.RootDir)
	if err != nil {
		return agent.TaskIsolationPolicy{}, "", err
	}
	workDir, err := existingTaskDirectory("work directory", params.Environment.WorkDir)
	if err != nil {
		return agent.TaskIsolationPolicy{}, "", err
	}
	taskTempDir, err := existingTaskDirectory("task temp directory", params.TaskTempDir)
	if err != nil {
		return agent.TaskIsolationPolicy{}, "", err
	}
	taskAuthority, err := existingTaskRegularFile("task authority", params.TaskAuthority)
	if err != nil {
		return agent.TaskIsolationPolicy{}, "", err
	}
	providerExecutable, err := existingTaskExecutable("provider executable", params.Executable)
	if err != nil {
		return agent.TaskIsolationPolicy{}, "", err
	}
	selfExecutable, err := existingTaskExecutable("Multica executable", params.SelfExecutable)
	if err != nil {
		return agent.TaskIsolationPolicy{}, "", err
	}

	writable := []string{envRoot, taskTempDir}
	if params.Environment.LocalDirectory && !taskPathWithin(workDir, envRoot) {
		writable = append(writable, workDir)
	}

	readOnly := []string{
		taskProviderRoot(providerExecutable),
		filepath.Dir(selfExecutable),
	}
	for _, repo := range params.Repos {
		barePath, pathErr := existingTaskDirectory("task repository", repo.BarePath)
		if pathErr != nil {
			return agent.TaskIsolationPolicy{}, "", fmt.Errorf("repository %q: %w", repo.URL, pathErr)
		}
		readOnly = append(readOnly, barePath)
	}

	pathDirs := make([]string, 0, 4)
	shebang, err := readTaskShebang(providerExecutable, params.LookupPath)
	if err != nil {
		return agent.TaskIsolationPolicy{}, "", err
	}
	if shebang != nil {
		readOnly = append(readOnly, taskRuntimeRoot(shebang.launcher))
		if shebang.runtime != "" {
			readOnly = append(readOnly, taskRuntimeRoot(shebang.runtime))
			pathDirs = append(pathDirs, filepath.Dir(shebang.runtime))
		} else {
			pathDirs = append(pathDirs, filepath.Dir(shebang.launcher))
		}
	}
	pathDirs = append(pathDirs, filepath.Dir(providerExecutable), filepath.Dir(selfExecutable))

	forbidden, err := existingOwnerConfigRoots(params.OwnerHome)
	if err != nil {
		return agent.TaskIsolationPolicy{}, "", err
	}
	if params.HermesSourceHome != "" {
		if _, statErr := os.Lstat(params.HermesSourceHome); statErr == nil {
			hermesSource, sourceErr := existingTaskDirectory("Hermes source home", params.HermesSourceHome)
			if sourceErr != nil {
				return agent.TaskIsolationPolicy{}, "", sourceErr
			}
			forbidden = append(forbidden, hermesSource)
		} else if !os.IsNotExist(statErr) {
			return agent.TaskIsolationPolicy{}, "", fmt.Errorf("inspect Hermes source home %q: %w", params.HermesSourceHome, statErr)
		}
	}
	policy := agent.TaskIsolationPolicy{
		WritableRoots: uniqueTaskPaths(writable),
		ReadOnlyRoots: uniqueTaskPaths(readOnly),
		ReadOnlyFiles: []agent.ReadOnlyFileMount{{
			Source: taskAuthority,
			Target: taskauth.FixedPath,
		}},
		SystemRoots:    existingTaskSystemRoots(),
		ForbiddenRoots: uniqueTaskPaths(forbidden),
		Network:        agent.NetworkAccessPublicAndLoopback,
	}
	validated, err := policy.Validated()
	if err != nil {
		return agent.TaskIsolationPolicy{}, "", fmt.Errorf("validate task isolation authority: %w", err)
	}
	return validated, strings.Join(uniqueTaskPaths(pathDirs), string(os.PathListSeparator)), nil
}

type taskShebang struct {
	launcher string
	runtime  string
}

func readTaskShebang(executable string, lookupPath func(string) (string, error)) (*taskShebang, error) {
	file, err := os.Open(executable)
	if err != nil {
		return nil, fmt.Errorf("open provider executable %q: %w", executable, err)
	}
	defer file.Close()

	line, err := bufio.NewReaderSize(file, maxTaskShebangBytes).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, nil
	}
	if len(line) > maxTaskShebangBytes {
		return nil, fmt.Errorf("provider executable %q has an oversized shebang", executable)
	}
	line = bytes.TrimSpace(line)
	if !bytes.HasPrefix(line, []byte("#!")) {
		return nil, nil
	}
	fields := strings.Fields(strings.TrimSpace(string(line[2:])))
	if len(fields) == 0 || !filepath.IsAbs(fields[0]) {
		return nil, fmt.Errorf("provider executable %q has a non-absolute shebang interpreter", executable)
	}
	launcher, err := existingTaskExecutable("shebang interpreter", fields[0])
	if err != nil {
		return nil, err
	}
	result := &taskShebang{launcher: launcher}
	if filepath.Base(launcher) != "env" {
		return result, nil
	}
	if len(fields) != 2 || strings.HasPrefix(fields[1], "-") {
		return nil, fmt.Errorf("provider executable %q has unsupported env shebang", executable)
	}
	runtimePath, err := lookupPath(fields[1])
	if err != nil {
		return nil, fmt.Errorf("resolve shebang runtime %q: %w", fields[1], err)
	}
	result.runtime, err = existingTaskExecutable("shebang runtime", runtimePath)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func existingTaskDirectory(kind, path string) (string, error) {
	resolved, err := existingTaskPath(kind, path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat %s %q: %w", kind, resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s %q is not a directory", kind, resolved)
	}
	return resolved, nil
}

func existingTaskExecutable(kind, path string) (string, error) {
	resolved, info, err := existingTaskRegularFileInfo(kind, path)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s %q is not executable", kind, resolved)
	}
	return resolved, nil
}

func existingTaskRegularFile(kind, path string) (string, error) {
	resolved, _, err := existingTaskRegularFileInfo(kind, path)
	return resolved, err
}

func existingTaskRegularFileInfo(kind, path string) (string, os.FileInfo, error) {
	resolved, err := existingTaskPath(kind, path)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", nil, fmt.Errorf("stat %s %q: %w", kind, resolved, err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("%s %q is not a regular file", kind, resolved)
	}
	return resolved, info, nil
}

func existingTaskPath(kind, path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("%s %q must be an absolute canonical path", kind, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s %q: %w", kind, path, err)
	}
	return resolved, nil
}

func taskProviderRoot(executable string) string {
	for current := filepath.Dir(executable); ; current = filepath.Dir(current) {
		parent := filepath.Dir(current)
		if filepath.Base(parent) == "node_modules" {
			return current
		}
		grandparent := filepath.Dir(parent)
		if strings.HasPrefix(filepath.Base(parent), "@") && filepath.Base(grandparent) == "node_modules" {
			return current
		}
		if parent == current {
			return filepath.Dir(executable)
		}
	}
}

func taskRuntimeRoot(executable string) string {
	return filepath.Dir(executable)
}

func existingOwnerConfigRoots(ownerHome string) ([]string, error) {
	if ownerHome == "" || !filepath.IsAbs(ownerHome) || filepath.Clean(ownerHome) != ownerHome {
		return nil, fmt.Errorf("daemon owner home %q must be an absolute canonical path", ownerHome)
	}
	var roots []string
	for _, relative := range []string{".multica", ".codex", filepath.Join(".config", "opencode"), ".openclaw", ".cursor"} {
		path := filepath.Join(ownerHome, relative)
		_, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect owner config root %q: %w", path, err)
		}
		resolved, err := existingTaskDirectory("owner config root", path)
		if err != nil {
			return nil, err
		}
		roots = append(roots, resolved)
	}
	return uniqueTaskPaths(roots), nil
}

func existingTaskSystemRoots() []string {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/System/Library", "/usr/lib", "/private/etc/ssl"}
	case "linux":
		candidates = []string{"/lib", "/lib64", "/usr/lib", "/usr/lib64", "/etc/ssl", "/etc/pki", "/etc/ca-certificates"}
	}
	var roots []string
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			roots = append(roots, candidate)
		}
	}
	return uniqueTaskPaths(roots)
}

func uniqueTaskPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result
}

func taskPathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
