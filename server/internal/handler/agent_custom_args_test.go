package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCustomArgsForRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		runtime     db.AgentRuntime
		args        []string
		managed     bool
		want        []string
		wantManaged bool
	}{
		{
			name: "windows codex stores the canonical two-token default",
			runtime: db.AgentRuntime{
				Provider: "codex",
				Metadata: []byte(`{"os":"windows"}`),
			},
			args:        []string{"--profile", "research"},
			want:        []string{"-c", `windows.sandbox="unelevated"`, "--profile", "research"},
			wantManaged: true,
		},
		{
			name: "windows codex preserves an explicit override without a duplicate",
			runtime: db.AgentRuntime{
				Provider: "codex",
				Metadata: []byte(`{"os":"windows"}`),
			},
			args: []string{"-c", `windows.sandbox="elevated"`, "--profile", "research"},
			want: []string{"-c", `windows.sandbox="elevated"`, "--profile", "research"},
		},
		{
			name: "runtime owned setting removes the managed prefix",
			runtime: db.AgentRuntime{
				Provider: "codex",
				Metadata: []byte(`{"os":"windows","codex_windows_sandbox_arg_configured":true}`),
			},
			args:    []string{"-c", `windows.sandbox="unelevated"`, "--profile", "research"},
			managed: true,
			want:    []string{"--profile", "research"},
		},
		{
			name: "explicit canonical custom setting beats the runtime setting",
			runtime: db.AgentRuntime{
				Provider: "codex",
				Metadata: []byte(`{"os":"windows","codex_windows_sandbox_arg_configured":true}`),
			},
			args: []string{"-c", `windows.sandbox="unelevated"`, "--profile", "research"},
			want: []string{"-c", `windows.sandbox="unelevated"`, "--profile", "research"},
		},
		{
			name: "non-windows codex stays unchanged",
			runtime: db.AgentRuntime{
				Provider: "codex",
				Metadata: []byte(`{"os":"linux"}`),
			},
			args: []string{"--profile", "research"},
			want: []string{"--profile", "research"},
		},
		{
			name: "non-codex runtime removes a proven managed prefix",
			runtime: db.AgentRuntime{
				Provider: "claude",
				Metadata: []byte(`{"os":"windows"}`),
			},
			args:    []string{"-c", `windows.sandbox="unelevated"`, "--profile", "research"},
			managed: true,
			want:    []string{"--profile", "research"},
		},
		{
			name: "non-codex runtime preserves an identical user-owned pair",
			runtime: db.AgentRuntime{
				Provider: "claude",
				Metadata: []byte(`{"os":"windows"}`),
			},
			args: []string{"-c", `windows.sandbox="unelevated"`, "--profile", "research"},
			want: []string{"-c", `windows.sandbox="unelevated"`, "--profile", "research"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, gotManaged := customArgsForRuntime(tt.runtime, tt.args, tt.managed)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("customArgsForRuntime() = %v, want %v", got, tt.want)
			}
			if gotManaged != tt.wantManaged {
				t.Fatalf("customArgsForRuntime() managed = %v, want %v", gotManaged, tt.wantManaged)
			}
		})
	}
}

func TestAgentCustomArgsPersistenceAcrossRuntimeOnlySwitches(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const agentName = "custom-args-runtime-switch"
	testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, agentName)

	createRuntime := func(name, metadata string) string {
		t.Helper()
		var runtimeID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (
				workspace_id, daemon_id, name, runtime_mode, provider, status,
				device_info, metadata, last_seen_at, owner_id
			)
			VALUES ($1, NULL, $2, 'cloud', 'codex', 'online', $3, $4::jsonb, now(), $5)
			RETURNING id
		`, testWorkspaceID, name, name, metadata, testUserID).Scan(&runtimeID); err != nil {
			t.Fatalf("create runtime %s: %v", name, err)
		}
		return runtimeID
	}

	windowsRuntimeID := createRuntime("custom-args-windows", `{"os":"windows"}`)
	linuxRuntimeID := createRuntime("custom-args-linux", `{"os":"linux"}`)
	runtimeOwnedID := createRuntime(
		"custom-args-windows-owned",
		`{"os":"windows","codex_windows_sandbox_arg_configured":true}`,
	)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, agentName)
		for _, runtimeID := range []string{windowsRuntimeID, linuxRuntimeID, runtimeOwnedID} {
			testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		}
	})

	assertArgs := func(w *httptest.ResponseRecorder, status int, want []string) string {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status = %d, want %d: %s", w.Code, status, w.Body.String())
		}
		var resp AgentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode agent response: %v", err)
		}
		if !reflect.DeepEqual(resp.CustomArgs, want) {
			t.Fatalf("custom_args = %v, want %v", resp.CustomArgs, want)
		}
		return resp.ID
	}

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":                 agentName,
		"runtime_id":           windowsRuntimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
		"custom_args":          []string{"--profile", "research"},
	}))
	agentID := assertArgs(w, http.StatusCreated, []string{
		"-c", `windows.sandbox="unelevated"`, "--profile", "research",
	})

	// Runtime deletion intentionally leaves an agent unbound. A custom-args
	// PATCH must remain valid and remove only the prefix whose persisted
	// provenance says Multica injected it.
	if _, err := testPool.Exec(ctx, `UPDATE agent SET runtime_id = NULL WHERE id = $1`, agentID); err != nil {
		t.Fatalf("unbind agent: %v", err)
	}
	unbound := httptest.NewRecorder()
	testHandler.UpdateAgent(unbound, withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
		"custom_args": []string{"-c", `windows.sandbox="unelevated"`, "--profile", "research"},
	}), "id", agentID))
	assertArgs(unbound, http.StatusOK, []string{"--profile", "research"})

	updateRuntime := func(runtimeID string, want []string) {
		t.Helper()
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
			"runtime_id": runtimeID,
		}), "id", agentID)
		testHandler.UpdateAgent(w, req)
		assertArgs(w, http.StatusOK, want)
	}

	updateRuntime(linuxRuntimeID, []string{"--profile", "research"})
	updateRuntime(windowsRuntimeID, []string{
		"-c", `windows.sandbox="unelevated"`, "--profile", "research",
	})
	updateRuntime(runtimeOwnedID, []string{"--profile", "research"})
}
