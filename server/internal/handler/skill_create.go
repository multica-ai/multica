package handler

import (
	"context"
	"encoding/json"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type skillCreateInput struct {
	WorkspaceID string
	CreatorID   string
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
		WorkspaceID: parseUUID(input.WorkspaceID),
		Name:        input.Name,
		Description: input.Description,
		Content:     input.Content,
		Config:      config,
		CreatedBy:   parseUUID(input.CreatorID),
	})
	if err != nil {
		return SkillWithFilesResponse{}, err
	}

	fileResps := make([]SkillFileResponse, 0, len(input.Files))
	for _, f := range input.Files {
		sf, err := qtx.UpsertSkillFile(ctx, db.UpsertSkillFileParams{
			SkillID: skill.ID,
			Path:    f.Path,
			Content: f.Content,
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
