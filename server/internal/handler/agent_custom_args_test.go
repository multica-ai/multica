package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type gateAgentArgsUpdateTxStarter struct {
	delegate txStarter
	entered  chan<- struct{}
	release  <-chan struct{}
}

func (s *gateAgentArgsUpdateTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	s.entered <- struct{}{}
	select {
	case <-s.release:
		return s.delegate.Begin(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type rollbackOnCommitTx struct {
	pgx.Tx
}

func (tx *rollbackOnCommitTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Rollback(ctx); err != nil {
		return fmt.Errorf("rollback before injected commit failure: %w", err)
	}
	return errors.New("injected agent argument update commit failure")
}

type failFirstAgentArgsUpdateCommitTxStarter struct {
	delegate txStarter
	calls    int
}

func (s *failFirstAgentArgsUpdateCommitTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.delegate.Begin(ctx)
	if err != nil {
		return nil, err
	}
	s.calls++
	if s.calls == 1 {
		return &rollbackOnCommitTx{Tx: tx}, nil
	}
	return tx, nil
}

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
			name: "shared config owned setting removes the managed prefix",
			runtime: db.AgentRuntime{
				Provider: "codex",
				Metadata: []byte(`{"os":"windows","codex_windows_sandbox_config_configured":true}`),
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

	assertArgs := func(w *httptest.ResponseRecorder, status int, want []string) AgentResponse {
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
		return resp
	}

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":                 agentName,
		"runtime_id":           windowsRuntimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
		"custom_args":          []string{"--profile", "research"},
	}))
	createdAgent := assertArgs(w, http.StatusCreated, []string{
		"-c", `windows.sandbox="unelevated"`, "--profile", "research",
	})
	agentID := createdAgent.ID
	if !createdAgent.IsCodexWindowsSandboxArgManaged {
		t.Fatal("created Windows Codex prefix must be marked managed")
	}

	// Runtime deletion intentionally leaves an agent unbound. A custom-args
	// PATCH must remain valid. The current UI sends only editable user args,
	// not the read-only managed pair, and explicitly declares them user-owned.
	if _, err := testPool.Exec(ctx, `UPDATE agent SET runtime_id = NULL WHERE id = $1`, agentID); err != nil {
		t.Fatalf("unbind agent: %v", err)
	}
	unbound := httptest.NewRecorder()
	testHandler.UpdateAgent(unbound, withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
		"custom_args":                          []string{"--profile", "research"},
		"is_codex_windows_sandbox_arg_managed": false,
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

	// Installed clients from before the provenance field echo the visible
	// managed pair on save. Omission must retain its old ownership rather than
	// silently turning it into a user override.
	legacy := httptest.NewRecorder()
	testHandler.UpdateAgent(legacy, withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
		"runtime_id": runtimeOwnedID,
		"custom_args": []string{
			"-c", `windows.sandbox="unelevated"`, "--profile", "research",
		},
	}), "id", agentID))
	legacyAgent := assertArgs(legacy, http.StatusOK, []string{"--profile", "research"})
	if legacyAgent.IsCodexWindowsSandboxArgManaged {
		t.Fatal("profile-owned setting must clear the legacy managed prefix")
	}
	updateRuntime(windowsRuntimeID, []string{
		"-c", `windows.sandbox="unelevated"`, "--profile", "research",
	})

	// A current client pairs its wholesale custom_args replacement with an
	// explicit false provenance hint. Even when byte-for-byte equal to the old
	// managed pair, it becomes user-owned and keeps custom-args precedence over
	// a profile-owned setting.
	explicit := httptest.NewRecorder()
	testHandler.UpdateAgent(explicit, withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
		"runtime_id": runtimeOwnedID,
		"custom_args": []string{
			"-c", `windows.sandbox="unelevated"`, "--profile", "research",
		},
		"is_codex_windows_sandbox_arg_managed": false,
	}), "id", agentID))
	explicitAgent := assertArgs(explicit, http.StatusOK, []string{
		"-c", `windows.sandbox="unelevated"`, "--profile", "research",
	})
	if explicitAgent.IsCodexWindowsSandboxArgManaged {
		t.Fatal("explicit custom_args replacement must become user-owned")
	}

	// A later client cannot manufacture platform provenance by asserting true
	// for a pair that is already proven user-owned.
	untrustedTrue := httptest.NewRecorder()
	testHandler.UpdateAgent(untrustedTrue, withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
		"custom_args": []string{
			"-c", `windows.sandbox="unelevated"`, "--profile", "research",
		},
		"is_codex_windows_sandbox_arg_managed": true,
	}), "id", agentID))
	untrustedTrueAgent := assertArgs(untrustedTrue, http.StatusOK, []string{
		"-c", `windows.sandbox="unelevated"`, "--profile", "research",
	})
	if untrustedTrueAgent.IsCodexWindowsSandboxArgManaged {
		t.Fatal("true hint must not manufacture managed provenance")
	}
	updateRuntime(linuxRuntimeID, []string{
		"-c", `windows.sandbox="unelevated"`, "--profile", "research",
	})
}

