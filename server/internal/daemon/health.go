package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/filetree"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// HealthResponse is returned by the daemon's local health endpoint.
type HealthResponse struct {
	Status     string            `json:"status"`
	PID        int               `json:"pid"`
	Uptime     string            `json:"uptime"`
	DaemonID   string            `json:"daemon_id"`
	DeviceName string            `json:"device_name"`
	ServerURL  string            `json:"server_url"`
	Agents     []string          `json:"agents"`
	Workspaces []healthWorkspace `json:"workspaces"`
}

type healthWorkspace struct {
	ID       string   `json:"id"`
	Runtimes []string `json:"runtimes"`
}

// listenHealth binds the health port. Returns the listener or an error if
// another daemon is already running (port taken).
func (d *Daemon) listenHealth() (net.Listener, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", d.cfg.HealthPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("another daemon is already running on %s: %w", addr, err)
	}
	return ln, nil
}

// repoCheckoutRequest is the body of a POST /repo/checkout request.
type repoCheckoutRequest struct {
	URL         string `json:"url"`
	WorkspaceID string `json:"workspace_id"`
	WorkDir     string `json:"workdir"`
	AgentName   string `json:"agent_name"`
	TaskID      string `json:"task_id"`
}

// serveHealth runs the health HTTP server on the given listener.
// Blocks until ctx is cancelled.
func (d *Daemon) serveHealth(ctx context.Context, ln net.Listener, startedAt time.Time) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		var wsList []healthWorkspace
		for id, ws := range d.workspaces {
			wsList = append(wsList, healthWorkspace{
				ID:       id,
				Runtimes: ws.runtimeIDs,
			})
		}
		d.mu.Unlock()

		agents := make([]string, 0, len(d.cfg.Agents))
		for name := range d.cfg.Agents {
			agents = append(agents, name)
		}

		resp := HealthResponse{
			Status:     "running",
			PID:        os.Getpid(),
			Uptime:     time.Since(startedAt).Truncate(time.Second).String(),
			DaemonID:   d.cfg.DaemonID,
			DeviceName: d.cfg.DeviceName,
			ServerURL:  d.cfg.ServerBaseURL,
			Agents:     agents,
			Workspaces: wsList,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/repo/checkout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req repoCheckoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}
		if req.WorkDir == "" {
			http.Error(w, "workdir is required", http.StatusBadRequest)
			return
		}

		if d.repoCache == nil {
			http.Error(w, "repo cache not initialized", http.StatusInternalServerError)
			return
		}

		result, err := d.repoCache.CreateWorktree(repocache.WorktreeParams{
			WorkspaceID: req.WorkspaceID,
			RepoURL:     req.URL,
			WorkDir:     req.WorkDir,
			AgentName:   req.AgentName,
			TaskID:      req.TaskID,
		})
		if err != nil {
			d.logger.Error("repo checkout failed", "url", req.URL, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// File content endpoint — serves files from active task worktrees.
	// GET /tasks/{taskId}/files/{path...}  — raw file content
	// GET /tasks/{taskId}/diff/{path...}   — git diff vs the branch base
	mux.HandleFunc("/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/tasks/")

		// Route between /files/ and /diff/ suffixes.
		if diffParts := strings.SplitN(path, "/diff/", 2); len(diffParts) == 2 && diffParts[0] != "" && diffParts[1] != "" {
			d.serveTaskFileDiff(w, r, diffParts[0], diffParts[1])
			return
		}

		// Parse: /tasks/{taskId}/files/{path...}
		parts := strings.SplitN(path, "/files/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			http.Error(w, "invalid path: expected /tasks/{taskId}/files/{path} or /tasks/{taskId}/diff/{path}", http.StatusBadRequest)
			return
		}

		taskID := parts[0]
		filePath := parts[1]

		// Look up the task's worktree.
		workDirVal, ok := d.taskWorkDirs.Load(taskID)
		if !ok {
			http.Error(w, "task not found or not active", http.StatusNotFound)
			return
		}
		workDir := workDirVal.(string)

		// Resolve and validate the full path.
		fullPath := filepath.Join(workDir, filePath)
		fullPath = filepath.Clean(fullPath)

		// Security: ensure path doesn't escape worktree root.
		if !strings.HasPrefix(fullPath, workDir) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		stat, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "file not found", http.StatusNotFound)
			} else {
				http.Error(w, "failed to stat file", http.StatusInternalServerError)
			}
			return
		}

		if stat.IsDir() {
			http.Error(w, "path is a directory", http.StatusBadRequest)
			return
		}

		// ETag for efficient polling.
		etag := fmt.Sprintf(`"%s-%s"`, strconv.FormatInt(stat.ModTime().UnixMilli(), 36), strconv.FormatInt(stat.Size(), 36))
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		// Size limit: 1MB.
		const maxFileSize = 1_048_576
		if stat.Size() > maxFileSize {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("ETag", etag)
			w.WriteHeader(422)
			json.NewEncoder(w).Encode(map[string]any{
				"error":   "too_large",
				"message": fmt.Sprintf("File is too large to preview (%.1fMB)", float64(stat.Size())/1024/1024),
				"path":    filePath,
				"size":    stat.Size(),
			})
			return
		}

		// Binary detection by extension.
		ext := strings.ToLower(filepath.Ext(filePath))
		if isBinaryExt(ext) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("ETag", etag)
			w.WriteHeader(422)
			json.NewEncoder(w).Encode(map[string]any{
				"error":   "binary",
				"message": "Binary file preview is not supported",
				"path":    filePath,
				"size":    stat.Size(),
			})
			return
		}

		content, err := os.ReadFile(fullPath)
		if err != nil {
			http.Error(w, "failed to read file", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", etag)
		json.NewEncoder(w).Encode(map[string]any{
			"content": string(content),
			"path":    filePath,
			"size":    stat.Size(),
			"mtime":   stat.ModTime().Format(time.RFC3339),
		})
	})

	srv := &http.Server{Handler: mux}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	d.logger.Info("health server listening", "addr", ln.Addr().String())
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		d.logger.Warn("health server error", "error", err)
	}
}

