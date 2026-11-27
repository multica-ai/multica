package execenv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

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

	// cursorMcpAuthLinkRecordFile, in a task's env root, records the login
	// copy the task placed in the user's Cursor data dir so the GC can remove
	// it.
	cursorMcpAuthLinkRecordFile = "cursor-mcp-auth-link.json"

	// cursorMcpAuthBaseFile, in an env root, holds the mcp-auth.json the daemon
	// last put in or took from the env's Cursor project, so a sync can tell
	// which entries Cursor changed since.
	cursorMcpAuthBaseFile = "cursor-mcp-auth-base.json"

	// The agent's standing login workspaces, next to its store.
	cursorMcpLoginWorkspaceDir     = "login"
	cursorMcpUserLoginWorkspaceDir = "user-login"
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
// next. Each task gets a copy of the store, and the entries Cursor changes in
// that copy are merged back while the task runs (CursorMcpAuthMirror), so a
// single-use refresh token one task spends is replaced in the store before
// the next task reads it. It is separate from the user's own ~/.cursor login,
// which therefore never gets signed out by a task. Concurrent tasks of one
// agent may still race a refresh; the store is last-writer-wins per server.
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

// prepareCursorMcp sets up a Cursor task's MCP sidecars and its copy of the
// agent's MCP login on env, first collecting any login made in the agent's
// login workspaces so the task starts with it.
func prepareCursorMcp(env *Environment, envRoot, workDir string, mcpConfig json.RawMessage, mcpAuthSource, mcpAuthStore, userDataDir string, manifest *sidecarManifest, logger *slog.Logger) error {
	SyncCursorMcpLoginWorkspaces(mcpAuthSource, mcpAuthStore, userDataDir, logger)
	cursorDataDir, mirror, err := prepareCursorMcpConfigMirror(envRoot, workDir, mcpConfig, mcpAuthSource, mcpAuthStore, manifest, logger)
	if err != nil {
		return err
	}
	env.CursorDataDir = cursorDataDir
	env.CursorUserDataDir = ""
	env.CursorMcpAuthMirror = mirror
	if cursorDataDir == "" {
		env.CursorUserDataDir, env.CursorMcpAuthMirror = linkCursorUserMcpAuthMirror(userDataDir, envRoot, workDir, mcpAuthSource, mcpAuthStore, logger)
	}
	return nil
}

// prepareCursorMcpConfig writes the Cursor-native MCP sidecars for agents that
// have an explicit managed mcp_config saved. A nil/null mcp_config means "let
// Cursor behave normally", so no .cursor/mcp.json or CURSOR_DATA_DIR is created.
//
// The task's mcp-auth.json is rebuilt on every run as a copy of the login it
// mirrors: an explicit mcpAuthSource wins, otherwise the agent's mcpAuthStore,
// otherwise it stays task-local.
func prepareCursorMcpConfig(envRoot, workDir string, mcpConfig json.RawMessage, mcpAuthSource, mcpAuthStore string, manifest *sidecarManifest, logger *slog.Logger) (string, error) {
	cursorDataDir, _, err := prepareCursorMcpConfigMirror(envRoot, workDir, mcpConfig, mcpAuthSource, mcpAuthStore, manifest, logger)
	return cursorDataDir, err
}

