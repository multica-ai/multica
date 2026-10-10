package execenv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Claude ignores skillOverrides for plugin skills. A same-name --plugin-dir
// copy takes precedence over a marketplace install for this session only.
// Copies omit discovery entrypoints, not supporting assets used by other
// components. They are not a filesystem sandbox.
func prepareClaudePluginSkillCopies(envRoot string, disabled []RuntimeSkillRefForEnv) ([]string, error) {
	groups := make(map[string][]RuntimeSkillRefForEnv)
	for _, ref := range disabled {
		if ref.Root == "plugin" {
			if ref.Plugin == "" || ref.PluginPath == "" {
				return nil, fmt.Errorf("disabled Claude plugin skill %q: cannot resolve installed plugin %q", ref.Key, ref.Plugin)
			}
			groups[ref.Plugin] = append(groups[ref.Plugin], ref)
		}
	}
	if len(groups) == 0 {
		return nil, nil
	}
	if envRoot == "" {
		return nil, fmt.Errorf("Claude plugin copies require a task environment root")
	}
	stage, err := os.MkdirTemp(envRoot, "claude-plugin-skills-")
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(stage)
		}
	}()
	realStage, err := filepath.EvalSymlinks(stage)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	names := make(map[string]bool)
	paths := make([]string, 0, len(ids))
	for i, id := range ids {
		refs := groups[id]
		source, err := filepath.EvalSymlinks(refs[0].PluginPath)
		if err != nil {
			return nil, fmt.Errorf("resolve Claude plugin %q: %w", id, err)
		}
		source, err = filepath.Abs(source)
		if err != nil {
			return nil, err
		}
		if pluginPathWithin(source, realStage) {
			return nil, fmt.Errorf("Claude plugin install contains task staging directory")
		}
		sourceRoot, err := os.OpenRoot(source)
		if err != nil {
			return nil, err
		}
		defer sourceRoot.Close()
		raw, err := sourceRoot.ReadFile(filepath.Join(".claude-plugin", "plugin.json"))
		missingManifest := os.IsNotExist(err)
		if err != nil && !missingManifest {
			return nil, err
		}
		manifest := make(map[string]json.RawMessage)
		if !missingManifest {
			if err := json.Unmarshal(raw, &manifest); err != nil {
				return nil, fmt.Errorf("Claude plugin %q manifest: %w", id, err)
			}
			if manifest == nil {
				return nil, fmt.Errorf("Claude plugin %q manifest must be an object", id)
			}
		}
		// Claude identifies a --plugin-dir copy as name@inline. Do not silently
		// discard marketplace-ID-scoped options or credentials during filtering.
		for _, field := range []string{"userConfig", "channels"} {
			if _, exists := manifest[field]; exists {
				return nil, fmt.Errorf("Claude plugin %q uses %s; task-local skill filtering cannot preserve its installed-plugin configuration", id, field)
			}
		}
		name := strings.SplitN(id, "@", 2)[0]
		if value, ok := manifest["name"]; ok {
			if err := json.Unmarshal(value, &name); err != nil {
				return nil, err
			}
		}
		if name == "" || strings.ContainsAny(name, "\\/:@") || names[name] {
			return nil, fmt.Errorf("ambiguous Claude plugin namespace %q", name)
		}
		names[name] = true
		roots := []string{"skills"}
		if value, ok := manifest["skills"]; ok {
			var one string
			if json.Unmarshal(value, &one) == nil {
				roots = append(roots, one)
			} else {
				var many []string
				if err := json.Unmarshal(value, &many); err != nil {
					return nil, fmt.Errorf("Claude plugin %q skills paths: %w", id, err)
				}
				roots = append(roots, many...)
			}
		}
		omit := make(map[string]bool)
		for _, root := range roots {
			root = filepath.Clean(filepath.FromSlash(root))
			if filepath.IsAbs(root) || !pluginPathWithin(source, filepath.Join(source, root)) {
				return nil, fmt.Errorf("Claude plugin %q has unsupported skills path %q", id, root)
			}
			for _, ref := range refs {
				if ref.PluginPath != refs[0].PluginPath {
					return nil, fmt.Errorf("conflicting install paths for Claude plugin %q", id)
				}
				key, ok := strings.CutPrefix(ref.Key, name+":")
				clean, safe := cleanRuntimeSkillKey(key)
				if !ok || !safe || strings.ContainsAny(key, "\\:") {
					return nil, fmt.Errorf("invalid Claude plugin skill key %q for %q", ref.Key, name)
				}
				entry := filepath.Join(source, root, filepath.FromSlash(clean), "SKILL.md")
				omit[filepath.Dir(entry)] = true
				// Also omit aliases to a disabled entrypoint within this plugin.
				if resolved, err := filepath.EvalSymlinks(entry); err == nil {
					if !pluginPathWithin(source, resolved) {
						return nil, fmt.Errorf("disabled Claude skill escapes plugin: %q", ref.Key)
					}
					omit[filepath.Dir(resolved)] = true
				} else if !os.IsNotExist(err) {
					return nil, err
				}
			}
		}
		dest := filepath.Join(stage, fmt.Sprintf("plugin-%d", i))
		budget := pluginCopyBudget{}
		if err := copyClaudePluginTree(sourceRoot, source, source, dest, omit, make(map[string]bool), &budget); err != nil {
			return nil, fmt.Errorf("copy Claude plugin %q: %w", id, err)
		}
		if missingManifest {
			// Without a manifest Claude uses the folder name as the namespace.
			data, err := json.Marshal(map[string]string{"name": name})
			if err != nil {
				return nil, err
			}
			if err := os.MkdirAll(filepath.Join(dest, ".claude-plugin"), 0o700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(dest, ".claude-plugin", "plugin.json"), data, 0o600); err != nil {
				return nil, err
			}
		}
		paths = append(paths, dest)
	}
	success = true
	return paths, nil
}

func pluginPathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// CleanupClaudePluginCopies removes only this run's scratch copies. Retained
// issue workdirs must not accumulate a full plugin snapshot on every follow-up.
func (env *Environment) CleanupClaudePluginCopies() error {
	if env == nil {
		return nil
	}
	stages := make(map[string]bool)
	for _, dir := range env.ClaudePluginDirs {
		stage := filepath.Dir(dir)
		if env.RootDir == "" || filepath.Dir(stage) != filepath.Clean(env.RootDir) || !strings.HasPrefix(filepath.Base(stage), "claude-plugin-skills-") {
			return fmt.Errorf("refusing to clean Claude plugin copy outside task staging: %s", dir)
		}
		stages[stage] = true
	}
	for stage := range stages {
		if err := os.RemoveAll(stage); err != nil {
			return err
		}
	}
	env.ClaudePluginDirs = nil
	return nil
}

type pluginCopyBudget struct {
	files int
	bytes int64
}

// Keep relative internal symlinks pointing within the copy, preserving plugin
// dependency layouts without linking back to the host. Refuse external links,
// cycles and special files rather than starting a partially functional plugin.
func copyClaudePluginTree(sourceRoot *os.Root, root, source, dest string, omit, ancestors map[string]bool, budget *pluginCopyBudget) error {
	if disabledPluginEntrypoint(source, omit) {
		return nil
	}
	real, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	if !pluginPathWithin(root, real) {
		return fmt.Errorf("plugin symlink escapes install: %s", source)
	}
	if disabledPluginEntrypoint(real, omit) {
		return nil
	}
	budget.files++
	if budget.files > 20000 {
		return fmt.Errorf("plugin copy exceeds 20000 entries")
	}
	rel, err := filepath.Rel(root, source)
	if err != nil {
		return err
	}
	info, err := sourceRoot.Lstat(rel)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := sourceRoot.Readlink(rel)
		if err != nil {
			return err
		}
		if filepath.IsAbs(link) || !pluginPathWithin(root, filepath.Join(filepath.Dir(source), link)) {
			return fmt.Errorf("unsupported Claude plugin symlink: %s", source)
		}
		if ancestors[real] {
			return fmt.Errorf("plugin symlink cycle: %s", source)
		}
		return os.Symlink(link, dest)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported plugin file mode %s: %s", info.Mode(), source)
	}
	// Root.Open enforces containment even if a link changes after inspection.
	input, err := sourceRoot.Open(rel)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err = input.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		if ancestors[real] {
			return fmt.Errorf("plugin symlink cycle: %s", source)
		}
		ancestors[real] = true
		defer delete(ancestors, real)
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return err
		}
		entries, err := input.ReadDir(-1)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyClaudePluginTree(sourceRoot, root, filepath.Join(source, entry.Name()), filepath.Join(dest, entry.Name()), omit, ancestors, budget); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported plugin file mode %s: %s", info.Mode(), source)
	}
	remaining := int64(256<<20) - budget.bytes
	if info.Size() > remaining {
		return fmt.Errorf("plugin copy exceeds 256 MiB")
	}
	output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600|info.Mode().Perm()&0o111)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(&pluginCopyContentWriter{output: output}, io.LimitReader(input, remaining+1))
	closeErr := output.Close()
	budget.bytes += n
	if copyErr != nil {
		return copyErr
	}
	if budget.bytes > 256<<20 {
		return fmt.Errorf("plugin copy exceeds 256 MiB")
	}
	return closeErr
}

// Persistent plugin data is keyed by the installed ID, not just its namespace.
// Refuse these references rather than relocating dependencies/state to @inline.
type pluginCopyContentWriter struct {
	output io.Writer
	tail   []byte
}

// Removing only a parent SKILL.md can expose nested example skills that the
// provider previously stopped walking at that parent. Keep those hidden too.
func disabledPluginEntrypoint(path string, roots map[string]bool) bool {
	if filepath.Base(path) != "SKILL.md" {
		return false
	}
	for root := range roots {
		if pluginPathWithin(root, path) {
			return true
		}
	}
	return false
}

func (w *pluginCopyContentWriter) Write(p []byte) (int, error) {
	const marker = "CLAUDE_PLUGIN_DATA"
	check := append(w.tail, p...)
	if bytes.Contains(check, []byte(marker)) {
		return 0, fmt.Errorf("Claude plugin references CLAUDE_PLUGIN_DATA; task-local skill filtering cannot preserve installed-plugin data identity")
	}
	if len(check) > len(marker)-1 {
		check = check[len(check)-(len(marker)-1):]
	}
	w.tail = append([]byte(nil), check...)
	return w.output.Write(p)
}
