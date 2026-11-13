package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	skillpkg "github.com/multica-ai/multica/server/internal/skill"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Skill version sources. A version records how the skill reached that state.
const (
	// skillVersionSourceBaseline is the pre-write snapshot of a skill that had
	// no history yet (it predates skill versioning).
	skillVersionSourceBaseline = "baseline"
	skillVersionSourceCreate   = "create"
	skillVersionSourceUpdate   = "update"
	skillVersionSourceImport   = "import"
	skillVersionSourceRefresh  = "refresh"
	skillVersionSourceRestore  = "restore"
)

type skillVersionFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type SkillVersionSummaryResponse struct {
	ID                  string  `json:"id"`
	SkillID             string  `json:"skill_id"`
	Version             int32   `json:"version"`
	Name                string  `json:"name"`
	Source              string  `json:"source"`
	RestoredFromVersion *int32  `json:"restored_from_version"`
	CreatedBy           *string `json:"created_by"`
	CreatedAt           string  `json:"created_at"`
}

type SkillVersionResponse struct {
	SkillVersionSummaryResponse
	Description string             `json:"description"`
	Content     string             `json:"content"`
	Files       []skillVersionFile `json:"files"`
}

type skillVersionRecord struct {
	Source       string
	CreatedBy    pgtype.UUID
	RestoredFrom pgtype.Int4
	// CreatedAt overrides the row timestamp. Baselines use the skill's last
	// update time so the list shows when that content was actually written.
	CreatedAt pgtype.Timestamptz
}

// isVersionedSkill reports whether a skill keeps content history. Plugin
// skills are owned by their plugin package, which carries its own version.
func isVersionedSkill(skill db.Skill) bool {
	return !skill.PluginInstallationID.Valid
}

