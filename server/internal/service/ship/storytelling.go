// Phase 5 Ship Hub — deploy storytelling.
//
// When a `deploy.status` flips to `succeeded` AND the environment is
// `production`, we synthesise a short prose summary and write it to
// `memory_artifact` as kind=runbook. The artifact is anchored to the
// linked Multica issue (if any) so it surfaces on the issue's Memory
// tab automatically; otherwise to the project. No frontend changes
// needed — the existing memory tab renders the entry.
//
// Why a runbook and not a new memory_artifact kind? The validator at
// `handler/memory_artifact.go:54-59` accepts only the four canonical
// kinds; "runbook" reads naturally for a "how this got shipped"
// document and avoids a CREATE TYPE migration just to introduce a
// fifth case.
//
// Best-effort: every step that fails logs a Warn and falls through.
// The deploy itself is already succeeded; failing to record the
// runbook should never roll it back.

package ship

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EmitDeployRunbook writes the storytelling memory_artifact for a
// production deploy that just succeeded. Idempotency comes from
// memory_artifact's content-addressed slug: if a runbook with the
// same slug exists we leave it alone (the same SHA shouldn't ship
// twice; if it did, the second event re-uses the first artifact).
//
// The artifact is authored by the workspace's orchestrator agent so
// the comment.author_type CHECK ('member'|'agent') is satisfied
// without forcing the user-id of whoever logged the deploy.
func (s *Service) EmitDeployRunbook(ctx context.Context, workspaceID pgtype.UUID, env db.DeployEnvironment, deploy db.Deploy) error {
	if env.Kind != db.DeployEnvironmentKindProduction {
		return nil
	}
	if deploy.Status != db.DeployStatusSucceeded {
		return nil
	}

	// Find the PR that produced this SHA so the prose has a title and
	// the cycle-time math has a "issue created" anchor. Best-effort —
	// no PR row means the runbook still lands with a generic title.
	var pr db.PullRequest
	var prFound bool
	prRow, err := s.findPullRequestForSHA(ctx, workspaceID, deploy.Sha)
	if err == nil {
		pr = prRow
		prFound = true
	}

	// Resolve the originating issue (Phase 4 linkage). When the PR
	// links to a Multica issue we anchor the artifact there; else
	// anchor to the project, which always exists for a deploy_env.
	var anchorType pgtype.Text
	var anchorID pgtype.UUID
	if prFound && pr.OriginatingIssueID.Valid {
		anchorType = pgtype.Text{String: "issue", Valid: true}
		anchorID = pr.OriginatingIssueID
	} else if env.ProjectID.Valid {
		anchorType = pgtype.Text{String: "project", Valid: true}
		anchorID = env.ProjectID
	}

	ws, err := s.Q.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("ship runbook: load workspace: %w", err)
	}
	authorID := ws.OrchestratorAgentID
	if !authorID.Valid {
		// Workspace without an orchestrator agent — defensive skip.
		// The platform creates the agent at ship_hub_enabled time so
		// reaching this branch implies a corruption; logging is enough.
		slog.Warn("ship runbook: workspace has no orchestrator agent", "workspace_id", uuidString(workspaceID))
		return nil
	}

	title, content := s.buildRunbookProse(env, deploy, pr, prFound)

	tags := []string{"deploy", "ship-hub", "production"}
	metadata := map[string]any{
		"deploy_id":      uuidString(deploy.ID),
		"environment_id": uuidString(env.ID),
		"sha":            deploy.Sha,
	}
	if prFound {
		metadata["pr_number"] = pr.PrNumber
		metadata["pr_title"] = pr.Title
	}
	metaJSON, _ := json.Marshal(metadata)

	if _, err := s.Q.CreateMemoryArtifact(ctx, db.CreateMemoryArtifactParams{
		WorkspaceID: workspaceID,
		Kind:        "runbook",
		Title:       title,
		Content:     content,
		AuthorType:  "agent",
		AuthorID:    authorID,
		Tags:        tags,
		Metadata:    metaJSON,
		AnchorType:  anchorType,
		AnchorID:    anchorID,
	}); err != nil {
		return fmt.Errorf("ship runbook: create memory_artifact: %w", err)
	}
	return nil
}

