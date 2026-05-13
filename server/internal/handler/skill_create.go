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
}

func (h *Handler) createSkillWithFiles(ctx context.Context, input skillCreateInput) (SkillWithFilesResponse, error) {
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
