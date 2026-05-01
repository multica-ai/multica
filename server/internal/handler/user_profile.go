package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// User communication profile (JEH-304). Single repository function per CTO
// review — all reads/writes for user_profile go through here, gated on
// the authenticated user's own user_id. There is no RLS; if anyone adds an
// ad-hoc query to user_profile elsewhere, the gate is bypassed.

const (
	maxAntiPatterns        = 20
	maxAntiPatternLength   = 100
	sliderMin              = 0
	sliderMax              = 100
)

var validProfilePersonas = map[string]bool{
	"utalmodig": true,
	"ekspert":   true,
	"grundig":   true,
	"larling":   true,
}

var validProfileLanguages = map[string]bool{
	"da": true,
	"en": true,
}

type UserProfileResponse struct {
	UserID       string   `json:"user_id"`
	Persona      string   `json:"persona"`
	Language     string   `json:"language"`
	LengthPref   int      `json:"length_pref"`
	AutonomyPref int      `json:"autonomy_pref"`
	TechPref     int      `json:"tech_pref"`
	AntiPatterns []string `json:"anti_patterns"`
	UpdatedAt    string   `json:"updated_at"`
}

type UpsertUserProfileRequest struct {
	Persona      string   `json:"persona"`
	Language     string   `json:"language"`
	LengthPref   int      `json:"length_pref"`
	AutonomyPref int      `json:"autonomy_pref"`
	TechPref     int      `json:"tech_pref"`
	AntiPatterns []string `json:"anti_patterns"`
}

func userProfileToResponse(p db.UserProfile) UserProfileResponse {
	var antiPatterns []string
	if len(p.AntiPatterns) > 0 {
		_ = json.Unmarshal(p.AntiPatterns, &antiPatterns)
	}
	if antiPatterns == nil {
		antiPatterns = []string{}
	}
	return UserProfileResponse{
		UserID:       uuidToString(p.UserID),
		Persona:      p.Persona,
		Language:     p.Language,
		LengthPref:   int(p.LengthPref),
		AutonomyPref: int(p.AutonomyPref),
		TechPref:     int(p.TechPref),
		AntiPatterns: antiPatterns,
		UpdatedAt:    timestampToString(p.UpdatedAt),
	}
}

// GetMyProfile returns the authenticated user's communication profile.
// Returns 404 if the user has not set one yet — the client falls back to
// the default profile in that case.
func (h *Handler) GetMyProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	profile, err := h.Queries.GetUserProfile(r.Context(), parseUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no profile set")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load profile")
		return
	}

	writeJSON(w, http.StatusOK, userProfileToResponse(profile))
}

// UpsertMyProfile creates or replaces the authenticated user's profile.
// All validation also lives in packages/core/profile/schema.ts so the UI
// can preflight; this is the authoritative gate.
func (h *Handler) UpsertMyProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req UpsertUserProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validProfilePersonas[req.Persona] {
		writeError(w, http.StatusBadRequest, "invalid persona")
		return
	}
	if !validProfileLanguages[req.Language] {
		writeError(w, http.StatusBadRequest, "invalid language")
		return
	}
	for _, s := range []struct {
		name  string
		value int
	}{
		{"length_pref", req.LengthPref},
		{"autonomy_pref", req.AutonomyPref},
		{"tech_pref", req.TechPref},
	} {
		if s.value < sliderMin || s.value > sliderMax {
			writeError(w, http.StatusBadRequest, s.name+" must be between 0 and 100")
			return
		}
	}

	if len(req.AntiPatterns) > maxAntiPatterns {
		writeError(w, http.StatusBadRequest, "too many anti-patterns")
		return
	}
	cleaned := make([]string, 0, len(req.AntiPatterns))
	seen := make(map[string]bool, len(req.AntiPatterns))
	for _, p := range req.AntiPatterns {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		if len(s) > maxAntiPatternLength {
			writeError(w, http.StatusBadRequest, "anti-pattern too long")
			return
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		cleaned = append(cleaned, s)
	}

	encoded, err := json.Marshal(cleaned)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode anti-patterns")
		return
	}

	updated, err := h.Queries.UpsertUserProfile(r.Context(), db.UpsertUserProfileParams{
		UserID:       parseUUID(userID),
		Persona:      req.Persona,
		Language:     req.Language,
		LengthPref:   int16(req.LengthPref),
		AutonomyPref: int16(req.AutonomyPref),
		TechPref:     int16(req.TechPref),
		AntiPatterns: encoded,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save profile")
		return
	}

	writeJSON(w, http.StatusOK, userProfileToResponse(updated))
}

// DeleteMyProfile clears the authenticated user's profile (resets to default).
func (h *Handler) DeleteMyProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	if err := h.Queries.DeleteUserProfile(r.Context(), parseUUID(userID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete profile")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
