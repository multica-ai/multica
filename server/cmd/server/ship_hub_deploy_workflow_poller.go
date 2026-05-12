// Phase 7d follow-up Ship Hub — auto-detect deploys via GitHub Actions.
//
// Most CI providers (Vercel, Netlify, Cloudflare Pages, custom CI) do
// NOT fire GitHub `deployment_status` webhooks when a deploy lands.
// Without those webhooks, the release page sits forever in "Awaiting
// staging deploy" / "Awaiting production deploy" until the user clicks
// the manual Mark-deployed button. That UX is bad — the deploy IS
// landing, Multica just can't see it.
//
// This goroutine fixes that: every 2 minutes, for every workspace
// with `ship_hub_enabled = TRUE` AND a configured deploy workflow
// filename, list the last 10 completed runs of that workflow on
// `main` via the GitHub Actions API, and for any run with
// `conclusion="success"` whose head_sha matches a release's
// merged_main_sha, synthesize a deploy row + run the same linkage
// flow the webhook path runs. The release advances without a manual
// click.
//
// Cadence: 2 minutes. Tighter than the 5-min reconciler / health
// monitor because the user perception of "deploy lag" is direct —
// they pushed, the release page should reflect it within a couple
// minutes. With the GitHub Actions API limits (5,000 req/hr per
// token), one poll per workspace per environment per 2min works
// out to <2 req/min/workspace, well under the budget for any team
// running Ship Hub.
//
// Per-workspace + per-project errors are logged and skipped so one
// bad token / archived repo doesn't starve the rest of the fleet.
//
// Long-lived sweepCtx — see CLAUDE.md goroutine context rule. The
// linkage callbacks below outlive any individual HTTP request.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// shipHubDeployWorkflowPollInterval — every 2 minutes. See file header
// for the rate-budget math. Exposed as a const so tests can override
// when driving runShipHubDeployWorkflowPollOnce directly.
const shipHubDeployWorkflowPollInterval = 2 * time.Minute

// shipHubDeployWorkflowRunsPerPoll — how many recent runs to fetch per
// (workspace, project, environment) tick. 10 covers the worst case of
// "we restarted, missed 4 minutes of CI, and three releases landed in
// quick succession" without paginating. Anything older than 10 runs
// will simply have to wait for the manual Mark-deployed button.
const shipHubDeployWorkflowRunsPerPoll = 10

// pollerBusAdapter wraps an *events.Bus to expose the slim shape the
// service-layer publisher contract expects. Same pattern as
// finalizerBusAdapter — keeping each adapter file-local prevents
// importers from accidentally taking a runtime dependency on
// *events.Bus, which the test fakes don't satisfy.
type pollerBusAdapter struct{ bus *events.Bus }

func (a *pollerBusAdapter) PublishMergeEvent(eventType, workspaceID string, payload map[string]any) {
	if a == nil || a.bus == nil {
		return
	}
	a.bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload:     payload,
	})
}

// runShipHubDeployWorkflowPoller is the goroutine entry. Boots a
// 2-minute ticker; the per-tick body is extracted into
// runShipHubDeployWorkflowPollOnce so a future test can drive it
// deterministically without leaking a goroutine.
func runShipHubDeployWorkflowPoller(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	slog.Info("ship hub deploy workflow poller started",
		"interval", shipHubDeployWorkflowPollInterval.String())
	t := time.NewTicker(shipHubDeployWorkflowPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("ship hub deploy workflow poller stopped")
			return
		case <-t.C:
			runShipHubDeployWorkflowPollOnce(ctx, queries, bus)
		}
	}
}

