package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
)

func TestEveryOperationHasARouteOrIsHostOnly(t *testing.T) {
	t.Parallel()

	names := map[string]bool{}
	for _, op := range Operations() {
		if op.Confirm {
			t.Errorf("%s still advertises a required user approval", op.Name)
		}
		names[op.Name] = true
		_, api := routes[op.Name]
		if api == hostOnly[op.Name] {
			t.Errorf("%s: api route %v, host only %v", op.Name, api, hostOnly[op.Name])
		}
	}
	for name := range routes {
		if !names[name] {
			t.Errorf("route %s is not in the registry", name)
		}
	}
}

type fakeAPI struct {
	mu     sync.Mutex
	calls  []string
	bodies []any
	reply  map[string]any
}

func (f *fakeAPI) Request(_ context.Context, method, path string, body any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := method + " " + path
	f.calls = append(f.calls, key)
	f.bodies = append(f.bodies, body)
	if v, ok := f.reply[key]; ok {
		if err, ok := v.(error); ok {
			return nil, err
		}
		return v, nil
	}
	return nil, &contextmcp.OperationError{Code: "not_found", Message: "missing " + key, Status: 404}
}

func (f *fakeAPI) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == key {
			n++
		}
	}
	return n
}

type memReceipts struct {
	mu   sync.Mutex
	rows map[string]*Receipt
}

func (m *memReceipts) Claim(_ context.Context, key, fingerprint string) (*Receipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.rows[key]; r != nil {
		copy := *r
		return &copy, nil
	}
	m.rows[key] = &Receipt{Fingerprint: fingerprint}
	return nil, nil
}

func (m *memReceipts) Finish(_ context.Context, key, result string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[key].Result = result
	return nil
}

type hostCall struct {
	kind    string
	payload map[string]any
}

func setup(t *testing.T, reply map[string]any, host func(kind string, payload map[string]any) (any, error)) (map[string]contextmcp.Tool, *fakeAPI, *[]hostCall) {
	t.Helper()
	api := &fakeAPI{reply: reply}
	var calls []hostCall
	var mu sync.Mutex
	opts := Options{Scope: Scope{WorkspaceID: "ws-1", UserID: "u-1"}, Request: api, Receipts: &memReceipts{rows: map[string]*Receipt{}}}
	if host != nil {
		opts.Host = func(_ context.Context, kind string, payload any) (json.RawMessage, error) {
			data, _ := json.Marshal(payload)
			var p map[string]any
			_ = json.Unmarshal(data, &p)
			mu.Lock()
			calls = append(calls, hostCall{kind, p})
			mu.Unlock()
			v, err := host(kind, p)
			if err != nil {
				return nil, err
			}
			out, _ := json.Marshal(v)
			return out, nil
		}
	}
	tools := map[string]contextmcp.Tool{}
	for _, tool := range Tools(opts) {
		tools[tool.Name] = tool
	}
	return tools, api, &calls
}

func run(t *testing.T, tools map[string]contextmcp.Tool, name, args string) (any, error) {
	t.Helper()
	tool, ok := tools[name]
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	res, err := tool.Call(context.Background(), json.RawMessage(args))
	return res.Value, err
}

func TestWritesDoNotRequireDesktopConfirmation(t *testing.T) {
	t.Parallel()
	for _, withHost := range []bool{false, true} {
		t.Run(fmt.Sprint(withHost), func(t *testing.T) {
			var host func(string, map[string]any) (any, error)
			if withHost {
				host = func(kind string, _ map[string]any) (any, error) {
					if kind != "settings.refresh" {
						t.Fatalf("unexpected host request: %s", kind)
					}
					return map[string]any{}, nil
				}
			}
			tools, api, _ := setup(t, map[string]any{
				"POST /api/workspaces/ws-1/members": map[string]any{"email": "a@example.com"},
			}, host)
			args := `{"email":"a@example.com","request_id":"invite-1"}`
			for range 2 {
				v, err := run(t, tools, "settings_invite_member", args)
				if err != nil || asObject(v)["status"] != "applied" {
					t.Fatalf("invite = %v, %v", v, err)
				}
			}
			if api.count("POST /api/workspaces/ws-1/members") != 1 {
				t.Fatal("write was not executed exactly once")
			}
			if _, err := run(t, tools, "settings_invite_member", `{"email":"a@example.com","approved":true,"request_id":"invite-2"}`); err == nil {
				t.Fatal("accepted an unknown approval field")
			}
		})
	}
}

