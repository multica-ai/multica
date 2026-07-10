package execenv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// CursorMcpAuthSourceEnv is an agent custom_env key the daemon consumes
	// before launching cursor-agent. When set, it must point at an explicit
	// mcp-auth.json file, or at a Cursor project data directory containing one.
	// The key is intentionally not MULTICA_* because custom_env blocks those
	// from being set by users.
	CursorMcpAuthSourceEnv = "CURSOR_MCP_AUTH_SOURCE"

	cursorWorkspaceTrustedFile = ".workspace-trusted"
	cursorMcpAuthFile          = "mcp-auth.json"
)

type cursorMcpConfigFile struct {
	McpServers map[string]json.RawMessage `json:"mcpServers"`
}

// prepareCursorMcpConfig writes the Cursor-native MCP sidecars for agents that
// have an explicit managed mcp_config saved. A nil/null mcp_config means "let
// Cursor behave normally", so no .cursor/mcp.json or CURSOR_DATA_DIR is created.
func prepareCursorMcpConfig(envRoot, workDir string, mcpConfig json.RawMessage, mcpAuthSource string, manifest *sidecarManifest) (string, error) {
	if !hasManagedCursorMcpConfig(mcpConfig) {
		return "", nil
	}
	if envRoot == "" {
		return "", fmt.Errorf("env root is required for managed cursor mcp_config")
	}

	projectRoot := cursorProjectRoot(workDir)
	servers, err := parseCursorManagedMcpServers(mcpConfig)
	if err != nil {
		return "", err
	}

	cursorDir := filepath.Join(projectRoot, ".cursor")
	if err := recordMkdirAll(cursorDir, 0o755, manifest); err != nil {
		return "", fmt.Errorf("create .cursor dir: %w", err)
	}
	configData, err := marshalCursorMcpConfig(servers)
	if err != nil {
		return "", err
	}
	if err := recordWriteFile(filepath.Join(cursorDir, "mcp.json"), configData, 0o600, manifest); err != nil {
		if errors.Is(err, errPathPreExists) {
			return "", fmt.Errorf("managed cursor mcp_config would overwrite existing .cursor/mcp.json")
		}
		return "", fmt.Errorf("write .cursor/mcp.json: %w", err)
	}

	cursorDataDir := filepath.Join(envRoot, "cursor-data")
	projectDataDir := filepath.Join(cursorDataDir, "projects", cursorSlugifyPath(projectRoot))
	if err := os.MkdirAll(projectDataDir, 0o700); err != nil {
		return "", fmt.Errorf("create cursor project data dir: %w", err)
	}
	if err := removeCursorMcpAuthFile(projectDataDir); err != nil {
		return "", err
	}
	approvals, err := cursorMcpApprovalKeys(projectRoot, servers)
	if err != nil {
		return "", err
	}
	approvalData, err := json.MarshalIndent(approvals, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal cursor mcp approvals: %w", err)
	}
	if err := os.WriteFile(filepath.Join(projectDataDir, "mcp-approvals.json"), approvalData, 0o600); err != nil {
		return "", fmt.Errorf("write cursor mcp approvals: %w", err)
	}
	trustData, err := json.MarshalIndent(map[string]string{
		"trustedAt":     "1970-01-01T00:00:00Z",
		"workspacePath": projectRoot,
		"trustMethod":   "multica-managed",
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal cursor workspace trust: %w", err)
	}
	if err := os.WriteFile(filepath.Join(projectDataDir, cursorWorkspaceTrustedFile), trustData, 0o600); err != nil {
		return "", fmt.Errorf("write cursor workspace trust: %w", err)
	}
	if strings.TrimSpace(mcpAuthSource) != "" {
		if err := seedCursorMcpAuthFile(projectDataDir, mcpAuthSource); err != nil {
			return "", err
		}
	}

	return cursorDataDir, nil
}

func seedCursorMcpAuthFile(projectDataDir, source string) error {
	sourcePath, err := resolveCursorMcpAuthSource(source)
	if err != nil {
		return err
	}
	target := filepath.Join(projectDataDir, cursorMcpAuthFile)
	if err := os.Symlink(sourcePath, target); err == nil {
		return nil
	}
	if err := copyCursorMcpAuthFile(target, sourcePath); err != nil {
		return fmt.Errorf("seed cursor mcp auth file: %w", err)
	}
	return nil
}

func removeCursorMcpAuthFile(projectDataDir string) error {
	target := filepath.Join(projectDataDir, cursorMcpAuthFile)
	if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove prior cursor mcp auth file: %w", err)
	}
	return nil
}

