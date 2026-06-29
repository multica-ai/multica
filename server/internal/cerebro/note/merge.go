// merge.go — AI-assisted note merge for Plan A conflict resolution (FIR-1317).
// When the note-lock feature is on and a save conflict is detected (the note
// was edited by someone else since the client last fetched it), the editor
// calls POST /api/notes/{id}/merge with both bodies. This endpoint asks a
// cheap Haiku model to produce a merged version, which the user reviews and
// accepts or edits before saving.
//
// Design: best-effort. If the gateway is not configured or the call fails,
// the handler returns 503 and the dialog falls back to letting the user pick
// one of the two versions manually. No merge state is stored — the call is
// stateless.
//
// Gateway: same FIRTAL_REGISTRY_URL + FIRTAL_REGISTRY_KEY env vars as the
// duplicate-check judge (server/internal/cerebro/duplicatecheck/judge.go).
// The path and bearer-auth are identical; only the prompt changes.
package note

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	mergeModel        = "claude-haiku-4-5"
	mergeTimeout      = 8 * time.Second
	mergeMaxRespBytes = 512 << 10 // 512 KB — notes are markdown, keep it tight
	mergeGatewayPath  = "/api/ai/proxy/v1/chat/completions"
	mergeMaxTokens    = 4096
)

type mergeRequest struct {
	YourBody   string `json:"your_body"`
	ServerBody string `json:"server_body"`
}

type mergeResponse struct {
	Merged string `json:"merged"`
}

const mergeSystemPrompt = `You are a text-merge assistant. Two people edited the same markdown note simultaneously and their versions conflict.
Produce one merged version that intelligently combines both edits, preserving meaning from both.
Return ONLY the merged markdown text — no explanation, no fences, no preamble.
If the two versions are identical or trivially compatible, return the combined result directly.`

// MergeNote handles POST /api/notes/{id}/merge. It asks an AI model to merge
// two concurrent edits of the same note. Authentication required; any member
// who can see the note may call this. The response is advisory — the caller
// decides whether to use it.
func (h *Handler) MergeNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, me, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireCanSee(w, r, noteID, me) {
		return
	}

	var req mergeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.YourBody == "" && req.ServerBody == "" {
		writeJSON(w, http.StatusOK, mergeResponse{Merged: ""})
		return
	}
	if req.YourBody == req.ServerBody {
		writeJSON(w, http.StatusOK, mergeResponse{Merged: req.YourBody})
		return
	}

	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("FIRTAL_REGISTRY_URL")), "/")
	apiKey := strings.TrimSpace(os.Getenv("FIRTAL_REGISTRY_KEY"))
	if baseURL == "" || apiKey == "" {
		writeError(w, http.StatusServiceUnavailable, "AI merge not configured")
		return
	}

	merged, err := callMergeGateway(r.Context(), baseURL, apiKey, req.YourBody, req.ServerBody)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "AI merge unavailable")
		return
	}

	writeJSON(w, http.StatusOK, mergeResponse{Merged: merged})
}

func callMergeGateway(parent context.Context, baseURL, apiKey, yourBody, serverBody string) (string, error) {
	userMsg := "THEIR VERSION (saved to server while you were editing):\n" + serverBody +
		"\n\n---\n\nYOUR VERSION (your unsaved edits):\n" + yourBody +
		"\n\n---\n\nPlease produce one merged markdown version that combines both."

	payload := map[string]any{
		"model": mergeModel,
		"messages": []map[string]any{
			{"role": "system", "content": mergeSystemPrompt},
			{"role": "user", "content": userMsg},
		},
		"temperature": 0,
		"max_tokens":  mergeMaxTokens,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(parent, mergeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+mergeGatewayPath, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-trace-name", "cerebro-note-merge")
	req.Header.Set("x-tags", "multica,note-conflict-merge")
	req.Header.Set("x-bq-label-app", "multica")
	req.Header.Set("x-bq-label-function", "note-conflict-merge")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, mergeMaxRespBytes))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gateway returned %d", resp.StatusCode)
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Choices) == 0 {
		return "", err
	}

	text := extractMergeText(parsed.Choices[0].Message.Content)
	return text, nil
}

func extractMergeText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var b strings.Builder
		for _, blk := range blocks {
			if blk.Type == "text" || blk.Type == "" {
				b.WriteString(blk.Text)
			}
		}
		return b.String()
	}
	return ""
}