// findPullRequestForSHA returns the most-recently-merged PR with a
// matching head_sha. We use the workspace-level list (no per-SHA
// index) because the cardinality is bounded; if Phase 6 wants this
// faster, an index on (workspace_id, head_sha) is the obvious win.
func (s *Service) findPullRequestForSHA(ctx context.Context, workspaceID pgtype.UUID, sha string) (db.PullRequest, error) {
	rows, err := s.Q.ListPullRequestsByWorkspace(ctx, db.ListPullRequestsByWorkspaceParams{
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.PullRequest{}, err
	}
	for _, pr := range rows {
		if pr.HeadSha == sha {
			return pr, nil
		}
	}
	return db.PullRequest{}, fmt.Errorf("no PR with head_sha=%s", sha)
}

// buildRunbookProse renders the title + content. Plain markdown with
// a few replaceable placeholders — the exact wording is from the
// spec.
func (s *Service) buildRunbookProse(env db.DeployEnvironment, deploy db.Deploy, pr db.PullRequest, prFound bool) (string, string) {
	deployedAt := s.now()
	if deploy.CompletedAt.Valid {
		deployedAt = deploy.CompletedAt.Time
	}
	dateStr := deployedAt.UTC().Format("2006-01-02")
	timeStr := deployedAt.UTC().Format("15:04 MST")

	prTitle := "Production deploy"
	prRef := fmt.Sprintf("SHA `%s`", deploy.Sha)
	cycleHint := ""
	source := "external"
	if prFound {
		prTitle = pr.Title
		prRef = fmt.Sprintf("PR #%d", pr.PrNumber)
		if !pr.PrCreatedAt.Time.IsZero() {
			days := int(deployedAt.Sub(pr.PrCreatedAt.Time).Hours() / 24)
			if days < 0 {
				days = 0
			}
			cycleHint = fmt.Sprintf("%d-day cycle from PR open → prod. ", days)
		}
		if pr.Source != "" {
			source = pr.Source
		}
	}

	title := fmt.Sprintf("Shipped: %s", prTitle)

	// Use plain markdown; the memory_artifact rendering already
	// supports it. Keep the body short — the artifact appears
	// inline on the Memory tab and the user shouldn't need to
	// expand to read the gist.
	content := fmt.Sprintf(
		`On %s at %s, **%s** (%s) shipped to %s.

%sOriginated from %s. Touched %d files (+%d / −%d).
Deployed SHA: ` + "`%s`" + `.
`,
		dateStr,
		timeStr,
		prTitle,
		prRef,
		env.Name,
		cycleHint,
		source,
		pr.ChangedFiles,
		pr.Additions,
		pr.Deletions,
		deploy.Sha,
	)
	return title, content
}

// emitRunbookFromOutcome is the webhook-side hook. Called from
// processDeploymentStatus when the new status is `succeeded` and the
// env is production. Wrapped here so the webhook flow can call it
// inline without dragging the storytelling code into webhook.go.
func (s *Service) emitRunbookFromOutcome(ctx context.Context, workspaceID pgtype.UUID, env db.DeployEnvironment, deploy db.Deploy) {
	if err := s.EmitDeployRunbook(ctx, workspaceID, env, deploy); err != nil {
		slog.Warn("ship runbook: emit failed",
			"workspace_id", uuidString(workspaceID),
			"deploy_id", uuidString(deploy.ID),
			"error", err)
	}
}

// nowReference is a small helper so the runbook tests can pin time.
// Not exported — callers go through s.now() / EmitDeployRunbook.
var _ = time.Now
