// CEREBRO-PATCH(persona-approvals): JEH-1078 — Multica-side proxy for
// the persona approval inbox + approve/deny. Net-new fork file: thin
// pass-through that lets a Multica user list "kræver din godkendelse"
// requests and resolve them. Cerebro talks to persona with the service
// token (system:cerebro) and resolves the calling user's persona actor
// so persona's pool check runs against the real human, not the proxy.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// personaApprovalHTTP shares config with personaProxyHTTP but with a
// slightly longer timeout: approvals carry richer payloads (resource
// attrs from the original tool call), and we never want a slow persona
// to surface as a "no approvals" empty list.
var personaApprovalHTTP = &http.Client{Timeout: 5 * time.Second}

// ListPersonaApprovals proxies persona's GET /v1/approvals for the
// calling Multica user's "kræver din godkendelse" inbox.
//
// Cerebro uses the SERVICE token (system:cerebro is privileged to list
// org-wide approvals) and then filters by approver-pool membership on
// the Multica side using the calling user's display name as the
// matching key (the MVP "names as role" stand-in until the platform
// adopts first-class roles / groups — see JEH-1078).
//
// Query: status=<pending|approved|denied|expired> (optional)
func (h *Handler) ListPersonaApprovals(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	personaURL := strings.TrimRight(os.Getenv("MULTICA_PERSONA_URL"), "/")
	token := os.Getenv("MULTICA_PERSONA_TOKEN")
	if personaURL == "" || token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"approvals": []any{}})
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	q := url.Values{}
	if s := r.URL.Query().Get("status"); s != "" {
		q.Set("status", s)
	}
	q.Set("limit", "200")
	reqURL := personaURL + "/v1/approvals?" + q.Encode()

	req, err := http.NewRequestWithContext(r.Context(), "GET", reqURL, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "persona unreachable")
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := personaApprovalHTTP.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "persona unreachable")
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "persona returned status "+resp.Status)
		return
	}

	var raw struct {
		Requests []map[string]any `json:"requests"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		writeError(w, http.StatusBadGateway, "persona response unparseable")
		return
	}
	// Filter by approver pool. Match by name since the only stable
	// identity link Multica → persona is the display name (cerebro
	// auto-creates persona actors named after the Multica user).
	out := make([]map[string]any, 0, len(raw.Requests))
	for _, req := range raw.Requests {
		pool, ok := req["approver_pool"].(map[string]any)
		if !ok {
			continue
		}
		if poolMatchesName(pool, user.Name) {
			out = append(out, req)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": out})
}

// ResolvePersonaApproval handles POST /api/persona/approvals/{id}/approve
// and /deny. Posts to persona with subject_actor_id set to the calling
// user's persona actor so persona authorises against the real human.
func (h *Handler) ResolvePersonaApproval(w http.ResponseWriter, r *http.Request, verb string) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing approval id")
		return
	}
	if verb != "approve" && verb != "deny" {
		writeError(w, http.StatusBadRequest, "verb must be approve or deny")
		return
	}

	personaURL := strings.TrimRight(os.Getenv("MULTICA_PERSONA_URL"), "/")
	token := os.Getenv("MULTICA_PERSONA_TOKEN")
	if personaURL == "" || token == "" {
		writeError(w, http.StatusServiceUnavailable, "persona not configured")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	subjectID, err := resolvePersonaActorByName(r.Context(), personaURL, token, user.Name)
	if err != nil || subjectID == "" {
		writeError(w, http.StatusForbidden, "no persona actor for this user — cannot approve on their behalf")
		return
	}

	var clientBody struct {
		Reason string `json:"reason,omitempty"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&clientBody)

	payload, _ := json.Marshal(map[string]string{
		"reason":           clientBody.Reason,
		"subject_actor_id": subjectID,
	})
	reqURL := personaURL + "/v1/approvals/" + id + "/" + verb
	req, err := http.NewRequestWithContext(r.Context(), "POST", reqURL, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusBadGateway, "persona unreachable")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := personaApprovalHTTP.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "persona unreachable")
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	// Pass-through status + body so the client sees persona's exact
	// answer (409 not_pending, 403 not in pool, etc.).
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

// ApprovePersonaApproval routes the approve verb.
func (h *Handler) ApprovePersonaApproval(w http.ResponseWriter, r *http.Request) {
	h.ResolvePersonaApproval(w, r, "approve")
}

// DenyPersonaApproval routes the deny verb.
func (h *Handler) DenyPersonaApproval(w http.ResponseWriter, r *http.Request) {
	h.ResolvePersonaApproval(w, r, "deny")
}

// poolMatchesName reports whether the approver_pool object (raw JSON
// map) includes the given display name. Used for the cerebro-side
// inbox filter.
func poolMatchesName(pool map[string]any, name string) bool {
	if names, ok := pool["names"].([]any); ok {
		for _, n := range names {
			if s, ok := n.(string); ok && s == name {
				return true
			}
		}
	}
	// Users list isn't used here — cerebro doesn't have the user→actor
	// mapping cached, and the for_me semantic only needs name matching
	// for the inbox view. Approve/deny goes through the actor-resolution
	// path which DOES check user IDs server-side.
	return false
}

// resolvePersonaActorByName asks persona for the actor whose name
// matches the Multica user's display name. Returns "" if persona has
// no such actor yet (which happens for users who haven't spawned any
// agents — they get no approval rights until cerebro auto-creates
// their actor).
func resolvePersonaActorByName(ctx context.Context, base, token, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/v1/actors", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := personaApprovalHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Actors []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"actors"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	for _, a := range out.Actors {
		if a.Name == name && a.Status != "deleted" {
			return a.ID, nil
		}
	}
	return "", nil
}