// runShipHubDeployWorkflowPollOnce — one pass over every workspace
// with ship_hub_enabled and at least one deploy workflow configured.
// Defensive throughout: every workspace / project / run is wrapped
// so a single failure logs + continues to the next.
func runShipHubDeployWorkflowPollOnce(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	workspaces, err := queries.ListWorkspacesWithShipHubEnabled(ctx)
	if err != nil {
		slog.Warn("ship hub deploy poller: list workspaces failed", "error", err)
		return
	}
	for _, ws := range workspaces {
		stagingWf := ""
		if ws.ShipHubDeployWorkflowStaging.Valid {
			stagingWf = ws.ShipHubDeployWorkflowStaging.String
		}
		prodWf := ""
		if ws.ShipHubDeployWorkflowProduction.Valid {
			prodWf = ws.ShipHubDeployWorkflowProduction.String
		}
		if stagingWf == "" && prodWf == "" {
			// Workspace-level defaults are empty. We still need to
			// check whether any project's env has its own override
			// before skipping — a multi-project workspace might use
			// only per-env settings and leave the workspace-level
			// fields blank.
			if !workspaceHasAnyEnvWorkflowOverride(ctx, queries, ws.ID) {
				// No workspace default AND no per-env override —
				// the manual Mark-deployed button is the active
				// path. Skip silently.
				continue
			}
		}

		token := loadShipHubTokenForWorkspace(ctx, queries, ws)
		if token == "" {
			// Same skip behavior as the reconciler: a workspace with
			// the feature enabled but no token can't make any GitHub
			// calls at all, so we don't even attempt the public-API
			// fallback (rate limit is too low for periodic polling).
			slog.Debug("ship hub deploy poller: skipping workspace without token",
				"workspace_id", ws.ID)
			continue
		}
		client := gh.NewClient(token)
		pollWorkspaceDeployWorkflows(ctx, queries, bus, ws, client, stagingWf, prodWf)
	}
}

// loadShipHubTokenForWorkspace mirrors the reconciler's token-load
// path: prefer the encrypted workspace_secret row, fall back to the
// legacy settings JSON. Defensive: any decryption failure surfaces as
// "no token" so the poller skips rather than crashing.
func loadShipHubTokenForWorkspace(ctx context.Context, queries *db.Queries, ws db.Workspace) string {
	token := handler.ReadShipHubGitHubTokenForReconciler(ws.Settings)
	if token != "" {
		return token
	}
	row, err := queries.GetWorkspaceSecret(ctx, db.GetWorkspaceSecretParams{
		WorkspaceID: ws.ID,
		Name:        "github_token",
	})
	if err == nil {
		return handler.ReadShipHubGitHubTokenFromEncrypted(row.ValueEncrypted)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("ship hub deploy poller: load encrypted token failed",
			"workspace_id", ws.ID, "error", err)
	}
	return ""
}

// pollWorkspaceDeployWorkflows iterates every project in the
// workspace that has a github_repo resource attached and polls the
// configured workflow(s) on it.
//
// Workflow filename precedence (per environment):
//  1. Environment's own `deploy_workflow_filename` (set via the
//     deploy env edit dialog) — preferred. Lets multi-project
//     workspaces target the right workflow file per repo.
//  2. Workspace-level `ship_hub_deploy_workflow_<kind>` — fallback
//     for backward compat with single-project workspaces that
//     configured the setting before per-env support landed.
//
// `stagingWf` / `prodWf` come in as the workspace-level defaults; the
// env-level override is read inline below.
func pollWorkspaceDeployWorkflows(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	ws db.Workspace,
	client *gh.Client,
	stagingWf, prodWf string,
) {
	projects, err := queries.ListProjects(ctx, db.ListProjectsParams{
		WorkspaceID: ws.ID,
	})
	if err != nil {
		slog.Warn("ship hub deploy poller: list projects failed",
			"workspace_id", ws.ID, "error", err)
		return
	}
	for _, p := range projects {
		repoURL, ok := firstGitHubRepoURL(ctx, queries, p.ID)
		if !ok {
			// Project has no github_repo — nothing to poll. Common
			// for a "Documentation" or "Customer support" project
			// in a multi-project workspace.
			continue
		}
		owner, repo, err := gh.ParseRepoURL(repoURL)
		if err != nil {
			slog.Debug("ship hub deploy poller: invalid repo url",
				"project_id", p.ID, "url", repoURL, "error", err)
			continue
		}
		// Resolve per-env workflow filename overrides. Empty result
		// means "use the workspace-level default for this kind."
		envWorkflows := loadEnvWorkflowFilenames(ctx, queries, p.ID)
		stagingFile := resolveWorkflowFilename(envWorkflows[db.DeployEnvironmentKindStaging], stagingWf)
		prodFile := resolveWorkflowFilename(envWorkflows[db.DeployEnvironmentKindProduction], prodWf)
		if stagingFile != "" {
			pollEnvironmentForRelease(ctx, queries, bus, ws, p, client, owner, repo, stagingFile, db.DeployEnvironmentKindStaging)
		}
		if prodFile != "" {
			pollEnvironmentForRelease(ctx, queries, bus, ws, p, client, owner, repo, prodFile, db.DeployEnvironmentKindProduction)
		}
	}
}