func TestUnavailableRefreshDoesNotFailDirectWrite(t *testing.T) {
	t.Parallel()
	tools, _, _ := setup(t, map[string]any{
		"POST /api/workspaces/ws-1/members": map[string]any{"email": "a@example.com"},
	}, func(kind string, _ map[string]any) (any, error) {
		if kind != "settings.refresh" {
			t.Fatalf("unexpected host call: %s", kind)
		}
		return nil, errors.New("This operation requires Multica Desktop")
	})
	v, err := run(t, tools, "settings_invite_member", `{"email":"a@example.com","request_id":"mobile-1"}`)
	if err != nil || asObject(v)["status"] != "applied" || asObject(v)["ui_refresh"] != "pending" {
		t.Fatalf("mobile write = %v, %v", v, err)
	}
}

func TestWorkflowPreviewFailureStopsApply(t *testing.T) {
	t.Parallel()
	tools, api, _ := setup(t, map[string]any{
		"GET /api/projects/p1":           map[string]any{"id": "p1", "workspace_id": "ws-1"},
		"POST /api/projects/p1/workflow": &contextmcp.OperationError{Code: "workflow_status_mapping_required", Message: "Status mapping required", Status: 409},
	}, nil)
	_, err := run(t, tools, "settings_set_project_workflow", `{"project_id":"p1","workflow_id":"wf1","dry_run":false,"request_id":"mapping-1"}`)
	var rejected *OperationError
	if !errors.As(err, &rejected) || rejected.Code != "workflow_status_mapping_required" {
		t.Fatalf("preview error = %v", err)
	}
	if api.count("POST /api/projects/p1/workflow") != 1 || asObject(api.bodies[1])["dry_run"] != true {
		t.Fatal("applied after a failed preview")
	}
}

func TestDirectWritesPreservePermissionErrors(t *testing.T) {
	t.Parallel()
	tools, api, _ := setup(t, map[string]any{
		"POST /api/workspaces/ws-1/members": &contextmcp.OperationError{Code: "forbidden", Message: "Admin required", Status: 403},
	}, nil)
	for range 2 {
		_, err := run(t, tools, "settings_invite_member", `{"email":"a@example.com","request_id":"denied-1"}`)
		var rejected *OperationError
		if !errors.As(err, &rejected) || rejected.Status != 403 {
			t.Fatalf("permission error = %v", err)
		}
	}
	if api.count("POST /api/workspaces/ws-1/members") != 1 {
		t.Fatal("replayed a rejected write")
	}
}

func TestTokenCreationStaysOnDesktop(t *testing.T) {
	t.Parallel()
	tools, api, _ := setup(t, nil, func(kind string, payload map[string]any) (any, error) {
		if kind != "settings.confirm" || payload["operation"] != "settings_create_token" {
			t.Fatalf("unexpected host call: %s %v", kind, payload)
		}
		return map[string]any{"approved": true, "raw": map[string]any{"id": "token-1"}}, nil
	})
	v, err := run(t, tools, "settings_create_token", `{"name":"CI","request_id":"token-1"}`)
	if err != nil || asObject(v)["secret_displayed_to_user"] != true || asObject(v)["id"] != "token-1" {
		t.Fatalf("token = %v, %v", v, err)
	}
	if api.count("POST /api/tokens") != 0 {
		t.Fatal("daemon received the raw token")
	}
}

func TestWorkflowApplyDoesNotWaitForConfirmation(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"settings_set_project_workflow", "settings_update_workflow"} {
		for _, affected := range []int{0, 3} {
			t.Run(fmt.Sprintf("%s/%d", op, affected), func(t *testing.T) {
				path := "/api/projects/p1/workflow"
				beforePath := "/api/projects/p1"
				args := `{"project_id":"p1","workflow_id":"wf1","dry_run":false,"request_id":"workflow-1"}`
				beforeID := "p1"
				method := "POST "
				if op == "settings_update_workflow" {
					path, beforePath, method = "/api/issue-workflows/wf1", "/api/issue-workflows/wf1", "PATCH "
					args = `{"id":"wf1","fields":{"name":"Updated"},"request_id":"workflow-1"}`
					beforeID = "wf1"
				}
				tools, api, _ := setup(t, map[string]any{
					"GET " + beforePath: map[string]any{"id": beforeID, "workspace_id": "ws-1"},
					method + path:       map[string]any{"plan": map[string]any{"affected_issues": float64(affected)}},
				}, nil)
				v, err := run(t, tools, op, args)
				if err != nil || asObject(v)["status"] != "applied" {
					t.Fatalf("apply = %v, %v", v, err)
				}
				if api.count(method+path) != 2 {
					t.Fatalf("expected preview and apply, got %v", api.calls)
				}
				if asObject(api.bodies[1])["dry_run"] != true {
					t.Fatalf("missing preview: %v", api.bodies)
				}
				if asObject(api.bodies[2])["dry_run"] == true {
					t.Fatalf("only previewed: %v", api.bodies)
				}
			})
		}
	}
}

