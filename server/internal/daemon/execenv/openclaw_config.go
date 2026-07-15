package execenv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const openclawConfigFile = "openclaw-config.json"
const openclawUserSnapshotFile = "openclaw-user-snapshot.json"

// OpenclawConfigPrep contains task-authoritative OpenClaw overrides. The
// selected binary is deliberately ignored during preparation: it is not safe
// to execute Issue-selected code before the task environment is isolated.
type OpenclawConfigPrep struct {
	OpenclawBin string
	McpConfig   json.RawMessage
	Gateway     OpenclawGatewayPin
}

type OpenclawGatewayPin struct {
	Host  string
	Port  int
	Token string
	TLS   bool
}

func (p OpenclawGatewayPin) IsZero() bool {
	return p == OpenclawGatewayPin{}
}

func (p OpenclawGatewayPin) String() string {
	token := ""
	if p.Token != "" {
		token = "***"
	}
	return fmt.Sprintf("OpenclawGatewayPin{Host:%q Port:%d Token:%s TLS:%t}", p.Host, p.Port, token, p.TLS)
}

func (p OpenclawGatewayPin) MarshalJSON() ([]byte, error) {
	type alias struct {
		Host  string `json:"host,omitempty"`
		Port  int    `json:"port,omitempty"`
		Token string `json:"token,omitempty"`
		TLS   bool   `json:"tls,omitempty"`
	}
	masked := alias{Host: p.Host, Port: p.Port, TLS: p.TLS}
	if p.Token != "" {
		masked.Token = "***"
	}
	return json.Marshal(masked)
}

type OpenclawConfigResult struct {
	ConfigPath string
}

// prepareOpenclawConfig creates a task-private OpenClaw wrapper without
// invoking OpenClaw. Existing owner configuration is read through the
// daemon's stable bounded reader, parsed as strict JSON, and projected onto a
// small allowlist. Unsupported loader features fail closed instead of being
// resolved by task-selected executable code.
func prepareOpenclawConfig(envRoot, workDir string, opts OpenclawConfigPrep) (OpenclawConfigResult, error) {
	managedMCP, hasManagedMCP, err := openclawManagedMcpServers(opts.McpConfig)
	if err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("render openclaw mcp_config: %w", err)
	}

	activePath, exists, err := openclawFallbackActiveConfigPath()
	if err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("locate openclaw active config: %w", err)
	}

	snapshotPath := ""
	if exists {
		raw, err := readStableRegularFile(activePath, openclawSnapshotMaxBytes, nil)
		if err != nil {
			return OpenclawConfigResult{}, fmt.Errorf("read openclaw config: %w", err)
		}
		owner, err := parseStrictOpenclawConfig(raw)
		if err != nil {
			return OpenclawConfigResult{}, fmt.Errorf("parse openclaw config: %w", err)
		}
		snapshot, err := allowlistedOpenclawSnapshot(owner, workDir)
		if err != nil {
			return OpenclawConfigResult{}, fmt.Errorf("sanitize openclaw config: %w", err)
		}
		if len(snapshot) > 0 {
			data, err := json.MarshalIndent(snapshot, "", "  ")
			if err != nil {
				return OpenclawConfigResult{}, fmt.Errorf("marshal openclaw snapshot: %w", err)
			}
			snapshotPath = filepath.Join(envRoot, openclawUserSnapshotFile)
			if err := writePrivateSnapshot(snapshotPath, data, openclawSnapshotMaxBytes); err != nil {
				return OpenclawConfigResult{}, fmt.Errorf("write openclaw snapshot: %w", err)
			}
		}
	}

	wrapper := buildPerTaskOpenclawConfig(snapshotPath, workDir, managedMCP, hasManagedMCP, opts.Gateway)
	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("marshal openclaw config: %w", err)
	}
	outPath := filepath.Join(envRoot, openclawConfigFile)
	if err := writePrivateSnapshot(outPath, data, openclawSnapshotMaxBytes); err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("write openclaw config: %w", err)
	}
	return OpenclawConfigResult{ConfigPath: outPath}, nil
}

func parseStrictOpenclawConfig(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var config map[string]any
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	if config == nil {
		return nil, fmt.Errorf("root must be a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values are not supported")
		}
		return nil, err
	}
	if err := rejectOpenclawLoaderFeatures(config); err != nil {
		return nil, err
	}
	return config, nil
}

