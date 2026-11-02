package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Charter field limits. The briefing renders these into the leader's
// instructions on every claim, so they are bounded here rather than left to
// the column CHECKs, which would surface as a 500.
const (
	squadCharterTextMax      = 2000
	squadCharterItemTextMax  = 500
	squadCharterKeyMax       = 64
	squadCharterKBKeyMax     = 128
	squadCharterCadenceMax   = 64
	squadCharterListMax      = 20
	squadCharterDefaultHours = 48
)

// squadCharterOutputKinds are the kinds an output may promise. They match the
// routine expected_output values, less "none".
var squadCharterOutputKinds = map[string]bool{
	"issue":       true,
	"proposal":    true,
	"kb_document": true,
	"report":      true,
}

type SquadCharterResponsibility struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

type SquadCharterOutput struct {
	Key         string `json:"key"`
	Kind        string `json:"kind"`
	KBKey       string `json:"kb_key"`
	CadenceHint string `json:"cadence_hint"`
}

// SquadCharterResponse is the charter as the API serves it. A squad with no
// charter yet gets the same shape at revision 0, so clients need no special
// case for "not written yet".
type SquadCharterResponse struct {
	SquadID                string                       `json:"squad_id"`
	Mission                string                       `json:"mission"`
	Responsibilities       []SquadCharterResponsibility `json:"responsibilities"`
	Outputs                []SquadCharterOutput         `json:"outputs"`
	Boundaries             string                       `json:"boundaries"`
	Escalation             string                       `json:"escalation"`
	Intake                 string                       `json:"intake"`
	ResponseHours          int32                        `json:"response_hours"`
	OperatingReviewEnabled bool                         `json:"operating_review_enabled"`
	Revision               int64                        `json:"revision"`
	UpdatedByType          string                       `json:"updated_by_type"`
	UpdatedByID            *string                      `json:"updated_by_id"`
	UpdatedAt              *string                      `json:"updated_at"`
}

// UpdateSquadCharterRequest replaces the whole charter. Revision is the revision
// the client read (0 when none exists yet). operating_review_enabled is the one
// optional field: omitted, it keeps the stored value.
type UpdateSquadCharterRequest struct {
	Revision               *int64                       `json:"revision"`
	Mission                string                       `json:"mission"`
	Responsibilities       []SquadCharterResponsibility `json:"responsibilities"`
	Outputs                []SquadCharterOutput         `json:"outputs"`
	Boundaries             string                       `json:"boundaries"`
	Escalation             string                       `json:"escalation"`
	Intake                 string                       `json:"intake"`
	ResponseHours          *int32                       `json:"response_hours"`
	OperatingReviewEnabled *bool                        `json:"operating_review_enabled"`
}

func emptySquadCharterResponse(squadID string) SquadCharterResponse {
	return SquadCharterResponse{
		SquadID:                squadID,
		Responsibilities:       []SquadCharterResponsibility{},
		Outputs:                []SquadCharterOutput{},
		ResponseHours:          squadCharterDefaultHours,
		OperatingReviewEnabled: true,
	}
}

func squadCharterToResponse(c db.SquadCharter) (SquadCharterResponse, error) {
	resp := SquadCharterResponse{
		SquadID:                uuidToString(c.SquadID),
		Mission:                c.Mission,
		Responsibilities:       []SquadCharterResponsibility{},
		Outputs:                []SquadCharterOutput{},
		Boundaries:             c.Boundaries,
		Escalation:             c.Escalation,
		Intake:                 c.Intake,
		ResponseHours:          c.ResponseHours,
		OperatingReviewEnabled: c.OperatingReviewEnabled,
		Revision:               c.Revision,
		UpdatedByType:          c.UpdatedByType,
		UpdatedByID:            uuidToPtr(c.UpdatedByID),
		UpdatedAt:              timestampToPtr(c.UpdatedAt),
	}
	if err := json.Unmarshal(c.Responsibilities, &resp.Responsibilities); err != nil {
		return resp, err
	}
	if err := json.Unmarshal(c.Outputs, &resp.Outputs); err != nil {
		return resp, err
	}
	return resp, nil
}

// validateSquadCharterRequest returns the first problem with the request, or
// "" when it can be saved.
func validateSquadCharterRequest(req UpdateSquadCharterRequest) string {
	if req.Revision == nil {
		return "revision is required: send the revision you read, or 0 for a squad with no charter"
	}
	if *req.Revision < 0 {
		return "revision must not be negative"
	}
	if len([]rune(req.Mission)) > squadCharterTextMax {
		return "mission must be 2000 characters or fewer"
	}
	for name, text := range map[string]string{
		"boundaries": req.Boundaries,
		"escalation": req.Escalation,
		"intake":     req.Intake,
	} {
		if len([]rune(text)) > squadCharterTextMax {
			return name + " must be 2000 characters or fewer"
		}
	}
	if req.ResponseHours != nil && (*req.ResponseHours < 1 || *req.ResponseHours > 720) {
		return "response_hours must be between 1 and 720"
	}
	if len(req.Responsibilities) > squadCharterListMax {
		return "responsibilities allows at most 20 entries"
	}
	seen := make(map[string]bool, len(req.Responsibilities))
	for _, item := range req.Responsibilities {
		if msg := validateSquadCharterKey("responsibility", item.Key, seen); msg != "" {
			return msg
		}
		if n := len([]rune(item.Text)); n == 0 || n > squadCharterItemTextMax {
			return "each responsibility needs text of 1 to 500 characters"
		}
	}
	if len(req.Outputs) > squadCharterListMax {
		return "outputs allows at most 20 entries"
	}
	seen = make(map[string]bool, len(req.Outputs))
	for _, item := range req.Outputs {
		if msg := validateSquadCharterKey("output", item.Key, seen); msg != "" {
			return msg
		}
		if !squadCharterOutputKinds[item.Kind] {
			return "output kind must be issue, proposal, kb_document or report"
		}
		if len([]rune(item.KBKey)) > squadCharterKBKeyMax {
			return "output kb_key must be 128 characters or fewer"
		}
		if len([]rune(item.CadenceHint)) > squadCharterCadenceMax {
			return "output cadence_hint must be 64 characters or fewer"
		}
	}
	return ""
}

