package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// --- Constants -------------------------------------------------------------

// AllowedLinkTypes must match the CHECK constraint in migration 1002.
//
// "blocks"      directional, prevents transitive cycles
// "depends_on"  directional, soft semantics (no enforcement)
// "duplicates"  directional, A is a duplicate of B
// "relates_to"  symmetric, free-form association
var AllowedLinkTypes = map[string]struct{}{
	"blocks":     {},
	"depends_on": {},
	"duplicates": {},
	"relates_to": {},
}

// inverseLinkType is the link_type to record on the mirror row. Symmetric
// types use the same type on both sides; directional types swap to a sibling
// only when we display them — storage uses the same link_type with a
// different direction. This keeps the data model symmetric: both rows of a
// pair share link_type, and 'direction' tells the reader which side they're on.
//
// (Earlier drafts used distinct types like 'blocked_by' on the mirror row;
//  that complicates listing queries because the same conceptual link surfaces
//  under two different type names. Storing one type + direction is simpler.)

const (
	maxLinksPerIssue = 50 // soft cap; prevents pathological link bombs
)

// --- Request / response types ----------------------------------------------

type CreateIssueLinkRequest struct {
	TargetIssueID string `json:"target_issue_id"`
	LinkType      string `json:"link_type"`
}

// IssueLinkResponse is one *direction* of a link, from the perspective of
// the issue it was loaded for. The "other side" is in the Target* fields.
type IssueLinkResponse struct {
	ID                   string  `json:"id"`
	PairID               string  `json:"pair_id"`
	LinkType             string  `json:"link_type"`
	Direction            string  `json:"direction"` // outgoing | incoming
	CreatorType          string  `json:"creator_type"`
	CreatorID            *string `json:"creator_id"`
	CreatedAt            string  `json:"created_at"`

	TargetIssueID        string  `json:"target_issue_id"`
	TargetIdentifier     string  `json:"target_identifier"`
	TargetTitle          string  `json:"target_title"`
	TargetStatus         string  `json:"target_status"`
	TargetNumber         int32   `json:"target_number"`
	TargetWorkspaceID    string  `json:"target_workspace_id"`
	TargetWorkspaceName  string  `json:"target_workspace_name"`
	TargetWorkspaceSlug  string  `json:"target_workspace_slug"`
}

type IssueBlockerResponse struct {
	BlockerIssueID       string `json:"blocker_issue_id"`
	BlockerIdentifier    string `json:"blocker_identifier"`
	BlockerTitle         string `json:"blocker_title"`
	BlockerStatus        string `json:"blocker_status"`
	BlockerNumber        int32  `json:"blocker_number"`
	BlockerWorkspaceID   string `json:"blocker_workspace_id"`
	BlockerWorkspaceName string `json:"blocker_workspace_name"`
	BlockerWorkspaceSlug string `json:"blocker_workspace_slug"`
}

func issueLinkRowToResponse(r db.ListLinksForIssueRow) IssueLinkResponse {
	return IssueLinkResponse{
		ID:                  uuidToString(r.ID),
		PairID:              uuidToString(r.PairID),
		LinkType:            r.LinkType,
		Direction:           r.Direction,
		CreatorType:         r.CreatorType,
		CreatorID:           uuidToPtr(r.CreatorID),
		CreatedAt:           timestampToString(r.CreatedAt),
		TargetIssueID:       uuidToString(r.TargetIssueID),
		TargetIdentifier:    identifierString(r.TargetIdentifier),
		TargetTitle:         r.TargetTitle,
		TargetStatus:        r.TargetStatus,
		TargetNumber:        r.TargetNumber,
		TargetWorkspaceID:   uuidToString(r.TargetWorkspaceID),
		TargetWorkspaceName: r.TargetWorkspaceName,
		TargetWorkspaceSlug: r.TargetWorkspaceSlug,
	}
}

// --- Handlers --------------------------------------------------------------

