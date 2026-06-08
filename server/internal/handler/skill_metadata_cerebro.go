package handler

// TECH-3077: skill metadata — category/domain/tag/status filtering and
// metadata sync on create/update. All new code lives here (cerebro zone).
// Two CEREBRO-PATCHes in skill.go + one in skill_create.go wire this in.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// SkillMetadataView is the metadata object included in skill list/detail responses.
type SkillMetadataView struct {
	Category  string   `json:"category,omitempty"`
	Domain    string   `json:"domain,omitempty"`
	Type      string   `json:"type,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	Status    string   `json:"status,omitempty"`
	Tags      []string `json:"tags"`
	AutoLearn bool     `json:"auto_learn"`
}

func decodeSkillMetadata(raw []byte) *SkillMetadataView {
	if len(raw) == 0 {
		return nil
	}
	var m cerebro.SkillMetadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	return &SkillMetadataView{
		Category:  m.Category,
		Domain:    m.Domain,
		Type:      m.Type,
		Scope:     m.Scope,
		Status:    m.Status,
		Tags:      tags,
		AutoLearn: m.AutoLearn,
	}
}

// SkillSummaryWithMetadata extends SkillSummaryResponse with metadata and version.
type SkillSummaryWithMetadata struct {
	SkillSummaryResponse
	Metadata       *SkillMetadataView `json:"metadata,omitempty"`
	CurrentVersion string             `json:"current_version,omitempty"`
}

// listSkillsByMetadata handles GET /skills when category/domain/tag/status/data_domain
// query params are present.
func (h *Handler) listSkillsByMetadata(w http.ResponseWriter, r *http.Request, workspaceID string) {
	// CEREBRO-PATCH(skill-metadata-test-fallback): Existing handler tests build Handler without CerebroQueries.
	if h.CerebroQueries == nil {
		h.listSkillSummariesWithoutMetadata(w, r, workspaceID)
		return
	}

	q := r.URL.Query()
	rows, err := h.CerebroQueries.ListSkillsByMetadata(r.Context(), cerebrodb.ListSkillsByMetadataParams{
		WorkspaceID: parseUUID(workspaceID),
		Category:    q.Get("category"),
		Domain:      q.Get("domain"),
		Tag:         q.Get("tag"),
		Status:      q.Get("status"),
		DataDomain:  q.Get("data_domain"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skills")
		return
	}

	resp := make([]SkillSummaryWithMetadata, len(rows))
	for i, s := range rows {
		resp[i] = SkillSummaryWithMetadata{
			SkillSummaryResponse: skillSummaryToResponse(
				s.ID, s.WorkspaceID, s.Name, s.Description, s.Config,
				s.CreatedBy, s.CreatedAt, s.UpdatedAt,
			),
			Metadata:       decodeSkillMetadata(s.Metadata),
			CurrentVersion: s.CurrentVersion,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listSkillSummariesWithoutMetadata(w http.ResponseWriter, r *http.Request, workspaceID string) {
	rows, err := h.Queries.ListSkillSummariesByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skills")
		return
	}

	resp := make([]SkillSummaryResponse, len(rows))
	for i, s := range rows {
		resp[i] = skillSummaryToResponse(
			s.ID, s.WorkspaceID, s.Name, s.Description, s.Config,
			s.CreatedBy, s.CreatedAt, s.UpdatedAt,
		)
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListSkillsAudit handles GET /skills/audit — returns skills with missing metadata.
func (h *Handler) ListSkillsAudit(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	rows, err := h.CerebroQueries.ListSkillsWithMissingMetadata(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skills")
		return
	}

	type auditRow struct {
		ID             string             `json:"id"`
		Name           string             `json:"name"`
		Metadata       *SkillMetadataView `json:"metadata"`
		CurrentVersion string             `json:"current_version"`
		Issues         []string           `json:"issues"`
	}

	resp := make([]auditRow, len(rows))
	for i, s := range rows {
		meta := decodeSkillMetadata(s.Metadata)
		var issues []string
		if meta == nil || meta.Category == "" {
			issues = append(issues, "missing category")
		}
		if meta == nil || meta.Domain == "" {
			issues = append(issues, "missing domain")
		}
		if meta == nil || meta.Status == "" {
			issues = append(issues, "missing status")
		}
		resp[i] = auditRow{
			ID:             uuidToString(s.ID),
			Name:           s.Name,
			Metadata:       meta,
			CurrentVersion: s.CurrentVersion,
			Issues:         issues,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// syncSkillMetadataAsync fires metadata sync in a goroutine so it never blocks
// the response. Also syncs automation links after metadata is written.
func (h *Handler) syncSkillMetadataAsync(skillID pgtype.UUID, content string) {
	if h.CerebroQueries == nil {
		return
	}
	go func() {
		if err := cerebro.SyncSkillMetadata(context.Background(), h.CerebroQueries, skillID, content); err != nil {
			slog.Error("skill metadata sync failed", "skill_id", uuidToString(skillID), "error", err)
			return
		}
		// CEREBRO-PATCH(skill-automation-sync): TECH-3077 — sync automation links after metadata.
		raw, _ := h.CerebroQueries.GetSkillMetadata(context.Background(), skillID)
		h.syncAutomationLinksAsync(skillID, raw)
	}()
}