// validateSquadCharterKey checks a list item's key: present, bounded, and unique
// within its list, since the UI addresses items by key.
func validateSquadCharterKey(kind, key string, seen map[string]bool) string {
	if n := len([]rune(key)); n == 0 || n > squadCharterKeyMax {
		return kind + " key must be 1 to 64 characters"
	}
	if seen[key] {
		return kind + " keys must be unique within the charter"
	}
	seen[key] = true
	return ""
}

// writeSquadCharterRevisionConflict answers a save whose revision is stale. It
// carries the current revision so the client can reload and retry, and 0 when
// the squad has no charter yet.
func (h *Handler) writeSquadCharterRevisionConflict(w http.ResponseWriter, r *http.Request, squad db.Squad) {
	revision := int64(0)
	current, err := h.Queries.GetSquadCharter(r.Context(), db.GetSquadCharterParams{
		SquadID:     squad.ID,
		WorkspaceID: squad.WorkspaceID,
	})
	switch {
	case err == nil:
		revision = current.Revision
	case !errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusInternalServerError, "failed to read squad charter")
		return
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":    "the charter changed since you read it; reload it and apply your edits again",
		"code":     "charter_revision_conflict",
		"revision": revision,
	})
}

// GetSquadCharter returns the squad's charter, or the empty charter at revision 0.
// Any workspace member may read it.
func (h *Handler) GetSquadCharter(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}

	charter, err := h.Queries.GetSquadCharter(r.Context(), db.GetSquadCharterParams{
		SquadID:     squad.ID,
		WorkspaceID: squad.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, emptySquadCharterResponse(uuidToString(squad.ID)))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load squad charter")
		return
	}
	resp, err := squadCharterToResponse(charter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load squad charter")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateSquadCharter saves the charter. Only workspace owners and admins and the
// squad's creator may edit it. A save at the revision the client read succeeds
// and increments the revision; any other revision returns 409 with the current one.
func (h *Handler) UpdateSquadCharter(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageSquad(member, squad) {
		writeError(w, http.StatusForbidden, "only the squad's creator or a workspace admin can edit its charter")
		return
	}

	var req UpdateSquadCharterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := validateSquadCharterRequest(req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	responsibilities, err := json.Marshal(nonNilSlice(req.Responsibilities))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save squad charter")
		return
	}
	outputs, err := json.Marshal(nonNilSlice(req.Outputs))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save squad charter")
		return
	}
	responseHours := int32(squadCharterDefaultHours)
	if req.ResponseHours != nil {
		responseHours = *req.ResponseHours
	}

	var charter db.SquadCharter
	if *req.Revision == 0 {
		// The first save. Operating review is on unless the request turns it off.
		operatingReview := true
		if req.OperatingReviewEnabled != nil {
			operatingReview = *req.OperatingReviewEnabled
		}
		charter, err = h.Queries.InsertSquadCharter(r.Context(), db.InsertSquadCharterParams{
			SquadID:                squad.ID,
			WorkspaceID:            squad.WorkspaceID,
			Mission:                req.Mission,
			Responsibilities:       responsibilities,
			Outputs:                outputs,
			Boundaries:             req.Boundaries,
			Escalation:             req.Escalation,
			Intake:                 req.Intake,
			ResponseHours:          responseHours,
			OperatingReviewEnabled: operatingReview,
			UpdatedByType:          "member",
			UpdatedByID:            member.UserID,
		})
	} else {
		charter, err = h.Queries.UpdateSquadCharter(r.Context(), db.UpdateSquadCharterParams{
			SquadID:                squad.ID,
			WorkspaceID:            squad.WorkspaceID,
			Mission:                req.Mission,
			Responsibilities:       responsibilities,
			Outputs:                outputs,
			Boundaries:             req.Boundaries,
			Escalation:             req.Escalation,
			Intake:                 req.Intake,
			ResponseHours:          responseHours,
			OperatingReviewEnabled: ptrToBool(req.OperatingReviewEnabled),
			UpdatedByType:          "member",
			UpdatedByID:            member.UserID,
			Revision:               *req.Revision,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		h.writeSquadCharterRevisionConflict(w, r, squad)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save squad charter")
		return
	}

	resp, err := squadCharterToResponse(charter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save squad charter")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// nonNilSlice keeps an omitted list from marshalling to JSON null, which the
// JSONB array CHECK rejects.
func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func ptrToBool(b *bool) pgtype.Bool {
	if b == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *b, Valid: true}
}