// CreateIssueLink POST /api/issues/{id}/links
//
// Body: { target_issue_id, link_type }
//
// The caller is authenticated against the SOURCE issue's workspace
// (via the `?workspace_id=` query param consumed by loadIssueForUser).
// Target may live in another workspace — we accept that explicitly because
// the user opted into cross-workspace linking.
func (h *Handler) CreateIssueLink(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	source, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	sourceWorkspaceID := uuidToString(source.WorkspaceID)

	var req CreateIssueLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TargetIssueID == "" {
		writeError(w, http.StatusBadRequest, "target_issue_id is required")
		return
	}
	if _, ok := AllowedLinkTypes[req.LinkType]; !ok {
		writeError(w, http.StatusBadRequest, "invalid link_type")
		return
	}

	// Lookup the target issue without enforcing same-workspace. We don't
	// require the caller to be a member of the target workspace — they're
	// only declaring a relationship, not modifying the target. Read-time
	// permissions still apply when the link surfaces in the target's UI.
	target, err := h.Queries.GetIssue(r.Context(), parseUUID(req.TargetIssueID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "target issue not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lookup target")
		return
	}

	// Resolve identifiers for activity payloads. We need each issue's
	// workspace's issue_prefix; one query each, cached in locals.
	sourceIdentifier := h.identifierFor(r.Context(), source)
	targetIdentifier := h.identifierFor(r.Context(), target)

	if uuidToString(source.ID) == uuidToString(target.ID) {
		writeError(w, http.StatusBadRequest, "cannot link an issue to itself")
		return
	}

	// Cap to keep things sane.
	count, _ := h.Queries.CountLinksForIssue(r.Context(), source.ID)
	if count >= int64(maxLinksPerIssue) {
		writeError(w, http.StatusConflict, "issue has reached the link limit")
		return
	}

	// Surface duplicate as 409 Conflict instead of an opaque UNIQUE
	// violation later.
	if existing, err := h.Queries.GetIssueLinkByTuple(r.Context(), db.GetIssueLinkByTupleParams{
		SourceIssueID: source.ID,
		TargetIssueID: target.ID,
		LinkType:      req.LinkType,
	}); err == nil && uuidToString(existing.ID) != "" {
		writeError(w, http.StatusConflict, "this link already exists")
		return
	}

	// Cycle prevention: only blocks links can form cycles (depends_on is
	// soft, duplicates is terminal, relates_to is symmetric).
	if req.LinkType == "blocks" {
		hit, _ := h.Queries.BlocksReachable(r.Context(), db.BlocksReachableParams{
			Column1: target.ID, // walk start
			Column2: source.ID, // search target
		})
		if hit == 1 {
			writeError(w, http.StatusConflict, "this link would create a blocks-cycle")
			return
		}
	}

	actorType, actorID := h.resolveActor(r, userID, sourceWorkspaceID)

	// Generate one shared pair_id and insert both mirror rows in a tx.
	pairID := pgtype.UUID{Bytes: newUUIDBytes(), Valid: true}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create link")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	insert := func(srcIssue db.Issue, srcWS pgtype.UUID, tgtIssue db.Issue, tgtWS pgtype.UUID, dir string) error {
		return qtx.InsertIssueLinkRow(r.Context(), db.InsertIssueLinkRowParams{
			PairID:             pairID,
			SourceIssueID:      srcIssue.ID,
			SourceWorkspaceID:  srcWS,
			TargetIssueID:      tgtIssue.ID,
			TargetWorkspaceID:  tgtWS,
			LinkType:           req.LinkType,
			Direction:          dir,
			CreatorType:        actorType,
			CreatorID:          parseUUIDNullable(actorID),
		})
	}

	if err := insert(source, source.WorkspaceID, target, target.WorkspaceID, "outgoing"); err != nil {
		slog.Warn("insert link outgoing failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create link")
		return
	}
	if err := insert(target, target.WorkspaceID, source, source.WorkspaceID, "incoming"); err != nil {
		slog.Warn("insert link incoming failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create link")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("commit link tx failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create link")
		return
	}

	// Activity rows: one on each issue. The source row records the outgoing
	// action ("linked to X"), the target row records the inverse perspective
	// ("Y linked to me"). Frontends pick the appropriate verb.
	srcDetails := map[string]any{
		"target_issue_id":   uuidToString(target.ID),
		"target_identifier": targetIdentifier,
		"link_type":         req.LinkType,
		"direction":         "outgoing",
	}
	tgtDetails := map[string]any{
		"target_issue_id":   uuidToString(source.ID),
		"target_identifier": sourceIdentifier,
		"link_type":         req.LinkType,
		"direction":         "incoming",
	}
	h.insertLabelActivity(r, source.WorkspaceID, source.ID, "link_added", actorType, actorID, srcDetails)
	h.insertLabelActivity(r, target.WorkspaceID, target.ID, "link_added", actorType, actorID, tgtDetails)

	// Refresh and emit. We re-list both sides so each WS subscriber sees
	// fresh state, mirroring the labels handler pattern.
	srcLinks := h.fetchLinksForIssue(r, source.ID)
	tgtLinks := h.fetchLinksForIssue(r, target.ID)
	h.publish(protocol.EventIssueLinksChanged, sourceWorkspaceID, actorType, actorID, map[string]any{
		"issue_id": uuidToString(source.ID),
		"links":    srcLinks,
	})
	h.publish(protocol.EventIssueLinksChanged, uuidToString(target.WorkspaceID), actorType, actorID, map[string]any{
		"issue_id": uuidToString(target.ID),
		"links":    tgtLinks,
	})

	// Respond with the outgoing row (the one the caller created) — it's the
	// canonical "what just got created" from their perspective.
	for _, link := range srcLinks {
		if link.PairID == uuidToString(pairID) && link.Direction == "outgoing" {
			writeJSON(w, http.StatusCreated, link)
			return
		}
	}
	// Fallback (shouldn't happen): return 201 with empty body.
	w.WriteHeader(http.StatusCreated)
}

