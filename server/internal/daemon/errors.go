package daemon

import "errors"

// Per-task worktree spawn failure modes (PUL-94). Sentinel errors used by
// execenv.Prepare / execenv.Reuse and repocache.CreateWorktree (when extended
// for per-task overrides) so:
//
//   - ops can triage failures via `errors.Is(err, ErrX)` against daemon.log,
//   - tests can assert specific failure paths,
//   - structured slog events can carry a stable error_class label without
//     stringifying internal error messages.
//
// Wrap with `fmt.Errorf("...: %w", ErrX, …)` to add context; unwrap with
// `errors.Is(err, ErrX)` to detect the class. Don't compare with == — the
// daemon wraps with context routinely.
//
// See: plans://Multica/2026-05-12-pul-94-agent-worktree-per-task.md (A7).
var (
	// ErrTargetRepoNotAllowed indicates the task's target_project_resource_id
	// references a row that is missing, not in the workspace's accessible
	// resources, or has resource_type != "github_repo". Spawn rejected before
	// any filesystem work; task moves to failed_to_spawn with a structured log.
	ErrTargetRepoNotAllowed = errors.New("target project resource not allowed for this workspace")

	// ErrBareMissing indicates the bare repo mirror for the resolved target_repo
	// is not provisioned on disk. Provisioning lives in
	// multica-server/scripts/14-agent-host-prep.sh — see Phase 4 of the plan.
	ErrBareMissing = errors.New("bare mirror not provisioned for target_repo")

	// ErrDiskBudgetExceeded indicates the spawn would push past the per-agent
	// worktree cap (5 concurrent) or the global /srv/agent-worktrees disk
	// budget (50% of free space on /srv). Tasks failing this are typically
	// re-tried once another worktree completes and the agent slot frees.
	ErrDiskBudgetExceeded = errors.New("worktree disk budget exceeded")

	// ErrFetchTimeout indicates `git fetch origin --prune` against the bare
	// repo exceeded the configured timeout. Daemon retries once before
	// surfacing this error.
	ErrFetchTimeout = errors.New("bare fetch timeout")

	// ErrFetchAuth indicates `git fetch` failed with an authentication error,
	// typically a rotated or expired PAT. Not retried — operator must rotate
	// the bare repo's stored credentials.
	ErrFetchAuth = errors.New("bare fetch authentication failed")

	// ErrBranchCollision indicates `git worktree add -b <branch>` failed
	// because <branch> already exists on origin. The branch name includes
	// shortID(task.ID), so collisions are practically impossible — surfacing
	// this is alert-worthy.
	ErrBranchCollision = errors.New("branch name already exists on origin")

	// ErrPathCollisionUnexpected indicates the computed worktree path is
	// already registered AND the corresponding envRoot is still active. This
	// is an impossible state that suggests a bug or a manual filesystem edit;
	// alert-worthy.
	ErrPathCollisionUnexpected = errors.New("worktree path in use by an active task")

	// ErrWorktreeMissing is returned by execenv.Reuse when a task resume
	// finds the recorded worktree path no longer exists on disk. Depending on
	// daemon config, the task either fails with this error or the worktree is
	// respawned from the bare and the task continues.
	ErrWorktreeMissing = errors.New("worktree missing on task resume")
)