// prepareCursorMcpConfigMirror is prepareCursorMcpConfig that also returns the
// mirror keeping the task's login copy in step, nil when the login is
// task-local.
func prepareCursorMcpConfigMirror(envRoot, workDir string, mcpConfig json.RawMessage, mcpAuthSource, mcpAuthStore string, manifest *sidecarManifest, logger *slog.Logger) (string, *CursorMcpAuthMirror, error) {
	if !hasManagedCursorMcpConfig(mcpConfig) {
		return "", nil, nil
	}
	if envRoot == "" {
		return "", nil, fmt.Errorf("env root is required for managed cursor mcp_config")
	}

	projectRoot := cursorProjectRoot(workDir)
	servers, err := parseCursorManagedMcpServers(mcpConfig)
	if err != nil {
		return "", nil, err
	}

	cursorDir := filepath.Join(projectRoot, ".cursor")
	if err := recordMkdirAll(cursorDir, 0o755, manifest); err != nil {
		return "", nil, fmt.Errorf("create .cursor dir: %w", err)
	}
	configData, err := marshalCursorMcpConfig(servers)
	if err != nil {
		return "", nil, err
	}
	if err := recordWriteFile(filepath.Join(cursorDir, "mcp.json"), configData, 0o600, manifest); err != nil {
		if errors.Is(err, errPathPreExists) {
			return "", nil, fmt.Errorf("managed cursor mcp_config would overwrite existing .cursor/mcp.json")
		}
		return "", nil, fmt.Errorf("write .cursor/mcp.json: %w", err)
	}

	cursorDataDir := filepath.Join(envRoot, "cursor-data")
	projectDataDir := filepath.Join(cursorDataDir, "projects", cursorSlugifyPath(projectRoot))
	if err := os.MkdirAll(projectDataDir, 0o700); err != nil {
		return "", nil, fmt.Errorf("create cursor project data dir: %w", err)
	}
	approvals, err := cursorMcpApprovalKeys(projectRoot, servers)
	if err != nil {
		return "", nil, err
	}
	approvalData, err := json.MarshalIndent(approvals, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("marshal cursor mcp approvals: %w", err)
	}
	if err := os.WriteFile(filepath.Join(projectDataDir, "mcp-approvals.json"), approvalData, 0o600); err != nil {
		return "", nil, fmt.Errorf("write cursor mcp approvals: %w", err)
	}
	trustData, err := json.MarshalIndent(map[string]string{
		"trustedAt":     "1970-01-01T00:00:00Z",
		"workspacePath": projectRoot,
		"trustMethod":   "multica-managed",
	}, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("marshal cursor workspace trust: %w", err)
	}
	if err := os.WriteFile(filepath.Join(projectDataDir, cursorWorkspaceTrustedFile), trustData, 0o600); err != nil {
		return "", nil, fmt.Errorf("write cursor workspace trust: %w", err)
	}

	explicit := strings.TrimSpace(mcpAuthSource) != ""
	target, err := cursorMcpAuthTarget(mcpAuthSource, mcpAuthStore)
	if err != nil {
		if explicit {
			return "", nil, err
		}
		if logger != nil {
			logger.Warn("execenv: cursor mcp auth store unavailable; MCP logins stay task-local", "store", mcpAuthStore, "error", err)
		}
		target = ""
	}
	mirror, err := mirrorCursorMcpAuth(filepath.Join(projectDataDir, cursorMcpAuthFile), envRoot, target, logger)
	if err != nil {
		if explicit {
			return "", nil, fmt.Errorf("seed cursor mcp auth file: %w", err)
		}
		if logger != nil {
			logger.Warn("execenv: seed cursor mcp login from the agent store failed; MCP logins stay task-local", "store", mcpAuthStore, "error", err)
		}
		if _, err := mirrorCursorMcpAuth(filepath.Join(projectDataDir, cursorMcpAuthFile), envRoot, "", logger); err != nil {
			return "", nil, err
		}
	}

	return cursorDataDir, mirror, nil
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
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	logins := make(map[string]bool, len(entries))
	for name, entry := range entries {
		logins[name] = cursorMcpAuthEntryHasLogin(entry)
	}
	return logins
}

