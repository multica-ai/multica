package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ProbePiManagedMCP inspects installed package metadata without executing Pi,
// npm, extension code, or MCP servers. Registration is advisory; execution
// repeats the probe against its effective environment.
func ProbePiManagedMCP() error {
	dir, err := piAgentDir(os.Environ())
	if err != nil {
		return err
	}
	_, err = findPiMCPAdapter(dir)
	return err
}

func piAgentDir(env []string) (string, error) {
	value := piEnvValue(env, "PI_CODING_AGENT_DIR")
	if value == "" {
		home := piEnvValue(env, "HOME")
		if runtime.GOOS == "windows" {
			home = piEnvValue(env, "USERPROFILE")
		}
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", err
			}
		}
		value = filepath.Join(home, ".pi", "agent")
	} else if value == "~" || strings.HasPrefix(value, "~/") {
		home := piEnvValue(env, "HOME")
		if runtime.GOOS == "windows" {
			home = piEnvValue(env, "USERPROFILE")
		}
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", err
			}
		}
		if value == "~" {
			value = home
		} else {
			value = filepath.Join(home, value[2:])
		}
	}
	return filepath.Abs(value)
}

func piEnvValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		k, v, ok := strings.Cut(env[i], "=")
		if ok && (k == key || (runtime.GOOS == "windows" && strings.EqualFold(k, key))) {
			return v
		}
	}
	return ""
}

// Package-specific discovery stays here rather than in the daemon or UI.
// Version pins cover the programmatic config contract and agent-dir-owned
// caches. New adapter implementations can provide another discovery branch.
func findPiMCPAdapter(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return "", errors.New("Pi MCP extension unavailable: cannot read Pi settings")
	}
	var settings struct {
		Packages []json.RawMessage `json:"packages"`
	}
	if json.Unmarshal(raw, &settings) != nil {
		return "", errors.New("Pi MCP extension unavailable: invalid Pi settings")
	}
	for _, item := range settings.Packages {
		var source string
		var extensionFilters *[]string
		if json.Unmarshal(item, &source) != nil {
			var p struct {
				Source     string    `json:"source"`
				Extensions *[]string `json:"extensions"`
				Autoload   *bool     `json:"autoload"`
			}
			if json.Unmarshal(item, &p) != nil {
				continue
			}
			source = p.Source
			if p.Autoload != nil && !*p.Autoload {
				continue
			}
			extensionFilters = p.Extensions
		}
		var root string
		if source == "npm:pi-mcp-adapter" || strings.HasPrefix(source, "npm:pi-mcp-adapter@") {
			root = filepath.Join(dir, "npm", "node_modules", "pi-mcp-adapter")
		} else if filepath.IsAbs(source) {
			root = source
		} else {
			continue
		}
		if extensionFilters != nil && !piMCPExtensionEnabled(*extensionFilters, root) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, "package.json"))
		if err != nil {
			continue
		}
		var pkg struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Pi      struct {
				Extensions []string `json:"extensions"`
			} `json:"pi"`
		}
		if json.Unmarshal(raw, &pkg) != nil || pkg.Name != "pi-mcp-adapter" {
			continue
		}
		if pkg.Version != "2.37.0" && pkg.Version != "3.1.0" {
			return "", errors.New("Pi MCP extension detected but unsupported/incompatible")
		}
		declared := false
		for _, p := range pkg.Pi.Extensions {
			if p == "./index.ts" || p == "index.ts" {
				declared = true
			}
		}
		entry := filepath.Join(root, "index.ts")
		data, err := os.ReadFile(entry)
		if !declared || err != nil || !bytes.Contains(data, []byte("export function createMcpAdapter(")) {
			return "", errors.New("Pi MCP extension detected but unsupported/incompatible")
		}
		return entry, nil
	}
	return "", errors.New("Pi MCP extension unavailable: no enabled compatible adapter")
}

func piMCPExtensionEnabled(patterns []string, root string) bool {
	// Pi uses minimatch. Fail closed on syntax not understood by Go's matcher
	// instead of accidentally re-enabling an excluded adapter.
	for _, pattern := range patterns {
		if strings.ContainsAny(pattern, "{}()") {
			return false
		}
	}
	if len(patterns) == 0 {
		return false
	}
	matches := func(pattern string) bool {
		pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
		ok, _ := filepath.Match(strings.TrimPrefix(pattern, "**/"), "index.ts")
		absolute, _ := filepath.Match(pattern, filepath.ToSlash(filepath.Join(root, "index.ts")))
		return ok || absolute
	}
	includes, enabled := false, false
	for _, p := range patterns {
		if p != "" && !strings.ContainsAny(p[:1], "!+-") {
			includes = true
			enabled = enabled || matches(p)
		}
	}
	if !includes {
		enabled = true
	}
	for _, p := range patterns {
		if strings.HasPrefix(p, "!") && matches(p[1:]) {
			enabled = false
		}
	}
	for _, p := range patterns {
		if strings.HasPrefix(p, "+") && (strings.TrimPrefix(p[1:], "./") == "index.ts" || filepath.Clean(p[1:]) == filepath.Join(root, "index.ts")) {
			enabled = true
		}
	}
	for _, p := range patterns {
		if strings.HasPrefix(p, "-") && (strings.TrimPrefix(p[1:], "./") == "index.ts" || filepath.Clean(p[1:]) == filepath.Join(root, "index.ts")) {
			enabled = false
		}
	}
	return enabled
}
