package agent

// CEREBRO-PATCH(agent-managed-mcp-project-config): FIR-1459 - project-scoped MCP config for local runtimes.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func hasManagedMCPConfig(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	return !bytes.Equal(trimmed, []byte("null"))
}

func parseManagedMCPServers(raw json.RawMessage) (map[string]any, []string, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse mcp_config: %w", err)
	}

	servers := map[string]any{}
	rawServers, ok := doc["mcpServers"]
	if ok && len(bytes.TrimSpace(rawServers)) > 0 && !bytes.Equal(bytes.TrimSpace(rawServers), []byte("null")) {
		if err := json.Unmarshal(rawServers, &servers); err != nil {
			return nil, nil, fmt.Errorf("parse mcp_config.mcpServers: %w", err)
		}
	}

	names := make([]string, 0, len(servers))
	for name, entry := range servers {
		if strings.TrimSpace(name) == "" {
			return nil, nil, fmt.Errorf("mcp_config.mcpServers contains an empty server name")
		}
		if _, ok := entry.(map[string]any); !ok {
			return nil, nil, fmt.Errorf("mcp_config.mcpServers.%s must be an object", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return servers, names, nil
}

func ensureCursorProjectMCPConfig(cwd string, mcpConfig json.RawMessage, disallowedMCPTools []string, logger *slog.Logger) (func(), error) {
	if strings.TrimSpace(cwd) == "" {
		if hasManagedMCPConfig(mcpConfig) || len(disallowedMCPTools) > 0 {
			return nil, fmt.Errorf("cursor: cwd is required to apply managed MCP config")
		}
		return func() {}, nil
	}

	var cleanups []func()
	cleanupAll := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	if hasManagedMCPConfig(mcpConfig) {
		servers, _, err := parseManagedMCPServers(mcpConfig)
		if err != nil {
			return nil, err
		}
		content, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal cursor mcp config: %w", err)
		}
		cleanup, err := materializeManagedJSONFile(filepath.Join(cwd, ".cursor", "mcp.json"), append(content, '\n'))
		if err != nil {
			return nil, fmt.Errorf("write cursor mcp config: %w", err)
		}
		cleanups = append(cleanups, cleanup)
	}

	denyRules := cursorMCPDenyPermissions(disallowedMCPTools)
	if len(denyRules) > 0 {
		path := filepath.Join(cwd, ".cursor", "cli.json")
		cfg, err := readJSONObjectFile(path)
		if err != nil {
			cleanupAll()
			return nil, fmt.Errorf("read cursor cli config: %w", err)
		}
		addCursorPermissionDenies(cfg, denyRules)
		content, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			cleanupAll()
			return nil, fmt.Errorf("marshal cursor cli config: %w", err)
		}
		cleanup, err := materializeManagedJSONFile(path, append(content, '\n'))
		if err != nil {
			cleanupAll()
			return nil, fmt.Errorf("write cursor cli config: %w", err)
		}
		cleanups = append(cleanups, cleanup)
	}

	if logger != nil && len(cleanups) > 0 {
		logger.Debug("cursor managed MCP config materialized", "cwd", cwd)
	}
	return cleanupAll, nil
}

func ensureGeminiProjectMCPConfig(cwd string, mcpConfig json.RawMessage, logger *slog.Logger) (func(), error) {
	if !hasManagedMCPConfig(mcpConfig) {
		return func() {}, nil
	}
	if strings.TrimSpace(cwd) == "" {
		return nil, fmt.Errorf("gemini: cwd is required to apply managed MCP config")
	}

	servers, names, err := parseManagedMCPServers(mcpConfig)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(cwd, ".gemini", "settings.json")
	cfg, err := readJSONObjectFile(path)
	if err != nil {
		return nil, fmt.Errorf("read gemini settings: %w", err)
	}

	cfg["mcpServers"] = geminiMCPServers(servers)
	mcpObj, _ := cfg["mcp"].(map[string]any)
	if mcpObj == nil {
		mcpObj = map[string]any{}
	}
	mcpObj["allowed"] = names
	delete(mcpObj, "excluded")
	cfg["mcp"] = mcpObj

	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal gemini settings: %w", err)
	}
	cleanup, err := materializeManagedJSONFile(path, append(content, '\n'))
	if err != nil {
		return nil, fmt.Errorf("write gemini settings: %w", err)
	}
	if logger != nil {
		logger.Debug("gemini managed MCP config materialized", "cwd", cwd)
	}
	return cleanup, nil
}

