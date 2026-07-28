package main

// FIR-3881 — `multica agent skills always-on` sent an empty list.
//
// The command registered --skill-ids as a plain String flag while the shared
// reader cleanSkillIDsFlag reads it with GetStringSlice. The type mismatch made
// GetStringSlice return an empty slice instead of the ids, so the request body
// carried always_on_skill_ids: [] — the server dutifully cleared the set and
// echoed the empty list back, and the call looked like it had succeeded.
//
// These tests drive the REAL command's flag set (not a hand-built cobra command
// with the flag type the test wishes for), because that is exactly the gap the
// original tests left open.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/spf13/pflag"
)

// setAlwaysOnSkillIDsFlag parses value into --skill-ids on the real command the
// same way the shell does, and restores the previous value afterwards so the
// shared command object stays clean for other tests in this package. Restoring
// goes through Replace rather than Set because a slice flag appends on a second
// Set.
func setAlwaysOnSkillIDsFlag(t *testing.T, value string) {
	t.Helper()
	flag := agentSkillsAlwaysOnCmd.Flags().Lookup("skill-ids")
	if flag == nil {
		t.Fatal("agent skills always-on must expose --skill-ids")
	}
	slice, ok := flag.Value.(pflag.SliceValue)
	if !ok {
		t.Fatal("--skill-ids must be a slice flag; cleanSkillIDsFlag reads it with GetStringSlice and a plain string flag reads back as empty")
	}
	original := slice.GetSlice()
	changed := flag.Changed
	t.Cleanup(func() {
		_ = slice.Replace(original)
		flag.Changed = changed
	})
	if err := agentSkillsAlwaysOnCmd.Flags().Set("skill-ids", value); err != nil {
		t.Fatalf("set --skill-ids: %v", err)
	}
}

func TestAgentSkillsAlwaysOnSendsTheIDsItWasGiven(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"agent_id":            "agent-123",
			"always_on_skill_ids": gotBody["always_on_skill_ids"],
			"context_version":     "1.0.6",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	setAlwaysOnSkillIDsFlag(t, "skill-a,skill-b")

	if err := runAgentSkillsAlwaysOn(agentSkillsAlwaysOnCmd, []string{"agent-123"}); err != nil {
		t.Fatalf("runAgentSkillsAlwaysOn: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/api/agents/agent-123/skills/always-on" {
		t.Fatalf("path = %q, want the always-on endpoint", gotPath)
	}
	if !reflect.DeepEqual(gotBody["always_on_skill_ids"], []string{"skill-a", "skill-b"}) {
		t.Fatalf("always_on_skill_ids body = %v, want both ids", gotBody["always_on_skill_ids"])
	}
}

// An empty --skill-ids still clears the set: the documented way to turn every
// always-on skill back into a load-on-demand one.
func TestAgentSkillsAlwaysOnEmptyValueClearsTheSet(t *testing.T) {
	var gotBody map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"agent_id":            "agent-123",
			"always_on_skill_ids": []string{},
			"context_version":     "1.0.6",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	setAlwaysOnSkillIDsFlag(t, "")

	if err := runAgentSkillsAlwaysOn(agentSkillsAlwaysOnCmd, []string{"agent-123"}); err != nil {
		t.Fatalf("runAgentSkillsAlwaysOn: %v", err)
	}
	if len(gotBody["always_on_skill_ids"]) != 0 {
		t.Fatalf("always_on_skill_ids body = %v, want empty", gotBody["always_on_skill_ids"])
	}
}
