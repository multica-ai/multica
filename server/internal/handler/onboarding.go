package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Upper bound on free-text fields. `cloudWaitlistReasonMaxLen` is a
// product cap ("we don't need an essay for a waitlist"); the body-size
// cap further down is defense in depth against arbitrary storage
// abuse via the JSON body.
const (
	cloudWaitlistReasonMaxLen = 500

	// PatchOnboarding body is a tiny JSON with at most a 3-question
	// questionnaire. 16 KiB is ~10x the realistic ceiling — it's the
	// minimum that keeps the door open for future fields without
	// letting a malicious user stuff the JSONB column.
	patchOnboardingBodyLimit = 16 * 1024
)

// completeOnboardingRequest carries the client's view of which exit the
// user took from the flow. Used purely as an analytics dimension — server
// state (onboarded_at) flips the same way regardless. Unknown / missing
// → OnboardingPathUnknown so legacy clients still complete cleanly, just
// without a funnel-ready label.
//
// `workspace_id` is retained for analytics enrichment; the v2 code path
// used it to seed an install-runtime issue inside the same transaction,
// but in v3 every workspace-content seeding lives in the frontend
// welcome hook (see packages/views/workspace/welcome-after-onboarding.tsx).
type completeOnboardingRequest struct {
	CompletionPath string `json:"completion_path,omitempty"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
}

var validCompletionPaths = map[string]struct{}{
	analytics.OnboardingPathFull:           {},
	analytics.OnboardingPathRuntimeSkipped: {},
	analytics.OnboardingPathCloudWaitlist:  {},
	analytics.OnboardingPathSkipExisting:   {},
	analytics.OnboardingPathInviteAccept:   {},
}

// CompleteOnboarding marks the authenticated user as having completed
// onboarding. Idempotent: the underlying query uses COALESCE so the
// original timestamp is preserved if called more than once.
//
// Emits `onboarding_completed` exactly once — the first call that
// actually flips `onboarded_at` from NULL. Subsequent calls are still
// 200 OK (for client-side retries) but skip the event so the funnel
// counts honest first-completion.
//
// V3 has no in-handler seeding side effect: workspace content (Helper
// agent, starter issues, install-runtime guides) is created by the
// frontend welcome hook via the generic CreateAgent / CreateIssue
// endpoints. This handler does one thing: flip the field.
func (h *Handler) CompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Body is optional — an empty body is a legal legacy call.
	var req completeOnboardingRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	// Validate workspace_id if supplied; we don't write with it, but a
	// malformed value should fail fast rather than silently land in
	// PostHog as a junk dimension.
	if req.WorkspaceID != "" {
		wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
		if !ok {
			return
		}
		req.WorkspaceID = uuidToString(wsUUID)
	}

	before, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}
	firstCompletion := !before.OnboardedAt.Valid

	user, err := h.Queries.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		slog.Warn("complete onboarding: mark user onboarded failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}

	if firstCompletion {
		path := req.CompletionPath
		if _, ok := validCompletionPaths[path]; !ok {
			path = analytics.OnboardingPathUnknown
		}
		onboardedAt := ""
		if user.OnboardedAt.Valid {
			onboardedAt = user.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		h.Analytics.Capture(analytics.OnboardingCompleted(
			userID,
			req.WorkspaceID,
			path,
			onboardedAt,
			user.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}

type patchOnboardingRequest struct {
	Questionnaire *json.RawMessage `json:"questionnaire,omitempty"`
}

// questionnaireAnswers mirrors the frontend's `QuestionnaireAnswers`
// shape. `use_case` is multi-select (Step 3 allows picking several);
// `source` is single-select (primary acquisition channel) but kept
// as `stringOrSlice` for back-compat with v2 multi-select rows — the
// client now always commits a one-element array. `role` stays
// single-select.
//
// stringOrSlice also tolerates pre-array rows that wrote a bare
// string into the JSONB column — `json.Unmarshal` would otherwise
// fail on type mismatch when reading those back.
type stringOrSlice []string

func (s *stringOrSlice) UnmarshalJSON(data []byte) error {
	// Empty / null both decode to nil slice.
	if len(data) == 0 || string(data) == "null" {
		*s = nil
		return nil
	}
	// Try array first (current shape).
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*s = arr
		return nil
	}
	// Fall back to single string (pre-array shape from before this
	// column held a slice). Empty string means "unanswered" — keep nil.
	var single string
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	if single == "" {
		*s = nil
		return nil
	}
	*s = []string{single}
	return nil
}

type questionnaireAnswers struct {
	Source         stringOrSlice `json:"source"`
	SourceOther    string        `json:"source_other"`
	SourceSkipped  bool          `json:"source_skipped"`
	Role           string        `json:"role"`
	RoleOther      string        `json:"role_other"`
	RoleSkipped    bool          `json:"role_skipped"`
	UseCase        stringOrSlice `json:"use_case"`
	UseCaseOther   string        `json:"use_case_other"`
	UseCaseSkipped bool          `json:"use_case_skipped"`
	Version        int           `json:"version"`
}

func (q questionnaireAnswers) sourceResolved() bool {
	return len(q.Source) > 0 || q.SourceSkipped
}
func (q questionnaireAnswers) roleResolved() bool {
	return q.Role != "" || q.RoleSkipped
}
func (q questionnaireAnswers) useCaseResolved() bool {
	return len(q.UseCase) > 0 || q.UseCaseSkipped
}

// questionnaireSchemaVersion is the schema this handler understands.
// `complete()` and the funnel event are scoped to this version so a
// future v3 row can't be silently mis-counted against v2 semantics.
const questionnaireSchemaVersion = 2

func (q questionnaireAnswers) complete() bool {
	if q.Version != questionnaireSchemaVersion {
		return false
	}
	return q.sourceResolved() && q.roleResolved() && q.useCaseResolved()
}

// PatchOnboarding persists the user's questionnaire answers. The
// field is optional; an omitted questionnaire is preserved. Which
// step the user is on is deliberately not persisted — every
// onboarding entry starts at Welcome.
//
// Emits `onboarding_questionnaire_submitted` exactly once per user:
// the first PATCH that transitions the answers from "at least one
// slot empty" to "all three filled". Revisions past that point don't
// re-emit — the funnel counts users, not edits.
func (h *Handler) PatchOnboarding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	// Bound the body so the JSONB column can't be weaponized as bulk
	// storage — otherwise every subsequent `/api/me` read would have
	// to return the bloat.
	r.Body = http.MaxBytesReader(w, r.Body, patchOnboardingBodyLimit)
	var req patchOnboardingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Read prior answers so we can detect the NULL/partial → complete
	// transition after the update. An errored decode on the prior row
	// is treated as "incomplete" — worst case we emit once more than
	// we should, never twice for the same transition.
	var before questionnaireAnswers
	if beforeUser, err := h.Queries.GetUser(r.Context(), parseUUID(userID)); err == nil {
		_ = json.Unmarshal(beforeUser.OnboardingQuestionnaire, &before)
	}

	params := db.PatchUserOnboardingParams{ID: parseUUID(userID)}
	if req.Questionnaire != nil {
		params.Questionnaire = []byte(*req.Questionnaire)
	}
	user, err := h.Queries.PatchUserOnboarding(r.Context(), params)
	if err != nil {
		slog.Warn("patch onboarding failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update onboarding")
		return
	}

	var after questionnaireAnswers
	_ = json.Unmarshal(user.OnboardingQuestionnaire, &after)
	if after.complete() && !before.complete() {
		h.Analytics.Capture(analytics.OnboardingQuestionnaireSubmitted(
			userID,
			[]string(after.Source),
			after.Role,
			[]string(after.UseCase),
			after.SourceSkipped,
			after.RoleSkipped,
			after.UseCaseSkipped,
			after.SourceOther != "",
			after.RoleOther != "",
			after.UseCaseOther != "",
		))
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}

type joinCloudWaitlistRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// JoinCloudWaitlist records a user's interest in cloud runtimes.
// Pure side effect — does NOT complete onboarding. The user still
// has to pick a real Step 3 path (CLI with a detected runtime) or
// Skip to move on. Repeating the call overwrites email + reason.
func (h *Handler) JoinCloudWaitlist(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req joinCloudWaitlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// RFC 5321 caps email at 254 chars; the column is VARCHAR(254) and
	// the format check below rejects anything net/mail can't parse.
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if len(email) > 254 {
		writeError(w, http.StatusBadRequest, "email is too long")
		return
	}
	if _, err := mail.ParseAddress(email); err != nil {
		writeError(w, http.StatusBadRequest, "email is invalid")
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if len(reason) > cloudWaitlistReasonMaxLen {
		writeError(w, http.StatusBadRequest, "reason is too long")
		return
	}

	reasonParam := pgtype.Text{}
	if reason != "" {
		reasonParam = pgtype.Text{String: reason, Valid: true}
	}

	user, err := h.Queries.JoinCloudWaitlist(r.Context(), db.JoinCloudWaitlistParams{
		ID:                  parseUUID(userID),
		CloudWaitlistEmail:  pgtype.Text{String: email, Valid: true},
		CloudWaitlistReason: reasonParam,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join waitlist")
		return
	}

	h.Analytics.Capture(analytics.CloudWaitlistJoined(userID, reason != ""))

	writeJSON(w, http.StatusOK, userToResponse(user))
}
