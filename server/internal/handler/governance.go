package handler

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The governance log (spec 4.5). Every member can read it; nothing in the API
// writes to it or changes an entry.

// governanceActorTypes are the actor types a filter may name.
var governanceActorTypes = []string{
	service.GovernanceActorMember, service.GovernanceActorAgent,
	service.GovernanceActorSystem, service.GovernanceActorPolicy,
}

// governanceSubjectTypeMax bounds the subject_type filter.
const governanceSubjectTypeMax = 64

// governanceExportPage is the page size the CSV export reads with.
const governanceExportPage int32 = 200

// governanceExportRowLimit caps the rows one CSV export writes (spec 4.9). The
// export is refused with 413 above it, before any byte is sent, and the cap is
// named in the X-Export-Row-Limit header of every export that is served. A
// variable so the test can lower it.
var governanceExportRowLimit int64 = 50000

// GovernanceEventResponse is one entry of the governance log.
type GovernanceEventResponse struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	ActorType   string          `json:"actor_type"`
	ActorID     *string         `json:"actor_id"`
	SubjectType string          `json:"subject_type"`
	SubjectID   *string         `json:"subject_id"`
	Summary     string          `json:"summary"`
	Details     json.RawMessage `json:"details"`
	CreatedAt   time.Time       `json:"created_at"`
}

// ListGovernanceEventsResponse is one page of the log. next_cursor is empty on
// the last page.
type ListGovernanceEventsResponse struct {
	Events     []GovernanceEventResponse `json:"events"`
	NextCursor string                    `json:"next_cursor"`
}

func governanceEventResponse(e db.GovernanceEvent) GovernanceEventResponse {
	details := json.RawMessage(e.Details)
	if len(details) == 0 {
		details = json.RawMessage("{}")
	}
	resp := GovernanceEventResponse{
		ID:          uuidToString(e.ID),
		Kind:        e.Kind,
		ActorType:   e.ActorType,
		SubjectType: e.SubjectType,
		Summary:     e.Summary,
		Details:     details,
		CreatedAt:   e.CreatedAt.Time,
	}
	if e.ActorID.Valid {
		id := uuidToString(e.ActorID)
		resp.ActorID = &id
	}
	if e.SubjectID.Valid {
		id := uuidToString(e.SubjectID)
		resp.SubjectID = &id
	}
	return resp
}

// governanceFilterFrom reads the filters and paging of a request. It writes the
// error and reports false when one is invalid.
func governanceFilterFrom(w http.ResponseWriter, r *http.Request, limitDefault int32) (service.GovernanceFilter, bool) {
	q := r.URL.Query()
	f := service.GovernanceFilter{Kind: q.Get("kind"), ActorType: q.Get("actor_type"), SubjectType: q.Get("subject_type"), Cursor: q.Get("cursor")}
	if f.Kind != "" && !slices.Contains(service.GovernanceKinds(), f.Kind) {
		writeError(w, http.StatusBadRequest, "kind is not a governance event kind")
		return f, false
	}
	if f.ActorType != "" && !slices.Contains(governanceActorTypes, f.ActorType) {
		writeError(w, http.StatusBadRequest, "actor_type must be member, agent, system or policy")
		return f, false
	}
	if len(f.SubjectType) > governanceSubjectTypeMax {
		writeError(w, http.StatusBadRequest, "subject_type is too long")
		return f, false
	}
	if raw := q.Get("subject_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "subject_id")
		if !ok {
			return f, false
		}
		f.SubjectID = id
	}
	f.Limit = limitDefault
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > int(service.GovernancePageMax) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be from 1 to %d", service.GovernancePageMax))
			return f, false
		}
		f.Limit = int32(n)
	}
	return f, true
}

// ListGovernanceEvents returns one page of the workspace's governance log,
// newest first.
func (h *Handler) ListGovernanceEvents(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	f, ok := governanceFilterFrom(w, r, service.GovernancePageDefault)
	if !ok {
		return
	}
	page, err := service.NewGovernanceLog().List(r.Context(), h.Queries, wsUUID, f)
	if errors.Is(err, service.ErrGovernanceCursorInvalid) {
		writeError(w, http.StatusBadRequest, "cursor is not valid")
		return
	}
	if err != nil {
		slog.Warn("list governance events failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list governance events")
		return
	}
	resp := ListGovernanceEventsResponse{Events: make([]GovernanceEventResponse, 0, len(page.Events)), NextCursor: page.NextCursor}
	for _, e := range page.Events {
		resp.Events = append(resp.Events, governanceEventResponse(e))
	}
	writeJSON(w, http.StatusOK, resp)
}

// ExportGovernanceEvents streams the log, newest first, as CSV. It takes the
// same filters as the list.
func (h *Handler) ExportGovernanceEvents(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	f, ok := governanceFilterFrom(w, r, governanceExportPage)
	if !ok {
		return
	}
	// The export has no cursor of its own; it starts at the first page and
	// follows next_cursor.
	f.Cursor = ""
	// The count is taken before the status is sent: once the CSV has started,
	// a refusal can no longer change the status code.
	total, err := service.NewGovernanceLog().Count(r.Context(), h.Queries, wsUUID, f)
	if err != nil {
		slog.Warn("count governance events for export failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to export the governance log")
		return
	}
	if total > governanceExportRowLimit {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf(
			"the governance log has %d entries matching this filter; the CSV export is limited to %d rows. Narrow it with the kind, actor or subject filters.",
			total, governanceExportRowLimit))
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="governance-log.csv"`)
	w.Header().Set("X-Export-Row-Limit", strconv.FormatInt(governanceExportRowLimit, 10))
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	defer cw.Flush()
	if err := cw.Write([]string{"id", "created_at", "kind", "actor_type", "actor_id", "subject_type", "subject_id", "summary", "details"}); err != nil {
		return
	}
	if err := writeGovernanceCSVPages(r.Context(), h, cw, wsUUID, f); err != nil {
		// The status is sent; the rows written so far are the export.
		slog.Warn("export governance events stopped", append(logger.RequestAttrs(r), "error", err)...)
	}
}

// writeGovernanceCSVPages writes every page of the filtered log as CSV rows.
func writeGovernanceCSVPages(ctx context.Context, h *Handler, cw *csv.Writer, ws pgtype.UUID, f service.GovernanceFilter) error {
	for {
		page, err := service.NewGovernanceLog().List(ctx, h.Queries, ws, f)
		if err != nil {
			return err
		}
		for _, e := range page.Events {
			resp := governanceEventResponse(e)
			actor, subject := "", ""
			if resp.ActorID != nil {
				actor = *resp.ActorID
			}
			if resp.SubjectID != nil {
				subject = *resp.SubjectID
			}
			if err := cw.Write([]string{
				resp.ID, resp.CreatedAt.UTC().Format(time.RFC3339Nano), resp.Kind, resp.ActorType, actor,
				resp.SubjectType, subject, resp.Summary, string(resp.Details),
			}); err != nil {
				return err
			}
		}
		cw.Flush()
		if page.NextCursor == "" {
			return cw.Error()
		}
		f.Cursor = page.NextCursor
	}
}