// loadEnvWorkflowFilenames returns a map[kind]filename of the per-env
// deploy_workflow_filename overrides for one project's environments.
// Missing kinds + null/empty values return "" (caller falls back to
// the workspace default).
func loadEnvWorkflowFilenames(
	ctx context.Context,
	queries *db.Queries,
	projectID pgtype.UUID,
) map[db.DeployEnvironmentKind]string {
	out := map[db.DeployEnvironmentKind]string{}
	envs, err := queries.ListDeployEnvironmentsByProject(ctx, projectID)
	if err != nil {
		// Non-fatal — fall back entirely to the workspace defaults
		// rather than skipping the whole project.
		slog.Debug("ship hub deploy poller: list envs failed",
			"project_id", projectID, "error", err)
		return out
	}
	for _, env := range envs {
		if env.DeployWorkflowFilename.Valid {
			out[env.Kind] = env.DeployWorkflowFilename.String
		}
	}
	return out
}

// resolveWorkflowFilename returns the env-level override when non-empty,
// otherwise the workspace-level default. Either may be "" — caller
// checks before polling.
func resolveWorkflowFilename(envLevel, workspaceLevel string) string {
	if envLevel != "" {
		return envLevel
	}
	return workspaceLevel
}

// workspaceHasAnyEnvWorkflowOverride returns true when at least one
// deploy_environment in the workspace has a non-empty
// deploy_workflow_filename. Used as an early-skip guard so workspaces
// that haven't configured ANY workflow (neither workspace-level nor
// per-env) don't trigger the deploy-env listing per project.
//
// Cheap: one indexed query that scans the env rows for this workspace.
// The result isn't cached — workspace count stays low and the poller
// runs every 2 minutes, so a per-tick recheck is fine.
func workspaceHasAnyEnvWorkflowOverride(
	ctx context.Context,
	queries *db.Queries,
	workspaceID pgtype.UUID,
) bool {
	envs, err := queries.ListDeployEnvironmentsByWorkspace(ctx, workspaceID)
	if err != nil {
		// If the listing fails, fall back to "no overrides" — the
		// safer-skip path. The error would surface on the next
		// tick if it persists.
		return false
	}
	for _, env := range envs {
		if env.DeployWorkflowFilename.Valid && env.DeployWorkflowFilename.String != "" {
			return true
		}
	}
	return false
}

// firstGitHubRepoURL — the project's first github_repo resource_ref
// URL, if any. Mirrors the same JSON shape the request handler uses;
// duplicating the logic here (instead of importing handler.repoURL...)
// keeps the cmd/server package free of a handler-internals dependency.
func firstGitHubRepoURL(ctx context.Context, queries *db.Queries, projectID pgtype.UUID) (string, bool) {
	resources, err := queries.ListProjectResources(ctx, projectID)
	if err != nil {
		return "", false
	}
	for _, res := range resources {
		if res.ResourceType != "github_repo" {
			continue
		}
		var payload struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(res.ResourceRef, &payload); err != nil {
			continue
		}
		if payload.URL != "" {
			return payload.URL, true
		}
	}
	return "", false
}

