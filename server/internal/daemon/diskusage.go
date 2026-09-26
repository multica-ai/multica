package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// TaskDiskUsage describes one task workdir's footprint on disk.
//
// ParentID is the id of the record that governs this directory's lifecycle,
// discriminated by Kind (issue id, chat session id, autopilot run id, or task
// id). ParentStatus is that record's current status; it stays empty until
// ResolveParentStatuses fills it in, because ScanDiskUsage itself is purely
// local and .gc_meta.json does not persist a status.
type TaskDiskUsage struct {
	WorkspaceID       string `json:"workspace_id"`
	WorkspaceShort    string `json:"workspace_short"`
	TaskShort         string `json:"task_short"`
	Path              string `json:"path"`
	Kind              string `json:"kind"`
	ParentID          string `json:"parent_id,omitempty"`
	ParentStatus      string `json:"parent_status"`
	AgeSeconds        int64  `json:"age_seconds"`
	SizeBytes         int64  `json:"size_bytes"`
	ArtifactSizeBytes int64  `json:"artifact_size_bytes"`
}

// WorkspaceDiskUsage aggregates per-workspace footprint across all tasks.
// ArtifactRatio is the fraction (0..1) of SizeBytes that the GC artifact
// cleanup could reclaim — kept here so the JSON consumer doesn't have to
// re-derive it (and so the table view can render the column without dividing
// by zero on empty workspaces).
type WorkspaceDiskUsage struct {
	WorkspaceID       string  `json:"workspace_id"`
	WorkspaceShort    string  `json:"workspace_short"`
	TaskCount         int     `json:"task_count"`
	SizeBytes         int64   `json:"size_bytes"`
	ArtifactSizeBytes int64   `json:"artifact_size_bytes"`
	ArtifactRatio     float64 `json:"artifact_ratio"`
	OldestAgeSeconds  int64   `json:"oldest_age_seconds"`
}

// DiskUsageReport is the full result of a single ScanDiskUsage call. Total*
// fields always reflect the entire scan, never the post-`--top` truncated
// view — consumers that need the displayed subtotals can sum the slice.
type DiskUsageReport struct {
	WorkspacesRoot          string               `json:"workspaces_root"`
	GeneratedAt             time.Time            `json:"generated_at"`
	ArtifactPatterns        []string             `json:"artifact_patterns"`
	ManagedArtifactSubpaths []string             `json:"managed_artifact_subpaths"`
	Tasks                   []TaskDiskUsage      `json:"tasks"`
	Workspaces              []WorkspaceDiskUsage `json:"workspaces"`
	TotalTaskCount          int                  `json:"total_task_count"`
	TotalWorkspaceCount     int                  `json:"total_workspace_count"`
	TotalSizeBytes          int64                `json:"total_size_bytes"`
	TotalArtifactSizeBytes  int64                `json:"total_artifact_size_bytes"`
	TotalArtifactRatio      float64              `json:"total_artifact_ratio"`
	// RepoCacheSizeBytes is the bare-repo cache (.repos) footprint. It is a
	// sibling of the task directories, not one of them, so it is reported
	// separately and deliberately excluded from Total*: those totals describe
	// task dirs, and folding a shared cache into them would double-count it
	// against per-task numbers that do not contain it.
	RepoCacheSizeBytes int64 `json:"repo_cache_size_bytes"`
	RepoCacheCount     int   `json:"repo_cache_count"`
}

// DiskUsageRoot pairs a workspaces root with the profile it was derived from
// ("" = the default root). ScanDiskUsageRoots scans each one and labels its
// report with the profile so the cross-root view can attribute footprint back
// to a profile (e.g. the Desktop app's `desktop-<host>` root).
type DiskUsageRoot struct {
	Profile string
	Root    string
}

// RootDiskUsage is one root's report inside an AggregateDiskUsageReport.
type RootDiskUsage struct {
	Profile string          `json:"profile"`
	Report  DiskUsageReport `json:"report"`
}