// ensureSkillVersionBaseline must run inside the write transaction, before
// the write. It locks the skill row (serializing version allocation for this
// skill until the transaction ends) and, when the skill has no history yet,
// snapshots its current state so the upcoming write can be reverted.
func ensureSkillVersionBaseline(ctx context.Context, qtx *db.Queries, skillID pgtype.UUID) error {
	skill, err := qtx.LockSkillForVersioning(ctx, skillID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if !isVersionedSkill(skill) {
		return nil
	}
	if _, err := qtx.GetLatestSkillVersion(ctx, skill.ID); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = appendSkillVersion(ctx, qtx, skill, skillVersionRecord{
		Source:    skillVersionSourceBaseline,
		CreatedBy: skill.CreatedBy,
		CreatedAt: skill.UpdatedAt,
	})
	return err
}

// recordSkillVersion must run inside the write transaction, after the write.
// It snapshots the skill's current state and appends a version unless the
// content is identical to the latest version (no-op saves, config-only edits).
func recordSkillVersion(ctx context.Context, qtx *db.Queries, skillID pgtype.UUID, rec skillVersionRecord) (bool, error) {
	skill, err := qtx.LockSkillForVersioning(ctx, skillID)
	if err != nil {
		return false, err
	}
	if !isVersionedSkill(skill) {
		return false, nil
	}
	return appendSkillVersion(ctx, qtx, skill, rec)
}

func appendSkillVersion(ctx context.Context, qtx *db.Queries, skill db.Skill, rec skillVersionRecord) (bool, error) {
	files, err := qtx.ListSkillFiles(ctx, skill.ID)
	if err != nil {
		return false, err
	}
	snapshot := make([]skillVersionFile, len(files))
	for i, f := range files {
		snapshot[i] = skillVersionFile{Path: f.Path, Content: f.Content}
	}
	hash, err := skillVersionHash(skill.Name, skill.Description, skill.Content, snapshot)
	if err != nil {
		return false, err
	}

	next := int32(1)
	latest, err := qtx.GetLatestSkillVersion(ctx, skill.ID)
	switch {
	case err == nil:
		if latest.ContentHash == hash {
			return false, nil
		}
		next = latest.Version + 1
	case !errors.Is(err, pgx.ErrNoRows):
		return false, err
	}

	filesJSON, err := json.Marshal(snapshot)
	if err != nil {
		return false, err
	}
	_, err = qtx.CreateSkillVersion(ctx, db.CreateSkillVersionParams{
		WorkspaceID:         skill.WorkspaceID,
		SkillID:             skill.ID,
		Version:             next,
		Name:                skill.Name,
		Description:         skill.Description,
		Content:             skill.Content,
		Files:               filesJSON,
		ContentHash:         hash,
		Source:              rec.Source,
		RestoredFromVersion: rec.RestoredFrom,
		CreatedBy:           rec.CreatedBy,
		CreatedAt:           rec.CreatedAt,
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// skillVersionHash fingerprints the versioned fields. Files come from
// ListSkillFiles, which orders by path, so the encoding is deterministic.
func skillVersionHash(name, description, content string, files []skillVersionFile) (string, error) {
	payload, err := json.Marshal(struct {
		Name        string             `json:"name"`
		Description string             `json:"description"`
		Content     string             `json:"content"`
		Files       []skillVersionFile `json:"files"`
	}{name, description, content, files})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func skillVersionSummaryToResponse(id, skillID pgtype.UUID, version int32, name, source string, restoredFrom pgtype.Int4, createdBy pgtype.UUID, createdAt pgtype.Timestamptz) SkillVersionSummaryResponse {
	return SkillVersionSummaryResponse{
		ID:                  uuidToString(id),
		SkillID:             uuidToString(skillID),
		Version:             version,
		Name:                name,
		Source:              source,
		RestoredFromVersion: int4ToPtr(restoredFrom),
		CreatedBy:           uuidToPtr(createdBy),
		CreatedAt:           timestampToString(createdAt),
	}
}

func decodeSkillVersionFiles(raw []byte) ([]skillVersionFile, error) {
	files := []skillVersionFile{}
	if len(raw) == 0 {
		return files, nil
	}
	if err := json.Unmarshal(raw, &files); err != nil {
		return nil, err
	}
	return files, nil
}

func (h *Handler) ListSkillVersions(w http.ResponseWriter, r *http.Request) {
	skill, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListSkillVersions(r.Context(), db.ListSkillVersionsParams{
		SkillID:     skill.ID,
		WorkspaceID: skill.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skill versions")
		return
	}
	resp := make([]SkillVersionSummaryResponse, len(rows))
	for i, v := range rows {
		resp[i] = skillVersionSummaryToResponse(v.ID, v.SkillID, v.Version, v.Name, v.Source, v.RestoredFromVersion, v.CreatedBy, v.CreatedAt)
	}
	writeJSON(w, http.StatusOK, resp)
}

// loadSkillVersion resolves the {version} URL param against an already
// authorized skill.
func (h *Handler) loadSkillVersion(w http.ResponseWriter, r *http.Request, q *db.Queries, skill db.Skill) (db.SkillVersion, bool) {
	n, err := strconv.ParseInt(chi.URLParam(r, "version"), 10, 32)
	if err != nil || n < 1 {
		writeError(w, http.StatusBadRequest, "invalid version")
		return db.SkillVersion{}, false
	}
	v, err := q.GetSkillVersion(r.Context(), db.GetSkillVersionParams{
		SkillID:     skill.ID,
		WorkspaceID: skill.WorkspaceID,
		Version:     int32(n),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "skill version not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load skill version")
		}
		return db.SkillVersion{}, false
	}
	return v, true
}

func (h *Handler) GetSkillVersion(w http.ResponseWriter, r *http.Request) {
	skill, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	v, ok := h.loadSkillVersion(w, r, h.Queries, skill)
	if !ok {
		return
	}
	files, err := decodeSkillVersionFiles(v.Files)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode skill version")
		return
	}
	writeJSON(w, http.StatusOK, SkillVersionResponse{
		SkillVersionSummaryResponse: skillVersionSummaryToResponse(v.ID, v.SkillID, v.Version, v.Name, v.Source, v.RestoredFromVersion, v.CreatedBy, v.CreatedAt),
		Description:                 v.Description,
		Content:                     v.Content,
		Files:                       files,
	})
}

// RestoreSkillVersion writes an old version's snapshot back to the skill and
// records the result as a new latest version. History is never rewritten.
func (h *Handler) RestoreSkillVersion(w http.ResponseWriter, r *http.Request) {
	skill, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}
	if !isVersionedSkill(skill) {
		writeError(w, http.StatusUnprocessableEntity, "plugin skills do not keep version history")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := ensureSkillVersionBaseline(r.Context(), qtx, skill.ID); err != nil {
		slog.Warn("skill version baseline failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to restore skill version")
		return
	}
	v, ok := h.loadSkillVersion(w, r, qtx, skill)
	if !ok {
		return
	}
	files, err := decodeSkillVersionFiles(v.Files)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode skill version")
		return
	}

	updated, err := qtx.UpdateSkill(r.Context(), db.UpdateSkillParams{
		ID:          skill.ID,
		Name:        pgtype.Text{String: v.Name, Valid: true},
		Description: pgtype.Text{String: v.Description, Valid: true},
		Content:     pgtype.Text{String: v.Content, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "skill not found")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "another skill in this workspace already uses the name \""+v.Name+"\"")
			return
		}
		slog.Warn("restore skill version failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to restore skill version")
		return
	}
	if err := qtx.DeleteSkillFilesBySkill(r.Context(), skill.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore skill files")
		return
	}
	fileResps := make([]SkillFileResponse, 0, len(files))
	for _, f := range files {
		if !validateFilePath(f.Path) || skillpkg.IsReservedContentPath(f.Path) {
			continue
		}
		sf, err := qtx.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{
			SkillID: skill.ID,
			Path:    f.Path,
			Content: f.Content,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to restore skill files")
			return
		}
		fileResps = append(fileResps, skillFileToResponse(sf))
	}
	if _, err := recordSkillVersion(r.Context(), qtx, skill.ID, skillVersionRecord{
		Source:       skillVersionSourceRestore,
		CreatedBy:    parseUUID(requestUserID(r)),
		RestoredFrom: pgtype.Int4{Int32: v.Version, Valid: true},
	}); err != nil {
		slog.Warn("record skill version failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to restore skill version")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	resp := SkillWithFilesResponse{
		SkillResponse: skillToResponse(updated),
		Files:         fileResps,
	}
	wsID := uuidToString(skill.WorkspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), wsID)
	h.publish(protocol.EventSkillUpdated, wsID, actorType, actorID, map[string]any{"skill": resp})
	writeJSON(w, http.StatusOK, resp)
}