// DeleteIssueLink DELETE /api/issues/{id}/links/{linkId}
//
// linkId is the row id of EITHER side of the pair. We resolve to the pair_id
// and delete both rows in one statement.
func (h *Handler) DeleteIssueLink(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	linkID := chi.URLParam(r, "linkId")
	source, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	sourceWorkspaceID := uuidToString(source.WorkspaceID)

	link, err := h.Queries.GetIssueLinkByID(r.Context(), parseUUID(linkID))
	if err != nil {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}

	// The link row's source must match the URL issue. (Prevents callers from
	// passing some unrelated link id under their own issue's URL.)
	if uuidToString(link.SourceIssueID) != uuidToString(source.ID) {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, sourceWorkspaceID)

	if err := h.Queries.DeleteIssueLinkByPair(r.Context(), link.PairID); err != nil {
		slog.Warn("delete link failed", append(logger.RequestAttrs(r), "error", err, "pair_id", uuidToString(link.PairID))...)
		writeError(w, http.StatusInternalServerError, "failed to delete link")
		return
	}

	// Activity on both sides. We resolve identifiers ("TIM-7") so the
	// frontend can render meaningful copy without a follow-up lookup —
	// matching the create-side payload shape.
	var srcIdent, tgtIdent string
	if srcIssue, err := h.Queries.GetIssue(r.Context(), link.SourceIssueID); err == nil {
		srcIdent = h.identifierFor(r.Context(), srcIssue)
	}
	if tgtIssue, err := h.Queries.GetIssue(r.Context(), link.TargetIssueID); err == nil {
		tgtIdent = h.identifierFor(r.Context(), tgtIssue)
	}
	srcDetails := map[string]any{
		"target_issue_id":   uuidToString(link.TargetIssueID),
		"target_identifier": tgtIdent,
		"link_type":         link.LinkType,
		"direction":         link.Direction,
	}
	h.insertLabelActivity(r, link.SourceWorkspaceID, link.SourceIssueID, "link_removed", actorType, actorID, srcDetails)
	tgtDetails := map[string]any{
		"target_issue_id":   uuidToString(link.SourceIssueID),
		"target_identifier": srcIdent,
		"link_type":         link.LinkType,
		"direction":         reverseDirection(link.Direction),
	}
	h.insertLabelActivity(r, link.TargetWorkspaceID, link.TargetIssueID, "link_removed", actorType, actorID, tgtDetails)

	// Re-emit fresh link lists for both sides.
	srcLinks := h.fetchLinksForIssue(r, link.SourceIssueID)
	tgtLinks := h.fetchLinksForIssue(r, link.TargetIssueID)
	h.publish(protocol.EventIssueLinksChanged, uuidToString(link.SourceWorkspaceID), actorType, actorID, map[string]any{
		"issue_id": uuidToString(link.SourceIssueID),
		"links":    srcLinks,
	})
	h.publish(protocol.EventIssueLinksChanged, uuidToString(link.TargetWorkspaceID), actorType, actorID, map[string]any{
		"issue_id": uuidToString(link.TargetIssueID),
		"links":    tgtLinks,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ListIssueLinks GET /api/issues/{id}/links
func (h *Handler) ListIssueLinks(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	links := h.fetchLinksForIssue(r, issue.ID)
	writeJSON(w, http.StatusOK, links)
}

// ListIssueBlockers GET /api/issues/{id}/blockers
//
// Returns open blockers (issues that are blocking this one, where the blocker
// itself is not yet closed). Used by the soft-warning UI on status change.
func (h *Handler) ListIssueBlockers(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	rows, err := h.Queries.ListBlockersForIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list blockers")
		return
	}
	resp := make([]IssueBlockerResponse, 0, len(rows))
	for _, b := range rows {
		resp = append(resp, IssueBlockerResponse{
			BlockerIssueID:       uuidToString(b.BlockerIssueID),
			BlockerIdentifier:    identifierString(b.BlockerIdentifier),
			BlockerTitle:         b.BlockerTitle,
			BlockerStatus:        b.BlockerStatus,
			BlockerNumber:        b.BlockerNumber,
			BlockerWorkspaceID:   uuidToString(b.BlockerWorkspaceID),
			BlockerWorkspaceName: b.BlockerWorkspaceName,
			BlockerWorkspaceSlug: b.BlockerWorkspaceSlug,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Helpers ---------------------------------------------------------------

func (h *Handler) fetchLinksForIssue(r *http.Request, issueID pgtype.UUID) []IssueLinkResponse {
	rows, err := h.Queries.ListLinksForIssue(r.Context(), issueID)
	if err != nil {
		slog.Warn("fetch links failed", append(logger.RequestAttrs(r), "error", err)...)
		return []IssueLinkResponse{}
	}
	out := make([]IssueLinkResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, issueLinkRowToResponse(row))
	}
	return out
}

func reverseDirection(d string) string {
	if d == "outgoing" {
		return "incoming"
	}
	return "outgoing"
}

// newUUIDBytes returns a fresh v7 UUID's raw 16 bytes. v7 sorts lexically by
// time, which is friendly for log scans on link rows.
func newUUIDBytes() [16]byte {
	id, err := uuid.NewV7()
	if err != nil {
		// Fall back to v4 if the system clock is broken (NewV7 only fails
		// when entropy is unavailable, which is exceedingly rare).
		id = uuid.New()
	}
	return [16]byte(id)
}

// identifierFor constructs the human-readable identifier for an issue (e.g.
// "TIM-3"). We need this to populate activity-log payloads so frontends can
// render meaningful text without a follow-up lookup. One workspace fetch per
// issue, cached at request-scope by sqlc's prepared statement reuse.
func (h *Handler) identifierFor(ctx context.Context, issue db.Issue) string {
	ws, err := h.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return strconv.Itoa(int(issue.Number))
	}
	return ws.IssuePrefix + "-" + strconv.Itoa(int(issue.Number))
}

// Bulk enrichment for DTOs is implemented in L-PR#2, when IssueResponse
// gains its Links field. Keeping that helper out of this commit avoids a
// half-wired field that the wider codebase doesn't yet know about.

// identifierString coerces sqlc's interface{} return (from the SQL string
// concatenation `prefix || '-' || number::text`) into a string. Postgres
// hands back a string for that expression, but sqlc cannot type-infer it.
func identifierString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return ""
}


// --- Enrich helpers for embedding links on issues -------------------------
// Mirrors the pattern in label.go (enrichIssuesWithLabels / enrichIssueWithLabels).

// enrichIssuesWithLinks bulk-fetches links for every issue in resp and
// populates resp[i].Links. Issues with no links keep the empty slice the
// converter initialised. Cross-workspace targets are returned with their
// foreign workspace metadata so the frontend can render an inline workspace
// chip without additional round-trips.
//
// Errors are logged but not returned: a missing Links field should never
// fail an issue list. The frontend treats an absent links array as "unknown"
// and shows nothing.
func (h *Handler) enrichIssuesWithLinks(ctx context.Context, resp []IssueResponse) {
	if len(resp) == 0 {
		return
	}
	ids := make([]pgtype.UUID, 0, len(resp))
	idxByID := make(map[string]int, len(resp))
	for i, r := range resp {
		ids = append(ids, parseUUID(r.ID))
		idxByID[r.ID] = i
	}

	rows, err := h.Queries.ListLinksForIssues(ctx, ids)
	if err != nil {
		slog.Debug("bulk fetch links failed", "error", err, "count", len(ids))
		return
	}

	for _, row := range rows {
		idx, ok := idxByID[uuidToString(row.SourceIssueID)]
		if !ok {
			continue
		}
		resp[idx].Links = append(resp[idx].Links, IssueLinkResponse{
			ID:                  uuidToString(row.ID),
			PairID:              uuidToString(row.PairID),
			LinkType:            row.LinkType,
			Direction:           row.Direction,
			CreatorType:         row.CreatorType,
			CreatorID:           uuidToPtr(row.CreatorID),
			CreatedAt:           timestampToString(row.CreatedAt),
			TargetIssueID:       uuidToString(row.TargetIssueID),
			TargetIdentifier:    identifierString(row.TargetIdentifier),
			TargetTitle:         row.TargetTitle,
			TargetStatus:        row.TargetStatus,
			TargetNumber:        row.TargetNumber,
			TargetWorkspaceID:   uuidToString(row.TargetWorkspaceID),
			TargetWorkspaceName: row.TargetWorkspaceName,
			TargetWorkspaceSlug: row.TargetWorkspaceSlug,
		})
	}
}

// enrichIssueWithLinks is the single-issue variant for GetIssue / CreateIssue
// / UpdateIssue paths. Returns shape identical to the per-issue /links
// endpoint (same converter), so the frontend can rely on a single shape.
func (h *Handler) enrichIssueWithLinks(ctx context.Context, resp *IssueResponse) {
	if resp == nil || resp.ID == "" {
		return
	}
	rows, err := h.Queries.ListLinksForIssue(ctx, parseUUID(resp.ID))
	if err != nil {
		slog.Debug("fetch links for single issue failed", "error", err, "issue_id", resp.ID)
		return
	}
	out := make([]IssueLinkResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, issueLinkRowToResponse(row))
	}
	resp.Links = out
}