func TestAgentRuntimeOnlyUpdatePreservesConcurrentCustomArgs(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const agentName = "custom-args-concurrent-runtime-switch"
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

	linuxRuntimeID := createRuntime("custom-args-concurrent-linux", `{"os":"linux"}`)
	windowsRuntimeID := createRuntime("custom-args-concurrent-windows", `{"os":"windows"}`)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, agentName)
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = ANY($1::uuid[])`, []string{linuxRuntimeID, windowsRuntimeID})
	})

	create := httptest.NewRecorder()
	testHandler.CreateAgent(create, newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":                 agentName,
		"runtime_id":           linuxRuntimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
		"custom_args":          []string{"--profile", "stale"},
	}))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", create.Code, create.Body.String())
	}
	var created AgentResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode created agent: %v", err)
	}

	editorTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer editorTx.Rollback(ctx)

	var editorPID int32
	if err := editorTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&editorPID); err != nil {
		t.Fatalf("read editor backend pid: %v", err)
	}
	concurrentArgs, err := json.Marshal([]string{"--profile", "concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := editorTx.Exec(ctx, `
		UPDATE agent
		SET custom_args = $2::jsonb,
		    is_codex_windows_sandbox_arg_managed = FALSE
		WHERE id = $1
	`, created.ID, string(concurrentArgs)); err != nil {
		t.Fatalf("stage concurrent custom_args edit: %v", err)
	}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+created.ID, map[string]any{
			"runtime_id": windowsRuntimeID,
		}), "id", created.ID)
		testHandler.UpdateAgent(w, req)
		done <- w
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		if err := testPool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE $1 = ANY(pg_blocking_pids(pid))
			)
		`, editorPID).Scan(&blocked); err != nil {
			t.Fatalf("inspect blocked runtime update: %v", err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime-only update never reached the row locked by the concurrent editor")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := editorTx.Commit(ctx); err != nil {
		t.Fatalf("commit concurrent custom_args edit: %v", err)
	}

	var response *httptest.ResponseRecorder
	select {
	case response = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime-only update did not finish after concurrent editor committed")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("runtime update status = %d, want 200: %s", response.Code, response.Body.String())
	}

	var updated AgentResponse
	if err := json.NewDecoder(response.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated agent: %v", err)
	}
	wantArgs := []string{
		"-c", `windows.sandbox="unelevated"`, "--profile", "concurrent",
	}
	if !reflect.DeepEqual(updated.CustomArgs, wantArgs) {
		t.Fatalf("response custom_args = %v, want concurrent edit %v", updated.CustomArgs, wantArgs)
	}
	if !updated.IsCodexWindowsSandboxArgManaged {
		t.Fatal("runtime-only Windows normalization did not retain managed provenance")
	}
	if updated.RuntimeID != windowsRuntimeID {
		t.Fatalf("runtime_id = %s, want %s", updated.RuntimeID, windowsRuntimeID)
	}

	var storedRaw []byte
	var storedManaged bool
	var storedRuntimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT custom_args, is_codex_windows_sandbox_arg_managed, runtime_id
		FROM agent
		WHERE id = $1
	`, created.ID).Scan(&storedRaw, &storedManaged, &storedRuntimeID); err != nil {
		t.Fatalf("read stored agent: %v", err)
	}
	var storedArgs []string
	if err := json.Unmarshal(storedRaw, &storedArgs); err != nil {
		t.Fatalf("decode stored custom_args: %v", err)
	}
	if !reflect.DeepEqual(storedArgs, wantArgs) || !storedManaged || storedRuntimeID != windowsRuntimeID {
		t.Fatalf(
			"stored runtime update = args:%v managed:%v runtime:%s, want args:%v managed:true runtime:%s",
			storedArgs,
			storedManaged,
			storedRuntimeID,
			wantArgs,
			windowsRuntimeID,
		)
	}
}

func TestAgentCustomArgsUpdateUsesRuntimeCommittedWhileWaitingForRowLock(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const managedSandbox = `windows.sandbox="unelevated"`
	tests := []struct {
		name              string
		initialMetadata   string
		targetMetadata    string
		requestArgs       []string
		wantSwitchArgs    []string
		wantSwitchManaged bool
		wantArgs          []string
		wantManaged       bool
	}{
		{
			name:              "linux to windows injects the managed pair",
			initialMetadata:   `{"os":"linux"}`,
			targetMetadata:    `{"os":"windows"}`,
			requestArgs:       []string{"--profile", "concurrent", "--flag"},
			wantSwitchArgs:    []string{"-c", managedSandbox, "--profile", "initial"},
			wantSwitchManaged: true,
			wantArgs:          []string{"-c", managedSandbox, "--profile", "concurrent", "--flag"},
			wantManaged:       true,
		},
		{
			name:              "windows to linux removes the managed pair",
			initialMetadata:   `{"os":"windows"}`,
			targetMetadata:    `{"os":"linux"}`,
			requestArgs:       []string{"-c", managedSandbox, "--profile", "concurrent", "--flag"},
			wantSwitchArgs:    []string{"--profile", "initial"},
			wantSwitchManaged: false,
			wantArgs:          []string{"--profile", "concurrent", "--flag"},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			createRuntime := func(suffix, metadata string) string {
				t.Helper()
				var runtimeID string
				name := fmt.Sprintf("custom-args-lock-%d-%s", i, suffix)
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

			initialRuntimeID := createRuntime("initial", tt.initialMetadata)
			targetRuntimeID := createRuntime("target", tt.targetMetadata)
			agentName := fmt.Sprintf("custom-args-lock-%d-agent", i)
			t.Cleanup(func() {
				if _, err := testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, agentName); err != nil {
					t.Errorf("delete test agent: %v", err)
				}
				if _, err := testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = ANY($1::uuid[])`, []string{initialRuntimeID, targetRuntimeID}); err != nil {
					t.Errorf("delete test runtimes: %v", err)
				}
			})

			create := httptest.NewRecorder()
			testHandler.CreateAgent(create, newRequest(http.MethodPost, "/api/agents", map[string]any{
				"name":                 agentName,
				"runtime_id":           initialRuntimeID,
				"visibility":           "private",
				"max_concurrent_tasks": 1,
				"custom_args":          []string{"--profile", "initial"},
			}))
			if create.Code != http.StatusCreated {
				t.Fatalf("create status = %d, want 201: %s", create.Code, create.Body.String())
			}
			var created AgentResponse
			if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
				t.Fatalf("decode created agent: %v", err)
			}

			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			releaseGate := func() { releaseOnce.Do(func() { close(release) }) }
			defer releaseGate()
			customArgsHandler := *testHandler
			customArgsHandler.TxStarter = &gateAgentArgsUpdateTxStarter{
				delegate: testHandler.TxStarter,
				entered:  entered,
				release:  release,
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				w := httptest.NewRecorder()
				req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+created.ID, map[string]any{
					"custom_args": tt.requestArgs,
				}), "id", created.ID)
				customArgsHandler.UpdateAgent(w, req)
				done <- w
			}()

			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("custom_args update did not reach its transaction after observing runtime A")
			}

			switchResponse := httptest.NewRecorder()
			testHandler.UpdateAgent(switchResponse, withURLParam(newRequest(http.MethodPatch, "/api/agents/"+created.ID, map[string]any{
				"runtime_id": targetRuntimeID,
			}), "id", created.ID))
			if switchResponse.Code != http.StatusOK {
				t.Fatalf("runtime-only switch status = %d, want 200: %s", switchResponse.Code, switchResponse.Body.String())
			}
			var switched AgentResponse
			if err := json.NewDecoder(switchResponse.Body).Decode(&switched); err != nil {
				t.Fatalf("decode runtime-only switch: %v", err)
			}
			if switched.RuntimeID != targetRuntimeID ||
				!reflect.DeepEqual(switched.CustomArgs, tt.wantSwitchArgs) ||
				switched.IsCodexWindowsSandboxArgManaged != tt.wantSwitchManaged {
				t.Fatalf(
					"runtime-only switch = runtime:%s args:%v managed:%v, want runtime:%s args:%v managed:%v",
					switched.RuntimeID,
					switched.CustomArgs,
					switched.IsCodexWindowsSandboxArgManaged,
					targetRuntimeID,
					tt.wantSwitchArgs,
					tt.wantSwitchManaged,
				)
			}
			releaseGate()

			var response *httptest.ResponseRecorder
			select {
			case response = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("custom_args update did not finish after runtime switch committed")
			}
			if response.Code != http.StatusOK {
				t.Fatalf("custom_args update status = %d, want 200: %s", response.Code, response.Body.String())
			}

			var updated AgentResponse
			if err := json.NewDecoder(response.Body).Decode(&updated); err != nil {
				t.Fatalf("decode updated agent: %v", err)
			}
			if updated.RuntimeID != targetRuntimeID ||
				!reflect.DeepEqual(updated.CustomArgs, tt.wantArgs) ||
				updated.IsCodexWindowsSandboxArgManaged != tt.wantManaged {
				t.Fatalf(
					"response = runtime:%s args:%v managed:%v, want runtime:%s args:%v managed:%v",
					updated.RuntimeID,
					updated.CustomArgs,
					updated.IsCodexWindowsSandboxArgManaged,
					targetRuntimeID,
					tt.wantArgs,
					tt.wantManaged,
				)
			}

			var storedRaw []byte
			var storedManaged bool
			var storedRuntimeID string
			if err := testPool.QueryRow(ctx, `
				SELECT custom_args, is_codex_windows_sandbox_arg_managed, runtime_id
				FROM agent
				WHERE id = $1
			`, created.ID).Scan(&storedRaw, &storedManaged, &storedRuntimeID); err != nil {
				t.Fatalf("read stored agent: %v", err)
			}
			var storedArgs []string
			if err := json.Unmarshal(storedRaw, &storedArgs); err != nil {
				t.Fatalf("decode stored custom_args: %v", err)
			}
			if storedRuntimeID != targetRuntimeID || !reflect.DeepEqual(storedArgs, tt.wantArgs) || storedManaged != tt.wantManaged {
				t.Fatalf(
					"stored = runtime:%s args:%v managed:%v, want runtime:%s args:%v managed:%v",
					storedRuntimeID,
					storedArgs,
					storedManaged,
					targetRuntimeID,
					tt.wantArgs,
					tt.wantManaged,
				)
			}
		})
	}
}

