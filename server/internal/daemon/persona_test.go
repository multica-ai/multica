// CEREBRO-PATCH(persona-test): persona integration changes.
package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/hvejsel/firtal-persona/sdk/go"
)

// TestChooseEffectivePersonaSandbox locks in the E1 precedence rule: when
// the runtime sets a persona sandbox it acts as a hard upper bound and the
// agent-level value is ignored. When the runtime is empty, the agent's
// value applies. When both are empty, persona gating is off for this spawn.
func TestChooseEffectivePersonaSandbox(t *testing.T) {
	tests := []struct {
		name        string
		runtime     string
		agent       string
		wantSandbox string
		wantSource  string
	}{
		{
			name:        "neither set → no gating",
			runtime:     "",
			agent:       "",
			wantSandbox: "",
			wantSource:  "",
		},
		{
			name:        "agent only → agent wins",
			runtime:     "",
			agent:       "claude-developer",
			wantSandbox: "claude-developer",
			wantSource:  "agent",
		},
		{
			name:        "runtime only → runtime wins",
			runtime:     "claude-readonly",
			agent:       "",
			wantSandbox: "claude-readonly",
			wantSource:  "runtime",
		},
		{
			name:        "both set, runtime more restrictive → runtime wins (cap)",
			runtime:     "claude-readonly",
			agent:       "claude-power",
			wantSandbox: "claude-readonly",
			wantSource:  "runtime",
		},
		{
			name:        "both set, runtime more permissive → runtime still wins (operator policy)",
			runtime:     "claude-power",
			agent:       "claude-readonly",
			wantSandbox: "claude-power",
			wantSource:  "runtime",
		},
		{
			name:        "both set to same value → runtime wins (source matters for logs)",
			runtime:     "claude-developer",
			agent:       "claude-developer",
			wantSandbox: "claude-developer",
			wantSource:  "runtime",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSandbox, gotSource := chooseEffectivePersonaSandbox(tt.runtime, tt.agent)
			if gotSandbox != tt.wantSandbox {
				t.Errorf("sandbox: got %q, want %q", gotSandbox, tt.wantSandbox)
			}
			if gotSource != tt.wantSource {
				t.Errorf("source: got %q, want %q", gotSource, tt.wantSource)
			}
		})
	}
}

