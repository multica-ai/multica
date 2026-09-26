package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Run code changes (MUL-7651). When a run ends, the daemon diffs what it
// changed and uploads the result here; the issue page shows it as the run's
// "changes" card and opens it in the diff viewer. The whole-issue view prefers
// the pull request's own diff and falls back to the branch diff the daemon
// captured alongside.

const (
	// maxCodeChangePatchBytes caps one stored patch. The daemon holds back
	// anything larger and sends only the file list, so a patch over this is a
	// daemon that does not know the cap — stored the same way.
	maxCodeChangePatchBytes = 1 << 20
	// maxCodeChangeFiles caps one file list, matching GitHub's own ceiling
	// for a pull request.
	maxCodeChangeFiles = 3000
	// maxCodeChangesPerReport bounds one upload: a run reports one or two
	// rows per repository it changed.
	maxCodeChangesPerReport = 16
	// maxCodeChangeReportBytes bounds the upload body: every patch at its
	// cap, JSON-escaped, plus the file lists.
	maxCodeChangeReportBytes = 48 << 20
	// maxPullRequestPatchBytes caps the patch assembled from a pull request's
	// file list. Past it the viewer shows the file list only.
	maxPullRequestPatchBytes = 2 << 20
	pullRequestFilesTimeout  = 30 * time.Second
)

var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// CodeChangeFile is one file of a code change.
type CodeChangeFile struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	// Status is added, modified, deleted, renamed, copied or type_changed.
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
}

var codeChangeFileStatuses = map[string]bool{
	"added": true, "modified": true, "deleted": true,
	"renamed": true, "copied": true, "type_changed": true,
}

// TaskCodeChangeResponse is one run's code change in one repository, without
// its file list or patch.
type TaskCodeChangeResponse struct {
	ID         string `json:"id"`
	IssueID    string `json:"issue_id"`
	TaskID     string `json:"task_id"`
	AgentID    string `json:"agent_id"`
	Scope      string `json:"scope"`
	Source     string `json:"source"`
	RepoKey    string `json:"repo_key"`
	RepoLabel  string `json:"repo_label"`
	RepoURL    string `json:"repo_url"`
	Branch     string `json:"branch"`
	BaseRef    string `json:"base_ref"`
	BaseCommit string `json:"base_commit"`
	HeadCommit string `json:"head_commit"`
	FileCount  int32  `json:"file_count"`
	Additions  int32  `json:"additions"`
	Deletions  int32  `json:"deletions"`
	// FilesTruncated reports that the file list stops short of FileCount.
	FilesTruncated bool  `json:"files_truncated"`
	PatchAvailable bool  `json:"patch_available"`
	PatchSize      int64 `json:"patch_size"`
	// PatchOmitted says why there is no patch: "too_large" or "unavailable".
	PatchOmitted *string `json:"patch_omitted"`
	CreatedAt    string  `json:"created_at"`
}

// TaskCodeChangeDetailResponse adds the file list and the patch text.
type TaskCodeChangeDetailResponse struct {
	TaskCodeChangeResponse
	Files []CodeChangeFile `json:"files"`
	Patch *string          `json:"patch"`
}

// PullRequestDiffResponse is a pull request's changes as GitHub lists them.
type PullRequestDiffResponse struct {
	PullRequestID  string           `json:"pull_request_id"`
	HeadSHA        string           `json:"head_sha"`
	FileCount      int              `json:"file_count"`
	Additions      int              `json:"additions"`
	Deletions      int              `json:"deletions"`
	Files          []CodeChangeFile `json:"files"`
	FilesTruncated bool             `json:"files_truncated"`
	Patch          *string          `json:"patch"`
	PatchOmitted   *string          `json:"patch_omitted"`
}

