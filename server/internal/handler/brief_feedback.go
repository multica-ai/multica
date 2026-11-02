package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Feedback on briefs (spec 2.8). A member rates a brief once; a second rating
// replaces the first. The Secretary and admins read the workspace's recent
// ratings, which is how the Secretary learns the boss's standing preferences.
const (
	briefFeedbackMaxNote        = 500
	briefFeedbackWindowDays     = 14
	briefFeedbackMaxRecentDays  = 35
	briefFeedbackRecentRowLimit = 200
)

// briefFeedbackTags are the reasons the dialog offers, in the order it shows them.
var briefFeedbackTags = []string{"too_long", "missing_something", "wrong", "too_vague", "not_for_me"}

// BriefFeedbackRequest is a member's rating. Rating is "useful" or "not_useful".
type BriefFeedbackRequest struct {
	Rating string   `json:"rating"`
	Tags   []string `json:"tags"`
	Note   string   `json:"note"`
}

// BriefFeedbackResponse is one member's rating of one brief. tags is never null.
type BriefFeedbackResponse struct {
	ID        string   `json:"id"`
	BriefID   string   `json:"brief_id"`
	Rating    string   `json:"rating"`
	Tags      []string `json:"tags"`
	Note      string   `json:"note"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// BriefFeedbackEntry is a rating in the workspace's recent feed. brief_kind and
// brief_created_at date the brief the rating is about.
type BriefFeedbackEntry struct {
	ID             string   `json:"id"`
	BriefID        string   `json:"brief_id"`
	MemberID       string   `json:"member_id"`
	Rating         string   `json:"rating"`
	Tags           []string `json:"tags"`
	Note           string   `json:"note"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	BriefKind      string   `json:"brief_kind"`
	BriefCreatedAt string   `json:"brief_created_at"`
}

// BriefFeedbackSummary is the rolling usefulness figure: the share of useful
// ratings given in the last days. usefulness is null when the window holds no
// ratings, so a quiet window does not read as 0%.
type BriefFeedbackSummary struct {
	Days       int      `json:"days"`
	Ratings    int64    `json:"ratings"`
	Useful     int64    `json:"useful"`
	Usefulness *float64 `json:"usefulness"`
}

