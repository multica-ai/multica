package daemon

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// PUL-94 per-task worktree resolution.
//
// When the feature flag is on and a claimed Task has TargetProjectResourceID
// set, the daemon resolves the assignment BEFORE forking the agent. The
// resolution is pure: project_resource lookup → JSONB parse → bare path map
// → worktree path. Then the daemon does the side-effecty pre-spawn work
// (disk budget check + conditional fetch on the bare) so the agent's
// `multica repo checkout` call hits a known-good state.
//
// The resolved assignment is exposed to the agent via env vars
// (MULTICA_TASK_BARE_PATH, MULTICA_TASK_WORKTREE_PATH, MULTICA_TASK_TARGET_REPO).
// The CLI passes them through to /repo/checkout, where the handler uses
// repocache's WtPathOverride+BarePathOverride to place the worktree at
// /srv/agent-worktrees/<agent>-<task[:8]>/ instead of inside the env workdir.
//
// See: plans://Multica/2026-05-12-pul-94-agent-worktree-per-task.md (PR-3).

// perTaskWorktreeAssignment is the resolved tuple a single per-task spawn
// needs. Empty WtPath signals "feature off or no target — use legacy path".
type perTaskWorktreeAssignment struct {
	TargetProjectResourceID string // pass-through, for GCMeta
	TargetRepo              string // "owner/name", for logging + CLAUDE.md
	BarePath                string // absolute, e.g. /srv/multica-bare.git
	WtPath                  string // absolute, e.g. /srv/agent-worktrees/agent-1-abc12345
}

// staleness window for the bare repo's last successful fetch. Hourly mirror
// timers refresh the bares out-of-band; within this window, the daemon
// trusts the cached state and skips its own fetch. Eng review Issue 2.
const bareFetchMaxAge = 5 * time.Minute