func geminiMCPServers(servers map[string]any) map[string]any {
	out := make(map[string]any, len(servers))
	for name, entry := range servers {
		src, _ := entry.(map[string]any)
		dst := make(map[string]any, len(src)+1)
		for k, v := range src {
			dst[k] = v
		}
		if _, hasHTTP := dst["httpUrl"]; !hasHTTP {
			if urlValue, ok := dst["url"].(string); ok && strings.TrimSpace(urlValue) != "" && !isSSEMCPEntry(dst) {
				dst["httpUrl"] = urlValue
				delete(dst, "url")
			}
		}
		out[name] = dst
	}
	return out
}

func isSSEMCPEntry(entry map[string]any) bool {
	for _, key := range []string{"type", "transport"} {
		if v, ok := entry[key].(string); ok && strings.EqualFold(strings.TrimSpace(v), "sse") {
			return true
		}
	}
	return false
}

func readJSONObjectFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return map[string]any{}, nil
	}
	return obj, nil
}

func materializeManagedJSONFile(path string, content []byte) (func(), error) {
	dir := filepath.Dir(path)
	_, dirStatErr := os.Stat(dir)
	dirExisted := dirStatErr == nil

	prior, readErr := os.ReadFile(path)
	existed := readErr == nil
	var mode os.FileMode = 0o600
	if existed {
		if st, err := os.Stat(path); err == nil {
			mode = st.Mode().Perm()
		}
	} else if readErr != nil && !os.IsNotExist(readErr) {
		return nil, readErr
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		if !dirExisted {
			_ = os.Remove(dir)
		}
		return nil, err
	}
	_ = os.Chmod(path, 0o600)

	return func() {
		if existed {
			_ = os.WriteFile(path, prior, mode)
			_ = os.Chmod(path, mode)
			return
		}
		_ = os.Remove(path)
		if !dirExisted {
			_ = os.Remove(dir)
		}
	}, nil
}

func cursorMCPDenyPermissions(tokens []string) []string {
	byServer := parseMCPDenyTokens(tokens)
	out := make([]string, 0)
	for server, tools := range byServer {
		for _, tool := range tools {
			out = append(out, fmt.Sprintf("Mcp(%s:%s)", server, tool))
		}
	}
	sort.Strings(out)
	return out
}

func parseMCPDenyTokens(tokens []string) map[string][]string {
	sets := map[string]map[string]struct{}{}
	for _, token := range tokens {
		rest := strings.TrimPrefix(strings.TrimSpace(token), "mcp__")
		server, tool, ok := strings.Cut(rest, "__")
		if !ok || server == "" || tool == "" {
			continue
		}
		if sets[server] == nil {
			sets[server] = map[string]struct{}{}
		}
		sets[server][tool] = struct{}{}
	}

	out := make(map[string][]string, len(sets))
	for server, tools := range sets {
		names := make([]string, 0, len(tools))
		for tool := range tools {
			names = append(names, tool)
		}
		sort.Strings(names)
		out[server] = names
	}
	return out
}

func addCursorPermissionDenies(cfg map[string]any, denyRules []string) {
	permissions, _ := cfg["permissions"].(map[string]any)
	if permissions == nil {
		permissions = map[string]any{}
	}
	seen := map[string]struct{}{}
	if existing, ok := permissions["deny"].([]any); ok {
		for _, item := range existing {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				seen[s] = struct{}{}
			}
		}
	}
	for _, rule := range denyRules {
		seen[rule] = struct{}{}
	}
	deny := make([]string, 0, len(seen))
	for rule := range seen {
		deny = append(deny, rule)
	}
	sort.Strings(deny)
	permissions["deny"] = deny
	cfg["permissions"] = permissions
}