// AggregateDiskUsageReport is the result of scanning several workspace roots in
// one pass. Total* fields are the grand totals across every root's full scan
// (never the post-`--top` truncated view) so the combined footprint stays
// accurate even when each root's table is trimmed for display.
type AggregateDiskUsageReport struct {
	GeneratedAt             time.Time       `json:"generated_at"`
	ArtifactPatterns        []string        `json:"artifact_patterns"`
	ManagedArtifactSubpaths []string        `json:"managed_artifact_subpaths"`
	Roots                   []RootDiskUsage `json:"roots"`
	TotalTaskCount          int             `json:"total_task_count"`
	TotalWorkspaceCount     int             `json:"total_workspace_count"`
	TotalSizeBytes          int64           `json:"total_size_bytes"`
	TotalArtifactSizeBytes  int64           `json:"total_artifact_size_bytes"`
	TotalArtifactRatio      float64         `json:"total_artifact_ratio"`
	TotalRepoCacheSizeBytes int64           `json:"total_repo_cache_size_bytes"`
	TotalRepoCacheCount     int             `json:"total_repo_cache_count"`
}

// WorkspaceReapSnapshot captures the task-root footprint before or after a
// reaper run. TaskRootRecordCount is the number of stable-root records under
// .task_roots; TaskRootCount and TotalSizeBytes are from ScanDiskUsage.
type WorkspaceReapSnapshot struct {
	TaskRootCount       int   `json:"task_root_count"`
	TaskRootRecordCount int   `json:"task_root_record_count"`
	TotalSizeBytes      int64 `json:"total_size_bytes"`
}

// WorkspaceReapResult records the decision for one scanned task root. Action
// is one of would_remove, removed, skipped, or failed.
type WorkspaceReapResult struct {
	Path         string   `json:"path"`
	ParentID     string   `json:"parent_id,omitempty"`
	ParentStatus string   `json:"parent_status"`
	Action       string   `json:"action"`
	Reason       string   `json:"reason"`
	Files        []string `json:"files,omitempty"`
}

// WorkspaceReapReport is the complete, per-root outcome of a workspace reap.
// Reaping is dry-run unless Applied is true.
type WorkspaceReapReport struct {
	WorkspacesRoot string                `json:"workspaces_root"`
	Applied        bool                  `json:"applied"`
	Before         WorkspaceReapSnapshot `json:"before"`
	After          WorkspaceReapSnapshot `json:"after"`
	Results        []WorkspaceReapResult `json:"results"`
}

type workspaceReapOptions struct {
	inspectTree func(context.Context, string) ([]string, error)
	removeRoot  func(context.Context, string, string, func(context.Context, string) ([]string, error)) error
}

type workspaceReapRefusal struct {
	reason string
	files  []string
}

func (e *workspaceReapRefusal) Error() string { return e.reason }

// Deterministic seams for identity-swap regressions around the reap lock.
// They are nil outside tests.
var (
	reapLockTestHook          func()
	reapBeforeRemovalTestHook func()
)

// ScanDiskUsageRoots scans every root in order and returns the combined report.
// It reuses ScanDiskUsage per root — a missing root yields an empty per-root
// report (not an error), matching the single-root command, so a never-used
// profile root simply contributes zero. A genuinely unreadable root (e.g. a
// permission error on the root directory itself) aborts the whole scan.
func ScanDiskUsageRoots(roots []DiskUsageRoot, artifactPatterns []string) (AggregateDiskUsageReport, error) {
	agg := AggregateDiskUsageReport{GeneratedAt: time.Now().UTC()}
	matcher := newArtifactMatcher(artifactPatterns, execenv.ManagedReclaimableArtifactSubpaths())
	agg.ArtifactPatterns = sortedKeys(matcher.basenames)
	agg.ManagedArtifactSubpaths = matcher.managedSubpaths()

	for _, r := range roots {
		report, err := ScanDiskUsage(r.Root, artifactPatterns)
		if err != nil {
			return agg, err
		}
		agg.Roots = append(agg.Roots, RootDiskUsage{Profile: r.Profile, Report: report})
		agg.TotalTaskCount += report.TotalTaskCount
		agg.TotalWorkspaceCount += report.TotalWorkspaceCount
		agg.TotalSizeBytes += report.TotalSizeBytes
		agg.TotalArtifactSizeBytes += report.TotalArtifactSizeBytes
		agg.TotalRepoCacheSizeBytes += report.RepoCacheSizeBytes
		agg.TotalRepoCacheCount += report.RepoCacheCount
	}
	agg.TotalArtifactRatio = ratio(agg.TotalArtifactSizeBytes, agg.TotalSizeBytes)
	return agg, nil
}