func taskCodeChangeListRowToResponse(c db.ListTaskCodeChangesByIssueRow) TaskCodeChangeResponse {
	return TaskCodeChangeResponse{
		ID:             uuidToString(c.ID),
		IssueID:        uuidToString(c.IssueID),
		TaskID:         uuidToString(c.TaskID),
		AgentID:        uuidToString(c.AgentID),
		Scope:          c.Scope,
		Source:         c.Source,
		RepoKey:        c.RepoKey,
		RepoLabel:      c.RepoLabel,
		RepoURL:        c.RepoUrl,
		Branch:         c.Branch,
		BaseRef:        c.BaseRef,
		BaseCommit:     c.BaseCommit,
		HeadCommit:     c.HeadCommit,
		FileCount:      c.FileCount,
		Additions:      c.Additions,
		Deletions:      c.Deletions,
		FilesTruncated: c.FilesTruncated,
		PatchAvailable: c.PatchAvailable,
		PatchSize:      c.PatchSize,
		PatchOmitted:   textToPtr(c.PatchOmitted),
		CreatedAt:      timestampToString(c.CreatedAt),
	}
}

func taskCodeChangeToResponse(c db.TaskCodeChange) TaskCodeChangeResponse {
	return taskCodeChangeListRowToResponse(db.ListTaskCodeChangesByIssueRow{
		ID: c.ID, WorkspaceID: c.WorkspaceID, IssueID: c.IssueID, TaskID: c.TaskID, AgentID: c.AgentID,
		Scope: c.Scope, Source: c.Source, RepoKey: c.RepoKey, RepoLabel: c.RepoLabel, RepoUrl: c.RepoUrl,
		Branch: c.Branch, BaseRef: c.BaseRef, BaseCommit: c.BaseCommit, HeadCommit: c.HeadCommit,
		FileCount: c.FileCount, Additions: c.Additions, Deletions: c.Deletions, FilesTruncated: c.FilesTruncated,
		PatchAvailable: c.PatchUrl.Valid, PatchSize: c.PatchSize, PatchOmitted: c.PatchOmitted, CreatedAt: c.CreatedAt,
	})
}

// ---------------------------------------------------------------------------
// Daemon upload — POST /api/daemon/tasks/{taskId}/code-changes
// ---------------------------------------------------------------------------

// codeChangeReport is one row of a daemon upload.
type codeChangeReport struct {
	Scope          string           `json:"scope"`
	Source         string           `json:"source"`
	RepoKey        string           `json:"repo_key"`
	RepoLabel      string           `json:"repo_label"`
	RepoURL        string           `json:"repo_url"`
	Branch         string           `json:"branch"`
	BaseRef        string           `json:"base_ref"`
	BaseCommit     string           `json:"base_commit"`
	HeadCommit     string           `json:"head_commit"`
	FileCount      int              `json:"file_count"`
	Additions      int              `json:"additions"`
	Deletions      int              `json:"deletions"`
	Files          []CodeChangeFile `json:"files"`
	FilesTruncated bool             `json:"files_truncated"`
	Patch          string           `json:"patch"`
	PatchOmitted   string           `json:"patch_omitted"`
}

