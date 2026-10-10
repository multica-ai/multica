package handler

// Code changes for issues whose project works on a local_directory repository.
//
// The backend container cannot reach the host's filesystem, so this feature
// only works when the self-hosted deployment mounts the host repo into the
// container (see docker-compose.selfhost.yml) and MULTICA_HOST_REPO_ROOT /
// MULTICA_HOST_REPO_MOUNT tell us how to map host paths to container paths.
// Without the mount the endpoint returns available=false and the UI hides the
// section.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	maxCodeChangeFiles  = 100
	maxDiffBytesPerFile = 200 << 10 // 200 KiB per file's unified diff
	gitDiffTimeout      = 15 * time.Second
)

// CodeChangeFile is one changed file in the issue's head branch.
type CodeChangeFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"` // A=added M=modified D=deleted
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Diff      string `json:"diff"` // unified diff, possibly truncated
	Truncated bool   `json:"truncated"`
}

// CodeChangesResponse is the JSON shape of GET /api/issues/{id}/code-changes.
type CodeChangesResponse struct {
	Available bool             `json:"available"`
	Reason    string           `json:"reason,omitempty"`
	RepoPath  string           `json:"repo_path,omitempty"`
	BaseRef   string           `json:"base_ref,omitempty"`
	HeadRef   string           `json:"head_ref,omitempty"`
	Summary   string           `json:"summary,omitempty"`
	Files     []CodeChangeFile `json:"files"`
}