// TestPersonaActorName_HumanReadable locks in the rule that actor names are
// human-readable in persona's UI: slug from agent display name + short UUID
// for stability across renames.
func TestPersonaActorName_HumanReadable(t *testing.T) {
	tests := []struct {
		name string
		in   *AgentData
		want string
	}{
		{name: "nil agent → unknown", in: nil, want: "cerebro:agent:unknown"},
		{name: "plain name", in: &AgentData{ID: "72995498-251a-4f13-85bc-70008a9ce3b9", Name: "CEO agent"}, want: "cerebro:agent:ceo-agent-72995498"},
		{name: "special chars stripped", in: &AgentData{ID: "abcd1234-...", Name: "Customer / Support v2!"}, want: "cerebro:agent:customer-support-v2-abcd1234"},
		{name: "empty name uses fallback slug", in: &AgentData{ID: "abcd1234-...", Name: ""}, want: "cerebro:agent:agent-abcd1234"},
		{name: "long name truncated", in: &AgentData{ID: "abcd1234-...", Name: "this-is-a-very-very-very-very-very-very-very-long-agent-name"}, want: "cerebro:agent:this-is-a-very-very-very-very-very-very-abcd1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := personaActorName(tt.in)
			if got != tt.want {
				t.Errorf("personaActorName(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestMcpServerNames verifies the helper extracts and sorts keys from the
// standard Claude Code mcp_config shape and returns an empty (non-nil)
// slice for the various edge cases — nil input, malformed JSON, missing
// "mcpServers" key, empty object. Persona's UI iterates over this value;
// nil would serialise as `null` and break rendering.
func TestMcpServerNames(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "nil/empty input", in: "", want: []string{}},
		{name: "malformed JSON", in: "{not json", want: []string{}},
		{name: "no mcpServers key", in: `{"other":1}`, want: []string{}},
		{name: "empty mcpServers object", in: `{"mcpServers":{}}`, want: []string{}},
		{
			name: "two servers sorted",
			in:   `{"mcpServers":{"supabase":{"command":"x"},"stripe":{"command":"y"}}}`,
			want: []string{"stripe", "supabase"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw json.RawMessage
			if tt.in != "" {
				raw = json.RawMessage(tt.in)
			}
			got := mcpServerNames(raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBuildActorAttributes locks in what the daemon pushes to persona on
// every spawn (E3 part 3): agent identity, runtime provider, the live tool
// list from providerCapabilities, current workdir, and the effective sandbox
// with its source. Persona's UI keys off this shape — fields drift here = UI
// goes blank silently, hence the explicit assertions.
func TestBuildActorAttributes(t *testing.T) {
	t.Run("nil agent → empty map", func(t *testing.T) {
		got := buildActorAttributes(nil, "claude", "/tmp", "", "")
		if len(got) != 0 {
			t.Fatalf("expected empty map, got %v", got)
		}
	})

	t.Run("claude with runtime cap", func(t *testing.T) {
		agent := &AgentData{ID: "agent-uuid", Name: "CEO agent"}
		got := buildActorAttributes(agent, "claude", "/tmp/wd", "claude-readonly", "runtime")

		// Keys persona's UI hard-depends on.
		want := map[string]any{
			"source":            "cerebro",
			"agent_id":          "agent-uuid",
			"agent_name":        "CEO agent",
			"runtime_provider":  "claude",
			"workdir":           "/tmp/wd",
			"effective_sandbox": "claude-readonly",
			"sandbox_source":    "runtime",
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("attr %s: got %v, want %v", k, got[k], v)
			}
		}
		// Tool list comes from providerCapabilities — verify it propagates.
		tools, ok := got["runtime_tools"].([]string)
		if !ok || len(tools) == 0 {
			t.Fatalf("runtime_tools missing or wrong type: %v", got["runtime_tools"])
		}
		hasBash := false
		for _, t := range tools {
			if t == "Bash" {
				hasBash = true
				break
			}
		}
		if !hasBash {
			t.Errorf("expected Bash in runtime_tools, got %v", tools)
		}
		// mcp_servers must be a slice (possibly empty), not missing — UI iterates.
		if _, ok := got["mcp_servers"].([]string); !ok {
			t.Errorf("mcp_servers missing or wrong type: %v", got["mcp_servers"])
		}
	})

	t.Run("no sandbox → omits sandbox fields", func(t *testing.T) {
		agent := &AgentData{ID: "id", Name: "x"}
		got := buildActorAttributes(agent, "claude", "/tmp", "", "")
		if _, ok := got["effective_sandbox"]; ok {
			t.Errorf("effective_sandbox should be omitted when empty, got %v", got["effective_sandbox"])
		}
		if _, ok := got["sandbox_source"]; ok {
			t.Errorf("sandbox_source should be omitted when sandbox empty, got %v", got["sandbox_source"])
		}
	})

	t.Run("unknown provider falls back to empty tools", func(t *testing.T) {
		agent := &AgentData{ID: "id", Name: "x"}
		got := buildActorAttributes(agent, "made-up-provider", "/tmp", "", "")
		tools, _ := got["runtime_tools"].([]string)
		if !reflect.DeepEqual(tools, []string{}) {
			t.Errorf("unknown provider tools: got %v, want []", tools)
		}
		// Provider name still propagates so persona's UI says "unknown" rather
		// than fabricating tool entries.
		if got["runtime_provider"] != "made-up-provider" {
			t.Errorf("runtime_provider: got %v, want made-up-provider", got["runtime_provider"])
		}
	})

	// Gap #1: per-agent mcp_config drives the mcp_servers attribute. Without
	// this, persona's UI either shows the static (empty) list from
	// providerCapabilities or — worse — silently drops the field. Two cases
	// we care about: real config → sorted names, missing config → [] not nil.
	t.Run("mcp_config drives mcp_servers", func(t *testing.T) {
		cases := []struct {
			name string
			cfg  json.RawMessage
			want []string
		}{
			{
				name: "two servers (sorted)",
				cfg:  json.RawMessage(`{"mcpServers":{"supabase":{"command":"echo"},"stripe":{"command":"echo"}}}`),
				want: []string{"stripe", "supabase"},
			},
			{
				name: "no config → empty slice",
				cfg:  nil,
				want: []string{},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				agent := &AgentData{ID: "id", Name: "x", McpConfig: tc.cfg}
				got := buildActorAttributes(agent, "claude", "/tmp", "", "")
				gotMcp, ok := got["mcp_servers"].([]string)
				if !ok {
					t.Fatalf("mcp_servers missing or wrong type: %v", got["mcp_servers"])
				}
				if !reflect.DeepEqual(gotMcp, tc.want) {
					t.Errorf("mcp_servers: got %v, want %v", gotMcp, tc.want)
				}
			})
		}
	})
}

// TestDeleteStalePersonaActors locks in the W4.1 cleanup rule: when an
// agent is renamed, the daemon's next spawn must soft-delete persona
// actors whose name shares the new agent's uuid8 suffix but whose slug
// differs (the old name). Other actors must NOT be touched.
func TestDeleteStalePersonaActors(t *testing.T) {
	const agentID = "72995498-251a-4f13-85bc-70008a9ce3b9"
	const currentName = "cerebro:agent:cto-agent-72995498"

	type fixtureActor struct {
		ID, Name, Status string
	}
	actors := []fixtureActor{
		{"id-old-1", "cerebro:agent:ceo-agent-72995498", "active"},   // stale, must delete
		{"id-old-2", "cerebro:agent:ceo-agent-72995498", "deleted"},  // already deleted, skip
		{"id-current", "cerebro:agent:cto-agent-72995498", "active"}, // current, skip
		{"id-other", "cerebro:agent:other-12345678", "active"},       // unrelated suffix, skip
		{"id-non-cerebro", "system:user:abc", "active"},              // non-cerebro prefix, skip
	}

	var (
		mu              sync.Mutex
		deletedActorIDs []string
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/actors", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		out := map[string]any{"actors": []map[string]string{}}
		entries := out["actors"].([]map[string]string)
		mu.Lock()
		for _, a := range actors {
			// Filter: skip already-deleted actors when listing, mirroring
			// persona's real /v1/actors filter.
			if a.Status == "deleted" {
				continue
			}
			entries = append(entries, map[string]string{
				"id":     a.ID,
				"name":   a.Name,
				"status": a.Status,
			})
		}
		mu.Unlock()
		out["actors"] = entries
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/v1/actors/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Strip "/v1/actors/" prefix to get the id.
		actorID := strings.TrimPrefix(r.URL.Path, "/v1/actors/")
		mu.Lock()
		deletedActorIDs = append(deletedActorIDs, actorID)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := persona.New(persona.Config{Endpoint: srv.URL, Token: "test"})
	if err != nil {
		t.Fatalf("persona.New: %v", err)
	}

	deleteStalePersonaActors(context.Background(), client, currentName, agentID, slog.New(slog.NewTextHandler(io.Discard, nil)))

	mu.Lock()
	got := append([]string{}, deletedActorIDs...)
	mu.Unlock()

	want := []string{"id-old-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deleted actor ids: got %v, want %v", got, want)
	}
}

// TestDeleteStalePersonaActors_ListFailureSilent locks in the best-effort
// contract: a list/delete failure must NOT block the spawn. The function
// returns silently after logging and the caller continues.
func TestDeleteStalePersonaActors_ListFailureSilent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client, err := persona.New(persona.Config{Endpoint: srv.URL, Token: "test"})
	if err != nil {
		t.Fatalf("persona.New: %v", err)
	}

	// No panic, no fatal — function returns even though list fails.
	deleteStalePersonaActors(context.Background(), client, "cerebro:agent:x-12345678", "12345678-1234-1234-1234-123456789012", slog.New(slog.NewTextHandler(io.Discard, nil)))
}
