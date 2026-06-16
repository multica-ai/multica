package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const sharedSkillOriginType = "runtime_shared"

type RuntimeSharedSkillSyncRequest struct {
	Skills []RuntimeSharedSkillBundle `json:"skills"`
}

type RuntimeSharedSkillBundle struct {
	Key         string                   `json:"key"`
	Name        string                   `json:"name"`
	Description string                   `json:"description,omitempty"`
	Content     string                   `json:"content"`
	SourcePath  string                   `json:"source_path"`
	Provider    string                   `json:"provider"`
	ContentHash string                   `json:"content_hash,omitempty"`
	Files       []CreateSkillFileRequest `json:"files,omitempty"`
}

type RuntimeSharedSkillSyncResponse struct {
	Status    string                            `json:"status"`
	Created   int                               `json:"created"`
	Updated   int                               `json:"updated"`
	Unchanged int                               `json:"unchanged"`
	Conflicts []RuntimeSharedSkillSyncConflict  `json:"conflicts,omitempty"`
	Errors    []RuntimeSharedSkillSyncItemError `json:"errors,omitempty"`
}

type RuntimeSharedSkillSyncConflict struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Skill  string `json:"skill_id,omitempty"`
	Reason string `json:"reason"`
}

type RuntimeSharedSkillSyncItemError struct {
	Key   string `json:"key"`
	Name  string `json:"name,omitempty"`
	Error string `json:"error"`
}