// GetIssueCodeChanges returns the local git diff between an issue's latest task
// branch and the repository's default branch, computed in the backend
// container against the mounted host repo.
func (h *Handler) GetIssueCodeChanges(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	empty := func(reason string) {
		writeJSON(w, http.StatusOK, CodeChangesResponse{Available: false, Reason: reason})
	}

	if !issue.ProjectID.Valid {
		empty("issue has no project")
		return
	}

	// 1. Find the project's local_directory resource.
	resources, err := h.Queries.ListProjectResources(r.Context(), issue.ProjectID)
	if err != nil {
		slog.Error("code-changes: list project resources", "project_id", issue.ProjectID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read project resources")
		return
	}
	var localRes *db.ProjectResource
	for i := range resources {
		if resources[i].ResourceType == "local_directory" {
			localRes = &resources[i]
			break
		}
	}
	if localRes == nil {
		empty("project has no local repository")
		return
	}

	var ref struct {
		LocalPath string `json:"local_path"`
	}
	if err := json.Unmarshal(localRes.ResourceRef, &ref); err != nil || ref.LocalPath == "" {
		empty("local repository path is missing")
		return
	}

	// 2. Find the issue's latest task branch (also carries the durable work dir).
	taskBranch, durableWorkDir, err := latestTaskBranchAndWorkDir(r.Context(), h, issue.ID.String())
	if err != nil {
		slog.Error("code-changes: latest task", "issue_id", issue.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read task state")
		return
	}
	if taskBranch == "" {
		empty("issue has no task branch yet")
		return
	}

	repoPath := durableWorkDir
	if repoPath == "" {
		repoPath = ref.LocalPath
	}

	// 3. Map the host path to the container path and locate git.
	mapped, reason := mapHostRepoPath(repoPath)
	if reason != "" {
		empty(reason)
		return
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		empty("git is not installed in the server container")
		return
	}

	// 4. Resolve the default branch as the diff base.
	baseRef := defaultBranch(r.Context(), gitBin, mapped)

	// 5. Verify the head branch exists before diffing (guards command injection
	// and bogus branch names from becoming git arguments).
	if !branchExists(r.Context(), gitBin, mapped, taskBranch) {
		empty("task branch not found in the repository")
		return
	}

	files, summary, err := gitDiff(r.Context(), gitBin, mapped, baseRef, taskBranch)
	if err != nil {
		slog.Error("code-changes: git diff", "repo", mapped, "base", baseRef, "head", taskBranch, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to compute diff")
		return
	}

	writeJSON(w, http.StatusOK, CodeChangesResponse{
		Available: true,
		RepoPath:  repoPath,
		BaseRef:   baseRef,
		HeadRef:   taskBranch,
		Summary:   summary,
		Files:     files,
	})
}

// latestTaskBranchAndWorkDir returns the most recent task branch_name and
// durable_work_dir for an issue, newest first. Either may be empty.
func latestTaskBranchAndWorkDir(ctx context.Context, h *Handler, issueID string) (branch, workDir string, err error) {
	row := h.DB.QueryRow(ctx, `
		SELECT branch_name, durable_work_dir
		FROM agent_task_queue
		WHERE issue_id = $1
		  AND branch_name IS NOT NULL AND branch_name <> ''
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, issueID)
	var b, w pgtype.Text
	if err := row.Scan(&b, &w); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil
		}
		return "", "", err
	}
	return b.String, w.String, nil
}

// mapHostRepoPath translates a host absolute path (e.g. D:\Multica) into the
// container mount path using MULTICA_HOST_REPO_ROOT and
// MULTICA_HOST_REPO_MOUNT. It returns the mapped path, or a reason when the
// repo sits outside the mounted root or the variables are unset.
//
// Host paths are Windows-style (drive letters + backslashes) even though the
// server runs inside a Linux container, so filepath.Abs would corrupt them;
// we normalize both sides to forward slashes ourselves and compare
// case-insensitively.
func mapHostRepoPath(hostPath string) (string, string) {
	root := strings.TrimSpace(os.Getenv("MULTICA_HOST_REPO_ROOT"))
	mount := strings.TrimSpace(os.Getenv("MULTICA_HOST_REPO_MOUNT"))
	if root == "" || mount == "" {
		return "", "local code changes require the host repo mount (MULTICA_HOST_REPO_ROOT/MULTICA_HOST_REPO_MOUNT)"
	}
	normRoot := normalizeHostPath(root)
	normHost := normalizeHostPath(hostPath)
	if !strings.EqualFold(normHost, normRoot) && !hasPathPrefix(normHost, normRoot) {
		return "", "repository is outside the mounted host root"
	}
	rel := strings.TrimPrefix(normHost, normRoot)
	rel = strings.TrimLeft(rel, "/")
	if rel == "" {
		return mount, ""
	}
	return strings.TrimRight(mount, "/") + "/" + rel, ""
}

// normalizeHostPath converts backslashes to forward slashes and trims
// trailing separators, so Windows host paths compare cleanly in the Linux
// container. Case is handled by EqualFold / hasPathPrefix.
func normalizeHostPath(p string) string {
	return strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
}

// hasPathPrefix reports whether child is p or lives under it, comparing
// case-insensitively. Inputs must already be normalized to forward slashes.
func hasPathPrefix(child, p string) bool {
	child = strings.TrimRight(child, "/")
	p = strings.TrimRight(p, "/")
	return strings.EqualFold(child, p) || strings.HasPrefix(strings.ToLower(child), strings.ToLower(p)+"/")
}

// defaultBranch resolves the repository's default branch, preferring the
// remote HEAD symbolic ref, then origin/main, then main.
func defaultBranch(ctx context.Context, gitBin, repo string) string {
	if out, err := gitOutput(ctx, gitBin, repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if b := strings.TrimSpace(string(out)); b != "" {
			return b
		}
	}
	for _, candidate := range []string{"origin/main", "main"} {
		if branchExists(ctx, gitBin, repo, candidate) {
			return candidate
		}
	}
	return "HEAD"
}

// branchExists verifies a ref resolves to a commit, so arbitrary strings never
// reach git as command arguments.
func branchExists(ctx context.Context, gitBin, repo, ref string) bool {
	ctx, cancel := context.WithTimeout(ctx, gitDiffTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitBin, "-C", repo, "rev-parse", "--verify", "--quiet", ref)
	return cmd.Run() == nil
}

// gitDiff computes per-file unified diffs between base and head.
func gitDiff(ctx context.Context, gitBin, repo, base, head string) ([]CodeChangeFile, string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitDiffTimeout)
	defer cancel()

	// numstat: "<add>\t<del>\t<path>" per changed file; paths may contain tabs.
	numstat, err := gitOutput(ctx, gitBin, repo, "diff", "--numstat", base+".."+head)
	if err != nil {
		return nil, "", err
	}

	var files []CodeChangeFile
	var totalAdd, totalDel, changed int
	scanner := bufio.NewScanner(bytes.NewReader(numstat))
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() && len(files) < maxCodeChangeFiles {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		add, errA := strconv.Atoi(parts[0])
		del, errD := strconv.Atoi(parts[1])
		if errA != nil || errD != nil {
			continue // "-" (binary) or malformed rows
		}
		path := parts[2]
		status := "M"
		if add > 0 && del == 0 {
			status = "A"
		} else if del > 0 && add == 0 {
			status = "D"
		}
		file := CodeChangeFile{Path: path, Status: status, Additions: add, Deletions: del}
		diffBody, truncated, err := fileDiff(ctx, gitBin, repo, base, head, path)
		if err == nil {
			file.Diff = diffBody
			file.Truncated = truncated
		}
		files = append(files, file)
		changed++
		totalAdd += add
		totalDel += del
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	summary := "0 files changed"
	if changed > 0 {
		summary = strconv.Itoa(changed) + plural(changed, " file", " files") + " changed, +" +
			strconv.Itoa(totalAdd) + " -" + strconv.Itoa(totalDel)
	}
	return files, summary, nil
}

// fileDiff returns the unified diff of one path between base and head,
// truncated to maxDiffBytesPerFile.
func fileDiff(ctx context.Context, gitBin, repo, base, head, path string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, gitDiffTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitBin, "-C", repo, "diff", "--no-color", "-U3", base+".."+head, "--", path)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", false, err
	}
	body := buf.String()
	if len(body) <= maxDiffBytesPerFile {
		return body, false, nil
	}
	// Cut on a line boundary near the limit so the frontend never renders a
	// half-line.
	cut := maxDiffBytesPerFile
	if idx := strings.LastIndexByte(body[:cut], '\n'); idx > 0 {
		cut = idx
	}
	return body[:cut] + "\n... (diff truncated)\n", true, nil
}

// gitOutput runs git with the given trailing args in repo and returns stdout.
func gitOutput(ctx context.Context, gitBin, repo string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitDiffTimeout)
	defer cancel()
	args = append([]string{"-C", repo}, args...)
	return exec.CommandContext(ctx, gitBin, args...).Output()
}

func plural(n int, single, plural string) string {
	if n == 1 {
		return single
	}
	return plural
}