func rejectOpenclawLoaderFeatures(value any) error {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			if key == "$include" {
				return fmt.Errorf("$include is unsupported during isolated preparation")
			}
			if strings.Contains(key, "${") {
				return fmt.Errorf("environment substitution is unsupported during isolated preparation")
			}
			if err := rejectOpenclawLoaderFeatures(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range current {
			if err := rejectOpenclawLoaderFeatures(child); err != nil {
				return err
			}
		}
	case string:
		if strings.Contains(current, "${") {
			return fmt.Errorf("environment substitution is unsupported during isolated preparation")
		}
	}
	return nil
}

func allowlistedOpenclawSnapshot(owner map[string]any, workDir string) (map[string]any, error) {
	snapshot := make(map[string]any)
	for _, key := range []string{"models", "providers", "gateway"} {
		value, ok := owner[key]
		if !ok {
			continue
		}
		if key == "providers" {
			providers := projectOpenclawPublicProviders(value)
			if len(providers) == 0 {
				continue
			}
			value = providers
		}
		if err := rejectOwnerAbsolutePaths(value); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		snapshot[key] = value
	}

	if rawAgents, ok := owner["agents"].(map[string]any); ok {
		agents := make(map[string]any)
		defaults := make(map[string]any)
		if rawDefaults, ok := rawAgents["defaults"].(map[string]any); ok {
			copyOpenclawAllowedFields(defaults, rawDefaults, []string{
				"model", "imageModel", "thinking", "reasoning", "temperature", "maxTokens",
			})
		}
		if err := rejectOwnerAbsolutePaths(defaults); err != nil {
			return nil, fmt.Errorf("agents.defaults: %w", err)
		}
		defaults["workspace"] = workDir
		agents["defaults"] = defaults

		if rawList, ok := rawAgents["list"].([]any); ok {
			list := make([]any, 0, len(rawList))
			for index, rawEntry := range rawList {
				entry, ok := rawEntry.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("agents.list.%d must be an object", index)
				}
				filtered := make(map[string]any)
				copyOpenclawAllowedFields(filtered, entry, []string{"id", "name", "default", "isDefault", "model"})
				if err := rejectOwnerAbsolutePaths(filtered); err != nil {
					return nil, fmt.Errorf("agents.list.%d: %w", index, err)
				}
				filtered["workspace"] = workDir
				list = append(list, filtered)
			}
			if len(list) > 0 {
				agents["list"] = list
			}
		}
		snapshot["agents"] = agents
	}
	return snapshot, nil
}

func projectOpenclawPublicProviders(value any) map[string]any {
	rawProviders, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}

	providers := make(map[string]any, len(rawProviders))
	for name, value := range rawProviders {
		rawProvider, ok := value.(map[string]any)
		if !ok {
			continue
		}
		projected := projectOpenclawPublicProvider(rawProvider)
		if len(projected) > 0 {
			providers[name] = projected
		}
	}
	return providers
}

func projectOpenclawPublicProvider(provider map[string]any) map[string]any {
	projected := make(map[string]any)
	for _, key := range []string{
		"baseUrl", "baseURL", "endpoint", "id", "name", "organization", "organizationId",
		"project", "projectId", "region", "type",
	} {
		if value, ok := provider[key].(string); ok {
			projected[key] = value
		}
	}
	if value, ok := provider["enabled"].(bool); ok {
		projected["enabled"] = value
	}
	return projected
}

