package handler

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestCreateAgent_CustomArgs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	claudeID := dbfx.Runtime(t, "custom-args-claude", testutil.Cols{"provider": "claude"})
	ompID := dbfx.Runtime(t, "custom-args-omp", testutil.Cols{"provider": "omp"})
	piID := dbfx.Runtime(t, "custom-args-pi", testutil.Cols{"provider": "pi"})
	for _, tc := range []struct {
		name         string
		runtimeID    string
		args         []string
		want         int
		rejectedFlag string
	}{
		{"permission mode", claudeID, []string{"--permission-mode", "dontAsk", "--tools", "Bash"}, http.StatusBadRequest, "--permission-mode"},
		{"inline permission mode", claudeID, []string{"--permission-mode=dontAsk"}, http.StatusBadRequest, "--permission-mode"},
		{"quoted permission mode", claudeID, []string{"'--permission-mode'", "dontAsk"}, http.StatusBadRequest, "--permission-mode"},
		{"allowed arguments", claudeID, []string{"--tools", "Bash", "--allowedTools", "Bash(multica issue *)"}, http.StatusCreated, ""},
		{"empty arguments", claudeID, []string{}, http.StatusCreated, ""},
		{"omp no session", ompID, []string{"--no-session"}, http.StatusBadRequest, "--no-session"},
		{"omp inline session directory", ompID, []string{"--session-dir=/other"}, http.StatusBadRequest, "--session-dir"},
		{"omp quoted resume", ompID, []string{"'--resume'", "other"}, http.StatusBadRequest, "--resume"},
		{"pi no session", piID, []string{"--no-session"}, http.StatusCreated, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "custom-args-create-" + tc.name
			dbfx.Cleanup(t, `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name)
			w := testutil.Call(t, testHandler.CreateAgent, newRequest(http.MethodPost, "/api/agents", map[string]any{
				"name": name, "runtime_id": tc.runtimeID, "custom_args": tc.args,
			})).Want(tc.want)
			if tc.want == http.StatusBadRequest {
				if !strings.Contains(w.Body.String(), tc.rejectedFlag) {
					t.Fatalf("error must identify the rejected flag: %s", w.Body.String())
				}
				if count := dbfx.Count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name); count != 0 {
					t.Fatalf("rejected create persisted %d agents", count)
				}
			}
		})
	}
}

func TestUpdateAgent_CustomArgs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	claudeID := dbfx.Runtime(t, "custom-args-claude", testutil.Cols{"provider": "claude"})
	codexID := dbfx.Runtime(t, "custom-args-codex", testutil.Cols{"provider": "codex"})
	ompID := dbfx.Runtime(t, "custom-args-omp", testutil.Cols{"provider": "omp"})
	piID := dbfx.Runtime(t, "custom-args-pi", testutil.Cols{"provider": "pi"})
	allowed := []string{"--verbose"}
	blocked := []string{"--permission-mode", "dontAsk"}
	ompBlocked := []string{"--no-session"}
	for _, tc := range []struct {
		name       string
		runtimeID  string
		storedArgs []string
		body       map[string]any
		want       int
		wantArgs   []string
	}{
		{"reject update", claudeID, allowed, map[string]any{"custom_args": blocked}, http.StatusBadRequest, allowed},
		{"reject runtime switch", codexID, blocked, map[string]any{"runtime_id": claudeID}, http.StatusBadRequest, blocked},
		{"switch and clear", codexID, blocked, map[string]any{"runtime_id": claudeID, "custom_args": []string{}}, http.StatusOK, []string{}},
		{"repair existing args", claudeID, blocked, map[string]any{"custom_args": allowed}, http.StatusOK, allowed},
		{"rename existing agent", claudeID, blocked, map[string]any{"name": "custom-args-renamed"}, http.StatusOK, blocked},
		{"reject omp update", ompID, allowed, map[string]any{"custom_args": ompBlocked}, http.StatusBadRequest, allowed},
		{"reject omp runtime switch", piID, ompBlocked, map[string]any{"runtime_id": ompID}, http.StatusBadRequest, ompBlocked},
		{"omp switch and clear", piID, ompBlocked, map[string]any{"runtime_id": ompID, "custom_args": []string{}}, http.StatusOK, []string{}},
		{"repair omp args", ompID, ompBlocked, map[string]any{"custom_args": allowed}, http.StatusOK, allowed},
		{"allow pi session args", piID, allowed, map[string]any{"custom_args": ompBlocked}, http.StatusOK, ompBlocked},
		{"switch from omp to pi", ompID, ompBlocked, map[string]any{"runtime_id": piID}, http.StatusOK, ompBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored, err := json.Marshal(tc.storedArgs)
			if err != nil {
				t.Fatal(err)
			}
			id := dbfx.Agent(t, "custom-args-update-"+tc.name, tc.runtimeID, testutil.Cols{"custom_args": stored})
			testutil.Call(t, testHandler.UpdateAgent, withURLParam(newRequest(http.MethodPut, "/api/agents/"+id, tc.body), "id", id)).Want(tc.want)
			var runtimeID string
			var args []string
			dbfx.QueryRow(t, `SELECT runtime_id::text, custom_args FROM agent WHERE id = $1`, id).Scan(&runtimeID, &args)
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("stored args = %v, want %v", args, tc.wantArgs)
			}
			if tc.want == http.StatusBadRequest && runtimeID != tc.runtimeID {
				t.Fatalf("rejected update changed runtime to %s", runtimeID)
			}
			if tc.want == http.StatusOK && tc.body["runtime_id"] != nil && runtimeID != tc.body["runtime_id"] {
				t.Fatalf("runtime = %s, want %s", runtimeID, tc.body["runtime_id"])
			}
		})
	}
}
