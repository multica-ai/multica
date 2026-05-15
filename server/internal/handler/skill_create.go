package handler

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
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
	// CEREBRO-PATCH(skill-ownership-input): owner defaults to creator when
	// unset; controls who can manage the skill without going through the
	// change-request workflow.
	OwnerID pgtype.UUID
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

	// CEREBRO-PATCH(skill-ownership-default): default owner to creator.
	ownerID := input.OwnerID
	if !ownerID.Valid {
		ownerID = input.CreatorID
	}

	skill, err := qtx.CreateSkill(ctx, db.CreateSkillParams{
		WorkspaceID: input.WorkspaceID,
		Name:        sanitizeNullBytes(input.Name),
		Description: sanitizeNullBytes(input.Description),
		Content:     sanitizeNullBytes(input.Content),
		Config:      config,
		CreatedBy:   input.CreatorID,
		OwnerID:     ownerID,
	})
	if err != nil {
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

	// CEREBRO-PATCH(skill-version-snapshot): seed v1.0.0 so the version-history
	// view always has a baseline and the first change request has something to
	// diff against.
	if _, err := qtx.CreateSkillVersion(ctx, db.CreateSkillVersionParams{
		SkillID:     skill.ID,
		Version:     skill.CurrentVersion,
		Content:     skill.Content,
		Files:       skillFilesToVersionJSON(fileResps),
		Description: skill.Description,
		CreatedBy:   input.CreatorID,
	}); err != nil {
		return SkillWithFilesResponse{}, err
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