func copyOpenclawAllowedFields(dst, src map[string]any, keys []string) {
	for _, key := range keys {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}

func rejectOwnerAbsolutePaths(value any) error {
	switch current := value.(type) {
	case map[string]any:
		for _, child := range current {
			if err := rejectOwnerAbsolutePaths(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range current {
			if err := rejectOwnerAbsolutePaths(child); err != nil {
				return err
			}
		}
	case string:
		if looksLikeAbsolutePath(current) {
			return fmt.Errorf("owner absolute path is not allowed")
		}
	}
	return nil
}

func looksLikeAbsolutePath(value string) bool {
	if filepath.IsAbs(value) || strings.HasPrefix(value, `\\`) {
		return true
	}
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) &&
		value[1] == ':' && (value[2] == '/' || value[2] == '\\')
}

func buildPerTaskOpenclawConfig(snapshotPath, workDir string, managedMCP map[string]any, hasManagedMCP bool, gateway OpenclawGatewayPin) map[string]any {
	config := map[string]any{
		"agents": map[string]any{
			"defaults": map[string]any{"workspace": workDir},
		},
	}
	if snapshotPath != "" {
		config["$include"] = []any{snapshotPath}
	}
	if hasManagedMCP {
		if managedMCP == nil {
			managedMCP = map[string]any{}
		}
		config["mcp"] = map[string]any{"servers": managedMCP}
	}
	if override := buildGatewayOverride(gateway); override != nil {
		config["gateway"] = override
	}
	return config
}

func buildGatewayOverride(pin OpenclawGatewayPin) map[string]any {
	if pin.IsZero() {
		return nil
	}
	override := make(map[string]any)
	if pin.Host != "" {
		override["host"] = pin.Host
	}
	if pin.Port != 0 {
		override["port"] = pin.Port
	}
	if pin.TLS {
		override["tls"] = true
	}
	if pin.Token != "" {
		override["auth"] = map[string]any{"mode": "token", "token": pin.Token}
	}
	if len(override) == 0 {
		return nil
	}
	return override
}

func openclawFallbackActiveConfigPath() (string, bool, error) {
	if explicit := strings.TrimSpace(os.Getenv("OPENCLAW_CONFIG_PATH")); explicit != "" {
		path, err := expandOpenclawPath(explicit)
		if err != nil {
			return "", false, err
		}
		return openclawStatConfigPath(path)
	}

	candidates, canonical, err := openclawFallbackConfigCandidates()
	if err != nil {
		return "", false, err
	}
	for _, candidate := range candidates {
		path, err := expandOpenclawPath(candidate)
		if err != nil {
			return "", false, err
		}
		exists, err := openclawConfigPathExists(path)
		if err != nil {
			return "", false, err
		}
		if exists {
			return path, true, nil
		}
	}
	return openclawStatConfigPath(canonical)
}

var openclawFallbackConfigFileNames = []string{"openclaw.json", "clawdbot.json", "moltbot.json", "moldbot.json"}
var openclawFallbackConfigDirNames = []string{".openclaw", ".clawdbot", ".moltbot", ".moldbot"}

func openclawFallbackConfigCandidates() ([]string, string, error) {
	var candidates []string
	if path := strings.TrimSpace(os.Getenv("CLAWDBOT_CONFIG_PATH")); path != "" {
		candidates = append(candidates, path)
	}
	for _, key := range []string{"OPENCLAW_STATE_DIR", "CLAWDBOT_STATE_DIR"} {
		if dir := strings.TrimSpace(os.Getenv(key)); dir != "" {
			candidates = appendOpenclawConfigFileCandidates(candidates, dir)
		}
	}

	home := strings.TrimSpace(os.Getenv("OPENCLAW_HOME"))
	var err error
	if home == "" {
		home, err = os.UserHomeDir()
	} else {
		home, err = expandOpenclawPath(home)
	}
	if err != nil {
		return nil, "", fmt.Errorf("resolve openclaw home: %w", err)
	}
	for _, dir := range openclawFallbackConfigDirNames {
		candidates = appendOpenclawConfigFileCandidates(candidates, filepath.Join(home, dir))
	}
	return candidates, filepath.Join(home, ".openclaw", "openclaw.json"), nil
}

func appendOpenclawConfigFileCandidates(candidates []string, dir string) []string {
	for _, name := range openclawFallbackConfigFileNames {
		candidates = append(candidates, filepath.Join(dir, name))
	}
	return candidates
}

func expandOpenclawPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand openclaw config path: %w", err)
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	if !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve openclaw config path: %w", err)
		}
		path = absolute
	}
	return filepath.Clean(path), nil
}

func openclawStatConfigPath(path string) (string, bool, error) {
	if !filepath.IsAbs(path) {
		return "", false, fmt.Errorf("openclaw config path must be absolute")
	}
	exists, err := openclawConfigPathExists(path)
	return path, exists, err
}

func openclawConfigPathExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect openclaw config: %w", err)
	}
	if info.IsDir() {
		return false, fmt.Errorf("openclaw config path is a directory")
	}
	return true, nil
}

func openclawManagedMcpServers(raw json.RawMessage) (map[string]any, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false, nil
	}
	var parsed struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil, false, fmt.Errorf("parse mcp_config json: %w", err)
	}
	if len(parsed.McpServers) == 0 {
		return map[string]any{}, true, nil
	}
	names := make([]string, 0, len(parsed.McpServers))
	for name := range parsed.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string]any, len(names))
	for _, name := range names {
		var entry map[string]any
		if err := json.Unmarshal(parsed.McpServers[name], &entry); err != nil {
			return nil, false, fmt.Errorf("mcp_servers.%s: %w", name, err)
		}
		if entry == nil {
			return nil, false, fmt.Errorf("mcp_servers.%s must be a JSON object", name)
		}
		command, _ := entry["command"].(string)
		url, _ := entry["url"].(string)
		if strings.TrimSpace(command) == "" && strings.TrimSpace(url) == "" {
			return nil, false, fmt.Errorf("mcp_servers.%s must declare command or url", name)
		}
		out[name] = entry
	}
	return out, true, nil
}