// resolvePerTaskWorktree returns the per-task worktree assignment for a
// claimed task, or a zero-value assignment + nil error when the feature is
// off / the task has no target (legacy fallback path).
//
// On error, the task should fail loudly with a structured slog event — the
// caller is responsible for that. Sentinel errors (ErrTargetRepoNotAllowed,
// ErrBareMissing, ErrDiskBudgetExceeded, ErrFetchTimeout, ErrFetchAuth)
// wrap the underlying cause so ops can grep daemon.log by error_class.
func (d *Daemon) resolvePerTaskWorktree(task *Task, agentName string, taskLog *slog.Logger) (perTaskWorktreeAssignment, error) {
	// Feature gate. Empty assignment + nil error → legacy fallback.
	if !d.cfg.UsePerTaskWorktree {
		return perTaskWorktreeAssignment{}, nil
	}
	if task.TargetProjectResourceID == "" {
		// Legacy task or one whose project doesn't have an explicit target.
		// Falls back to per-workspace cache via existing /repo/checkout path.
		return perTaskWorktreeAssignment{}, nil
	}
	if d.cfg.WorktreesRoot == "" {
		// Misconfigured: flag is on but no worktrees root set. Treat as
		// "feature unavailable for this task" rather than failing — admin
		// can switch the flag off in env if this is a problem.
		taskLog.Warn("worktree.spawn_skipped_no_root",
			"task_uuid", task.ID, "target_project_resource_id", task.TargetProjectResourceID)
		return perTaskWorktreeAssignment{}, nil
	}

	// 1. Find the matching project_resource on the claimed task. The claim
	//    endpoint already populated task.ProjectResources from the issue's
	//    project; we just match by ID locally.
	var resource *ProjectResourceData
	for i := range task.ProjectResources {
		if task.ProjectResources[i].ID == task.TargetProjectResourceID {
			resource = &task.ProjectResources[i]
			break
		}
	}
	if resource == nil {
		return perTaskWorktreeAssignment{}, fmt.Errorf("target_project_resource_id %s not in task.ProjectResources: %w",
			task.TargetProjectResourceID, ErrTargetRepoNotAllowed)
	}
	if resource.ResourceType != "github_repo" {
		return perTaskWorktreeAssignment{}, fmt.Errorf("project resource %s has type %q, want github_repo: %w",
			resource.ID, resource.ResourceType, ErrTargetRepoNotAllowed)
	}

	// 2. Parse owner/name from the resource_ref JSONB.
	ref, err := repocache.ParseGithubRepoRef(resource.ResourceRef)
	if err != nil {
		return perTaskWorktreeAssignment{}, fmt.Errorf("parse resource_ref for %s: %w (%v)",
			resource.ID, ErrTargetRepoNotAllowed, err)
	}

	// 3. Resolve bare path via the configured map. Miss = ErrBareMissing,
	//    which surfaces clearly in the failure event.
	barePath, ok := repocache.ResolveBareFromGithubRef(d.cfg.BareRepoMap, ref)
	if !ok {
		return perTaskWorktreeAssignment{}, fmt.Errorf("no bare repo configured for %s: %w",
			ref.String(), ErrBareMissing)
	}
	if _, err := os.Stat(barePath); err != nil {
		return perTaskWorktreeAssignment{}, fmt.Errorf("bare path %s for %s not on disk: %w",
			barePath, ref.String(), ErrBareMissing)
	}

	// 4. Disk budget. Per-agent count + global %used. Cheap.
	budget := repocache.DiskBudget{WorktreesRoot: d.cfg.WorktreesRoot}
	if n, err := budget.PerAgentWorktreeCount(agentName); err == nil && n >= 5 {
		return perTaskWorktreeAssignment{}, fmt.Errorf("agent %s has %d/5 worktrees: %w",
			agentName, n, ErrDiskBudgetExceeded)
	}
	if frac, err := budget.GlobalFractionUsed(); err == nil && frac >= 0.5 {
		return perTaskWorktreeAssignment{}, fmt.Errorf("global worktree share %.1f%% over 50%%: %w",
			frac*100, ErrDiskBudgetExceeded)
	}

	// 5. Conditional fetch — only if the bare mirror is stale. Hourly mirror
	//    cron refreshes them out-of-band, so most spawns skip this entirely.
	//    Eng review Issue 2.
	if d.repoCache != nil {
		if err := d.repoCache.FetchIfStale(barePath, bareFetchMaxAge); err != nil {
			return perTaskWorktreeAssignment{}, classifyFetchError(err)
		}
	}

	// 6. Compute worktree path. Pure function — no I/O.
	wtPath := repocache.PerTaskWorktreePath(d.cfg.WorktreesRoot, agentName, task.ID)

	return perTaskWorktreeAssignment{
		TargetProjectResourceID: resource.ID,
		TargetRepo:              ref.String(),
		BarePath:                barePath,
		WtPath:                  wtPath,
	}, nil
}

// errorClassOf names the sentinel kind for a wrapped per-task spawn error,
// for use as a structured log field. Falls back to "unknown" so logs always
// have a value to filter on.
func errorClassOf(err error) string {
	switch {
	case errors.Is(err, ErrTargetRepoNotAllowed):
		return "target_repo_not_allowed"
	case errors.Is(err, ErrBareMissing):
		return "bare_missing"
	case errors.Is(err, ErrDiskBudgetExceeded):
		return "disk_budget"
	case errors.Is(err, ErrFetchTimeout):
		return "fetch_timeout"
	case errors.Is(err, ErrFetchAuth):
		return "fetch_auth"
	case errors.Is(err, ErrBranchCollision):
		return "branch_collision"
	case errors.Is(err, ErrPathCollisionUnexpected):
		return "path_collision_unexpected"
	case errors.Is(err, ErrWorktreeMissing):
		return "worktree_missing"
	default:
		return "unknown"
	}
}

// classifyFetchError wraps a transport-level fetch failure into the
// appropriate sentinel. Best-effort string-matching on the underlying error;
// when in doubt, returns the raw error wrapped in ErrFetchTimeout (the
// common transient failure that ops typically retries first).
func classifyFetchError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrFetchAuth) || errors.Is(err, ErrFetchTimeout) {
		return err
	}
	msg := strings.ToLower(err.Error())
	authMarkers := []string{
		"authentication failed",
		"could not read username",
		"could not read password",
		"403",
		"401",
		"invalid credentials",
	}
	for _, m := range authMarkers {
		if strings.Contains(msg, m) {
			return fmt.Errorf("%w: %v", ErrFetchAuth, err)
		}
	}
	return fmt.Errorf("%w: %v", ErrFetchTimeout, err)
}
