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

	// cursorMcpAuthLinkRecordFile, in a task's env root, records the link the
	// task placed in the user's Cursor data dir so the GC can remove it.
	cursorMcpAuthLinkRecordFile = "cursor-mcp-auth-link.json"
)

// CursorUserDataDir returns the Cursor data directory a cursor-agent launched
// without a per-task CURSOR_DATA_DIR uses: the daemon's own CURSOR_DATA_DIR,
// else ~/.cursor. Empty when neither resolves to an absolute path.
func CursorUserDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("CURSOR_DATA_DIR")); dir != "" {
		if !filepath.IsAbs(dir) {
			return ""
		}
		return filepath.Clean(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".cursor")
}

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
	root := cursorMcpAuthStoreRootDir(daemonProfile)
	if root == "" {
		return ""
	}
	return filepath.Join(root, agent, cursorMcpAuthFile)
}

func cursorMcpAuthStoreRootDir(daemonProfile string) string {
	profileDir, err := cli.ProfileDir(daemonProfile)
	if err != nil {
		return ""
	}
	return filepath.Join(profileDir, cursorMcpAuthStoreRoot)
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
	var names []string
	for name, loggedIn := range cursorMcpAuthLogins(CursorMcpAuthPath(cursorDataDir, workDir)) {
		if !loggedIn {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// cursorMcpAuthLogins reports, per server recorded in a Cursor mcp-auth.json,
// whether it holds a login. A missing or unreadable file reports nil.
func cursorMcpAuthLogins(path string) map[string]bool {
	data, err := os.ReadFile(path)
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
	logins := make(map[string]bool, len(entries))
	for name, entry := range entries {
		logins[name] = entry.Tokens != nil && (entry.Tokens.AccessToken != "" || entry.Tokens.RefreshToken != "")
	}
	return logins
}

func cursorMcpAuthHasLogin(path string) bool {
	for _, loggedIn := range cursorMcpAuthLogins(path) {
		if loggedIn {
			return true
		}
	}
	return false
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
	if _, err := prepareCursorMcpLoginWorkDir(envRoot, workDir); err != nil {
		return "", "", err
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

// PrepareCursorMcpUserLoginWorkspace is PrepareCursorMcpLoginWorkspace for
// agents without managed MCP, whose tasks run on the user's Cursor data dir. It
// returns a standing workdir whose project in userDataDir resolves to the same
// login the agent's tasks use. The servers come from the user's own Cursor
// configuration, so nothing is written into the workdir.
func PrepareCursorMcpUserLoginWorkspace(userDataDir, mcpAuthSource, mcpAuthStore string) (string, error) {
	if userDataDir == "" || mcpAuthStore == "" {
		return "", fmt.Errorf("cursor user data dir and mcp auth store are required")
	}
	envRoot := filepath.Join(filepath.Dir(mcpAuthStore), "user-login")
	workDir := filepath.Join(envRoot, "workdir")
	projectRoot, err := prepareCursorMcpLoginWorkDir(envRoot, workDir)
	if err != nil {
		return "", err
	}
	projectDataDir := filepath.Join(userDataDir, "projects", cursorSlugifyPath(projectRoot))
	if _, err := linkCursorProjectMcpAuth(projectDataDir, mcpAuthSource, mcpAuthStore); err != nil {
		return "", fmt.Errorf("link cursor mcp login workspace to the agent's login: %w", err)
	}
	return workDir, nil
}

// prepareCursorMcpLoginWorkDir creates a standing login workdir and returns it
// resolved, which is also its Cursor project root.
func prepareCursorMcpLoginWorkDir(envRoot, workDir string) (string, error) {
	if err := os.MkdirAll(envRoot, 0o700); err != nil {
		return "", fmt.Errorf("create cursor mcp login workspace: %w", err)
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", fmt.Errorf("create cursor mcp login workspace: %w", err)
	}
	// Cursor treats the nearest enclosing git repository as the project, so a
	// repository above the profile dir would send the login somewhere else.
	resolved, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		return "", fmt.Errorf("resolve cursor mcp login workspace: %w", err)
	}
	if root := cursorProjectRoot(workDir); root != resolved {
		return "", fmt.Errorf("cursor mcp login workspace resolves to enclosing project %s", root)
	}
	return resolved, nil
}

// linkCursorUserMcpAuth links the mcp-auth.json Cursor keeps for a task's
// project in the user's Cursor data dir to the agent's login, for tasks without
// managed MCP. It returns userDataDir once the link is in place, else "".
//
// Only a project inside the task's env root is touched. Any other project (an
// in-place local directory, or a git repository enclosing the workdir) is one
// the user works in too, and its mcp-auth.json is the user's own login.
func linkCursorUserMcpAuth(userDataDir, envRoot, workDir, mcpAuthSource, mcpAuthStore string, logger *slog.Logger) string {
	if userDataDir == "" || envRoot == "" || workDir == "" || (strings.TrimSpace(mcpAuthSource) == "" && mcpAuthStore == "") {
		return ""
	}
	projectRoot := cursorProjectRoot(workDir)
	if !cursorPathWithin(envRoot, projectRoot) {
		return ""
	}
	projectDataDir := filepath.Join(userDataDir, "projects", cursorSlugifyPath(projectRoot))
	target, err := linkCursorProjectMcpAuth(projectDataDir, mcpAuthSource, mcpAuthStore)
	if err == nil {
		err = writeCursorMcpAuthLinkRecord(envRoot, filepath.Join(projectDataDir, cursorMcpAuthFile), target)
		if err != nil {
			_ = removeCursorMcpAuthFile(projectDataDir)
		}
	}
	if err != nil {
		if logger != nil {
			logger.Warn("execenv: link cursor mcp auth into user data dir failed; MCP logins stay task-local", "project", projectDataDir, "error", err)
		}
		return ""
	}
	return userDataDir
}

// linkCursorProjectMcpAuth points a Cursor project's mcp-auth.json at the
// explicit mcpAuthSource, else at the agent's mcpAuthStore, and returns what it
// points at.
func linkCursorProjectMcpAuth(projectDataDir, mcpAuthSource, mcpAuthStore string) (string, error) {
	if err := os.MkdirAll(projectDataDir, 0o700); err != nil {
		return "", fmt.Errorf("create cursor project data dir: %w", err)
	}
	if strings.TrimSpace(mcpAuthSource) != "" {
		source, err := resolveCursorMcpAuthSource(mcpAuthSource)
		if err != nil {
			return "", err
		}
		if err := removeCursorMcpAuthFile(projectDataDir); err != nil {
			return "", err
		}
		if err := seedCursorMcpAuthFile(projectDataDir, source); err != nil {
			return "", err
		}
		return source, nil
	}
	if err := ensureCursorMcpAuthStore(mcpAuthStore); err != nil {
		return "", err
	}
	if err := adoptCursorMcpAuthLogin(filepath.Join(projectDataDir, cursorMcpAuthFile), mcpAuthStore); err != nil {
		return "", err
	}
	if err := removeCursorMcpAuthFile(projectDataDir); err != nil {
		return "", err
	}
	if err := linkCursorMcpAuthStore(projectDataDir, mcpAuthStore); err != nil {
		return "", err
	}
	return mcpAuthStore, nil
}

// adoptCursorMcpAuthLogin carries a login Cursor already saved in a project's
// own mcp-auth.json into the agent's store while the store holds none, so
// linking the project does not sign the agent out.
func adoptCursorMcpAuthLogin(projectAuth, store string) error {
	projectInfo, err := os.Stat(projectAuth)
	if err != nil {
		return nil
	}
	if storeInfo, err := os.Stat(store); err == nil && os.SameFile(projectInfo, storeInfo) {
		return nil
	}
	if cursorMcpAuthHasLogin(store) || !cursorMcpAuthHasLogin(projectAuth) {
		return nil
	}
	data, err := os.ReadFile(projectAuth)
	if err != nil {
		return nil
	}
	// In place: other tasks may hold hard links to the store.
	if err := os.WriteFile(store, data, 0o600); err != nil {
		return fmt.Errorf("adopt cursor mcp login into store: %w", err)
	}
	return nil
}

type cursorMcpAuthLinkRecord struct {
	Link   string `json:"link"`
	Target string `json:"target"`
}

func writeCursorMcpAuthLinkRecord(envRoot, link, target string) error {
	data, err := json.Marshal(cursorMcpAuthLinkRecord{Link: link, Target: target})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(envRoot, cursorMcpAuthLinkRecordFile), data, 0o600); err != nil {
		return fmt.Errorf("record cursor mcp auth link: %w", err)
	}
	return nil
}

// RemoveCursorMcpAuthLink removes the link a task without managed MCP placed in
// the user's Cursor data dir, before the GC removes the task's env root. A
// leftover symlink would dangle, and a hard link (the Windows fallback) would
// keep a copy of the agent's tokens.
func RemoveCursorMcpAuthLink(envRoot, daemonProfile string) error {
	return removeCursorMcpAuthLink(envRoot, CursorUserDataDir(), cursorMcpAuthStoreRootDir(daemonProfile))
}

// removeCursorMcpAuthLink trusts the env root's record only as far as it
// matches what linkCursorUserMcpAuth creates: an mcp-auth.json directly under
// a project of userDataDir that is a symlink to the recorded target, or a hard
// link to a file in the agent stores. A file Cursor wrote itself is left alone.
func removeCursorMcpAuthLink(envRoot, userDataDir, storeRoot string) error {
	data, err := os.ReadFile(filepath.Join(envRoot, cursorMcpAuthLinkRecordFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var record cursorMcpAuthLinkRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return fmt.Errorf("parse cursor mcp auth link record: %w", err)
	}
	link := filepath.Clean(record.Link)
	if userDataDir == "" || filepath.Base(link) != cursorMcpAuthFile ||
		filepath.Dir(filepath.Dir(link)) != filepath.Join(userDataDir, "projects") {
		return nil
	}
	info, err := os.Lstat(link)
	if err != nil {
		return nil
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		if dest, err := os.Readlink(link); err != nil || dest != record.Target {
			return nil
		}
	case info.Mode().IsRegular():
		if storeRoot == "" || !cursorPathWithin(storeRoot, record.Target) {
			return nil
		}
		targetInfo, err := os.Stat(record.Target)
		if err != nil || !os.SameFile(info, targetInfo) {
			return nil
		}
	default:
		return nil
	}
	if err := os.Remove(link); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove cursor mcp auth link: %w", err)
	}
	return nil
}

// cursorPathWithin reports whether path is root or inside it, comparing
// symlink-resolved paths where they resolve.
func cursorPathWithin(root, path string) bool {
	resolve := func(p string) string {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		return filepath.Clean(p)
	}
	rel, err := filepath.Rel(resolve(root), resolve(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
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