func (h *Handler) SyncRuntimeSharedSkills(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	var req RuntimeSharedSkillSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp := RuntimeSharedSkillSyncResponse{Status: "ok"}
	for _, incoming := range req.Skills {
		result, err := h.syncRuntimeSharedSkill(r.Context(), rt, incoming)
		if err != nil {
			if conflict := (*runtimeSharedSkillConflictError)(nil); errors.As(err, &conflict) {
				resp.Conflicts = append(resp.Conflicts, conflict.Conflict)
				continue
			}
			resp.Errors = append(resp.Errors, RuntimeSharedSkillSyncItemError{Key: incoming.Key, Name: incoming.Name, Error: err.Error()})
			continue
		}
		switch result.Status {
		case "created":
			resp.Created++
			h.publish(protocol.EventSkillCreated, uuidToString(rt.WorkspaceID), "daemon", uuidToString(rt.ID), map[string]any{"skill": result.Skill})
		case "updated":
			resp.Updated++
			h.publish(protocol.EventSkillUpdated, uuidToString(rt.WorkspaceID), "daemon", uuidToString(rt.ID), map[string]any{"skill": result.Skill})
		case "unchanged":
			resp.Unchanged++
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type runtimeSharedSkillSyncResult struct {
	Status string
	Skill  SkillWithFilesResponse
}

type runtimeSharedSkillConflictError struct {
	Conflict RuntimeSharedSkillSyncConflict
}

func (e *runtimeSharedSkillConflictError) Error() string { return e.Conflict.Reason }

func (h *Handler) syncRuntimeSharedSkill(ctx context.Context, rt db.AgentRuntime, incoming RuntimeSharedSkillBundle) (runtimeSharedSkillSyncResult, error) {
	key := strings.TrimSpace(incoming.Key)
	name := sanitizeNullBytes(strings.TrimSpace(incoming.Name))
	if key == "" {
		return runtimeSharedSkillSyncResult{}, fmt.Errorf("skill key is required")
	}
	if name == "" {
		return runtimeSharedSkillSyncResult{}, fmt.Errorf("skill name is required")
	}
	if strings.TrimSpace(incoming.Content) == "" {
		return runtimeSharedSkillSyncResult{}, fmt.Errorf("skill content is required")
	}

	files := make([]CreateSkillFileRequest, 0, len(incoming.Files))
	for _, f := range incoming.Files {
		if !validateFilePath(f.Path) {
			continue
		}
		files = append(files, f)
	}

	hash := strings.TrimSpace(incoming.ContentHash)
	if hash == "" {
		hash = hashRuntimeSharedSkill(incoming.Content, files)
	}
	config := map[string]any{
		"origin": map[string]any{
			"type":         sharedSkillOriginType,
			"runtime_id":   uuidToString(rt.ID),
			"provider":     strings.TrimSpace(incoming.Provider),
			"source_path":  strings.TrimSpace(incoming.SourcePath),
			"sync_key":     key,
			"content_hash": hash,
			"synced_at":    time.Now().UTC().Format(time.RFC3339Nano),
		},
	}

	existing, found, err := h.lookupSkillByName(ctx, rt.WorkspaceID, name)
	if err != nil {
		return runtimeSharedSkillSyncResult{}, err
	}
	if !found {
		creator := pgtype.UUID{}
		if rt.OwnerID.Valid {
			creator = rt.OwnerID
		}
		resp, err := h.createSkillWithFiles(ctx, skillCreateInput{
			WorkspaceID: rt.WorkspaceID,
			CreatorID:   creator,
			Name:        name,
			Description: incoming.Description,
			Content:     incoming.Content,
			Config:      config,
			Files:       files,
		})
		if err != nil {
			return runtimeSharedSkillSyncResult{}, err
		}
		return runtimeSharedSkillSyncResult{Status: "created", Skill: resp}, nil
	}

	origin := runtimeSharedSkillOrigin(existing.Config)
	if origin == nil || origin.SyncKey != key {
		return runtimeSharedSkillSyncResult{}, &runtimeSharedSkillConflictError{Conflict: RuntimeSharedSkillSyncConflict{
			Key: key, Name: name, Skill: uuidToString(existing.ID), Reason: "same-name skill exists with non-shared origin or a different shared sync key",
		}}
	}
	if origin.ContentHash == hash {
		return runtimeSharedSkillSyncResult{Status: "unchanged", Skill: SkillWithFilesResponse{SkillResponse: skillToResponse(existing)}}, nil
	}

	resp, err := h.overwriteRuntimeSharedSkillWithFiles(ctx, runtimeSharedSkillOverwriteInput{
		WorkspaceID:   rt.WorkspaceID,
		TargetSkillID: existing.ID,
		Description:   incoming.Description,
		Content:       incoming.Content,
		Config:        config,
		Files:         files,
	})
	if err != nil {
		return runtimeSharedSkillSyncResult{}, err
	}
	return runtimeSharedSkillSyncResult{Status: "updated", Skill: resp}, nil
}

type runtimeSharedSkillOriginInfo struct {
	SyncKey     string
	ContentHash string
}

func runtimeSharedSkillOrigin(raw []byte) *runtimeSharedSkillOriginInfo {
	var config struct {
		Origin struct {
			Type        string `json:"type"`
			SyncKey     string `json:"sync_key"`
			ContentHash string `json:"content_hash"`
		} `json:"origin"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil
	}
	if config.Origin.Type != sharedSkillOriginType {
		return nil
	}
	return &runtimeSharedSkillOriginInfo{SyncKey: config.Origin.SyncKey, ContentHash: config.Origin.ContentHash}
}

func hashRuntimeSharedSkill(content string, files []CreateSkillFileRequest) string {
	h := sha256.New()
	_, _ = h.Write([]byte(content))
	for _, f := range files {
		_, _ = h.Write([]byte("\x00" + f.Path + "\x00" + f.Content))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

type runtimeSharedSkillOverwriteInput struct {
	WorkspaceID   pgtype.UUID
	TargetSkillID pgtype.UUID
	Description   string
	Content       string
	Config        any
	Files         []CreateSkillFileRequest
}

func (h *Handler) overwriteRuntimeSharedSkillWithFiles(ctx context.Context, input runtimeSharedSkillOverwriteInput) (SkillWithFilesResponse, error) {
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
	existing, err := qtx.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{ID: input.TargetSkillID, WorkspaceID: input.WorkspaceID})
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	if runtimeSharedSkillOrigin(existing.Config) == nil {
		return SkillWithFilesResponse{}, fmt.Errorf("target skill is no longer a shared runtime skill")
	}

	skill, err := qtx.UpdateSkill(ctx, db.UpdateSkillParams{
		ID:          existing.ID,
		Description: pgtype.Text{String: sanitizeNullBytes(input.Description), Valid: true},
		Content:     pgtype.Text{String: sanitizeNullBytes(input.Content), Valid: true},
		Config:      config,
	})
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	if err := qtx.DeleteSkillFilesBySkill(ctx, skill.ID); err != nil {
		return SkillWithFilesResponse{}, err
	}
	fileResps := make([]SkillFileResponse, 0, len(input.Files))
	for _, f := range input.Files {
		sf, err := qtx.UpsertSkillFile(ctx, db.UpsertSkillFileParams{SkillID: skill.ID, Path: sanitizeNullBytes(f.Path), Content: sanitizeNullBytes(f.Content)})
		if err != nil {
			return SkillWithFilesResponse{}, err
		}
		fileResps = append(fileResps, skillFileToResponse(sf))
	}
	if err := tx.Commit(ctx); err != nil {
		return SkillWithFilesResponse{}, err
	}
	return SkillWithFilesResponse{SkillResponse: skillToResponse(skill), Files: fileResps}, nil
}