func resolveCursorMcpAuthSource(source string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", fmt.Errorf("%s is empty", CursorMcpAuthSourceEnv)
	}
	if source == "~" || strings.HasPrefix(source, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve %s home directory: %w", CursorMcpAuthSourceEnv, err)
		}
		if source == "~" {
			source = home
		} else {
			source = filepath.Join(home, source[2:])
		}
	}
	if !filepath.IsAbs(source) {
		return "", fmt.Errorf("%s must be an absolute path to %s or its containing Cursor project directory", CursorMcpAuthSourceEnv, cursorMcpAuthFile)
	}
	source = filepath.Clean(source)
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", CursorMcpAuthSourceEnv, err)
	}
	if info.IsDir() {
		source = filepath.Join(source, cursorMcpAuthFile)
		info, err = os.Stat(source)
		if err != nil {
			return "", fmt.Errorf("stat %s %s: %w", CursorMcpAuthSourceEnv, cursorMcpAuthFile, err)
		}
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s must resolve to a file, got directory %s", CursorMcpAuthSourceEnv, source)
	}
	if filepath.Base(source) != cursorMcpAuthFile {
		return "", fmt.Errorf("%s must point at %s, got %s", CursorMcpAuthSourceEnv, cursorMcpAuthFile, filepath.Base(source))
	}
	return source, nil
}

func copyCursorMcpAuthFile(target, source string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return closeErr
	}
	return nil
}

func hasManagedCursorMcpConfig(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func parseCursorManagedMcpServers(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var cfg struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse mcp_config json: %w", err)
	}
	if cfg.McpServers == nil {
		return map[string]json.RawMessage{}, nil
	}
	for name, server := range cfg.McpServers {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("mcp server name must not be empty")
		}
		var obj map[string]any
		if err := json.Unmarshal(server, &obj); err != nil {
			return nil, fmt.Errorf("mcp_servers.%s: %w", name, err)
		}
		if obj == nil {
			return nil, fmt.Errorf("mcp_servers.%s must be a JSON object", name)
		}
	}
	return cfg.McpServers, nil
}

func marshalCursorMcpConfig(servers map[string]json.RawMessage) ([]byte, error) {
	if servers == nil {
		servers = map[string]json.RawMessage{}
	}
	data, err := json.MarshalIndent(cursorMcpConfigFile{McpServers: servers}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal cursor mcp config: %w", err)
	}
	return append(data, '\n'), nil
}

func cursorMcpApprovalKeys(projectRoot string, servers map[string]json.RawMessage) ([]string, error) {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	approvals := make([]string, 0, len(names))
	for _, name := range names {
		compact := &bytes.Buffer{}
		if err := json.Compact(compact, servers[name]); err != nil {
			return nil, fmt.Errorf("compact mcp_servers.%s: %w", name, err)
		}
		pathJSON, err := json.Marshal(projectRoot)
		if err != nil {
			return nil, fmt.Errorf("marshal cursor project root: %w", err)
		}
		payload := []byte(`{"path":`)
		payload = append(payload, pathJSON...)
		payload = append(payload, []byte(`,"server":`)...)
		payload = append(payload, compact.Bytes()...)
		payload = append(payload, '}')

		sum := sha256.Sum256(payload)
		approvals = append(approvals, name+"-"+hex.EncodeToString(sum[:])[:16])
	}
	return approvals, nil
}

func cursorProjectRoot(workDir string) string {
	if workDir == "" {
		return workDir
	}
	dir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		dir = workDir
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		dir = workDir
	}
	fallback := dir
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fallback
		}
		dir = parent
	}
}

func cursorSlugifyPath(path string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range path {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
