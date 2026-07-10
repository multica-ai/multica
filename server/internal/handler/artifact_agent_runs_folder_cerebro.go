package handler

// CEREBRO-PATCH(agent-runs-folder): FIR-2697 part 3 — automatic Agent Runs
// folder structure at Collections level. The FIRST time an agent saves a
// document/note in a run and does not pick a folder itself, the server silently
// (no notifications, no inbox, no realtime folder event) get-or-creates the
// path
//
//     Agent Runs  >  <owner member>  >  <agent>  >  <run>
//
// in the artifact_folder tree for that surface (kind='document' or 'note') and
// files the artifact into the leaf. Every folder the save actually creates is
// stamped from day one with a Collections sharing grant (cerebro_folder_grant,
// surface='artifact') so the structure is governed by the same access layer as
// every other folder — matching the locked decision that these folders get the
// same workspace visibility documents have today and can be tightened later
// through Collections without new development.
//
// The run leaf is named after the issue the run served (not the raw task UUID):
// a folder per task UUID would violate the "no UUID interfaces" rule and bury
// each agent under a wall of meaningless siblings, so all of an agent's work on
// one issue lands in one navigable folder. Chat/quick-create runs with no issue
// fall back to a short "Run <id8>" label.
//
// This file holds all the logic; the upstream CreateArtifact handler only calls
// autoFileAgentArtifact in a 1-line hook. Access is hand-written SQL through the
// request-scoped transaction (matching the folder-suggestion sibling), reusing
// the existing UpsertCerebroFolderGrant conflict semantics for the grant.
//
// Gated by the workspace feature flag cerebro_agent_runs_folders (default off).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// agentRunsFoldersFlagKey is the workspace kill switch for the whole auto Agent
// Runs structure. Mirrored in packages/cerebro-feature-flags/registry.ts.
const agentRunsFoldersFlagKey = "cerebro_agent_runs_folders"

// agentRunsRootName is the fixed top-level folder every auto structure hangs
// under. English on purpose — it is a user-visible UI string.
const agentRunsRootName = "Agent Runs"

// folderNameMaxLen keeps an auto folder name inside a sane, readable width; a
// long issue title is truncated rather than filling the sidebar.
const folderNameMaxLen = 120

// agentRunsFoldersEnabled reports whether the feature is switched ON for this
// workspace. The flag default is OFF (no stored row), so a missing override or
// a lookup error resolves to OFF — the structure stays sealed unless an admin
// explicitly enabled it.
func (h *Handler) agentRunsFoldersEnabled(ctx context.Context, wsID pgtype.UUID) bool {
	if h.CerebroQueries == nil {
		return false
	}
	enabled, err := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: wsID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     agentRunsFoldersFlagKey,
	})
	if err != nil {
		return false
	}
	return enabled
}

// autoFileAgentArtifact is the single hook the upstream CreateArtifact handler
// calls. It returns the folder the artifact should be filed under. It only
// diverts the save when ALL of these hold; otherwise it returns currentFolder
// unchanged so the caller's behaviour is identical to before:
//
//   - the feature flag is ON for this workspace,
//   - the actor is an agent (authorType == "agent"),
//   - the agent did not choose a folder itself (currentFolder is unset),
//   - the run identity (X-Task-ID) resolves to a real task for this agent.
//
// A failure to build the structure is logged and swallowed: a document must
// still save even if its auto-folder could not be resolved. Silent by design —
// no events are published for the folders created here.
func (h *Handler) autoFileAgentArtifact(
	r *http.Request,
	workspaceID, authorType, authorID, artifactKind string,
	currentFolder pgtype.UUID,
) pgtype.UUID {
	if authorType != "agent" || currentFolder.Valid {
		return currentFolder
	}
	wsUUID := parseUUID(workspaceID)
	if !h.agentRunsFoldersEnabled(r.Context(), wsUUID) {
		return currentFolder
	}

	surface := "document"
	if artifactKind == "note" {
		surface = "note"
	}

	folderID, err := h.ensureAgentRunFolder(r, wsUUID, authorID, surface)
	if err != nil {
		slog.Warn("agent-runs-folder: auto-file skipped, filing at root",
			"error", err, "workspace_id", workspaceID, "agent_id", authorID, "surface", surface)
		return currentFolder
	}
	return folderID
}

