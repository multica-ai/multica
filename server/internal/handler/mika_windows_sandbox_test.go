package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

func TestCreateMikaAgentPersistsWindowsCodexSandboxArgs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`DELETE FROM agent WHERE workspace_id = $1 AND system_key = $2`,
		testWorkspaceID, service.MikaSystemKey,
	); err != nil {
		t.Fatalf("remove existing Mika: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id
		)
		VALUES ($1, NULL, 'mika-windows-codex', 'cloud', 'codex', 'online',
			'mika-windows-codex', '{"os":"windows"}'::jsonb, now(), $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create Windows Codex runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	cleanupMika(t)

	w := createMika(t, map[string]any{
		"runtime_id": runtimeID,
		"language":   "en",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create Mika: status = %d, want 201: %s", w.Code, w.Body.String())
	}
	resp := decodeAgent(t, w)

	var rawArgs []byte
	var managed bool
	if err := testPool.QueryRow(ctx,
		`SELECT custom_args, is_codex_windows_sandbox_arg_managed FROM agent WHERE id = $1`,
		resp.ID,
	).Scan(&rawArgs, &managed); err != nil {
		t.Fatalf("load persisted Mika args: %v", err)
	}
	var args []string
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		t.Fatalf("decode persisted Mika args: %v", err)
	}
	want := []string{"-c", `windows.sandbox="unelevated"`}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("persisted custom_args = %v, want %v", args, want)
	}
	if !managed {
		t.Fatal("persisted Mika sandbox prefix must be marked managed")
	}
}
