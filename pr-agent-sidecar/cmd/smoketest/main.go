// smoketest exercises pr-agent-sidecar's Multica integration without
// requiring GitHub App credentials. It reads MULTICA_* env vars, calls
// POST /api/issues exactly the way the webhook handler does, and prints
// the result.
//
// Run:
//
//	cd pr-agent-sidecar
//	set -a && source .env && set +a
//	go run ./cmd/smoketest
package main

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

type createIssueReq struct {
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	AssigneeType string `json:"assignee_type,omitempty"`
	AssigneeID   string `json:"assignee_id,omitempty"`
}

type issueResp struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title,omitempty"`
	Status     string `json:"status,omitempty"`
}

func main() {
	pat := mustGetenv("MULTICA_PAT")
	base := strings.TrimRight(mustGetenv("MULTICA_BASE_URL"), "/")
	workspaceID := mustGetenv("MULTICA_WORKSPACE_ID")
	agentID := mustGetenv("PR_REVIEWER_AGENT_ID")

	body, err := json.Marshal(createIssueReq{
		Title:        "[smoketest] Hello from pr-agent-sidecar",
		Description:  fmt.Sprintf("Smoketest at %s. Safe to delete.", time.Now().UTC().Format(time.RFC3339)),
		AssigneeType: "agent",
		AssigneeID:   agentID,
	})
	if err != nil {
		die("marshal request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/issues", bytes.NewReader(body))
	if err != nil {
		die("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("X-Workspace-ID", workspaceID)
	req.Header.Set("Content-Type", "application/json")

	fmt.Printf("POST %s\n", req.URL.String())
	fmt.Printf("  X-Workspace-ID: %s\n", workspaceID)
	fmt.Printf("  assignee_id:    %s (type=agent)\n\n", agentID)

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		die("call multica: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	fmt.Printf("HTTP %d\n", resp.StatusCode)
	fmt.Printf("Response body:\n%s\n\n", string(raw))

	if resp.StatusCode != http.StatusCreated {
		fmt.Fprintln(os.Stderr, "[FAIL] expected HTTP 201")
		os.Exit(1)
	}

	var out issueResp
	if err := json.Unmarshal(raw, &out); err != nil {
		die("parse response: %v", err)
	}
	fmt.Printf("[OK] Multica created issue %s (id=%s)\n", out.Identifier, out.ID)
}

func mustGetenv(k string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		die("missing env var: %s (source .env or export it in your shell)", k)
	}
	return v
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