// cursorMcpAuthEntryHasLogin reports whether one server's mcp-auth.json entry
// holds OAuth tokens.
func cursorMcpAuthEntryHasLogin(entry json.RawMessage) bool {
	var parsed struct {
		Tokens *struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(entry, &parsed); err != nil {
		return false
	}
	return parsed.Tokens != nil && (parsed.Tokens.AccessToken != "" || parsed.Tokens.RefreshToken != "")
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
// written, and the project's mcp-auth.json is a copy of the login the agent's
// tasks use (mcpAuthSource, else mcpAuthStore); the agent's next task merges a
// login made there back (SyncCursorMcpLoginWorkspaces). Servers from the
// user's own ~/.cursor configuration load from any directory.
func PrepareCursorMcpLoginWorkspace(mcpAuthSource, mcpAuthStore string, mcpConfig json.RawMessage, servers []string) (string, string, error) {
	if mcpAuthStore == "" {
		return "", "", fmt.Errorf("cursor mcp auth store is required")
	}
	envRoot := filepath.Join(filepath.Dir(mcpAuthStore), cursorMcpLoginWorkspaceDir)
	workDir := filepath.Join(envRoot, "workdir")
	if _, err := prepareCursorMcpLoginWorkDir(envRoot, workDir); err != nil {
		return "", "", err
	}
	baseLegacyCursorMcpLogin(CursorMcpAuthPath(filepath.Join(envRoot, "cursor-data"), workDir), envRoot, mcpAuthSource, mcpAuthStore)

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
		return "", "", fmt.Errorf("seed cursor mcp login workspace with the agent's login: %w", err)
	}
	return workDir, cursorDataDir, nil
}

// PrepareCursorMcpUserLoginWorkspace is PrepareCursorMcpLoginWorkspace for
// agents without managed MCP, whose tasks run on the user's Cursor data dir. It
// returns a standing workdir whose project in userDataDir holds a copy of the
// login the agent's tasks use. The servers come from the user's own Cursor
// configuration, so nothing is written into the workdir.
func PrepareCursorMcpUserLoginWorkspace(userDataDir, mcpAuthSource, mcpAuthStore string) (string, error) {
	if userDataDir == "" || mcpAuthStore == "" {
		return "", fmt.Errorf("cursor user data dir and mcp auth store are required")
	}
	envRoot := filepath.Join(filepath.Dir(mcpAuthStore), cursorMcpUserLoginWorkspaceDir)
	workDir := filepath.Join(envRoot, "workdir")
	projectRoot, err := prepareCursorMcpLoginWorkDir(envRoot, workDir)
	if err != nil {
		return "", err
	}
	projectDataDir := filepath.Join(userDataDir, "projects", cursorSlugifyPath(projectRoot))
	baseLegacyCursorMcpLogin(filepath.Join(projectDataDir, cursorMcpAuthFile), envRoot, mcpAuthSource, mcpAuthStore)
	if _, err := linkCursorProjectMcpAuth(projectDataDir, envRoot, mcpAuthSource, mcpAuthStore, nil); err != nil {
		return "", fmt.Errorf("seed cursor mcp login workspace with the agent's login: %w", err)
	}
	return workDir, nil
}

// SyncCursorMcpLoginWorkspaces merges logins made in the agent's standing
// login workspaces into the login they copy. Those logins happen outside any
// task, so the agent's next task is the first chance to collect them.
func SyncCursorMcpLoginWorkspaces(mcpAuthSource, mcpAuthStore, userDataDir string, logger *slog.Logger) {
	if mcpAuthStore == "" {
		return
	}
	agentDir := filepath.Dir(mcpAuthStore)
	var mirrors []*CursorMcpAuthMirror
	loginRoot := filepath.Join(agentDir, cursorMcpLoginWorkspaceDir)
	if loginWork := filepath.Join(loginRoot, "workdir"); dirExists(loginWork) {
		mirrors = append(mirrors, newCursorMcpAuthMirror(CursorMcpAuthPath(filepath.Join(loginRoot, "cursor-data"), loginWork), loginRoot, ""))
	}
	userLoginRoot := filepath.Join(agentDir, cursorMcpUserLoginWorkspaceDir)
	if userLoginWork := filepath.Join(userLoginRoot, "workdir"); userDataDir != "" && dirExists(userLoginWork) {
		mirrors = append(mirrors, newCursorMcpAuthMirror(CursorMcpAuthPath(userDataDir, userLoginWork), userLoginRoot, ""))
	}
	if len(mirrors) == 0 {
		return
	}
	target, err := cursorMcpAuthTarget(mcpAuthSource, mcpAuthStore)
	if err != nil || target == "" {
		return
	}
	for _, m := range mirrors {
		m.Target = target
		err := m.baseLegacyLogin()
		if err == nil {
			err = m.Sync()
		}
		if err != nil && logger != nil {
			logger.Warn("execenv: merge cursor mcp login workspace login failed", "project_auth", m.ProjectAuth, "error", err)
		}
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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

// linkCursorUserMcpAuth seeds the mcp-auth.json Cursor keeps for a task's
// project in the user's Cursor data dir with the agent's login, for tasks
// without managed MCP. It returns userDataDir once the copy is in place, else
// "".
//
// Only a project inside the task's env root is touched. Any other project (an
// in-place local directory, or a git repository enclosing the workdir) is one
// the user works in too, and its mcp-auth.json is the user's own login.
func linkCursorUserMcpAuth(userDataDir, envRoot, workDir, mcpAuthSource, mcpAuthStore string, logger *slog.Logger) string {
	dir, _ := linkCursorUserMcpAuthMirror(userDataDir, envRoot, workDir, mcpAuthSource, mcpAuthStore, logger)
	return dir
}

// linkCursorUserMcpAuthMirror is linkCursorUserMcpAuth that also returns the
// mirror keeping the task's login copy in step.
func linkCursorUserMcpAuthMirror(userDataDir, envRoot, workDir, mcpAuthSource, mcpAuthStore string, logger *slog.Logger) (string, *CursorMcpAuthMirror) {
	if userDataDir == "" || envRoot == "" || workDir == "" || (strings.TrimSpace(mcpAuthSource) == "" && mcpAuthStore == "") {
		return "", nil
	}
	projectRoot := cursorProjectRoot(workDir)
	if !cursorPathWithin(envRoot, projectRoot) {
		return "", nil
	}
	projectDataDir := filepath.Join(userDataDir, "projects", cursorSlugifyPath(projectRoot))
	mirror, err := linkCursorProjectMcpAuth(projectDataDir, envRoot, mcpAuthSource, mcpAuthStore, logger)
	if err == nil {
		err = writeCursorMcpAuthLinkRecord(envRoot, filepath.Join(projectDataDir, cursorMcpAuthFile), mirror.Target, projectRoot)
		if err != nil {
			_ = removeCursorMcpAuthFile(projectDataDir)
		}
	}
	if err != nil {
		if logger != nil {
			logger.Warn("execenv: seed cursor mcp auth into user data dir failed; MCP logins stay task-local", "project", projectDataDir, "error", err)
		}
		return "", nil
	}
	return userDataDir, mirror
}

// linkCursorProjectMcpAuth seeds a Cursor project's mcp-auth.json with the
// explicit mcpAuthSource, else the agent's mcpAuthStore, and returns the mirror
// that merges Cursor's changes back.
func linkCursorProjectMcpAuth(projectDataDir, envRoot, mcpAuthSource, mcpAuthStore string, logger *slog.Logger) (*CursorMcpAuthMirror, error) {
	if err := os.MkdirAll(projectDataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create cursor project data dir: %w", err)
	}
	target, err := cursorMcpAuthTarget(mcpAuthSource, mcpAuthStore)
	if err != nil {
		return nil, err
	}
	m := newCursorMcpAuthMirror(filepath.Join(projectDataDir, cursorMcpAuthFile), envRoot, target)
	if err := m.Sync(); err != nil && logger != nil {
		logger.Warn("execenv: merge prior cursor mcp login failed", "project_auth", m.ProjectAuth, "error", err)
	}
	if strings.TrimSpace(mcpAuthSource) == "" {
		if err := adoptCursorMcpAuthLogin(m.ProjectAuth, target); err != nil {
			return nil, err
		}
	}
	if err := m.seed(); err != nil {
		return nil, err
	}
	return m, nil
}

// adoptCursorMcpAuthLogin carries a login Cursor already saved in a project's
// own mcp-auth.json into the agent's store while the store holds none, so
// seeding the project does not sign the agent out.
func adoptCursorMcpAuthLogin(projectAuth, store string) error {
	unlock := lockCursorMcpAuthTarget(store)
	defer unlock()
	if cursorMcpAuthHasLogin(store) || !cursorMcpAuthHasLogin(projectAuth) {
		return nil
	}
	data, err := os.ReadFile(projectAuth)
	if err != nil {
		return nil
	}
	if err := writeFileAtomic(store, data, 0o600); err != nil {
		return fmt.Errorf("adopt cursor mcp login into store: %w", err)
	}
	return nil
}

type cursorMcpAuthLinkRecord struct {
	Link   string `json:"link"`
	Target string `json:"target"`
	// Project is the task's Cursor project root, resolved when the copy was
	// placed. The GC may run after the project itself is gone (a worktree
	// removed at finalize), so it cannot be derived again then.
	Project string `json:"project,omitempty"`
}

func writeCursorMcpAuthLinkRecord(envRoot, link, target, project string) error {
	data, err := json.Marshal(cursorMcpAuthLinkRecord{Link: link, Target: target, Project: project})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(envRoot, cursorMcpAuthLinkRecordFile), data, 0o600); err != nil {
		return fmt.Errorf("record cursor mcp auth link: %w", err)
	}
	return nil
}

// RemoveCursorMcpAuthLink removes the login copy a task without managed MCP
// placed in the user's Cursor data dir, before the GC removes the task's env
// root, so the agent's tokens do not outlive the task there.
func RemoveCursorMcpAuthLink(envRoot, daemonProfile string) error {
	return removeCursorMcpAuthLink(envRoot, CursorUserDataDir(), cursorMcpAuthStoreRootDir(daemonProfile))
}

// removeCursorMcpAuthLink trusts the env root's record only as far as it
// matches what linkCursorUserMcpAuth creates: an mcp-auth.json directly under
// a project of userDataDir that is the task's own project (the recorded
// project, or for older records the workdir, lies in the env root), or, for
// records written by daemons that linked the login, a symlink to the recorded
// target or a hard link to a file in the agent stores. Any other file Cursor
// wrote is left alone.
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
	taskProject := filepath.Clean(record.Project)
	if record.Project == "" {
		taskProject = cursorProjectRoot(filepath.Join(envRoot, "workdir"))
	}
	ownProject := filepath.IsAbs(taskProject) && cursorPathWithin(envRoot, taskProject) &&
		link == filepath.Join(userDataDir, "projects", cursorSlugifyPath(taskProject), cursorMcpAuthFile)
	switch {
	case ownProject && (info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0):
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

// CursorMcpAuthMirror keeps a Cursor project's mcp-auth.json, a copy Cursor
// owns, in step with the login it was seeded from. A link cannot share the
// file: cursor-agent 2026.11.19 saves it by renaming a new file over it, which
// replaces a symlink or hard link with a file of its own. So the daemon copies
// the login in and merges back each server entry Cursor changes.
type CursorMcpAuthMirror struct {
	// ProjectAuth is the mcp-auth.json Cursor reads and writes.
	ProjectAuth string
	// Target is the login it mirrors: the explicit source or the agent store.
	Target string
	// BaseFile records ProjectAuth as the daemon last seeded or synced it.
	BaseFile string

	mu   sync.Mutex
	seen cursorMcpAuthStamp
}

type cursorMcpAuthStamp struct {
	modTime time.Time
	size    int64
}

type cursorMcpAuthBase struct {
	ProjectAuth string `json:"project_auth"`
	Auth        string `json:"auth"`
}

// cursorMcpAuthTargetLocks serializes merges into one login file across the
// daemon's tasks, keyed by the cleaned target path.
var cursorMcpAuthTargetLocks sync.Map

func lockCursorMcpAuthTarget(target string) func() {
	v, _ := cursorMcpAuthTargetLocks.LoadOrStore(filepath.Clean(target), &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func newCursorMcpAuthMirror(projectAuth, envRoot, target string) *CursorMcpAuthMirror {
	return &CursorMcpAuthMirror{
		ProjectAuth: projectAuth,
		Target:      target,
		BaseFile:    filepath.Join(envRoot, cursorMcpAuthBaseFile),
	}
}

// cursorMcpAuthTarget resolves the login a project mirrors: the explicit
// mcpAuthSource, else the agent's mcpAuthStore (created when missing), else ""
// for a task-local login.
func cursorMcpAuthTarget(mcpAuthSource, mcpAuthStore string) (string, error) {
	if strings.TrimSpace(mcpAuthSource) != "" {
		return resolveCursorMcpAuthSource(mcpAuthSource)
	}
	if mcpAuthStore == "" {
		return "", nil
	}
	if err := ensureCursorMcpAuthStore(mcpAuthStore); err != nil {
		return "", err
	}
	return mcpAuthStore, nil
}

// mirrorCursorMcpAuth seeds projectAuth from target after merging back what
// Cursor changed in the copy a previous run left there. An empty target leaves
// the project with no login of its own.
func mirrorCursorMcpAuth(projectAuth, envRoot, target string, logger *slog.Logger) (*CursorMcpAuthMirror, error) {
	m := newCursorMcpAuthMirror(projectAuth, envRoot, target)
	if target == "" {
		if err := os.Remove(m.BaseFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("remove cursor mcp auth base: %w", err)
		}
		return nil, removeCursorMcpAuthFile(filepath.Dir(projectAuth))
	}
	if err := m.Sync(); err != nil && logger != nil {
		logger.Warn("execenv: merge prior cursor mcp login failed", "project_auth", projectAuth, "error", err)
	}
	if err := m.seed(); err != nil {
		return nil, err
	}
	return m, nil
}

// seed replaces ProjectAuth with a copy of Target and records it as the base.
// The rename also replaces a link an older daemon left there instead of
// writing through it.
func (m *CursorMcpAuthMirror) seed() error {
	unlock := lockCursorMcpAuthTarget(m.Target)
	defer unlock()
	data, err := os.ReadFile(m.Target)
	if err != nil {
		return fmt.Errorf("read cursor mcp login: %w", err)
	}
	if err := writeFileAtomic(m.ProjectAuth, data, 0o600); err != nil {
		return err
	}
	if err := m.writeBase(data); err != nil {
		_ = os.Remove(m.ProjectAuth)
		return err
	}
	if info, err := os.Stat(m.ProjectAuth); err == nil {
		m.seen = cursorMcpAuthStamp{modTime: info.ModTime(), size: info.Size()}
	}
	return nil
}

func (m *CursorMcpAuthMirror) writeBase(auth []byte) error {
	data, err := json.Marshal(cursorMcpAuthBase{ProjectAuth: m.ProjectAuth, Auth: string(auth)})
	if err != nil {
		return err
	}
	if err := writeFileAtomic(m.BaseFile, data, 0o600); err != nil {
		return fmt.Errorf("record cursor mcp auth base: %w", err)
	}
	return nil
}

// Sync merges into Target the server entries Cursor changed in ProjectAuth
// since the daemon last seeded or synced it. Without a base for this
// ProjectAuth nothing is known to have changed, so nothing is merged.
func (m *CursorMcpAuthMirror) Sync() error {
	if m == nil || m.Target == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.syncLocked()
}

// SyncIfChanged is Sync, skipped while ProjectAuth's size and modification
// time are what the previous check saw. It is cheap enough to poll.
func (m *CursorMcpAuthMirror) SyncIfChanged() error {
	if m == nil || m.Target == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	info, err := os.Stat(m.ProjectAuth)
	if err != nil {
		return nil
	}
	stamp := cursorMcpAuthStamp{modTime: info.ModTime(), size: info.Size()}
	if stamp == m.seen {
		return nil
	}
	if err := m.syncLocked(); err != nil {
		return err
	}
	m.seen = stamp
	return nil
}

// baseLegacyLogin gives a login workspace set up by an older daemon a base to
// sync from. That daemon linked the workspace's mcp-auth.json to the login and
// recorded no base, so once Cursor replaced the link with a file of its own, a
// login made there would never be merged, and the next seed would overwrite
// it. The whole file counts as changed when it is newer than the login it
// copies, or that login holds none; tokenless entries still never replace a
// login (mergeCursorMcpAuthEntries).
func (m *CursorMcpAuthMirror) baseLegacyLogin() error {
	if m == nil || m.Target == "" {
		return nil
	}
	if _, err := os.Stat(m.BaseFile); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	info, err := os.Lstat(m.ProjectAuth)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	if targetInfo, err := os.Stat(m.Target); err == nil {
		if os.SameFile(info, targetInfo) {
			return nil
		}
		if cursorMcpAuthHasLogin(m.Target) && !info.ModTime().After(targetInfo.ModTime()) {
			return nil
		}
	}
	return m.writeBase([]byte("{}"))
}

// baseLegacyCursorMcpLogin is baseLegacyLogin for a login workspace about to
// be seeded again, whose own sync then merges the login.
func baseLegacyCursorMcpLogin(projectAuth, envRoot, mcpAuthSource, mcpAuthStore string) {
	target, err := cursorMcpAuthTarget(mcpAuthSource, mcpAuthStore)
	if err != nil || target == "" {
		return
	}
	_ = newCursorMcpAuthMirror(projectAuth, envRoot, target).baseLegacyLogin()
}

func (m *CursorMcpAuthMirror) syncLocked() error {
	local, err := os.ReadFile(m.ProjectAuth)
	if err != nil {
		return nil
	}
	baseData, err := os.ReadFile(m.BaseFile)
	if err != nil {
		return nil
	}
	var base cursorMcpAuthBase
	if err := json.Unmarshal(baseData, &base); err != nil || base.ProjectAuth != m.ProjectAuth {
		return nil
	}
	if string(local) == base.Auth {
		return nil
	}
	localEntries, err := parseCursorMcpAuthEntries(local)
	if err != nil {
		// Cursor writes the file in place in older versions; a half-written
		// file is merged on the next sync instead.
		return nil
	}
	baseEntries, err := parseCursorMcpAuthEntries([]byte(base.Auth))
	if err != nil {
		baseEntries = map[string]json.RawMessage{}
	}

	unlock := lockCursorMcpAuthTarget(m.Target)
	defer unlock()
	targetData, err := os.ReadFile(m.Target)
	if err != nil {
		return fmt.Errorf("read cursor mcp login: %w", err)
	}
	targetEntries, err := parseCursorMcpAuthEntries(targetData)
	if err != nil {
		return fmt.Errorf("parse cursor mcp login %s: %w", m.Target, err)
	}
	if mergeCursorMcpAuthEntries(targetEntries, baseEntries, localEntries) {
		data, err := json.MarshalIndent(targetEntries, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal cursor mcp login: %w", err)
		}
		dst := m.Target
		if resolved, err := filepath.EvalSymlinks(dst); err == nil {
			dst = resolved
		}
		if err := writeFileAtomic(dst, data, 0o600); err != nil {
			return fmt.Errorf("write cursor mcp login: %w", err)
		}
	}
	return m.writeBase(local)
}

// mergeCursorMcpAuthEntries applies to target each server entry that changed
// from base to local, and reports whether target changed. Entries other tasks
// changed meanwhile and Cursor left alone here are kept. An entry that lost its
// tokens never replaces one that has them: a task whose refresh token another
// task already spent must not sign the agent out.
func mergeCursorMcpAuthEntries(target, base, local map[string]json.RawMessage) bool {
	changed := false
	for name, entry := range local {
		if prior, ok := base[name]; ok && cursorJSONEqual(prior, entry) {
			continue
		}
		current, ok := target[name]
		if ok && (cursorJSONEqual(current, entry) || (cursorMcpAuthEntryHasLogin(current) && !cursorMcpAuthEntryHasLogin(entry))) {
			continue
		}
		target[name] = entry
		changed = true
	}
	for name, prior := range base {
		if _, ok := local[name]; ok {
			continue
		}
		if current, ok := target[name]; ok && cursorJSONEqual(current, prior) {
			delete(target, name)
			changed = true
		}
	}
	return changed
}

func parseCursorMcpAuthEntries(data []byte) (map[string]json.RawMessage, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		entries = map[string]json.RawMessage{}
	}
	return entries, nil
}

func cursorJSONEqual(a, b json.RawMessage) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(av, bv)
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