// DiskUsageKindUnknown is the kind reported for task directories whose
// .gc_meta.json is missing or unreadable. Mirrors how the GC orphan path
// treats them — present on disk, but no parent record we can lock onto.
const DiskUsageKindUnknown = "unknown"

// ScanDiskUsage walks workspacesRoot and returns the disk-usage report. The
// walk is read-only, never follows symlinks, and counts only regular files.
// artifactPatterns is filtered through the basename-only check used by
// cleanTaskArtifacts, and exact daemon-managed artifact paths are included, so
// the reported "artifact" footprint matches the bytes the GC would actually
// reclaim. A .git subtree counts toward the total but never toward that
// artifact footprint — see taskSize. Missing roots return an empty report
// (not an error) — a daemon that's never run yet has no directory to walk.
//
// The scan is purely local. ParentStatus is left empty; callers that want the
// STATUS column populated run ResolveParentStatuses afterwards.
func ScanDiskUsage(workspacesRoot string, artifactPatterns []string) (DiskUsageReport, error) {
	report := DiskUsageReport{
		WorkspacesRoot:   workspacesRoot,
		GeneratedAt:      time.Now().UTC(),
		ArtifactPatterns: nil,
	}
	if workspacesRoot == "" {
		return report, fmt.Errorf("disk-usage: workspaces root is required")
	}

	matcher := newArtifactMatcher(artifactPatterns, execenv.ManagedReclaimableArtifactSubpaths())
	report.ArtifactPatterns = sortedKeys(matcher.basenames)
	report.ManagedArtifactSubpaths = matcher.managedSubpaths()

	wsEntries, err := os.ReadDir(workspacesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return report, nil
		}
		return report, fmt.Errorf("disk-usage: read workspaces root: %w", err)
	}

	wsAgg := map[string]*WorkspaceDiskUsage{}

	for _, wsEntry := range wsEntries {
		if !wsEntry.IsDir() {
			continue
		}
		// The bare-repo cache is not a workspace. Measure it separately rather
		// than skipping it outright: it is reclaimed on its own schedule
		// (GCRepoTTL) and used to be invisible here, which made the reported
		// total disagree with the user's file manager for no stated reason.
		if wsEntry.Name() == reposDirName {
			report.RepoCacheSizeBytes, report.RepoCacheCount = repoCacheSize(filepath.Join(workspacesRoot, wsEntry.Name()))
			continue
		}
		// Other dot-directories are daemon-internal caches (skill bundles and
		// friends), never workspaces. Counting them as workspaces put rows like
		// ".skillca" in the per-workspace table.
		if strings.HasPrefix(wsEntry.Name(), ".") {
			continue
		}
		physicalWorkspace := wsEntry.Name()
		wsDir := filepath.Join(workspacesRoot, physicalWorkspace)
		taskEntries, err := os.ReadDir(wsDir)
		if err != nil {
			continue
		}
		for _, t := range taskEntries {
			if !t.IsDir() {
				continue
			}
			taskDir := filepath.Join(wsDir, t.Name())
			usage := buildTaskUsage(taskDir, physicalWorkspace, t.Name(), matcher)

			report.Tasks = append(report.Tasks, usage)
			report.TotalSizeBytes += usage.SizeBytes
			report.TotalArtifactSizeBytes += usage.ArtifactSizeBytes

			workspaceID := usage.WorkspaceID
			ws, ok := wsAgg[workspaceID]
			if !ok {
				ws = &WorkspaceDiskUsage{
					WorkspaceID:    workspaceID,
					WorkspaceShort: usage.WorkspaceShort,
				}
				wsAgg[workspaceID] = ws
			}
			ws.TaskCount++
			ws.SizeBytes += usage.SizeBytes
			ws.ArtifactSizeBytes += usage.ArtifactSizeBytes
			if usage.AgeSeconds > ws.OldestAgeSeconds {
				ws.OldestAgeSeconds = usage.AgeSeconds
			}
		}
	}

	sort.Slice(report.Tasks, func(i, j int) bool {
		return report.Tasks[i].SizeBytes > report.Tasks[j].SizeBytes
	})

	report.Workspaces = make([]WorkspaceDiskUsage, 0, len(wsAgg))
	for _, ws := range wsAgg {
		ws.ArtifactRatio = ratio(ws.ArtifactSizeBytes, ws.SizeBytes)
		report.Workspaces = append(report.Workspaces, *ws)
	}
	sort.Slice(report.Workspaces, func(i, j int) bool {
		return report.Workspaces[i].SizeBytes > report.Workspaces[j].SizeBytes
	})

	report.TotalTaskCount = len(report.Tasks)
	report.TotalWorkspaceCount = len(report.Workspaces)
	report.TotalArtifactRatio = ratio(report.TotalArtifactSizeBytes, report.TotalSizeBytes)

	return report, nil
}

