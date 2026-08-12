package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/piagent"
)

// ModelConnectionProber issues the single verification request. *piagent.Prober
// is the production implementation.
type ModelConnectionProber interface {
	Probe(ctx context.Context, cfg piagent.Config, apiKey string) piagent.ProbeResult
}

// ValidateRuntimeModelConnectionRequest carries a candidate model connection
// that has not been saved yet. Onboarding calls this before the first write so
// a wrong key fails in the form, in seconds, instead of inside the first task.
type ValidateRuntimeModelConnectionRequest struct {
	Provider string `json:"provider"`
	API      string `json:"api"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
}

type ValidateRuntimeModelConnectionResponse struct {
	Valid bool `json:"valid"`
	// Outcome is a stable machine-readable reason the UI maps to an action.
	// Empty when Valid is true.
	Outcome string `json:"outcome,omitempty"`
	Status  int    `json:"status,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// ValidateRuntimeModelConnection proves that provider + endpoint + model + key
// work together, by making one minimal request from the server.
//
// Deliberately not runtime-scoped: during onboarding the key is entered before
// any runtime row is worth writing, and validating a candidate needs no access
// to an existing connection. That also keeps this endpoint from becoming a way
// to read anything back out of a runtime.
func (h *Handler) ValidateRuntimeModelConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	// Outbound network egress on a user-supplied URL is not something an agent
	// should be able to drive.
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not validate model connections")
		return
	}
	if h.ModelProbeRateLimiter != nil &&
		!h.ModelProbeRateLimiter.Allow(r.Context(), uuidToString(member.UserID)) {
		writeError(w, http.StatusTooManyRequests, "too many verification attempts; wait a moment and try again")
		return
	}

	var req ValidateRuntimeModelConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		writeError(w, http.StatusBadRequest, "api_key is required")
		return
	}
	cfg := piagent.Normalize(piagent.Config{
		Provider: req.Provider,
		API:      req.API,
		BaseURL:  req.BaseURL,
		Model:    req.Model,
	})
	if err := piagent.ValidateForRemoteProbe(cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	prober := h.PiProber
	if prober == nil {
		prober = piagent.NewProber()
	}
	result := prober.Probe(r.Context(), cfg, apiKey)

	// Always 200: a rejected key is a successful verification, not a failed
	// request. The UI branches on `valid`, and an HTTP error code here would
	// make a normal "wrong key" indistinguishable from a broken endpoint.
	writeJSON(w, http.StatusOK, ValidateRuntimeModelConnectionResponse{
		Valid:   result.Outcome == piagent.OutcomeOK,
		Outcome: outcomeForResponse(result.Outcome),
		Status:  result.Status,
		Detail:  result.Detail,
	})
}

func outcomeForResponse(outcome piagent.Outcome) string {
	if outcome == piagent.OutcomeOK {
		return ""
	}
	return string(outcome)
}
