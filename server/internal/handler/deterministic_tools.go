package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/pkg/detsteps"
)

// testDeterministicToolRequest is the body for the author/test loop: the Go
// source of a deterministic step plus a sample input the step's Run receives.
type testDeterministicToolRequest struct {
	Source string         `json:"source"`
	Input  map[string]any `json:"input"`
}

// TestDeterministicTool compiles and runs a user-authored deterministic step in
// the sandboxed interpreter and returns the dettools.Result envelope. It always
// responds 200: a compile error or policy failure is a step-level outcome
// carried inside the envelope (status/error_code), not an HTTP error — the only
// 4xx here is a malformed request body or empty source.
func (h *Handler) TestDeterministicTool(w http.ResponseWriter, r *http.Request) {
	var req testDeterministicToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Source) == "" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}

	res := detsteps.Run(r.Context(), req.Source, req.Input, detsteps.Options{})
	writeJSON(w, http.StatusOK, res)
}