// ensureAgentRunFolder resolves (creating as needed) the run-leaf folder for the
// agent's current task and returns its id. The four-level walk runs inside one
// transaction under a per-(workspace,surface) advisory lock, so two saves racing
// on the very first run of an agent cannot create duplicate "Agent Runs" roots
// (the artifact_folder unique key does not dedupe rows whose parent_id is NULL).
func (h *Handler) ensureAgentRunFolder(
	r *http.Request,
	wsUUID pgtype.UUID,
	agentID, surface string,
) (pgtype.UUID, error) {
	ctx := r.Context()

	// Run identity: the daemon pairs X-Agent-ID with X-Task-ID on every agent
	// request. Without a resolvable task there is no run to file under.
	taskID := r.Header.Get("X-Task-ID")
	if taskID == "" {
		return pgtype.UUID{}, errors.New("no X-Task-ID on request")
	}
	agentUUID := parseUUID(agentID)
	agent, err := h.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("load agent: %w", err)
	}
	if uuidToString(agent.WorkspaceID) != uuidToString(wsUUID) {
		return pgtype.UUID{}, errors.New("agent/workspace mismatch")
	}

	// member level = the agent's owner. Fall back to the agent name if the agent
	// is owner-less (system agents) so the structure still nests cleanly.
	memberName := "Unassigned"
	var ownerID pgtype.UUID
	if agent.OwnerID.Valid {
		ownerID = agent.OwnerID
		if owner, err := h.Queries.GetUser(ctx, agent.OwnerID); err == nil {
			memberName = displayNameOr(owner.Name, owner.Email, "Unassigned")
		}
	}
	agentName := displayNameOr(agent.Name, "", "Agent")

	// run level = the issue the task served, else a short run label.
	runName := runFolderName(ctx, h, wsUUID, taskID)

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Serialise concurrent first-saves for this workspace+surface so the NULL
	// parent_id root cannot be double-inserted.
	lockKey := uuidToString(wsUUID) + ":agent-runs:" + surface
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return pgtype.UUID{}, fmt.Errorf("advisory lock: %w", err)
	}

	createdBy := requesterFolderCreator(r)

	root, err := h.ensureFolderTx(ctx, tx, wsUUID, surface, pgtype.UUID{}, agentRunsRootName, ownerID, createdBy)
	if err != nil {
		return pgtype.UUID{}, err
	}
	member, err := h.ensureFolderTx(ctx, tx, wsUUID, surface, root, memberName, ownerID, createdBy)
	if err != nil {
		return pgtype.UUID{}, err
	}
	agentFolder, err := h.ensureFolderTx(ctx, tx, wsUUID, surface, member, agentName, ownerID, createdBy)
	if err != nil {
		return pgtype.UUID{}, err
	}
	runFolder, err := h.ensureFolderTx(ctx, tx, wsUUID, surface, agentFolder, runName, ownerID, createdBy)
	if err != nil {
		return pgtype.UUID{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return pgtype.UUID{}, fmt.Errorf("commit: %w", err)
	}
	return runFolder, nil
}

