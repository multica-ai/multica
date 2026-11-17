package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

const codexDefaultProjectDocBytes int64 = 32 * 1024

type codexInstructionConfig struct {
	MaxBytes      *int64    `json:"project_doc_max_bytes"`
	FallbackNames []string  `json:"project_doc_fallback_filenames"`
	RootMarkers   *[]string `json:"project_root_markers"`
}

// Ask the native loader rather than duplicating CLI/profile/project precedence.
// Root discovery ignores project layers in Codex, so it uses a separate read
// without cwd. Neither response nor native error bodies are logged.
func (c *codexClient) checkManagedInstructions(ctx context.Context, opts ExecOptions) error {
	if opts.CodexInstructionsPath == "" {
		return nil
	}
	read := func(cwd string) (codexInstructionConfig, error) {
		params := map[string]any{"includeLayers": false}
		if cwd != "" {
			params["cwd"] = cwd
		}
		response, err := c.request(ctx, "config/read", params)
		if err != nil {
			if ctx.Err() != nil {
				return codexInstructionConfig{}, ctx.Err()
			}
			return codexInstructionConfig{}, errors.New("codex instruction budget could not be verified: effective configuration unavailable; check the Codex runtime configuration")
		}
		var envelope struct {
			Config *codexInstructionConfig `json:"config"`
		}
		if json.Unmarshal(response, &envelope) != nil || envelope.Config == nil {
			return codexInstructionConfig{}, errors.New("codex instruction budget could not be verified: invalid effective configuration")
		}
		return *envelope.Config, nil
	}
	base, err := read("")
	if err != nil {
		return err
	}
	cwd, err := filepath.Abs(opts.Cwd)
	if err != nil {
		return errors.New("codex instruction budget could not be verified: invalid working directory")
	}
	config, err := read(cwd)
	if err != nil {
		return err
	}
	config.RootMarkers = base.RootMarkers
	return checkCodexInstructionFiles(cwd, opts.CodexInstructionsPath, config)
}

// Mirrors Codex's project-only byte accounting: one selected file per directory,
// root first, with whitespace-only prefixes consuming no budget. Global user
// instructions are supplied separately and are not charged to this budget.
func checkCodexInstructionFiles(cwd, managedPath string, config codexInstructionConfig) error {
	limit := codexDefaultProjectDocBytes
	if config.MaxBytes != nil {
		limit = *config.MaxBytes
	}
	if limit < 0 {
		return errors.New("codex instruction budget could not be verified: invalid project_doc_max_bytes")
	}
	managed, err := os.Stat(managedPath)
	if err != nil || !managed.Mode().IsRegular() {
		return fmt.Errorf("codex managed instruction file %q is unavailable", managedPath)
	}
	markers := []string{".git"}
	if config.RootMarkers != nil {
		markers = *config.RootMarkers
	}
	dirs := []string{cwd}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		found := false
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				found = true
				break
			}
		}
		if found {
			dirs = nil
			for child := cwd; ; child = filepath.Dir(child) {
				dirs = append(dirs, child)
				if child == dir {
					break
				}
			}
			slices.Reverse(dirs)
			break
		}
		if filepath.Dir(dir) == dir || len(markers) == 0 {
			break
		}
	}
	names := []string{"AGENTS.override.md", "AGENTS.md"}
	for _, name := range config.FallbackNames {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") ||
			(runtime.GOOS == "windows" && strings.ContainsAny(name, "\\:")) || slices.Contains(names, name) {
			continue
		}
		names = append(names, name)
	}
	remaining := limit
	for _, dir := range dirs {
		for _, name := range names {
			path := filepath.Join(dir, name)
			info, err := os.Stat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("codex instruction budget could not be verified: cannot inspect %q", path)
			}
			if !info.Mode().IsRegular() {
				continue
			}
			if remaining == 0 {
				return codexInstructionOverflow(managedPath, limit, remaining, path)
			}
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("codex instruction budget could not be verified: cannot read %q", path)
			}
			data, readErr := io.ReadAll(io.LimitReader(file, remaining))
			file.Close()
			if readErr != nil {
				return fmt.Errorf("codex instruction budget could not be verified: cannot read %q", path)
			}
			if os.SameFile(info, managed) {
				if int64(len(data)) < info.Size() || len(strings.TrimSpace(string(data))) == 0 {
					return codexInstructionOverflow(managedPath, limit, remaining, path)
				}
				return nil
			}
			if strings.TrimSpace(string(data)) != "" {
				remaining -= int64(len(data))
			}
			break
		}
	}
	return fmt.Errorf("codex managed instruction file %q is not selected by project instruction discovery (project_doc_max_bytes=%d); check AGENTS.override.md and the project root", managedPath, limit)
}

func codexInstructionOverflow(path string, limit, remaining int64, source string) error {
	return fmt.Errorf("codex managed instruction file %q cannot be loaded completely: project_doc_max_bytes=%d, remaining_bytes=%d at %q; reduce project instructions or explicitly raise the Codex project instruction limit", path, limit, remaining, source)
}
