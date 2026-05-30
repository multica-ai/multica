package traceupload

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// LocateParams describes where to find the session JSONL the runtime wrote for
// a finished task.
type LocateParams struct {
	Runtime   string // "claude" | "codex"
	SessionID string // Claude session id (file stem) — required for claude
	EnvRoot   string // per-task env root (<workspacesRoot>/<ws>/<shortTaskID>) — required for codex
	// WorkDir is the agent's working directory for this task. Codex records it
	// as session_meta.payload.cwd in the rollout's first line, and because the
	// per-task codex-home/sessions is a symlink into the SHARED ~/.codex/sessions
	// tree (logs stay global), it is the only reliable way to pick THIS task's
	// rollout rather than a concurrent sibling's. Optional — empty falls back
	// to newest-file.
	WorkDir string
}

// LocateJSONL returns the absolute path of the session transcript JSONL for a
// finished task, or an error if it cannot be found. This is the same file the
// daemon already reads token usage from (Codex) / the transcript the runtime
// persisted (Claude).
//
// Claude Code writes <claudeConfigDir>/projects/<cwd-slug>/<sessionID>.jsonl.
// The session id is globally unique, so we glob across every project dir by
// the session-id stem rather than reconstructing the cwd slug.
//
// Codex writes <CODEX_HOME>/sessions/YYYY/MM/DD/rollout-*.jsonl. The daemon
// gives each task its own CODEX_HOME at <envRoot>/codex-home, but its sessions
// subdir is a symlink into the shared ~/.codex/sessions, so we resolve the
// symlink and disambiguate concurrent rollouts by the task's WorkDir (cwd).
func LocateJSONL(p LocateParams) (string, error) {
	switch p.Runtime {
	case "claude":
		return locateClaude(p.SessionID)
	case "codex":
		return locateCodex(p.EnvRoot, p.WorkDir)
	default:
		return "", fmt.Errorf("unsupported runtime %q", p.Runtime)
	}
}

func locateClaude(sessionID string) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("claude: empty session id")
	}
	root := claudeConfigDir()
	matches, err := filepath.Glob(filepath.Join(root, "projects", "*", sessionID+".jsonl"))
	if err != nil {
		return "", fmt.Errorf("claude: glob session file: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("claude: no transcript for session %s under %s", sessionID, root)
	}
	// A session id is unique; if multiple project dirs somehow match, take the
	// most recently modified.
	return newestFile(matches), nil
}

func locateCodex(envRoot, workDir string) (string, error) {
	if envRoot == "" {
		return "", fmt.Errorf("codex: empty env root")
	}
	sessionsDir := filepath.Join(envRoot, "codex-home", "sessions")
	// codex-home/sessions is a symlink to the shared ~/.codex/sessions, and
	// filepath.WalkDir does NOT follow a symlinked root — it would visit the
	// link as a single non-dir entry and find nothing. Resolve it first.
	if resolved, err := filepath.EvalSymlinks(sessionsDir); err == nil {
		sessionsDir = resolved
	}
	var files []string
	err := filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable subtrees rather than abort the walk
		}
		if !d.IsDir() && filepath.Ext(path) == ".jsonl" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("codex: walk sessions: %w", err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("codex: no rollout jsonl under %s", sessionsDir)
	}
	// The sessions tree is shared across every concurrent codex task on the
	// host, so newest-file alone can return a sibling task's rollout. Walk
	// candidates newest-first and return the first whose session_meta cwd
	// matches this task's WorkDir; only if none matches (or WorkDir is unknown)
	// fall back to the newest file. Reading the first line of just the newest
	// few files keeps this cheap.
	//
	// macOS resolves /tmp to /private/tmp via getcwd, so a daemon-supplied
	// WorkDir of `/tmp/...` would never match a session_meta cwd of
	// `/private/tmp/...`. Resolve symlinks on the lookup side (falling back to
	// the raw string when EvalSymlinks fails — e.g. the workdir was already
	// torn down) so this divergence does not silently tier-fall to newest-file.
	resolvedWorkDir := workDir
	if workDir != "" {
		if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
			resolvedWorkDir = resolved
		}
	}
	if workDir != "" {
		for _, p := range byMtimeDesc(files) {
			if cwd := codexSessionCwd(p); cwd == workDir || cwd == resolvedWorkDir {
				return p, nil
			}
		}
	}
	return newestFile(files), nil
}

// codexSessionCwd reads the first JSONL line of a codex rollout and returns its
// session_meta.payload.cwd. Empty on any error or if the first line is not a
// session_meta record.
func codexSessionCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	if !sc.Scan() {
		return ""
	}
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(sc.Bytes(), &meta); err != nil {
		return ""
	}
	return meta.Payload.Cwd
}

// byMtimeDesc returns paths ordered newest-modified first (ties broken
// lexically-descending for determinism).
func byMtimeDesc(paths []string) []string {
	out := append([]string(nil), paths...)
	mod := make(map[string]int64, len(out))
	for _, p := range out {
		if info, err := os.Stat(p); err == nil {
			mod[p] = info.ModTime().UnixNano()
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if mod[out[i]] != mod[out[j]] {
			return mod[out[i]] > mod[out[j]]
		}
		return out[i] > out[j]
	})
	return out
}

// claudeConfigDir mirrors Claude Code's own resolution: CLAUDE_CONFIG_DIR when
// set, otherwise $HOME/.claude.
func claudeConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// newestFile returns the path with the most recent modification time. Ties and
// stat errors fall back to lexically-last so the result is deterministic.
func newestFile(paths []string) string {
	sort.Strings(paths)
	best := paths[len(paths)-1]
	var bestMod int64 = -1
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if m := info.ModTime().UnixNano(); m > bestMod {
			bestMod = m
			best = p
		}
	}
	return best
}
