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
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
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

	// cursorMcpAuthStoreRoot is the directory under the daemon's Multica
	// profile dir that holds each agent's persistent Cursor MCP OAuth state.
	cursorMcpAuthStoreRoot = "cursor-mcp-auth"
)

// CursorMcpAuthStorePath returns the agent's persistent Cursor mcp-auth.json on
// this machine, or "" when there is no agent to key on or the Multica profile
// dir cannot be resolved.
//
// Cursor keys MCP OAuth state by project directory, and every task runs in a
// new one, so without a store an MCP login made in one task never reaches the
// next. The store is linked into each task rather than copied: Cursor rewrites
// the file in place, so all tasks of the agent spend the same single-use
// refresh tokens instead of rotating each other out. It is separate from the
// user's own ~/.cursor login, which therefore never gets signed out by a task.
// Concurrent tasks of one agent may still race a refresh; the store is
// last-writer-wins, like Cursor's own file.
func CursorMcpAuthStorePath(daemonProfile, agentID string) string {
	agent := sanitizePathSegment(agentID)
	if agent == "" {
		return ""
	}
	profileDir, err := cli.ProfileDir(daemonProfile)
	if err != nil {
		return ""
	}
	return filepath.Join(profileDir, cursorMcpAuthStoreRoot, agent, cursorMcpAuthFile)
}

type cursorMcpConfigFile struct {
	McpServers map[string]json.RawMessage `json:"mcpServers"`
}

type cursorStdioMcpApprovalServer struct {
	Type    json.RawMessage `json:"type,omitempty"`
	Command json.RawMessage `json:"command"`
	Args    json.RawMessage `json:"args,omitempty"`
	Env     json.RawMessage `json:"env,omitempty"`
	Cwd     json.RawMessage `json:"cwd,omitempty"`
}

type cursorRemoteMcpApprovalServer struct {
	Type    json.RawMessage `json:"type,omitempty"`
	URL     json.RawMessage `json:"url"`
	Headers json.RawMessage `json:"headers,omitempty"`
}

// prepareCursorMcpConfig writes the Cursor-native MCP sidecars for agents that
// have an explicit managed mcp_config saved. A nil/null mcp_config means "let
// Cursor behave normally", so no .cursor/mcp.json or CURSOR_DATA_DIR is created.
//
// The task's mcp-auth.json is rebuilt on every run: an explicit mcpAuthSource
// wins, otherwise it links to the agent's mcpAuthStore, otherwise it stays
// task-local.
func prepareCursorMcpConfig(envRoot, workDir string, mcpConfig json.RawMessage, mcpAuthSource, mcpAuthStore string, manifest *sidecarManifest, logger *slog.Logger) (string, error) {
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
	switch {
	case strings.TrimSpace(mcpAuthSource) != "":
		if err := seedCursorMcpAuthFile(projectDataDir, mcpAuthSource); err != nil {
			return "", err
		}
	case mcpAuthStore != "":
		if err := linkCursorMcpAuthStore(projectDataDir, mcpAuthStore); err != nil {
			if logger != nil {
				logger.Warn("execenv: link cursor mcp auth store failed; MCP logins stay task-local", "store", mcpAuthStore, "error", err)
			}
		}
	}

	return cursorDataDir, nil
}

// CursorMcpAuthPath is the mcp-auth.json Cursor reads for a task's workdir
// under cursorDataDir.
func CursorMcpAuthPath(cursorDataDir, workDir string) string {
	return filepath.Join(cursorDataDir, "projects", cursorSlugifyPath(cursorProjectRoot(workDir)), cursorMcpAuthFile)
}