// serveTaskFileDiff returns a git diff of filePath (relative to the task's
// workdir) against the merge-base of the enclosing repo's branch. Handles two
// workdir layouts:
//  1. workdir is itself a git repo — diff the path directly.
//  2. workdir contains nested repos (top-level subdir per checked-out repo) —
//     find the subdir containing filePath and diff within it, using the
//     file's path relative to that subdir.
//
// Untracked files (not in any base commit) are returned as `content` instead
// of `diff` so the preview can still show something useful.
func (d *Daemon) serveTaskFileDiff(w http.ResponseWriter, r *http.Request, taskID, filePath string) {
	workDirVal, ok := d.taskWorkDirs.Load(taskID)
	if !ok {
		http.Error(w, "task not found or not active", http.StatusNotFound)
		return
	}
	workDir := workDirVal.(string)

	fullPath := filepath.Clean(filepath.Join(workDir, filePath))
	if !strings.HasPrefix(fullPath, workDir) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	// Binary shortcut — don't even try to diff binary files.
	ext := strings.ToLower(filepath.Ext(filePath))
	if isBinaryExt(ext) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   "binary",
			"message": "Binary file diff is not supported",
			"path":    filePath,
		})
		return
	}

	// Figure out which git repo contains this file.
	repoPath, relPath := findEnclosingRepo(workDir, filePath)
	if repoPath == "" {
		http.Error(w, "file is not in a git repo", http.StatusNotFound)
		return
	}

	// Use the same base-ref logic the scanner uses so status/diff agree.
	base := filetree.FindBaseRef(repoPath)

	// Determine whether the file is untracked (never committed on HEAD).
	// `git status --porcelain -- <file>` returns "?? path" for untracked.
	statusCmd := exec.Command("git", "-C", repoPath, "status", "--porcelain", "--", relPath)
	statusOut, _ := statusCmd.Output()
	var fileStatus string
	if len(statusOut) >= 2 {
		firstLine := strings.SplitN(string(statusOut), "\n", 2)[0]
		if strings.HasPrefix(firstLine, "??") {
			fileStatus = "?"
		}
	}

	// Untracked file — no diff to run; just return the raw content so the
	// preview shows what the agent added.
	if fileStatus == "?" {
		stat, err := os.Stat(fullPath)
		if err != nil || stat.IsDir() {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		const maxUntrackedSize = 1_048_576
		if stat.Size() > maxUntrackedSize {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(422)
			json.NewEncoder(w).Encode(map[string]any{
				"error":   "too_large",
				"message": fmt.Sprintf("File is too large to diff (%.1fMB)", float64(stat.Size())/1024/1024),
				"path":    filePath,
			})
			return
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			http.Error(w, "failed to read file", http.StatusInternalServerError)
			return
		}
		writeDiffJSON(w, r, filePath, "?", "", string(content))
		return
	}

	// Tracked file — run the actual diff against the base commit.
	args := []string{"-C", repoPath, "diff"}
	if base != "" {
		args = append(args, base)
	} else {
		args = append(args, "HEAD")
	}
	args = append(args, "--", relPath)

	diffCmd := exec.Command("git", args...)
	diffOut, err := diffCmd.Output()
	if err != nil {
		// Exit code 1 from git diff just means "there are differences" — not an error.
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]any{
				"error":   "git_failed",
				"message": err.Error(),
				"path":    filePath,
			})
			return
		}
	}

	diffText := string(diffOut)
	if strings.Contains(diffText, "Binary files ") && strings.Contains(diffText, " differ") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   "binary",
			"message": "Binary file diff is not supported",
			"path":    filePath,
		})
		return
	}

	const maxDiffBytes = 512 * 1024
	if len(diffText) > maxDiffBytes {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   "diff_too_large",
			"message": "Diff too large to display",
			"path":    filePath,
		})
		return
	}

	// Derive status from the first diff header line — falls back to M.
	statusCode := "M"
	if strings.Contains(diffText, "\nnew file mode") {
		statusCode = "A"
	} else if strings.Contains(diffText, "\ndeleted file mode") {
		statusCode = "D"
	}

	writeDiffJSON(w, r, filePath, statusCode, diffText, "")
}