func TestDeviceActionDoesNotRequestApproval(t *testing.T) {
	t.Parallel()
	tools, _, calls := setup(t, nil, func(kind string, payload map[string]any) (any, error) {
		if kind != "settings.host" || payload["operation"] != "settings_device_action" {
			t.Fatalf("unexpected host call: %s %v", kind, payload)
		}
		return map[string]any{"status": "applied"}, nil
	})
	v, err := run(t, tools, "settings_device_action", `{"action":"daemon_restart","request_id":"device-1"}`)
	if err != nil || asObject(v)["status"] != "applied" || len(*calls) != 1 {
		t.Fatalf("device action = %v, %v (%v)", v, err, *calls)
	}
}

func TestWritesRunOncePerRequestID(t *testing.T) {
	t.Parallel()

	tools, api, _ := setup(t, map[string]any{
		"POST /api/labels": map[string]any{"id": "l1", "workspace_id": "ws-1", "name": "bug"},
		"PATCH /api/me":    errors.New("connection reset"),
		"GET /api/me":      map[string]any{"id": "u-1"},
	}, nil)
	args := `{"name":"bug","color":"#ff0000","request_id":"x1"}`
	first, err := run(t, tools, "settings_create_label", args)
	if err != nil {
		t.Fatal(err)
	}
	again, err := run(t, tools, "settings_create_label", args)
	if err != nil || api.count("POST /api/labels") != 1 || again.(map[string]any)["status"] != first.(map[string]any)["status"] {
		t.Fatalf("retry = %v, %v (posts %d)", again, err, api.count("POST /api/labels"))
	}
	if _, err := run(t, tools, "settings_create_label", `{"name":"other","color":"#ff0000","request_id":"x1"}`); err == nil || !strings.Contains(err.Error(), "different input") {
		t.Fatalf("conflict = %v", err)
	}
	profile := `{"fields":{"name":"Me"},"request_id":"p1"}`
	if _, err := run(t, tools, "settings_update_profile", profile); err == nil {
		t.Fatal("transport failure reported as success")
	}
	if _, err := run(t, tools, "settings_update_profile", profile); err == nil || !strings.Contains(err.Error(), "already attempted") {
		t.Fatalf("unknown outcome replayed: %v", err)
	}
	if api.count("PATCH /api/me") != 1 {
		t.Fatal("an uncertain write was repeated")
	}
	if _, err := run(t, tools, "settings_update_profile", `{"fields":{},"request_id":"p2"}`); err == nil {
		t.Fatal("accepted an empty patch")
	}
	if _, err := run(t, tools, "settings_update_profile", `{"fields":{"timezone":"Mars/Base"},"request_id":"p3"}`); err == nil {
		t.Fatal("accepted an unknown time zone")
	}
}

func TestCodeSettingsMergeAndScopeChecks(t *testing.T) {
	t.Parallel()

	tools, api, _ := setup(t, map[string]any{
		"GET /api/workspaces/ws-1":   map[string]any{"id": "ws-1", "settings": map[string]any{"github_enabled": true, "other": 1}},
		"PATCH /api/workspaces/ws-1": map[string]any{"id": "ws-1"},
		"GET /api/tokens":            []any{map[string]any{"id": "t1", "name": "ci", "token_prefix": "mul_", "token": "secret"}},
	}, nil)
	if _, err := run(t, tools, "settings_update_code_settings", `{"fields":{"co_authored_by_enabled":false},"request_id":"c1"}`); err != nil {
		t.Fatal(err)
	}
	body := api.bodies[len(api.bodies)-1].(map[string]any)["settings"].(map[string]any)
	if body["github_enabled"] != true || body["other"] != float64(1) && body["other"] != 1 || body["co_authored_by_enabled"] != false {
		t.Fatalf("merged settings = %v", body)
	}
	tokens, err := run(t, tools, "settings_list_tokens", `{}`)
	if err != nil || strings.Contains(mustJSON(tokens), "secret") {
		t.Fatalf("tokens = %v, %v", tokens, err)
	}
	if _, ok := tools["settings_read_device"]; ok {
		t.Fatal("device operations offered without Desktop")
	}
	if _, ok := tools["settings_create_token"]; ok {
		t.Fatal("token creation offered without Desktop to show the secret")
	}
	_ = http.MethodGet
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
