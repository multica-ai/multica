package runtime

// CEREBRO-PATCH(memory-tools): FIR-1794 — unit tests for the memory tool gate
// resolution (fail closed on every path), the offering subsets, and the
// server-side identity stamping (a spoofed subject_id can never reach the
// memory service).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type fakeMemoryGateQuerier struct {
	flags       []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow
	flagsErr    error
	capability  bool
	capErr      error
	settings    cerebrodb.GetUserAgentMemorySettingsRow
	settingsErr error
}

func (f *fakeMemoryGateQuerier) ListCerebroWorkspaceFeatureFlags(ctx context.Context, workspaceID pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error) {
	return f.flags, f.flagsErr
}

func (f *fakeMemoryGateQuerier) HasCerebroCapability(ctx context.Context, arg cerebrodb.HasCerebroCapabilityParams) (bool, error) {
	return f.capability, f.capErr
}

func (f *fakeMemoryGateQuerier) GetUserAgentMemorySettings(ctx context.Context, arg cerebrodb.GetUserAgentMemorySettingsParams) (cerebrodb.GetUserAgentMemorySettingsRow, error) {
	return f.settings, f.settingsErr
}

func memTestUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}

const (
	memTestWorkspace = "11111111-1111-1111-1111-111111111111"
	memTestAgent     = "22222222-2222-2222-2222-222222222222"
	memTestUser      = "33333333-3333-3333-3333-333333333333"
)

func memFlagOn() []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow {
	return []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{{FlagKey: cerebroMemoryFlagKey, Enabled: true}}
}

