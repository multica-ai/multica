//go:build integration

package qoderruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestLocalMultica exercises the actual authenticated HTTP router and database.
// Qoder is a local fixture: no remote credentials, agent execution or quota.
func TestLocalMultica(t *testing.T) {
	path := os.Getenv("MULTICA_QODER_INTEGRATION_CONFIG")
	if path == "" {
		t.Skip("set MULTICA_QODER_INTEGRATION_CONFIG to a local bridge config")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal("invalid integration config")
	}
	u, err := url.Parse(cfg.MulticaURL)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		t.Fatal("integration test requires a loopback Multica server")
	}
	var mu sync.Mutex
	var prompt string
	creates, sends := 0, 0
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/sessions" && r.Method == http.MethodPost:
			creates++
			io.WriteString(w, `{"id":"sess_fixture"}`)
		case strings.HasSuffix(r.URL.Path, "/events") && r.Method == http.MethodPost:
			sends++
			var req struct {
				Events []event `json:"events"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			prompt = req.Events[0].text()
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/events"):
			events := []map[string]any{
				{"id": "evt_user", "type": "user.message", "content": []map[string]string{{"type": "text", "text": prompt}}},
				{"id": "evt_result", "type": "agent.message", "content": []map[string]string{{"type": "text", "text": "Local bridge integration passed"}}},
				{"id": "evt_idle", "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}},
			}
			if r.URL.Query().Get("after_id") == "evt_user" {
				events = events[1:]
			}
			json.NewEncoder(w).Encode(map[string]any{"data": events, "has_more": false})
		case r.URL.Path == "/agents/agent_fixture":
			io.WriteString(w, `{"id":"agent_fixture"}`)
		case r.URL.Path == "/environments/env_fixture":
			io.WriteString(w, `{"id":"env_fixture"}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	defer q.Close()
	cfg.QoderURL = q.URL
	cfg.QoderToken = "test-only"
	cfg.EnvironmentID = "env_fixture"
	cfg.StateDir = t.TempDir()
	b, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b.state = &journal{Binding: "local-integration"}
	if err = b.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if err = b.register(ctx, strings.ReplaceAll(uuid.NewString(), "-", "")); err != nil {
		t.Fatal(err)
	}
	cleanup := func(method, path string) {
		t.Cleanup(func() {
			if err := b.multica.call(context.Background(), method, path, map[string]any{}, nil); err != nil {
				t.Errorf("cleanup %s failed: %v", path, err)
			}
		})
	}
	cleanup(http.MethodDelete, "/api/runtimes/"+b.state.RuntimeID)
	var runtimes []struct {
		ID   string `json:"id"`
		Mode string `json:"runtime_mode"`
	}
	if err = b.multica.call(ctx, http.MethodGet, "/api/runtimes/", nil, &runtimes); err != nil {
		t.Fatal(err)
	}
	mode := ""
	for _, rt := range runtimes {
		if rt.ID == b.state.RuntimeID {
			mode = rt.Mode
		}
	}
	if mode != "cloud" {
		t.Fatalf("registered runtime_mode=%s", mode)
	}
	var agent struct {
		ID string `json:"id"`
	}
	if err = b.multica.call(ctx, http.MethodPost, "/api/agents/", map[string]any{"name": "Qoder fixture " + uuid.NewString(), "runtime_id": b.state.RuntimeID, "runtime_config": map[string]string{"qoder_agent_id": "agent_fixture"}, "instructions": "Return a concise result", "max_concurrent_tasks": 1}, &agent); err != nil {
		t.Fatal(err)
	}
	cleanup(http.MethodPost, "/api/agents/"+agent.ID+"/archive")
	var issue struct {
		ID string `json:"id"`
	}
	if err = b.multica.call(ctx, http.MethodPost, "/api/issues/", map[string]any{"title": "Qoder bridge fixture " + uuid.NewString(), "description": "Validate remote execution", "status": "todo", "assignee_type": "agent", "assignee_id": agent.ID}, &issue); err != nil {
		t.Fatal(err)
	}
	cleanup(http.MethodDelete, "/api/issues/"+issue.ID)
	// Lose exactly one successful transcript response after the real server commits.
	b.multica.client = &http.Client{Timeout: 10 * time.Second, Transport: &loseMessageResponse{base: http.DefaultTransport}}
	err = b.step(ctx)
	if err == nil || b.state.Run == nil {
		t.Fatalf("expected lost message response, got err=%v", err)
	}
	taskID := b.state.Run.TaskID
	if err = b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if b.state.Run != nil {
		t.Fatalf("run did not finish: phase=%s failure=%s", b.state.Run.Phase, b.state.Run.Failure)
	}
	var status struct {
		Status string `json:"status"`
	}
	if err = b.multica.call(ctx, http.MethodGet, "/api/daemon/tasks/"+taskID+"/status", nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.Status != "completed" {
		t.Fatalf("task ended as %s", status.Status)
	}
	var messages []struct {
		Content string `json:"content"`
	}
	if err = b.multica.call(ctx, http.MethodGet, "/api/tasks/"+taskID+"/messages", nil, &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Content != "Local bridge integration passed" {
		t.Fatalf("transcript replay duplicated or lost output: %+v", messages)
	}
	mu.Lock()
	defer mu.Unlock()
	if creates != 1 || sends != 1 {
		t.Fatalf("remote execution duplicated: creates=%d sends=%d", creates, sends)
	}
}

type loseMessageResponse struct {
	base http.RoundTripper
	lost bool
}

func (t *loseMessageResponse) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(r)
	if err == nil && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages") && !t.lost {
		t.lost = true
		resp.Body.Close()
		return nil, fmt.Errorf("injected lost response")
	}
	return resp, err
}