// ReportTaskCodeChanges stores a finished run's code changes. Only issue runs
// have somewhere to show them; any other run is acknowledged and dropped.
func (h *Handler) ReportTaskCodeChanges(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCodeChangeReportBytes)
	var req struct {
		Changes []codeChangeReport `json:"changes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Changes) > maxCodeChangesPerReport {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d code changes per report", maxCodeChangesPerReport))
		return
	}
	if !task.IssueID.Valid {
		writeJSON(w, http.StatusOK, map[string]any{"stored": 0})
		return
	}
	for i := range req.Changes {
		if err := normalizeCodeChangeReport(&req.Changes[i]); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("changes[%d]: %v", i, err))
			return
		}
	}

	stored := 0
	for _, change := range req.Changes {
		created, err := h.storeTaskCodeChange(r.Context(), task, workspaceID, change)
		if err != nil {
			slog.Error("store run code change failed", "task_id", taskID, "scope", change.Scope, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to store code change")
			return
		}
		if created {
			stored++
		}
	}
	if stored > 0 {
		h.publish(protocol.EventCodeChangesCreated, workspaceID, "agent", uuidToString(task.AgentID), map[string]any{
			"issue_id": uuidToString(task.IssueID),
			"task_id":  uuidToString(task.ID),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"stored": stored})
}

// storeTaskCodeChange writes the patch to object storage, then the row. The
// row is keyed by (run, scope, repository); when it already exists — a retried
// upload — the object just written is removed again and nothing changes.
func (h *Handler) storeTaskCodeChange(ctx context.Context, task db.AgentTaskQueue, workspaceID string, change codeChangeReport) (bool, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return false, fmt.Errorf("generate id: %w", err)
	}
	files, err := json.Marshal(change.Files)
	if err != nil {
		return false, fmt.Errorf("encode files: %w", err)
	}
	params := db.CreateTaskCodeChangeParams{
		ID:             pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID:    parseUUID(workspaceID),
		IssueID:        task.IssueID,
		TaskID:         task.ID,
		AgentID:        task.AgentID,
		Scope:          change.Scope,
		Source:         change.Source,
		RepoKey:        change.RepoKey,
		RepoLabel:      change.RepoLabel,
		RepoUrl:        change.RepoURL,
		Branch:         change.Branch,
		BaseRef:        change.BaseRef,
		BaseCommit:     change.BaseCommit,
		HeadCommit:     change.HeadCommit,
		FileCount:      clampInt32(change.FileCount),
		Additions:      clampInt32(change.Additions),
		Deletions:      clampInt32(change.Deletions),
		Files:          files,
		FilesTruncated: change.FilesTruncated,
	}
	if change.PatchOmitted != "" {
		params.PatchOmitted = pgtype.Text{String: change.PatchOmitted, Valid: true}
	}

	var patchURL string
	if change.Patch != "" {
		if h.Storage == nil {
			params.PatchOmitted = pgtype.Text{String: "unavailable", Valid: true}
		} else {
			key := "workspaces/" + workspaceID + "/code-changes/" + id.String() + ".patch"
			filename := sanitizePatchFilename(change.RepoLabel) + "-" + change.Scope + ".patch"
			link, uploadErr := h.Storage.Upload(ctx, key, []byte(change.Patch), "text/x-diff; charset=utf-8", filename)
			if uploadErr != nil {
				return false, fmt.Errorf("upload patch: %w", uploadErr)
			}
			patchURL = link
			params.PatchUrl = pgtype.Text{String: link, Valid: true}
			params.PatchSize = int64(len(change.Patch))
		}
	}

	if _, err := h.Queries.CreateTaskCodeChange(ctx, params); err != nil {
		if patchURL != "" {
			h.deleteS3Objects(ctx, []string{patchURL})
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// normalizeCodeChangeReport validates one uploaded row and clamps it to what
// the table stores. It rejects what cannot be displayed correctly (an unknown
// scope, a malformed commit) and trims what can.
func normalizeCodeChangeReport(c *codeChangeReport) error {
	if c.Scope != "run" && c.Scope != "branch" {
		return fmt.Errorf("invalid scope %q", c.Scope)
	}
	if c.Source != "local_worktree" && c.Source != "repo_checkout" {
		return fmt.Errorf("invalid source %q", c.Source)
	}
	c.BaseCommit = strings.ToLower(strings.TrimSpace(c.BaseCommit))
	c.HeadCommit = strings.ToLower(strings.TrimSpace(c.HeadCommit))
	if !commitSHAPattern.MatchString(c.BaseCommit) || !commitSHAPattern.MatchString(c.HeadCommit) {
		return errors.New("base_commit and head_commit must be commit ids")
	}
	c.RepoKey = strings.TrimSpace(c.RepoKey)
	if c.RepoKey == "" || len(c.RepoKey) > 1024 {
		return errors.New("repo_key must be 1-1024 bytes")
	}
	c.RepoLabel = truncateUTF8(strings.TrimSpace(c.RepoLabel), 256)
	if c.RepoLabel == "" {
		c.RepoLabel = "repository"
	}
	c.RepoURL = sanitizeRepoURL(c.RepoURL)
	c.Branch = truncateUTF8(strings.TrimSpace(c.Branch), 512)
	c.BaseRef = truncateUTF8(strings.TrimSpace(c.BaseRef), 512)

	if len(c.Files) > maxCodeChangeFiles {
		c.Files = c.Files[:maxCodeChangeFiles]
		c.FilesTruncated = true
	}
	for i := range c.Files {
		f := &c.Files[i]
		f.Path = truncateUTF8(f.Path, 4096)
		f.OldPath = truncateUTF8(f.OldPath, 4096)
		if !codeChangeFileStatuses[f.Status] {
			f.Status = "modified"
		}
		f.Additions = max(f.Additions, 0)
		f.Deletions = max(f.Deletions, 0)
	}
	c.FileCount = max(c.FileCount, len(c.Files))
	c.Additions = max(c.Additions, 0)
	c.Deletions = max(c.Deletions, 0)

	switch c.PatchOmitted {
	case "", "too_large":
	default:
		c.PatchOmitted = "unavailable"
	}
	if len(c.Patch) > maxCodeChangePatchBytes {
		c.Patch = ""
		c.PatchOmitted = "too_large"
	}
	if c.Patch != "" {
		c.PatchOmitted = ""
	}
	return nil
}

// sanitizeRepoURL keeps a remote URL that can name a repository and drops any
// credentials embedded in it. scp-style remotes (git@host:owner/repo) carry no
// secret beyond the user name and are kept as they are.
func sanitizeRepoURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return ""
	}
	if !strings.Contains(raw, "://") {
		if strings.ContainsAny(raw, " \t\r\n") {
			return ""
		}
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func sanitizePatchFilename(label string) string {
	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-.")
	if name == "" {
		return "changes"
	}
	return truncateUTF8(name, 80)
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func clampInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	if n < 0 {
		return 0
	}
	return int32(n)
}

// ---------------------------------------------------------------------------
// Reads — GET /api/issues/{id}/code-changes[/{changeId}]
// ---------------------------------------------------------------------------

func (h *Handler) ListIssueCodeChanges(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListTaskCodeChangesByIssue(r.Context(), db.ListTaskCodeChangesByIssueParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list code changes")
		return
	}
	out := make([]TaskCodeChangeResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, taskCodeChangeListRowToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"code_changes": out})
}

func (h *Handler) GetIssueCodeChange(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	changeID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "changeId"), "change id")
	if !ok {
		return
	}
	change, err := h.Queries.GetTaskCodeChange(r.Context(), db.GetTaskCodeChangeParams{
		ID: changeID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || change.IssueID != issue.ID {
		writeError(w, http.StatusNotFound, "code change not found")
		return
	}

	resp := TaskCodeChangeDetailResponse{
		TaskCodeChangeResponse: taskCodeChangeToResponse(change),
		Files:                  []CodeChangeFile{},
	}
	if err := json.Unmarshal(change.Files, &resp.Files); err != nil {
		slog.Warn("decode code change files failed", "id", uuidToString(change.ID), "error", err)
		resp.Files = []CodeChangeFile{}
	}
	if change.PatchUrl.Valid {
		patch, err := h.readCodeChangePatch(r.Context(), change.PatchUrl.String)
		if err != nil {
			slog.Warn("read code change patch failed", "id", uuidToString(change.ID), "error", err)
			unavailable := "unavailable"
			resp.PatchAvailable = false
			resp.PatchOmitted = &unavailable
		} else {
			resp.Patch = &patch
		}
	}
	// Never cached: the patch is workspace-private and membership can change.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) readCodeChangePatch(ctx context.Context, patchURL string) (string, error) {
	if h.Storage == nil {
		return "", errors.New("storage not configured")
	}
	reader, err := h.Storage.GetReader(ctx, h.Storage.KeyFromURL(patchURL))
	if err != nil {
		return "", err
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, maxCodeChangePatchBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxCodeChangePatchBytes {
		return "", errors.New("stored patch exceeds the size cap")
	}
	return string(body), nil
}

// ---------------------------------------------------------------------------
// Pull request diff — GET /api/issues/{id}/pull-requests/{prId}/diff
// ---------------------------------------------------------------------------

// GetIssuePullRequestDiff reads a linked GitHub pull request's changes through
// the GitHub App. Anything that keeps it from answering — the App not
// configured, a PR from another provider, GitHub refusing — is an error the
// viewer answers by showing the branch diff the daemon captured instead.
func (h *Handler) GetIssuePullRequestDiff(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	prID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "prId"), "pull request id")
	if !ok {
		return
	}
	pr, err := h.Queries.GetGitHubPullRequestInWorkspace(r.Context(), db.GetGitHubPullRequestInWorkspaceParams{
		ID: prID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	linked, err := h.Queries.ListIssueIDsForPullRequest(r.Context(), pr.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load pull request")
		return
	}
	isLinked := false
	for _, id := range linked {
		if id == issue.ID {
			isLinked = true
			break
		}
	}
	if !isLinked {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), pullRequestFilesTimeout)
	defer cancel()
	files, truncated, err := h.PRRefresh.PullRequestFiles(ctx, pr.InstallationID, pr.RepoOwner, pr.RepoName, pr.PrNumber, maxCodeChangeFiles)
	if err != nil {
		if errors.Is(err, ghsnapshot.ErrDisabled) {
			writeFeatureDisabled(w, "github_api_not_configured", "the GitHub App API is not configured")
			return
		}
		slog.Warn("list pull request files failed", "pr_id", uuidToString(pr.ID), "error", err)
		writeError(w, http.StatusBadGateway, "could not read the pull request from GitHub")
		return
	}
	resp := pullRequestDiffFromFiles(files, maxPullRequestPatchBytes)
	resp.PullRequestID = uuidToString(pr.ID)
	resp.HeadSHA = pr.HeadSha
	resp.FilesTruncated = truncated
	if truncated && int(pr.ChangedFiles) > resp.FileCount {
		resp.FileCount = int(pr.ChangedFiles)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// pullRequestDiffFromFiles turns GitHub's per-file hunks into one git-style
// patch the viewer parses like any other. A file GitHub gave no hunks for
// (binary, or too large to diff) stays in the file list and out of the patch.
func pullRequestDiffFromFiles(files []ghsnapshot.PullRequestFile, maxPatchBytes int) PullRequestDiffResponse {
	resp := PullRequestDiffResponse{Files: make([]CodeChangeFile, 0, len(files))}
	var patch strings.Builder
	for _, f := range files {
		status := githubFileStatus(f.Status)
		oldPath := f.Filename
		if f.PreviousFilename != "" {
			oldPath = f.PreviousFilename
		}
		file := CodeChangeFile{
			Path:      f.Filename,
			Status:    status,
			Additions: f.Additions,
			Deletions: f.Deletions,
			Binary:    f.Patch == "" && f.Additions == 0 && f.Deletions == 0 && status != "renamed",
		}
		if oldPath != f.Filename {
			file.OldPath = oldPath
		}
		resp.Files = append(resp.Files, file)
		resp.Additions += f.Additions
		resp.Deletions += f.Deletions
		if f.Patch == "" {
			continue
		}
		fmt.Fprintf(&patch, "diff --git a/%s b/%s\n", oldPath, f.Filename)
		from, to := "a/"+oldPath, "b/"+f.Filename
		switch status {
		case "added":
			patch.WriteString("new file mode 100644\n")
			from = "/dev/null"
		case "deleted":
			patch.WriteString("deleted file mode 100644\n")
			to = "/dev/null"
		case "renamed":
			fmt.Fprintf(&patch, "rename from %s\nrename to %s\n", oldPath, f.Filename)
		}
		fmt.Fprintf(&patch, "--- %s\n+++ %s\n", from, to)
		patch.WriteString(f.Patch)
		if !strings.HasSuffix(f.Patch, "\n") {
			patch.WriteByte('\n')
		}
	}
	resp.FileCount = len(resp.Files)
	if patch.Len() > maxPatchBytes {
		tooLarge := "too_large"
		resp.PatchOmitted = &tooLarge
	} else if patch.Len() > 0 {
		text := patch.String()
		resp.Patch = &text
	}
	return resp
}

func githubFileStatus(status string) string {
	switch status {
	case "added":
		return "added"
	case "removed":
		return "deleted"
	case "renamed":
		return "renamed"
	case "copied":
		return "copied"
	default:
		return "modified"
	}
}
