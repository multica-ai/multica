package handler

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	skillpkg "github.com/multica-ai/multica/server/internal/skill"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type skillCreateInput struct {
	WorkspaceID pgtype.UUID
	CreatorID   pgtype.UUID
	Name        string
	Description string
	Content     string
	Config      any
	Files       []CreateSkillFileRequest
}

// createSkillWithFilesInTx writes a skill plus its supporting files using the
// provided sqlc Queries handle, which must already be bound to an open
// transaction. Callers compose skill creation with other writes (e.g. agent
// template materialization) inside one outer transaction. For standalone
// skill creation, prefer createSkillWithFiles, which manages its own tx.
func createSkillWithFilesInTx(ctx context.Context, qtx *db.Queries, input skillCreateInput) (SkillWithFilesResponse, error) {
	config, err := json.Marshal(input.Config)
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	if input.Config == nil {
		config = []byte("{}")
	}

	skill, err := qtx.CreateSkill(ctx, db.CreateSkillParams{
		WorkspaceID: input.WorkspaceID,
		Name:        sanitizeNullBytes(input.Name),
		Description: sanitizeNullBytes(input.Description),
		Content:     sanitizeNullBytes(input.Content),
		Config:      config,
		CreatedBy:   input.CreatorID,
	})
	if err != nil {
		return SkillWithFilesResponse{}, err
	}

	fileResps := make([]SkillFileResponse, 0, len(input.Files))
	for _, f := range input.Files {
		// SKILL.md is reserved for the primary skill content (skill.Content).
		// Supporting files must carry additional assets, not duplicate the main file.
		if skillpkg.IsReservedContentPath(f.Path) {
			continue
		}
		sf, err := qtx.UpsertSkillFile(ctx, db.UpsertSkillFileParams{
			SkillID: skill.ID,
			Path:    sanitizeNullBytes(f.Path),
			Content: sanitizeNullBytes(f.Content),
		})
		if err != nil {
			return SkillWithFilesResponse{}, err
		}
		fileResps = append(fileResps, skillFileToResponse(sf))
	}

	return SkillWithFilesResponse{
		SkillResponse: skillToResponse(skill),
		Files:         fileResps,
	}, nil
}

func (h *Handler) createSkillWithFiles(ctx context.Context, input skillCreateInput) (SkillWithFilesResponse, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)

	result, err := createSkillWithFilesInTx(ctx, qtx, input)
	if err != nil {
		return SkillWithFilesResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return SkillWithFilesResponse{}, err
	}

	return result, nil
}

// errSkillOverwriteNotFound / errSkillOverwriteForbidden are the terminal
// boundary cases of overwriteSkillWithFiles: the target was deleted (or moved
// out of the workspace) or the caller lost overwrite permission between the
// user's confirm and this write. Callers map them to a failed import and must
// NOT fall back to creating a new skill.
var (
	errSkillOverwriteNotFound     = errors.New("target skill not found")
	errSkillOverwriteForbidden    = errors.New("not permitted to overwrite target skill")
	errSkillOverwriteNameMismatch = errors.New("target skill name does not match the imported skill")
	errSkillOverwriteNameConflict = errors.New("another skill in the workspace already has the imported name")
	errSkillOverwriteStale        = errors.New("target skill changed since it was read")
)

// skillOverwriteAuthz names the permission rule overwriteSkillWithFiles applies
// inside its transaction. It is a policy, not a decision: the tx evaluates it
// against state it reads itself, so a caller that did slow work (a network
// fetch) between its own check and this write cannot carry a stale verdict in.
type skillOverwriteAuthz int

const (
	// overwriteAuthzCreatorOnly is the default. Only the skill's current
	// creator may overwrite it — see canOverwriteSkillByLocalImport.
	overwriteAuthzCreatorOnly skillOverwriteAuthz = iota
	// overwriteAuthzCreatorOrManager mirrors canManageSkill: the caller must
	// still be a workspace member, and must be an owner/admin or the skill's
	// current creator. Membership and role are re-read inside the tx.
	overwriteAuthzCreatorOrManager
)

type skillOverwriteInput struct {
	WorkspaceID   pgtype.UUID
	TargetSkillID pgtype.UUID
	UserID        string // re-checked against the skill creator inside the tx
	// ExpectedName, when non-empty, must equal the target's current name. Guards
	// against a client sending the wrong target_skill_id and overwriting a
	// different skill than the one the conflict dialog showed the user. The
	// caller passes the sanitized effective import name.
	ExpectedName string
	// NewName, when non-empty, renames the target to the imported skill's name.
	// A collision with another skill in the workspace (UNIQUE(workspace_id,
	// name)) fails the whole overwrite with errSkillOverwriteNameConflict.
	NewName string
	// Authz selects the permission rule re-applied inside the tx.
	Authz skillOverwriteAuthz
	// ExpectedUpdatedAt, when valid, must equal the target's current updated_at.
	// Otherwise the overwrite fails with errSkillOverwriteStale. Callers that
	// read the skill, do slow work, then overwrite pass what they read.
	ExpectedUpdatedAt pgtype.Timestamptz
	Description       string
	Content           string
	Config            any
	Files             []CreateSkillFileRequest
}