// pollEnvironmentForRelease — poll one (workspace, project, env_kind)
// triple. Lists the last N completed workflow runs on main; for each
// success run:
//
//  1. Record the run as a deploy on the matching env if we haven't
//     seen this (env, sha) before. This advances env.current_sha so
//     the "what's running" pill is accurate even when the deploy
//     didn't come from a Ship Hub release (direct PR merges, hotfixes
//     pushed straight to main, etc.).
//
//  2. Optionally link the new deploy to a Ship Hub release whose
//     merged_main_sha / production_main_sha matches the run. Misses
//     here are common — most direct merges have no release object —
//     and are silently ignored.
//
// Pre-fix, step 2 was the only path. A direct merge to main left
// env.current_sha permanently stale because no release ever matched
// the workflow run, and the deploy row was never inserted.
func pollEnvironmentForRelease(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	ws db.Workspace,
	project db.Project,
	client *gh.Client,
	owner, repo, workflowFile string,
	envKind db.DeployEnvironmentKind,
) {
	runs, err := client.ListWorkflowRuns(ctx, owner, repo, workflowFile, gh.ListWorkflowRunsOptions{
		Branch:  "main",
		Status:  "completed",
		PerPage: shipHubDeployWorkflowRunsPerPoll,
	})
	if err != nil {
		slog.Warn("ship hub deploy poller: list workflow runs failed",
			"workspace_id", ws.ID,
			"project_id", project.ID,
			"workflow", workflowFile,
			"env_kind", envKind,
			"error", err)
		return
	}
	for _, run := range runs {
		if run.Conclusion != "success" {
			continue
		}
		if run.HeadSHA == "" {
			continue
		}
		deploy, ok := recordRunAsDeployIfNew(ctx, queries, ws.ID, project.ID, envKind, run)
		if !ok {
			continue
		}
		linkDeployToReleaseIfAny(ctx, queries, bus, ws, project, client, run, deploy, envKind)
	}
}

// recordRunAsDeployIfNew is the new always-on path: idempotently insert
// a deploy row for (env, run.HeadSHA) and bump env.current_sha. Returns
// the existing deploy if one already exists for this sha (no-op), or
// the freshly-inserted row, or {} + false on error. The release
// linkage in linkDeployToReleaseIfAny runs after this — they're now
// orthogonal concerns instead of nested.
func recordRunAsDeployIfNew(
	ctx context.Context,
	queries *db.Queries,
	workspaceID, projectID pgtype.UUID,
	envKind db.DeployEnvironmentKind,
	run gh.WorkflowRun,
) (db.Deploy, bool) {
	// Resolve the env once up front so the dedup check has a key.
	// upsertSyntheticDeploy reads envs again internally for the create
	// path — fine, the row count is tiny.
	envs, err := queries.ListDeployEnvironmentsByProject(ctx, projectID)
	if err != nil {
		slog.Warn("ship hub deploy poller: list envs for dedup failed",
			"project_id", projectID, "error", err)
		return db.Deploy{}, false
	}
	var envID pgtype.UUID
	for _, e := range envs {
		if e.Kind == envKind {
			envID = e.ID
			break
		}
	}
	if !envID.Valid {
		// No env of this kind for the project. Phantom-env defense:
		// the poller used to auto-create one here, which produced
		// stuck-in-staging releases on direct-to-prod projects.
		// Skip silently — the operator can create the env explicitly
		// in Settings → Deploy Environments when they're ready.
		slog.Debug("ship hub deploy poller: no env of kind, skipping",
			"project_id", projectID, "kind", envKind, "sha", run.HeadSHA)
		return db.Deploy{}, false
	}
	// Dedup: have we already recorded a deploy with this exact
	// (env, sha)? Poller runs every 2 minutes; without this guard
	// we'd insert a duplicate row on every tick.
	existing, err := queries.GetDeployByEnvAndSHA(ctx, db.GetDeployByEnvAndSHAParams{
		EnvironmentID: envID,
		Sha:           run.HeadSHA,
	})
	if err == nil {
		return existing, true
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("ship hub deploy poller: dedup lookup failed",
			"env_id", envID, "sha", run.HeadSHA, "error", err)
		return db.Deploy{}, false
	}
	return upsertSyntheticDeploy(ctx, queries, workspaceID, projectID, envKind, run)
}