func TestResolveCerebroMemoryGatesFailClosed(t *testing.T) {
	ctx := context.Background()
	ws := memTestUUID(t, memTestWorkspace)
	agent := memTestUUID(t, memTestAgent)
	user := memTestUUID(t, memTestUser)

	cases := []struct {
		name string
		q    memoryGateQuerier
		user pgtype.UUID
		want memoryGates
	}{
		{name: "nil querier", q: nil, user: user, want: memoryGates{}},
		{name: "flags lookup error", q: &fakeMemoryGateQuerier{flagsErr: errors.New("db down")}, user: user, want: memoryGates{}},
		{name: "flag absent", q: &fakeMemoryGateQuerier{}, user: user, want: memoryGates{}},
		{
			name: "flag off",
			q:    &fakeMemoryGateQuerier{flags: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{{FlagKey: cerebroMemoryFlagKey, Enabled: false}}},
			user: user,
			want: memoryGates{},
		},
		{
			name: "flag on but no originating user",
			q:    &fakeMemoryGateQuerier{flags: memFlagOn(), capability: true, settings: cerebrodb.GetUserAgentMemorySettingsRow{CanReadMemory: true, CanWriteMemory: true}},
			user: pgtype.UUID{},
			want: memoryGates{FlagOn: true},
		},
		{
			name: "capability denied",
			q:    &fakeMemoryGateQuerier{flags: memFlagOn(), capability: false},
			user: user,
			want: memoryGates{FlagOn: true},
		},
		{
			name: "capability lookup error",
			q:    &fakeMemoryGateQuerier{flags: memFlagOn(), capability: true, capErr: errors.New("db down")},
			user: user,
			want: memoryGates{FlagOn: true},
		},
		{
			name: "settings absent = default off",
			q:    &fakeMemoryGateQuerier{flags: memFlagOn(), capability: true, settingsErr: pgx.ErrNoRows},
			user: user,
			want: memoryGates{FlagOn: true, HasCapability: true},
		},
		{
			name: "settings lookup error",
			q:    &fakeMemoryGateQuerier{flags: memFlagOn(), capability: true, settingsErr: errors.New("db down")},
			user: user,
			want: memoryGates{FlagOn: true, HasCapability: true},
		},
		{
			name: "all gates open",
			q:    &fakeMemoryGateQuerier{flags: memFlagOn(), capability: true, settings: cerebrodb.GetUserAgentMemorySettingsRow{CanReadMemory: true, CanWriteMemory: true}},
			user: user,
			want: memoryGates{FlagOn: true, HasCapability: true, CanRead: true, CanWrite: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveCerebroMemoryGates(ctx, tc.q, ws, agent, tc.user)
			if got != tc.want {
				t.Errorf("gates = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCerebroMemoryToolsForTaskOffering(t *testing.T) {
	ctx := context.Background()
	tctx := ToolContext{WorkspaceID: memTestUUID(t, memTestWorkspace), AgentID: memTestUUID(t, memTestAgent)}
	user := memTestUUID(t, memTestUser)

	names := func(tools []Tool) []string {
		out := make([]string, 0, len(tools))
		for _, tl := range tools {
			out = append(out, tl.Name())
		}
		return out
	}

	t.Run("flag off offers nothing", func(t *testing.T) {
		got := CerebroMemoryToolsForTask(ctx, &fakeMemoryGateQuerier{}, tctx, user)
		if len(got) != 0 {
			t.Errorf("offered %v, want none", names(got))
		}
	})

	t.Run("flag on without write offers recall only", func(t *testing.T) {
		q := &fakeMemoryGateQuerier{flags: memFlagOn(), capability: true, settings: cerebrodb.GetUserAgentMemorySettingsRow{CanReadMemory: true}}
		got := names(CerebroMemoryToolsForTask(ctx, q, tctx, user))
		if len(got) != 1 || got[0] != "memory_recall" {
			t.Errorf("offered %v, want [memory_recall]", got)
		}
	})

	t.Run("write switch offers remember and delete", func(t *testing.T) {
		q := &fakeMemoryGateQuerier{flags: memFlagOn(), capability: true, settings: cerebrodb.GetUserAgentMemorySettingsRow{CanReadMemory: true, CanWriteMemory: true}}
		got := names(CerebroMemoryToolsForTask(ctx, q, tctx, user))
		want := map[string]bool{"memory_recall": true, "memory_remember": true, "memory_delete": true}
		if len(got) != 3 {
			t.Fatalf("offered %v, want all three memory tools", got)
		}
		for _, n := range got {
			if !want[n] {
				t.Errorf("unexpected tool %q in %v", n, got)
			}
		}
	})
}

// memoryServiceCapture spins an httptest MCP endpoint that records every
// tools/call argument object and answers with a fixed text result.
func memoryServiceCapture(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var calls []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		calls = append(calls, req.Params.Arguments)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func memToolBase(t *testing.T, srvURL string, q memoryGateQuerier) cerebroMemoryToolBase {
	t.Helper()
	return cerebroMemoryToolBase{
		cerebro: q,
		tctx:    ToolContext{WorkspaceID: memTestUUID(t, memTestWorkspace), AgentID: memTestUUID(t, memTestAgent)},
		origin:  memTestUUID(t, memTestUser),
		cfg:     cogneeMemoryConfig{URL: srvURL, BearerToken: "test-token"},
	}
}

func allGatesOpen() *fakeMemoryGateQuerier {
	return &fakeMemoryGateQuerier{
		flags:      memFlagOn(),
		capability: true,
		settings:   cerebrodb.GetUserAgentMemorySettingsRow{CanReadMemory: true, CanWriteMemory: true},
	}
}

func TestMemoryRememberStampsIdentity(t *testing.T) {
	srv, calls := memoryServiceCapture(t)
	tool := &CerebroMemoryRememberTool{memToolBase(t, srv.URL, allGatesOpen())}

	// The model tries to spoof someone else's memory — the schema has no
	// subject_id, and even a smuggled one must be discarded.
	_, err := tool.Call(context.Background(), map[string]any{
		"text":       "Jesper prefers plain Danish",
		"subject_id": "someone-else",
		"scope":      "private",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("service calls = %d, want 1", len(*calls))
	}
	got := (*calls)[0]
	wantSubject := "user-" + memTestUser + "-agent-" + memTestAgent
	if got["subject_id"] != wantSubject {
		t.Errorf("subject_id = %v, want stamped %q", got["subject_id"], wantSubject)
	}
	if got["scope"] != "private" {
		t.Errorf("scope = %v, want private", got["scope"])
	}
}

func TestMemoryRememberCompanyScope(t *testing.T) {
	srv, calls := memoryServiceCapture(t)
	tool := &CerebroMemoryRememberTool{memToolBase(t, srv.URL, allGatesOpen())}
	if _, err := tool.Call(context.Background(), map[string]any{"text": "fact", "scope": "company"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	got := (*calls)[0]
	if got["scope"] != "company" {
		t.Errorf("scope = %v, want company", got["scope"])
	}
	if got["subject_id"] != "workspace-"+memTestWorkspace {
		t.Errorf("subject_id = %v, want workspace-scoped", got["subject_id"])
	}
}

func TestMemoryRememberDeniedWithoutWrite(t *testing.T) {
	srv, calls := memoryServiceCapture(t)
	q := &fakeMemoryGateQuerier{flags: memFlagOn(), capability: true, settings: cerebrodb.GetUserAgentMemorySettingsRow{CanReadMemory: true}}
	tool := &CerebroMemoryRememberTool{memToolBase(t, srv.URL, q)}
	if _, err := tool.Call(context.Background(), map[string]any{"text": "fact"}); err == nil {
		t.Fatal("Call succeeded, want write-switch denial")
	}
	if len(*calls) != 0 {
		t.Errorf("service was called %d times despite denial", len(*calls))
	}
}

func TestMemoryRecallAllMergesPermittedStores(t *testing.T) {
	srv, calls := memoryServiceCapture(t)
	tool := &CerebroMemoryRecallTool{memToolBase(t, srv.URL, allGatesOpen())}
	out, err := tool.Call(context.Background(), map[string]any{"query": "what do we know"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("service calls = %d, want 2 (private + company)", len(*calls))
	}
	scopes := []any{(*calls)[0]["scope"], (*calls)[1]["scope"]}
	if scopes[0] != "private" || scopes[1] != "company" {
		t.Errorf("scopes = %v, want [private company]", scopes)
	}
	if !strings.Contains(out, "[private]") || !strings.Contains(out, "[company]") {
		t.Errorf("output missing store labels: %q", out)
	}
}

func TestMemoryRecallWithoutReadFallsBackToCompany(t *testing.T) {
	srv, calls := memoryServiceCapture(t)
	q := &fakeMemoryGateQuerier{flags: memFlagOn()}
	tool := &CerebroMemoryRecallTool{memToolBase(t, srv.URL, q)}

	if _, err := tool.Call(context.Background(), map[string]any{"query": "q"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0]["scope"] != "company" {
		t.Fatalf("calls = %+v, want a single company recall", *calls)
	}

	if _, err := tool.Call(context.Background(), map[string]any{"query": "q", "scope": "private"}); err == nil {
		t.Error("private recall succeeded without the read switch")
	}
}

func TestMemoryDeletePassesStampedOwnership(t *testing.T) {
	srv, calls := memoryServiceCapture(t)
	tool := &CerebroMemoryDeleteTool{memToolBase(t, srv.URL, allGatesOpen())}
	if _, err := tool.Call(context.Background(), map[string]any{"memory_id": "mem-1"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	got := (*calls)[0]
	if got["memory_id"] != "mem-1" {
		t.Errorf("memory_id = %v", got["memory_id"])
	}
	if got["scope"] != "private" || got["subject_id"] != "user-"+memTestUser+"-agent-"+memTestAgent {
		t.Errorf("ownership stamp = scope %v subject %v", got["scope"], got["subject_id"])
	}
}