func briefFeedbackResponse(f db.BriefFeedback) BriefFeedbackResponse {
	return BriefFeedbackResponse{
		ID:        uuidToString(f.ID),
		BriefID:   uuidToString(f.BriefID),
		Rating:    f.Rating,
		Tags:      nonNilStrings(f.Tags),
		Note:      f.Note,
		CreatedAt: timestampToString(f.CreatedAt),
		UpdatedAt: timestampToString(f.UpdatedAt),
	}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// checkBriefFeedback validates a rating and returns the tags (deduplicated, in
// the order given) and the trimmed note. The message is written for the caller,
// or is empty when the rating is valid.
func checkBriefFeedback(req BriefFeedbackRequest) (tags []string, note, msg string) {
	if req.Rating != "useful" && req.Rating != "not_useful" {
		return nil, "", `rating must be "useful" or "not_useful"`
	}
	tags = []string{}
	for _, t := range req.Tags {
		if !slices.Contains(briefFeedbackTags, t) {
			return nil, "", fmt.Sprintf("unknown tag %q; the tags are %s", t, strings.Join(briefFeedbackTags, ", "))
		}
		if !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	note = strings.TrimSpace(sanitizeNullBytes(req.Note))
	if n := utf8.RuneCountInString(note); n > briefFeedbackMaxNote {
		return nil, "", fmt.Sprintf("note is %d characters; at most %d", n, briefFeedbackMaxNote)
	}
	return tags, note, ""
}

// briefFeedbackMember resolves the caller as the member whose rating is read or
// written, and the brief in this workspace it is about. A rating belongs to a
// member, so an agent is refused. It writes the refusal.
func (h *Handler) briefFeedbackMember(w http.ResponseWriter, r *http.Request) (wsUUID, userUUID, briefUUID pgtype.UUID, ok bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	if actorType, _ := h.resolveActor(r, userID, workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "brief feedback belongs to a member; an agent cannot rate a brief")
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	if wsUUID, ok = parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	if userUUID, ok = parseUUIDOrBadRequest(w, userID, "user id"); !ok {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	if briefUUID, ok = parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "brief id"); !ok {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetBriefInWorkspace(r.Context(), db.GetBriefInWorkspaceParams{ID: briefUUID, WorkspaceID: wsUUID}); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "brief not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load brief")
		}
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	return wsUUID, userUUID, briefUUID, true
}

// SubmitBriefFeedback records the caller's rating of a brief. A second
// submission replaces the first, tags and note included.
//
//	POST /api/briefs/{id}/feedback {rating, tags?, note?}
func (h *Handler) SubmitBriefFeedback(w http.ResponseWriter, r *http.Request) {
	wsUUID, userUUID, briefUUID, ok := h.briefFeedbackMember(w, r)
	if !ok {
		return
	}
	var req BriefFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tags, note, msg := checkBriefFeedback(req)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	saved, err := h.Queries.UpsertBriefFeedback(r.Context(), db.UpsertBriefFeedbackParams{
		ID:          dbid.NewV7(),
		WorkspaceID: wsUUID,
		BriefID:     briefUUID,
		MemberID:    userUUID,
		Rating:      req.Rating,
		Tags:        tags,
		Note:        note,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save brief feedback")
		return
	}
	writeJSON(w, http.StatusOK, briefFeedbackResponse(saved))
}

// GetMyBriefFeedback returns the caller's rating of a brief, or null when they
// have not rated it.
//
//	GET /api/briefs/{id}/feedback
func (h *Handler) GetMyBriefFeedback(w http.ResponseWriter, r *http.Request) {
	wsUUID, userUUID, briefUUID, ok := h.briefFeedbackMember(w, r)
	if !ok {
		return
	}
	fb, err := h.Queries.GetBriefFeedbackForMember(r.Context(), db.GetBriefFeedbackForMemberParams{
		WorkspaceID: wsUUID, BriefID: briefUUID, MemberID: userUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load brief feedback")
		return
	}
	writeJSON(w, http.StatusOK, briefFeedbackResponse(fb))
}

// briefFeedbackReader resolves the caller for the workspace-wide feedback reads.
// Those are the Secretary's and an admin's, the same actors who publish briefs.
func (h *Handler) briefFeedbackReader(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.requireSecretaryOrAdmin(w, r, workspaceID, userID, actorType, actorID, "read brief feedback") {
		return pgtype.UUID{}, false
	}
	return parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
}

// ListBriefFeedbackRecent returns the workspace's ratings given in the last
// days (default 14, at most 35), newest first. The Secretary reads it at the
// start of each run; the CLI is `multica brief feedback --recent 14d --json`.
//
//	GET /api/briefs/feedback/recent?days=14
func (h *Handler) ListBriefFeedbackRecent(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.briefFeedbackReader(w, r)
	if !ok {
		return
	}
	days := briefFeedbackWindowDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > briefFeedbackMaxRecentDays {
			writeError(w, http.StatusBadRequest, "days must be between 1 and "+strconv.Itoa(briefFeedbackMaxRecentDays))
			return
		}
		days = n
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := h.Queries.ListRecentBriefFeedback(r.Context(), db.ListRecentBriefFeedbackParams{
		WorkspaceID: wsUUID,
		Since:       pgtype.Timestamptz{Time: since, Valid: true},
		RowLimit:    briefFeedbackRecentRowLimit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list brief feedback")
		return
	}
	resp := make([]BriefFeedbackEntry, len(rows))
	for i, row := range rows {
		resp[i] = BriefFeedbackEntry{
			ID:             uuidToString(row.ID),
			BriefID:        uuidToString(row.BriefID),
			MemberID:       uuidToString(row.MemberID),
			Rating:         row.Rating,
			Tags:           nonNilStrings(row.Tags),
			Note:           row.Note,
			CreatedAt:      timestampToString(row.CreatedAt),
			UpdatedAt:      timestampToString(row.UpdatedAt),
			BriefKind:      row.BriefKind,
			BriefCreatedAt: timestampToString(row.BriefCreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetBriefFeedbackSummary returns the usefulness figure over the last 14 days.
//
//	GET /api/briefs/feedback/summary
func (h *Handler) GetBriefFeedbackSummary(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.briefFeedbackReader(w, r)
	if !ok {
		return
	}
	since := time.Now().Add(-time.Duration(briefFeedbackWindowDays) * 24 * time.Hour)
	counts, err := h.Queries.CountBriefFeedbackSince(r.Context(), db.CountBriefFeedbackSinceParams{
		WorkspaceID: wsUUID,
		Since:       pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to summarise brief feedback")
		return
	}
	out := BriefFeedbackSummary{Days: briefFeedbackWindowDays, Ratings: counts.Ratings, Useful: counts.Useful}
	if counts.Ratings > 0 {
		u := float64(counts.Useful) / float64(counts.Ratings)
		out.Usefulness = &u
	}
	writeJSON(w, http.StatusOK, out)
}