func TestAgentCustomArgsUpdateTransactionFailureCanRetry(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const agentName = "custom-args-transaction-retry"
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'codex', 'online', $2, '{"os":"windows"}'::jsonb, now(), $3)
		RETURNING id
	`, testWorkspaceID, agentName+"-runtime", testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, agentName); err != nil {
			t.Errorf("delete test agent: %v", err)
		}
		if _, err := testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID); err != nil {
			t.Errorf("delete test runtime: %v", err)
		}
	})

	create := httptest.NewRecorder()
	testHandler.CreateAgent(create, newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":                 agentName,
		"runtime_id":           runtimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
		"custom_args":          []string{"--profile", "initial"},
	}))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", create.Code, create.Body.String())
	}
	var created AgentResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode created agent: %v", err)
	}

	failingHandler := *testHandler
	failingHandler.TxStarter = &failFirstAgentArgsUpdateCommitTxStarter{delegate: testHandler.TxStarter}
	requestBody := map[string]any{
		"custom_args": []string{"-c", `windows.sandbox="unelevated"`, "--profile", "retry"},
	}
	update := func() *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+created.ID, requestBody), "id", created.ID)
		failingHandler.UpdateAgent(w, req)
		return w
	}

	failed := update()
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("injected commit failure status = %d, want 500: %s", failed.Code, failed.Body.String())
	}
	var afterFailureRaw []byte
	var afterFailureManaged bool
	if err := testPool.QueryRow(ctx, `
		SELECT custom_args, is_codex_windows_sandbox_arg_managed
		FROM agent
		WHERE id = $1
	`, created.ID).Scan(&afterFailureRaw, &afterFailureManaged); err != nil {
		t.Fatalf("read agent after injected failure: %v", err)
	}
	var afterFailureArgs []string
	if err := json.Unmarshal(afterFailureRaw, &afterFailureArgs); err != nil {
		t.Fatalf("decode custom_args after injected failure: %v", err)
	}
	if !reflect.DeepEqual(afterFailureArgs, created.CustomArgs) || afterFailureManaged != created.IsCodexWindowsSandboxArgManaged {
		t.Fatalf(
			"failed update changed args/provenance: args:%v managed:%v, want args:%v managed:%v",
			afterFailureArgs,
			afterFailureManaged,
			created.CustomArgs,
			created.IsCodexWindowsSandboxArgManaged,
		)
	}

	retried := update()
	if retried.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200: %s", retried.Code, retried.Body.String())
	}
	var updated AgentResponse
	if err := json.NewDecoder(retried.Body).Decode(&updated); err != nil {
		t.Fatalf("decode retried agent: %v", err)
	}
	wantArgs := []string{"-c", `windows.sandbox="unelevated"`, "--profile", "retry"}
	if updated.RuntimeID != runtimeID || !updated.IsCodexWindowsSandboxArgManaged || !reflect.DeepEqual(updated.CustomArgs, wantArgs) {
		t.Fatalf(
			"retry response = runtime:%s args:%v managed:%v, want runtime:%s args:%v managed:true",
			updated.RuntimeID,
			updated.CustomArgs,
			updated.IsCodexWindowsSandboxArgManaged,
			runtimeID,
			wantArgs,
		)
	}
}