// linkDeployToReleaseIfAny — the per-run release-linkage logic. Returns
// nothing because the entire path is best-effort; every miss is silent
// and every error is logged + swallowed.
//
// Pre-fix this function ALSO created the deploy row; now the deploy
// has already been recorded by recordRunAsDeployIfNew before we get
// here. This function only handles the optional "tag this deploy to a
// Ship Hub release" step.
//
// Match criteria:
//   - staging → release whose merged_main_sha == run.head_sha AND
//     stage is in the staging-eligible set (FindReleaseByMergedMainSHA
//     handles the stage filter)
//   - production → release whose production_main_sha == run.head_sha
//     OR (production_main_sha empty AND merged_main_sha == run.head_sha)
//     AND stage is verifying / promoting / in_production
//     (FindReleaseByProductionMainSHA handles the OR + stage filter)
//   - production fallback → if exact SHA match misses, fall back to the
//     oldest stuck "promoting" release in the same project whose
//     merged_at predates this run. Handles the squash-merge /
//     subsequent-commit case where the prod deploy fires on a SHA
//     descended from the release's merge tip but not equal to it.
//     Mirrors the Kanban time-based fallback in
//     packages/views/ship/hooks/use-pr-state.ts.
//
// Already-linked releases are skipped (the existing deploy_id check).
// This keeps the poller idempotent across restarts and across
// multiple workspaces sharing a sha (rare but possible).
func linkDeployToReleaseIfAny(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	ws db.Workspace,
	project db.Project,
	client *gh.Client,
	run gh.WorkflowRun,
	deploy db.Deploy,
	envKind db.DeployEnvironmentKind,
) {
	var release db.ShipRelease
	var lookupErr error
	switch envKind {
	case db.DeployEnvironmentKindStaging:
		release, lookupErr = queries.FindReleaseByMergedMainSHA(ctx, db.FindReleaseByMergedMainSHAParams{
			WorkspaceID:   ws.ID,
			MergedMainSha: pgtype.Text{String: run.HeadSHA, Valid: true},
		})
	case db.DeployEnvironmentKindProduction:
		release, lookupErr = queries.FindReleaseByProductionMainSHA(ctx, db.FindReleaseByProductionMainSHAParams{
			WorkspaceID:       ws.ID,
			ProductionMainSha: pgtype.Text{String: run.HeadSHA, Valid: true},
		})
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			// Time-based fallback. The deploy ran successfully but no
			// release's merged_main_sha / production_main_sha matches
			// run.head_sha — almost always because the prod workflow
			// fired on a SHA descended from the release's tip rather
			// than the tip itself (squash merge, manual
			// workflow_dispatch on a later commit, etc.). Look for the
			// oldest stuck-in-promoting release in this project whose
			// merged_at predates the deploy and link it.
			if deployTime, ok := workflowRunDeployTime(run); ok {
				release, lookupErr = queries.FindStuckPromotingReleaseForProject(ctx,
					db.FindStuckPromotingReleaseForProjectParams{
						WorkspaceID: ws.ID,
						ProjectID:   project.ID,
						MergedAt:    pgtype.Timestamptz{Time: deployTime, Valid: true},
					})
				if lookupErr == nil {
					slog.Info("ship hub deploy poller: time-based fallback matched stuck promoting release",
						"workspace_id", ws.ID,
						"project_id", project.ID,
						"release_id", release.ID,
						"deploy_sha", run.HeadSHA,
						"release_merged_main_sha", release.MergedMainSha.String,
						"deploy_time", deployTime)
				}
			}
		}
	default:
		return
	}
	if lookupErr != nil {
		// pgx.ErrNoRows is the common case — most workflow runs aren't
		// from a tracked release (a hotfix, a docs change, etc).
		if !errors.Is(lookupErr, pgx.ErrNoRows) {
			slog.Warn("ship hub deploy poller: find release by sha failed",
				"workspace_id", ws.ID,
				"sha", run.HeadSHA,
				"env", envKind,
				"error", lookupErr)
		}
		return
	}
	// Cross-project guard: a release is project-scoped, but
	// workspace-level workflow polling will pick up runs from every
	// repo in the workspace. Skip if the matched release belongs to a
	// different project than the one we're scanning, otherwise we'd
	// link a deploy from repo-A's CI to repo-B's release.
	if release.ProjectID != project.ID {
		return
	}
	// Skip if already linked. This is the cheap idempotency guard
	// that lets the poller re-run after a restart without
	// re-creating deploy rows.
	switch envKind {
	case db.DeployEnvironmentKindStaging:
		if release.StagingDeployID.Valid {
			return
		}
	case db.DeployEnvironmentKindProduction:
		if release.ProductionDeployID.Valid {
			return
		}
	}

	// Deploy row was created by the caller (recordRunAsDeployIfNew);
	// we just use it for the release linkage.
	deps := buildPollerStagingDeps(ctx, bus)
	switch envKind {
	case db.DeployEnvironmentKindStaging:
		smokeWorkflow := ""
		if ws.ShipHubSmokeWorkflow.Valid {
			smokeWorkflow = ws.ShipHubSmokeWorkflow.String
		}
		repoURL, _ := firstGitHubRepoURL(ctx, queries, project.ID)
		svc := &ship.Service{Q: queries, Github: client}
		if _, err := svc.LinkStagingDeploy(ctx, release.ID, deploy.ID, run.HeadSHA, smokeWorkflow, repoURL, deps); err != nil {
			slog.Warn("ship hub deploy poller: link staging deploy failed",
				"release_id", release.ID, "error", err)
			return
		}
		slog.Info("ship hub deploy poller: linked staging deploy",
			"workspace_id", ws.ID,
			"release_id", release.ID,
			"deploy_id", deploy.ID,
			"sha", run.HeadSHA,
			"run_url", run.HTMLURL)
	case db.DeployEnvironmentKindProduction:
		svc := &ship.Service{Q: queries, Github: client}
		if _, err := svc.LinkProductionDeploy(ctx, release.ID, deploy.ID, run.HeadSHA, deps); err != nil {
			slog.Warn("ship hub deploy poller: link production deploy failed",
				"release_id", release.ID, "error", err)
			return
		}
		slog.Info("ship hub deploy poller: linked production deploy",
			"workspace_id", ws.ID,
			"release_id", release.ID,
			"deploy_id", deploy.ID,
			"sha", run.HeadSHA,
			"run_url", run.HTMLURL)
	}

	// Generic stage-update fan-out so any rail listeners pick up the
	// stage flip from the LinkStagingDeploy / LinkProductionDeploy
	// path. The service-layer linkage already publishes the
	// release-specific event; this is the workspace-level rail signal.
	if bus != nil {
		bus.Publish(events.Event{
			Type:        protocol.EventReleaseUpdated,
			WorkspaceID: uuidStringFromPg(ws.ID),
			ActorType:   "system",
			Payload: map[string]any{
				"release_id": uuidStringFromPg(release.ID),
			},
		})
	}
}