// ensureFolderTx get-or-creates one folder level and, when it actually creates
// the row, stamps a Collections sharing grant on it. parent is the zero UUID for
// a root-level folder. It returns the folder id whether found or created.
func (h *Handler) ensureFolderTx(
	ctx context.Context,
	tx pgx.Tx,
	wsUUID pgtype.UUID,
	surface string,
	parent pgtype.UUID,
	name string,
	ownerID, createdBy pgtype.UUID,
) (pgtype.UUID, error) {
	name = normaliseFolderName(name)

	// Look up an existing folder at this level. NULL parent must match with IS
	// NULL, not '=' (the unique key treats NULLs as distinct, which is exactly
	// why the advisory lock guards the root).
	var existing pgtype.UUID
	row := tx.QueryRow(ctx, `
		SELECT id FROM artifact_folder
		WHERE workspace_id = $1 AND kind = $2 AND name = $3
		  AND ((parent_id IS NULL AND $4::uuid IS NULL) OR parent_id = $4)
		LIMIT 1`,
		wsUUID, surface, name, nullableUUID(parent))
	switch err := row.Scan(&existing); {
	case err == nil:
		return existing, nil
	case errors.Is(err, pgx.ErrNoRows):
		// fall through to create
	default:
		return pgtype.UUID{}, fmt.Errorf("lookup folder %q: %w", name, err)
	}

	newID, _ := uuid.NewV7()
	folderID := pgtype.UUID{Bytes: newID, Valid: true}
	if _, err := tx.Exec(ctx, `
		INSERT INTO artifact_folder (id, workspace_id, parent_id, name, kind, owner_id, visibility)
		VALUES ($1, $2, $3, $4, $5, $6, 'workspace')`,
		folderID, wsUUID, nullableUUID(parent), name, surface, nullableUUID(ownerID),
	); err != nil {
		return pgtype.UUID{}, fmt.Errorf("create folder %q: %w", name, err)
	}

	// Collections sharing from day one: a workspace-level viewer grant reproduces
	// today's document visibility (visible to the whole workspace) as an explicit,
	// tightenable Collections setting. ON CONFLICT DO NOTHING keeps it idempotent
	// if the same folder is re-stamped.
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_folder_grant (surface, folder_id, grantee_type, grantee_id, role, created_by)
		VALUES ('artifact', $1, 'workspace', NULL, 'viewer', $2)
		ON CONFLICT (surface, folder_id, grantee_type,
		             COALESCE(grantee_id, '00000000-0000-0000-0000-000000000000'::uuid))
		DO NOTHING`,
		folderID, nullableUUID(createdBy),
	); err != nil {
		return pgtype.UUID{}, fmt.Errorf("grant folder %q: %w", name, err)
	}

	return folderID, nil
}

// runFolderName picks a human name for the run leaf: the issue title the task
// served, else a short "Run <id8>" fallback for chat / quick-create runs.
func runFolderName(ctx context.Context, h *Handler, wsUUID pgtype.UUID, taskID string) string {
	fallback := "Run " + shortID(taskID)
	task, err := h.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		return fallback
	}
	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          task.IssueID,
			WorkspaceID: wsUUID,
		}); err == nil {
			return displayNameOr(issue.Title, "", fallback)
		}
	}
	return fallback
}

// requesterFolderCreator records who the auto-folders are attributed to. The
// authenticated user (the run's initiator for a task-token agent) is the natural
// created_by; an unresolvable id leaves the grant creator NULL.
func requesterFolderCreator(r *http.Request) pgtype.UUID {
	if uid := requestUserID(r); uid != "" {
		return parseUUID(uid)
	}
	return pgtype.UUID{}
}

// displayNameOr returns the first non-empty of primary/secondary, else def.
func displayNameOr(primary, secondary, def string) string {
	if s := strings.TrimSpace(primary); s != "" {
		return s
	}
	if s := strings.TrimSpace(secondary); s != "" {
		return s
	}
	return def
}

// normaliseFolderName trims and bounds a folder name so it is never empty and
// never absurdly long (artifact_folder.name is NOT NULL free text).
func normaliseFolderName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Untitled"
	}
	if len(name) > folderNameMaxLen {
		name = strings.TrimSpace(name[:folderNameMaxLen])
	}
	return name
}

// shortID returns the first 8 chars of an id for a compact run label.
func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// nullableUUID returns a pgtype.UUID unchanged, letting an invalid (zero) value
// bind as SQL NULL through the driver.
func nullableUUID(u pgtype.UUID) pgtype.UUID {
	return u
}