// repoCacheSize measures the bare-repo cache and counts the repos in it.
// Layout is .repos/<workspace-id>/<repo-dir>, so the count is the number of
// second-level directories — the unit the GC evicts.
func repoCacheSize(reposRoot string) (sizeBytes int64, repoCount int) {
	wsEntries, err := os.ReadDir(reposRoot)
	if err != nil {
		return 0, 0
	}
	for _, wsEntry := range wsEntries {
		if !wsEntry.IsDir() {
			continue
		}
		wsDir := filepath.Join(reposRoot, wsEntry.Name())
		repoEntries, err := os.ReadDir(wsDir)
		if err != nil {
			continue
		}
		for _, repoEntry := range repoEntries {
			if !repoEntry.IsDir() {
				continue
			}
			repoCount++
			sizeBytes += dirSize(filepath.Join(wsDir, repoEntry.Name()))
		}
	}
	return sizeBytes, repoCount
}

// ratio returns numerator / denominator, mapping 0/0 (and any 0 denominator)
// to 0 instead of NaN. Callers render the result as a percentage so a NaN
// would surface as "NaN%" in the table — guard at the source.
func ratio(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func buildPatternSet(patterns []string) map[string]struct{} {
	set := make(map[string]struct{}, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" || strings.ContainsAny(p, "/\\") {
			continue
		}
		set[p] = struct{}{}
	}
	return set
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func buildTaskUsage(taskDir, wsID, taskShort string, matcher artifactMatcher) TaskDiskUsage {
	usage := TaskDiskUsage{
		WorkspaceID:    wsID,
		WorkspaceShort: ShortID(wsID),
		TaskShort:      taskShort,
		Path:           taskDir,
		Kind:           DiskUsageKindUnknown,
	}

	metaPresent := false
	if provenance, err := execenv.ReadManagedEnvProvenance(taskDir); err == nil && provenance != nil {
		if workspaceID := strings.TrimSpace(provenance.WorkspaceID); workspaceID != "" {
			usage.WorkspaceID = workspaceID
			usage.WorkspaceShort = ShortID(workspaceID)
		}
	}
	if owner, err := execenv.ReadEnvRootOwner(taskDir); err == nil && owner != nil {
		if workspaceID := strings.TrimSpace(owner.WorkspaceID); workspaceID != "" {
			usage.WorkspaceID = workspaceID
			usage.WorkspaceShort = ShortID(workspaceID)
		}
	}
	if meta, err := execenv.ReadGCMeta(taskDir); err == nil && meta != nil {
		metaPresent = true
		if workspaceID := strings.TrimSpace(meta.WorkspaceID); workspaceID != "" {
			usage.WorkspaceID = workspaceID
			usage.WorkspaceShort = ShortID(workspaceID)
		}
		usage.Kind = string(meta.Kind)
		usage.ParentID = parentIDForMeta(meta)
		if !meta.CompletedAt.IsZero() {
			usage.AgeSeconds = int64(time.Since(meta.CompletedAt).Seconds())
		} else if age, ok := gcMetaFileAge(taskDir); ok {
			usage.AgeSeconds = int64(age.Seconds())
		}
	}
	// With no readable metadata, use taskDir mtime just like orphanByMTime.
	// Legacy readable metadata without completed_at uses its own file mtime,
	// matching gcDecisionIssueResult's managed-only fallback.
	if usage.AgeSeconds <= 0 && !metaPresent {
		if info, err := os.Stat(taskDir); err == nil {
			usage.AgeSeconds = int64(time.Since(info.ModTime()).Seconds())
		}
	}

	usage.SizeBytes, usage.ArtifactSizeBytes = taskSize(taskDir, matcher)
	return usage
}

// parentIDForMeta returns the id of the record that governs this task dir's
// lifecycle. GCMeta is a discriminated union keyed on Kind, so only the field
// matching Kind is meaningful.
func parentIDForMeta(meta *execenv.GCMeta) string {
	switch meta.Kind {
	case execenv.GCKindIssue:
		return strings.TrimSpace(meta.IssueID)
	case execenv.GCKindChat:
		return strings.TrimSpace(meta.ChatSessionID)
	case execenv.GCKindAutopilotRun:
		return strings.TrimSpace(meta.AutopilotRunID)
	case execenv.GCKindQuickCreate:
		return strings.TrimSpace(meta.TaskID)
	default:
		return ""
	}
}

// ParentStatusFetcher resolves a batch of issue ids in one workspace to their
// current status. Ids the server does not return (deleted, or invisible to
// this token) must be omitted from the result rather than mapped to a
// placeholder, so callers can tell "unresolved" from a real status.
type ParentStatusFetcher func(ctx context.Context, workspaceID string, issueIDs []string) (map[string]string, error)

// ResolveParentStatuses fills in ParentStatus on every issue-kind task in the
// report. ScanDiskUsage is deliberately network-free — this is the opt-in
// second pass that turns the STATUS column into real data.
//
// Only issue-kind tasks are resolved: they are the overwhelming majority of
// task dirs and the only kind with a batch reconciliation endpoint. Chat,
// autopilot-run, and quick-create dirs keep an empty ParentStatus rather than
// costing one request each.
//
// Best-effort by design: a workspace whose fetch fails leaves its tasks
// unresolved and the error is returned for the caller to surface, but every
// other workspace is still filled in. Callers must not treat a non-nil error
// as "the report is unusable".
func ResolveParentStatuses(ctx context.Context, report *DiskUsageReport, fetch ParentStatusFetcher) error {
	if report == nil || fetch == nil {
		return nil
	}

	idsByWorkspace := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, task := range report.Tasks {
		if task.Kind != string(execenv.GCKindIssue) || task.ParentID == "" {
			continue
		}
		if seen[task.WorkspaceID] == nil {
			seen[task.WorkspaceID] = map[string]bool{}
		}
		// Several task dirs can share one issue (a re-dispatched task reuses
		// the prior workdir), so de-duplicate before asking the server.
		if seen[task.WorkspaceID][task.ParentID] {
			continue
		}
		seen[task.WorkspaceID][task.ParentID] = true
		idsByWorkspace[task.WorkspaceID] = append(idsByWorkspace[task.WorkspaceID], task.ParentID)
	}
	if len(idsByWorkspace) == 0 {
		return nil
	}

	var firstErr error
	statuses := make(map[string]map[string]string, len(idsByWorkspace))
	for workspaceID, ids := range idsByWorkspace {
		resolved := make(map[string]string, len(ids))
		// Same chunk size the GC loop uses, so one oversized root cannot trip
		// the server's batch cap.
		for start := 0; start < len(ids); start += issueGCBatchSize {
			end := min(start+issueGCBatchSize, len(ids))
			chunk, err := fetch(ctx, workspaceID, ids[start:end])
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			for id, status := range chunk {
				resolved[id] = status
			}
		}
		statuses[workspaceID] = resolved
	}

	for i := range report.Tasks {
		task := &report.Tasks[i]
		if task.Kind != string(execenv.GCKindIssue) || task.ParentID == "" {
			continue
		}
		if status, ok := statuses[task.WorkspaceID][task.ParentID]; ok {
			task.ParentStatus = status
		}
	}
	return firstErr
}

// ReapTerminalCleanWorkspaces removes only issue task roots whose parent card
// is done or cancelled and whose Git worktree has no changes. The caller must
// resolve ParentStatus first; an empty or unrecognized status is a refusal.
// apply is deliberately opt-in so callers can present every decision before
// any local workspace is removed.
func ReapTerminalCleanWorkspaces(ctx context.Context, workspacesRoot string, diskReport DiskUsageReport, artifactPatterns []string, apply bool) (WorkspaceReapReport, error) {
	return reapTerminalCleanWorkspaces(ctx, workspacesRoot, diskReport, artifactPatterns, apply, workspaceReapOptions{
		inspectTree: inspectGitWorktree,
		removeRoot:  removeOwnedCleanTaskRoot,
	})
}

func reapTerminalCleanWorkspaces(ctx context.Context, workspacesRoot string, diskReport DiskUsageReport, artifactPatterns []string, apply bool, options workspaceReapOptions) (WorkspaceReapReport, error) {
	if options.inspectTree == nil || options.removeRoot == nil {
		return WorkspaceReapReport{}, errors.New("workspace reaper requires a tree inspector and root remover")
	}
	before, err := workspaceReapSnapshot(workspacesRoot, diskReport)
	if err != nil {
		return WorkspaceReapReport{}, err
	}
	report := WorkspaceReapReport{
		WorkspacesRoot: workspacesRoot,
		Applied:        apply,
		Before:         before,
		Results:        make([]WorkspaceReapResult, 0, len(diskReport.Tasks)),
	}

	for _, task := range diskReport.Tasks {
		result := WorkspaceReapResult{
			Path:         task.Path,
			ParentID:     task.ParentID,
			ParentStatus: task.ParentStatus,
		}
		switch {
		case task.Kind != string(execenv.GCKindIssue):
			result.Action = "skipped"
			result.Reason = "task is not backed by an issue card"
		case task.ParentID == "":
			result.Action = "skipped"
			result.Reason = "task has no parent card identity"
		case task.ParentStatus == "":
			result.Action = "skipped"
			result.Reason = "card status could not be resolved"
		case task.ParentStatus != "done" && task.ParentStatus != "cancelled":
			result.Action = "skipped"
			result.Reason = fmt.Sprintf("card status %q is not done or cancelled", task.ParentStatus)
		default:
			changes, inspectErr := options.inspectTree(ctx, filepath.Join(task.Path, "workdir"))
			if inspectErr != nil {
				result.Action = "skipped"
				result.Reason = fmt.Sprintf("could not verify clean Git tree: %v", inspectErr)
			} else if len(changes) > 0 {
				result.Action = "skipped"
				result.Reason = "Git tree has uncommitted or untracked files"
				result.Files = changes
			} else if !apply {
				result.Action = "would_remove"
				result.Reason = "terminal card and clean Git tree"
			} else if removeErr := options.removeRoot(ctx, workspacesRoot, task.Path, options.inspectTree); removeErr != nil {
				var refusal *workspaceReapRefusal
				if errors.As(removeErr, &refusal) {
					result.Action = "skipped"
					result.Reason = refusal.reason
					result.Files = refusal.files
				} else {
					result.Action = "failed"
					result.Reason = fmt.Sprintf("could not remove task root: %v", removeErr)
				}
			} else {
				result.Action = "removed"
				result.Reason = "terminal card and clean Git tree"
			}
		}
		report.Results = append(report.Results, result)
	}

	if !apply {
		report.After = before
		return report, nil
	}
	afterDiskReport, err := ScanDiskUsage(workspacesRoot, artifactPatterns)
	if err != nil {
		return report, fmt.Errorf("scan workspaces root after reap: %w", err)
	}
	report.After, err = workspaceReapSnapshot(workspacesRoot, afterDiskReport)
	if err != nil {
		return report, err
	}
	return report, nil
}

func workspaceReapSnapshot(workspacesRoot string, report DiskUsageReport) (WorkspaceReapSnapshot, error) {
	recordCount, err := taskRootRecordCount(workspacesRoot)
	if err != nil {
		return WorkspaceReapSnapshot{}, err
	}
	return WorkspaceReapSnapshot{
		TaskRootCount:       report.TotalTaskCount,
		TaskRootRecordCount: recordCount,
		TotalSizeBytes:      report.TotalSizeBytes,
	}, nil
}

func taskRootRecordCount(workspacesRoot string) (int, error) {
	entries, err := os.ReadDir(filepath.Join(workspacesRoot, ".task_roots"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read task root records: %w", err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}
	return count, nil
}

// inspectGitWorktree finds every Git worktree under workDir and returns each
// changed path reported by porcelain status. A task without a Git worktree is
// deliberately not treated as clean: there is no tree whose contents we can
// prove safe to remove.
func inspectGitWorktree(ctx context.Context, workDir string) ([]string, error) {
	repositories, err := gitWorktreeRoots(workDir)
	if err != nil {
		return nil, err
	}
	if len(repositories) == 0 {
		return nil, fmt.Errorf("no Git worktree found below %s", workDir)
	}

	changes := make([]string, 0)
	for _, repository := range repositories {
		out, err := exec.CommandContext(ctx, "git", "-C", repository, "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none").CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("git status in %s: %w: %s", repository, err, strings.TrimSpace(string(out)))
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			path := strings.TrimSpace(line)
			if len(line) > 3 {
				path = strings.TrimSpace(line[3:])
			}
			changes = append(changes, repository+": "+path)
		}
	}
	return changes, nil
}

func gitWorktreeRoots(workDir string) ([]string, error) {
	repositories := make([]string, 0)
	seen := make(map[string]struct{})
	err := filepath.WalkDir(workDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Name() != ".git" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		repository := filepath.Dir(path)
		if _, ok := seen[repository]; !ok {
			seen[repository] = struct{}{}
			repositories = append(repositories, repository)
		}
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk workdir %s: %w", workDir, err)
	}
	sort.Strings(repositories)
	return repositories, nil
}

func removeOwnedCleanTaskRoot(ctx context.Context, workspacesRoot, taskRoot string, inspectTree func(context.Context, string) ([]string, error)) error {
	// Prove ownership before creating the lock file. A terminal card's metadata
	// is not enough to justify modifying an arbitrary directory under a custom
	// workspaces root, and ownership is checked again after the lock below.
	if _, err := reapTaskRootOwner(workspacesRoot, taskRoot); err != nil {
		return &workspaceReapRefusal{reason: fmt.Sprintf("task root ownership could not be proven: %v", err)}
	}
	validatedInfo, err := os.Stat(taskRoot)
	if err != nil {
		return &workspaceReapRefusal{reason: fmt.Sprintf("could not inspect task root before locking: %v", err)}
	}
	root, err := os.OpenRoot(workspacesRoot)
	if err != nil {
		return fmt.Errorf("open workspaces root: %w", err)
	}
	defer root.Close()

	rel, err := filepath.Rel(workspacesRoot, taskRoot)
	if err != nil || !filepath.IsLocal(rel) {
		return &workspaceReapRefusal{reason: "task root is outside the workspaces root"}
	}
	if reapLockTestHook != nil {
		reapLockTestHook()
	}
	claim, lockedInfo, err := execenv.LockEnvRootForReuse(root, rel, taskRoot)
	if err != nil {
		return &workspaceReapRefusal{reason: fmt.Sprintf("task root is active or could not be exclusively locked: %v", err)}
	}
	if claim == nil {
		return &workspaceReapRefusal{reason: "task root disappeared before it could be removed"}
	}
	if lockedInfo == nil || !os.SameFile(validatedInfo, lockedInfo) {
		claim.Release()
		return &workspaceReapRefusal{reason: "task root changed identity before it could be locked"}
	}
	defer claim.Release()

	owner, err := reapTaskRootOwner(workspacesRoot, taskRoot)
	if err != nil {
		return &workspaceReapRefusal{reason: fmt.Sprintf("task root ownership could not be proven: %v", err)}
	}
	changes, err := inspectTree(ctx, filepath.Join(taskRoot, "workdir"))
	if err != nil {
		return &workspaceReapRefusal{reason: fmt.Sprintf("could not recheck clean Git tree before removal: %v", err)}
	}
	if len(changes) > 0 {
		return &workspaceReapRefusal{reason: "Git tree changed before removal", files: changes}
	}
	if reapBeforeRemovalTestHook != nil {
		reapBeforeRemovalTestHook()
	}
	currentInfo, err := os.Stat(taskRoot)
	if err != nil {
		return &workspaceReapRefusal{reason: fmt.Sprintf("could not inspect task root before removal: %v", err)}
	}
	if !os.SameFile(lockedInfo, currentInfo) {
		return &workspaceReapRefusal{reason: "task root changed identity before removal"}
	}
	if err := os.RemoveAll(taskRoot); err != nil {
		return err
	}
	if err := execenv.RemoveRootDirRecord(workspacesRoot, taskRoot, *owner); err != nil {
		return fmt.Errorf("remove task root record: %w", err)
	}
	return nil
}

func reapTaskRootOwner(workspacesRoot, taskRoot string) (*execenv.EnvRootOwner, error) {
	owner, err := execenv.ReadEnvRootOwner(taskRoot)
	if err != nil {
		return nil, err
	}
	if owner == nil || owner.TaskID == "" {
		return nil, errors.New("task owner is missing")
	}
	validated := *owner
	if validated.WorkspaceID == "" {
		meta, metaErr := execenv.ReadGCMeta(taskRoot)
		if metaErr != nil || strings.TrimSpace(meta.WorkspaceID) == "" {
			return nil, errors.New("task owner has no workspace identity")
		}
		validated.WorkspaceID = strings.TrimSpace(meta.WorkspaceID)
	}
	if err := execenv.ValidateEnvRootOwnerPath(workspacesRoot, taskRoot, validated); err != nil {
		return nil, err
	}
	return &validated, nil
}

// taskSize walks taskDir and returns (totalBytes, artifactBytes). It never
// follows symlinks and counts only regular files. A directory matched by
// matcher is treated as an artifact subtree — its size is added to both totals
// and the walk does not descend further so the size matches what os.RemoveAll
// would reclaim if the GC ran cleanTaskArtifacts on it.
//
// A .git subtree is counted whole into totalBytes but never into artifactBytes.
// It is real footprint the user sees in their file manager, and a full
// gcActionClean removes it along with the rest of the task dir (dirSize, which
// reports bytes_reclaimed there, counts it too) — so excluding it made SizeBytes
// disagree with what the GC would actually free. The walk still refuses to
// descend into it, which keeps the artifact accounting aligned with
// cleanTaskArtifacts' refusal to reclaim anything inside .git.
func taskSize(taskDir string, matcher artifactMatcher) (totalBytes int64, artifactBytes int64) {
	if taskDir == "" {
		return
	}
	absRoot, err := filepath.Abs(taskDir)
	if err != nil {
		return
	}

	_ = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == absRoot {
			return nil
		}
		// Links: never followed, never counted. WalkDir already refuses to
		// descend through a symlink, but a symlinked file would otherwise show
		// up here as a non-dir entry, and a Windows junction is reported as a
		// directory WalkDir does descend (see linkedDirModes) — drop both
		// explicitly so the size stays consistent with cleanTaskArtifacts'
		// refusal to touch link targets.
		if entry.Type()&linkedDirModes != 0 {
			if entry.IsDir() {
				// A junction: WalkDir would descend into the link target.
				// SkipDir is safe here only because the entry is a directory —
				// returning it for a file would skip the remaining siblings.
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				totalBytes += dirSize(path)
				return filepath.SkipDir
			}
			if _, ok := matcher.matchDirectory(absRoot, path, entry); ok {
				size := dirSize(path)
				totalBytes += size
				artifactBytes += size
				return filepath.SkipDir
			}
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			totalBytes += info.Size()
		}
		return nil
	})
	return
}

// ShortID returns the first 8 chars (dashes stripped) of a UUID, falling back
// to the raw input when shorter. Mirrors execenv.shortID, which lives in an
// internal subpackage and isn't exported.
func ShortID(id string) string {
	s := strings.ReplaceAll(id, "-", "")
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