// buildPollerStagingDeps — the StagingDeps the service-layer linkage
// expects. We supply a publisher (so WS events fire) but no
// PostToReleaseChannel — the channel post is best-effort and the
// poller has no per-request channel context. The webhook path
// supplies the channel poster; from the poller, an audit-row +
// WS event is the durable record (the channel post is a nice-to-have).
func buildPollerStagingDeps(parentCtx context.Context, bus *events.Bus) *ship.StagingDeps {
	return &ship.StagingDeps{
		ParentCtx: parentCtx,
		Publisher: &pollerBusAdapter{bus: bus},
	}
}

// upsertSyntheticDeploy — find or create the env, then insert a
// successful deploy row keyed to the workflow run. ok=false on any
// DB failure with the error already logged. Mirrors the
// MarkReleaseStagingDeployed handler's synthesizing path so the
// auto-detected and manually-clicked rows look identical
// downstream (same env, same triggered_by=NULL, same status).
func upsertSyntheticDeploy(
	ctx context.Context,
	queries *db.Queries,
	workspaceID, projectID pgtype.UUID,
	envKind db.DeployEnvironmentKind,
	run gh.WorkflowRun,
) (db.Deploy, bool) {
	envs, err := queries.ListDeployEnvironmentsByProject(ctx, projectID)
	if err != nil {
		slog.Warn("ship hub deploy poller: list deploy envs failed",
			"project_id", projectID, "error", err)
		return db.Deploy{}, false
	}
	var env *db.DeployEnvironment
	for i := range envs {
		if envs[i].Kind == envKind {
			env = &envs[i]
			break
		}
	}
	if env == nil {
		// Pre-fix: the poller auto-created a deploy_environment row
		// here ("Staging" / "Production") on first sight of a
		// successful workflow run. That produced phantom envs: a
		// project that ships direct-to-prod would still get a
		// `kind='staging'` row materialized as soon as any workflow
		// run was observed, and the release flow would then park
		// every release in `in_staging` forever (see #46's release
		// page for the exact symptom).
		//
		// Post-fix: the poller is read-only with respect to env
		// creation. If no env of the right kind exists, we skip the
		// deploy recording silently. The operator must explicitly
		// create envs via the Settings → Deploy Environments UI or
		// via the user-initiated "Mark deploy as landed" path (which
		// is intentionally explicit — see MarkReleaseStagingDeployed
		// in the handler package). Workflow runs against a project
		// with no configured env simply don't produce deploy rows
		// until an env is created.
		slog.Debug("ship hub deploy poller: no env of kind, skipping deploy record",
			"project_id", projectID, "kind", envKind, "sha", run.HeadSHA)
		return db.Deploy{}, false
	}

	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	deploy, err := queries.InsertDeploy(ctx, db.InsertDeployParams{
		WorkspaceID:   workspaceID,
		EnvironmentID: env.ID,
		Ref:           env.TargetBranch,
		Sha:           run.HeadSHA,
		Status:        db.DeployStatusSucceeded,
		TriggeredBy:   pgtype.UUID{}, // automated — no user actor
		StartedAt:     now,
		CompletedAt:   now,
		LogUrl:        pgtype.Text{String: run.HTMLURL, Valid: run.HTMLURL != ""},
	})
	if err != nil {
		slog.Warn("ship hub deploy poller: insert deploy failed",
			"project_id", projectID, "kind", envKind, "error", err)
		return db.Deploy{}, false
	}
	// Best-effort env current_sha bump — same as the manual path.
	_, _ = queries.UpdateDeployEnvironmentCurrent(ctx, db.UpdateDeployEnvironmentCurrentParams{
		ID:                env.ID,
		CurrentSha:        pgtype.Text{String: deploy.Sha, Valid: true},
		CurrentDeployedAt: deploy.TriggeredAt,
	})
	return deploy, true
}

// workflowRunDeployTime returns the best-available wall-clock time for
// when the workflow run "happened" for purposes of the time-based
// fallback. We prefer UpdatedAt (which on a status="completed" run is
// the completion time), and fall back to RunStartedAt / CreatedAt if
// UpdatedAt is somehow missing.
//
// Returns ok=false if no timestamp parses — the caller will skip the
// fallback rather than guessing. Better to leave the release stuck and
// rely on the manual button than to link a deploy with no time anchor.
func workflowRunDeployTime(run gh.WorkflowRun) (time.Time, bool) {
	candidates := []string{run.UpdatedAt, run.RunStartedAt, run.CreatedAt}
	for _, s := range candidates {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// uuidStringFromPg is a local copy of the handler-package helper. We
// can't import handler.uuidToString because it's lowercase, and a
// public alias would clutter the API surface for one cmd/server use
// case.
func uuidStringFromPg(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	for i, j := 0, 0; i < 16; i++ {
		switch i {
		case 4, 6, 8, 10:
			out[j] = '-'
			j++
		}
		out[j] = hex[b[i]>>4]
		out[j+1] = hex[b[i]&0x0f]
		j += 2
	}
	return string(out)
}