// authorizeSkillOverwrite applies input.Authz to the locked target row. Under
// overwriteAuthzCreatorOrManager it re-reads the caller's membership, so a
// caller removed from the workspace or demoted since the request-time check is
// rejected here rather than landing the write.
func authorizeSkillOverwrite(ctx context.Context, qtx *db.Queries, input skillOverwriteInput, existing db.Skill) error {
	isCreator := canOverwriteSkillByLocalImport(input.UserID, existing)
	if input.Authz == overwriteAuthzCreatorOnly {
		if !isCreator {
			return errSkillOverwriteForbidden
		}
		return nil
	}

	userUUID, err := util.ParseUUID(input.UserID)
	if err != nil {
		return errSkillOverwriteForbidden
	}
	member, err := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userUUID,
		WorkspaceID: input.WorkspaceID,
	})
	if err != nil {
		// No membership row: the caller left or was removed since the
		// request-time check.
		if errors.Is(err, pgx.ErrNoRows) {
			return errSkillOverwriteForbidden
		}
		return err
	}
	if !roleAllowed(member.Role, "owner", "admin", "member") {
		return errSkillOverwriteForbidden
	}
	if !roleAllowed(member.Role, "owner", "admin") && !isCreator {
		return errSkillOverwriteForbidden
	}
	return nil
}

// overwriteSkillWithFiles re-imports a bundle onto an existing skill in a single
// transaction. It locks the target row and re-verifies three things in that tx:
// the target still exists in the workspace, UserID may overwrite it under
// input.Authz (see authorizeSkillOverwrite), and it has not changed since
// input.ExpectedUpdatedAt. A deleted target, a creator change, a role change, or
// a concurrent edit fails cleanly via errSkillOverwriteNotFound,
// errSkillOverwriteForbidden or errSkillOverwriteStale rather than falling back
// to create.
//
// Preserved: id, created_by, created_at, name, and agent_skill bindings (the
// row identity and the binding table are never touched). Replaced: description,
// content, config (origin), and the full file set — files absent from the new
// bundle are pruned via DeleteSkillFilesBySkill. On any error the tx rolls back,
// leaving the original skill unchanged.
func (h *Handler) overwriteSkillWithFiles(ctx context.Context, input skillOverwriteInput) (SkillWithFilesResponse, error) {
	config, err := json.Marshal(input.Config)
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	if input.Config == nil {
		config = []byte("{}")
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)

	// FOR UPDATE locks the row for the rest of the tx, so the checks below
	// (permission, updated_at, name) still hold at the UPDATE. Under READ
	// COMMITTED a plain read would let a concurrent edit commit in between.
	existing, err := qtx.GetSkillInWorkspaceForUpdate(ctx, db.GetSkillInWorkspaceForUpdateParams{
		ID:          input.TargetSkillID,
		WorkspaceID: input.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SkillWithFilesResponse{}, errSkillOverwriteNotFound
		}
		return SkillWithFilesResponse{}, err
	}
	if err := authorizeSkillOverwrite(ctx, qtx, input, existing); err != nil {
		return SkillWithFilesResponse{}, err
	}
	// Compare-and-set against the row the caller read before its slow work.
	// UpdateSkill replaces name, description, content and config outright.
	// Without this check an edit made in that window disappears with no signal
	// — including a change to the source URL in config.origin.
	if input.ExpectedUpdatedAt.Valid &&
		!existing.UpdatedAt.Time.Equal(input.ExpectedUpdatedAt.Time) {
		return SkillWithFilesResponse{}, errSkillOverwriteStale
	}
	// The overwrite is keyed on target_skill_id, but the conflict the user
	// confirmed was a same-name collision; reject if the target's name no longer
	// matches the imported skill so a stale/wrong target_skill_id can't write
	// one skill's content onto another.
	if input.ExpectedName != "" && existing.Name != input.ExpectedName {
		return SkillWithFilesResponse{}, errSkillOverwriteNameMismatch
	}

	// Name stays unset by default (COALESCE keeps the existing name): the
	// import-conflict overwrite targets the same-name skill, so preserving it
	// avoids any unique-name churn. Refresh-from-source passes NewName to adopt
	// an upstream rename.
	var newName pgtype.Text
	if n := sanitizeNullBytes(input.NewName); n != "" && n != existing.Name {
		newName = pgtype.Text{String: n, Valid: true}
	}
	skill, err := qtx.UpdateSkill(ctx, db.UpdateSkillParams{
		ID:          existing.ID,
		Name:        newName,
		Description: pgtype.Text{String: sanitizeNullBytes(input.Description), Valid: true},
		Content:     pgtype.Text{String: sanitizeNullBytes(input.Content), Valid: true},
		Config:      config,
	})
	if err != nil {
		// A committed concurrent DELETE can land between the read above and this
		// UPDATE (READ COMMITTED), so UpdateSkill matches 0 rows. Classify it as
		// the same "target gone" terminal case rather than a generic failure.
		if errors.Is(err, pgx.ErrNoRows) {
			return SkillWithFilesResponse{}, errSkillOverwriteNotFound
		}
		if newName.Valid && isUniqueViolation(err) {
			return SkillWithFilesResponse{}, errSkillOverwriteNameConflict
		}
		return SkillWithFilesResponse{}, err
	}

	// Full replace: drop every existing file, then re-insert the new set so
	// files no longer present in the local source are removed.
	if err := qtx.DeleteSkillFilesBySkill(ctx, skill.ID); err != nil {
		return SkillWithFilesResponse{}, err
	}
	fileResps := make([]SkillFileResponse, 0, len(input.Files))
	for _, f := range input.Files {
		sf, err := qtx.UpsertSkillFile(ctx, db.UpsertSkillFileParams{
			SkillID: skill.ID,
			Path:    sanitizeNullBytes(f.Path),
			Content: sanitizeNullBytes(f.Content),
		})
		if err != nil {
			return SkillWithFilesResponse{}, err
		}
		fileResps = append(fileResps, skillFileToResponse(sf))
	}

	if err := tx.Commit(ctx); err != nil {
		return SkillWithFilesResponse{}, err
	}

	return SkillWithFilesResponse{
		SkillResponse: skillToResponse(skill),
		Files:         fileResps,
	}, nil
}