// CursorMcpServersNeedingLogin returns, sorted, the MCP servers whose OAuth
// login is missing in the task's mcp-auth.json. When a server answers with an
// OAuth challenge, Cursor registers a client and records it under the server
// name; until someone logs in, that entry has no tokens. Servers that never
// asked for OAuth have no entry. A missing or unreadable file reports nothing.
func CursorMcpServersNeedingLogin(cursorDataDir, workDir string) []string {
	if cursorDataDir == "" {
		return nil
	}
	data, err := os.ReadFile(CursorMcpAuthPath(cursorDataDir, workDir))
	if err != nil {
		return nil
	}
	var entries map[string]struct {
		Tokens *struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	var names []string
	for name, entry := range entries {
		if entry.Tokens == nil || (entry.Tokens.AccessToken == "" && entry.Tokens.RefreshToken == "") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// PrepareCursorMcpLoginWorkspace builds the agent's standing Cursor project for
// `cursor-agent mcp login`, next to its auth store, and returns its workdir and
// CURSOR_DATA_DIR. A task's own directory is no place to log in from: local
// directory runs remove the generated .cursor/mcp.json at cleanup and worktree
// runs remove the whole workdir. Only the managed servers that need a login are
// written, and the project's mcp-auth.json resolves to the same login the
// agent's tasks use (mcpAuthSource, else mcpAuthStore). Servers from the user's
// own ~/.cursor configuration load from any directory.
func PrepareCursorMcpLoginWorkspace(mcpAuthSource, mcpAuthStore string, mcpConfig json.RawMessage, servers []string) (string, string, error) {
	if mcpAuthStore == "" {
		return "", "", fmt.Errorf("cursor mcp auth store is required")
	}
	envRoot := filepath.Join(filepath.Dir(mcpAuthStore), "login")
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(envRoot, 0o700); err != nil {
		return "", "", fmt.Errorf("create cursor mcp login workspace: %w", err)
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create cursor mcp login workspace: %w", err)
	}
	// Cursor treats the nearest enclosing git repository as the project, so a
	// repository above the profile dir would send the login somewhere else.
	resolved, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve cursor mcp login workspace: %w", err)
	}
	if root := cursorProjectRoot(workDir); root != resolved {
		return "", "", fmt.Errorf("cursor mcp login workspace resolves to enclosing project %s", root)
	}

	managed, err := parseCursorManagedMcpServers(mcpConfig)
	if err != nil && hasManagedCursorMcpConfig(mcpConfig) {
		return "", "", err
	}
	selected := map[string]json.RawMessage{}
	for _, name := range servers {
		if server, ok := managed[name]; ok {
			selected[name] = server
		}
	}
	loginConfig, err := json.Marshal(cursorMcpConfigFile{McpServers: selected})
	if err != nil {
		return "", "", fmt.Errorf("marshal cursor mcp login config: %w", err)
	}
	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, loginConfig, mcpAuthSource, mcpAuthStore, nil, nil)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Lstat(CursorMcpAuthPath(cursorDataDir, workDir)); err != nil {
		return "", "", fmt.Errorf("link cursor mcp login workspace to the agent's login: %w", err)
	}
	return workDir, cursorDataDir, nil
}

// linkCursorMcpAuthStore points the task's mcp-auth.json at the agent's
// persistent store. A hard link is the fallback where symlinks need privileges
// (Windows); it still shares one file because Cursor rewrites it in place.
func linkCursorMcpAuthStore(projectDataDir, store string) error {
	if err := ensureCursorMcpAuthStore(store); err != nil {
		return err
	}
	target := filepath.Join(projectDataDir, cursorMcpAuthFile)
	symlinkErr := os.Symlink(store, target)
	if symlinkErr == nil {
		return nil
	}
	if err := os.Link(store, target); err != nil {
		return errors.Join(symlinkErr, err)
	}
	return nil
}

func ensureCursorMcpAuthStore(store string) error {
	if err := os.MkdirAll(filepath.Dir(store), 0o700); err != nil {
		return fmt.Errorf("create cursor mcp auth store dir: %w", err)
	}
	f, err := os.OpenFile(store, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create cursor mcp auth store: %w", err)
	}
	_, writeErr := f.WriteString("{}\n")
	if closeErr := f.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(store)
		return fmt.Errorf("create cursor mcp auth store: %w", writeErr)
	}
	return nil
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
		server, err := marshalCursorMcpApprovalServer(servers[name])
		if err != nil {
			return nil, fmt.Errorf("marshal mcp_servers.%s for cursor approval: %w", name, err)
		}
		pathJSON, err := json.Marshal(projectRoot)
		if err != nil {
			return nil, fmt.Errorf("marshal cursor project root: %w", err)
		}
		payload := []byte(`{"path":`)
		payload = append(payload, pathJSON...)
		payload = append(payload, []byte(`,"server":`)...)
		payload = append(payload, server...)
		payload = append(payload, '}')

		sum := sha256.Sum256(payload)
		approvals = append(approvals, name+"-"+hex.EncodeToString(sum[:])[:16])
	}
	return approvals, nil
}

func marshalCursorMcpApprovalServer(raw json.RawMessage) ([]byte, error) {
	compact := &bytes.Buffer{}
	if err := json.Compact(compact, raw); err != nil {
		return nil, err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(compact.Bytes(), &fields); err != nil {
		return nil, err
	}
	if command, isStdio := fields["command"]; isStdio {
		// Cursor normalizes stdio servers to this exact field set and order
		// before hashing; unknown fields are dropped and absent fields omitted.
		return marshalJSONStringifyCompatible(cursorStdioMcpApprovalServer{
			Type:    fields["type"],
			Command: command,
			Args:    fields["args"],
			Env:     fields["env"],
			Cwd:     fields["cwd"],
		})
	}
	if url, isRemote := fields["url"]; isRemote {
		// Remote servers use a separate normalized shape before hashing.
		return marshalJSONStringifyCompatible(cursorRemoteMcpApprovalServer{
			Type:    fields["type"],
			URL:     url,
			Headers: fields["headers"],
		})
	}
	return compact.Bytes(), nil
}

// marshalJSONStringifyCompatible serializes a value without Go's default HTML
// escaping so approval payload bytes match Cursor's JSON.stringify output.
func marshalJSONStringifyCompatible(value any) ([]byte, error) {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
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