// writeDiffJSON writes the standard diff-response envelope with an ETag
// computed from the diff body for cheap polling-based cache-validation.
func writeDiffJSON(w http.ResponseWriter, r *http.Request, filePath, status, diff, content string) {
	sum := sha256.Sum256([]byte(diff + content))
	etag := fmt.Sprintf(`"d-%s"`, hex.EncodeToString(sum[:])[:16])
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	body := map[string]any{
		"path":   filePath,
		"status": status,
	}
	if diff != "" {
		body["diff"] = diff
		body["content"] = nil
	} else {
		body["diff"] = nil
		body["content"] = content
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag)
	json.NewEncoder(w).Encode(body)
}

// findEnclosingRepo returns the repo path and the file path relative to that
// repo. Scans workDir first (case 1: workDir is a repo) then the top-level
// subdir containing filePath's first segment (case 2: nested repos).
func findEnclosingRepo(workDir, relFilePath string) (repoPath, relToRepo string) {
	if _, err := os.Stat(filepath.Join(workDir, ".git")); err == nil {
		return workDir, relFilePath
	}
	segments := strings.SplitN(relFilePath, "/", 2)
	if len(segments) < 2 {
		return "", ""
	}
	sub := filepath.Join(workDir, segments[0])
	if _, err := os.Stat(filepath.Join(sub, ".git")); err == nil {
		return sub, segments[1]
	}
	return "", ""
}

// isBinaryExt returns true for file extensions that are binary.
func isBinaryExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".webp",
		".woff", ".woff2", ".ttf", ".eot",
		".pdf", ".zip", ".tar", ".gz", ".7z",
		".exe", ".dll", ".so", ".dylib",
		".mp3", ".mp4", ".avi", ".mov", ".mkv",
		".sqlite", ".db":
		return true
	}
	return false
}
