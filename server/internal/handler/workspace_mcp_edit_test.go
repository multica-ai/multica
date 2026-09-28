package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestMcpEditProjection(t *testing.T) {
	for _, declared := range []string{"http", "HTTP", "innocent-secret", ""} {
		config, _ := json.Marshal(map[string]any{"type": declared, "url": workspaceMcpTestSecret, "env": map[string]string{workspaceMcpTestSecret: workspaceMcpTestSecret}, "command": workspaceMcpTestSecret, "args": []string{workspaceMcpTestSecret}, "headers": map[string]string{"ordinary": workspaceMcpTestSecret}, workspaceMcpTestSecret: "value"})
		out := mcpEditableProjection(db.WorkspaceMcpServer{Config: config, Revision: 1})
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), workspaceMcpTestSecret) || strings.Contains(string(raw), "innocent-secret") {
			t.Fatal("projection disclosed protected data")
		}
		if out.Revision != "1" || !out.HasOpaqueFields || len(out.ProtectedFields) != 5 {
			t.Fatalf("incorrect projection: %+v", out)
		}
		if (out.VisibleConfig["type"] == "http") != (declared == "http") {
			t.Fatal("type must be an exact allowlisted enum")
		}
	}
}

func TestMcpEditMerge(t *testing.T) {
	stored := []byte(`{"type":"http","url":"secret-url","env":{"KEEP":"secret","REMOVE":"old"},"custom":{"n":9007199254740993},"a/b":{"~key":1},"args":["a","b"]}`)
	patch, err := decodeMcpPatch([]byte(`{"operations":[{"op":"set","path":"/env/NEW","value":null},{"op":"remove","path":"/env/REMOVE"},{"op":"set","path":"/a~1b/~0key","value":2},{"op":"set","path":"/args","value":["c"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := applyMcpPatch(stored, patch)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{`"url":"secret-url"`, `"KEEP":"secret"`, `"NEW":null`, `9007199254740993`, `"~key":2`, `"args":["c"]`} {
		if !strings.Contains(string(out), wanted) {
			t.Fatalf("missing %s in %s", wanted, out)
		}
	}
	if strings.Contains(string(out), "REMOVE") {
		t.Fatal("remove was not applied")
	}
	if !strings.Contains(string(stored), "REMOVE") {
		t.Fatal("mutated original config")
	}
}

func TestMcpEditRejectsInvalidInput(t *testing.T) {
	cases := []string{
		`null`, `{"operations":null}`, `{"operations":[],"extra":1}`,
		`{"operations":[],"operations":[]}`, `{"operations":[]} {}`,
		`{"operations":[{"op":"set","path":"/a","value":{"x":1,"x":2}}]}`,
		`{"operations":[{"op":"test","path":"/url","value":"secret"}]}`,
		`{"operations":[{"op":"copy","path":"/type","from":"/url"}]}`,
		`{"operations":[{"op":"move","path":"/type"}]}`,
		`{"operations":[{"op":"set","path":"" ,"value":{}}]}`,
		`{"operations":[{"op":"set","path":"/bad~2", "value":1}]}`,
		`{"operations":[{"op":"set","path":"/a"}]}`,
		`{"operations":[{"op":"remove","path":"/url","value":null}]}`,
		`{"operations":[{"op":"remove","path":"/missing"}]}`,
		`{"operations":[{"op":"set","path":"/args/0","value":"a"}]}`,
		`{"operations":[{"op":"set","path":"/missing/child","value":"a"}]}`,
		`{"operations":[{"op":"set","path":"/env","value":{}},{"op":"set","path":"/env/X","value":1}]}`,
		`{"operations":[{"op":"remove","path":"/url"},{"op":"remove","path":"/url"}]}`,
		`{"operations":[{"op":"set","path":"/x","value":` + strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40) + `}]}`,
		`{"operations":[` + strings.Repeat(`{"op":"remove","path":"/x"},`, 64) + `{"op":"remove","path":"/x"}]}`,
		strings.Repeat(" ", mcpEditMaxBytes+1),
	}
	for i, raw := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			patch, err := decodeMcpPatch([]byte(raw))
			if err == nil {
				_, err = applyMcpPatch([]byte(`{"url":"secret","args":[],"env":{}}`), patch)
			}
			if err == nil {
				t.Fatal("accepted invalid changes")
			}
			if err.Error() != "invalid configuration changes" {
				t.Fatal("error disclosed input")
			}
		})
	}
}

func mcpEditRequest(method, id, user, body, etag string) *http.Request {
	req := testutil.JSONRequest(method, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+id, body)
	req = testutil.WithURLParams(req, "id", testWorkspaceID, "serverId", id)
	req.Header.Set("X-User-ID", user)
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	return req
}

func TestMcpEditRoutesNeverEchoSecrets(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	secret := workspaceMcpTestSecret
	config, _ := json.Marshal(map[string]any{"type": secret, "url": secret, "command": secret, "args": []string{secret}, "env": map[string]string{secret: secret}, "headers": map[string]string{secret: secret}, secret: map[string]string{"innocent": secret}})
	caller := dbfx.Agent(t, "mcp-safe-prefill-actor", "")
	taskID := insertHandlerTestTask(t, caller)
	for _, role := range []string{"owner", "admin", "member", "agent"} {
		t.Run(role, func(t *testing.T) {
			user := testUserID
			if role == "admin" || role == "member" {
				user = dbfx.User(t, "mcp-"+role, "mcp-"+role+"@example.test")
				dbfx.Member(t, testWorkspaceID, user, role)
			}
			id := dbfx.Insert(t, "workspace_mcp_server", testutil.Cols{"workspace_id": testWorkspaceID, "name": "safe-" + role, "config": string(config)})
			for _, tc := range []struct {
				name, method, body string
				status             int
			}{
				{"get", "GET", "", 200},
				{"patch", "PATCH", `{"operations":[{"op":"set","path":"/url","value":"` + secret + `"}]}`, 200},
				{"malformed-path", "PATCH", `{"operations":[{"op":"set","path":"/` + secret + `~2","value":"` + secret + `"}]}`, 400},
				{"oversized", "PATCH", `{"operations":[{"op":"set","path":"/url","value":"` + strings.Repeat(secret, 3000) + `"}]}`, 400},
			} {
				t.Run(tc.name, func(t *testing.T) {
					row, err := testHandler.Queries.GetWorkspaceMcpServer(context.Background(), db.GetWorkspaceMcpServerParams{ID: parseUUID(id), WorkspaceID: parseUUID(testWorkspaceID)})
					if err != nil {
						t.Fatal(err)
					}
					req := mcpEditRequest(tc.method, id, user, tc.body, strconv.Quote(strconv.FormatInt(row.Revision, 10)))
					want := tc.status
					if role == "agent" {
						req.Header.Set("X-Actor-Source", "task_token")
						req.Header.Set("X-Agent-ID", caller)
						req.Header.Set("X-Task-ID", taskID)
					}
					if role == "member" || role == "agent" {
						want = 403
					}
					h := testHandler.GetWorkspaceMcpEditableConfig
					if tc.method == "PATCH" {
						h = testHandler.PatchWorkspaceMcpConfig
					}
					response := testutil.Call(t, h, req).Want(want)
					if strings.Contains(response.Body.String(), secret) {
						t.Fatal("response leaked stored or submitted secret")
					}
					if response.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("missing no-store")
					}
					if want == 200 {
						var view WorkspaceMcpEditableConfig
						response.JSON(&view)
						if response.Header().Get("ETag") != strconv.Quote(view.Revision) {
							t.Fatal("ETag/revision mismatch")
						}
					}
				})
			}
		})
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatal("audit logs disclosed a secret")
	}
}

func TestMcpEditConcurrentWriters(t *testing.T) {
	id := dbfx.Insert(t, "workspace_mcp_server", testutil.Cols{"workspace_id": testWorkspaceID, "name": "safe-concurrent", "config": `{"url":"secret"}`})
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, name := range []string{"writer-one", "writer-two"} {
		go func(name string) {
			<-start
			req := mcpEditRequest("PATCH", id, testUserID, `{"name":"`+name+`","operations":[]}`, `"1"`)
			w := httptest.NewRecorder()
			testHandler.PatchWorkspaceMcpConfig(w, req)
			results <- w.Code
		}(name)
	}
	close(start)
	one, two := <-results, <-results
	if !((one == 200 && two == 412) || (one == 412 && two == 200)) {
		t.Fatalf("concurrent statuses = %d, %d", one, two)
	}
}

func TestMcpEditRevisionMigrationBackfills(t *testing.T) {
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// An isolated schema exercises the actual migration against preexisting
	// rows without altering the shared test fixture's live table.
	if _, err := tx.Exec(ctx, `CREATE SCHEMA mcp_revision_migration_test; SET LOCAL search_path TO mcp_revision_migration_test; CREATE TABLE workspace_mcp_server (name text, config jsonb); INSERT INTO workspace_mcp_server VALUES ('existing', '{}')`); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../migrations/564_workspace_mcp_revision.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(raw)); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM workspace_mcp_server`).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("backfill = %d, error = %v", revision, err)
	}
	if err := tx.QueryRow(ctx, `UPDATE workspace_mcp_server SET name='legacy-write' RETURNING revision`).Scan(&revision); err != nil || revision != 2 {
		t.Fatalf("legacy write revision = %d, error = %v", revision, err)
	}
	if _, err := tx.Exec(ctx, `SAVEPOINT invalid_revision`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workspace_mcp_server (name, config, revision) VALUES ('bad', '{}', 0)`); err == nil {
		t.Fatal("accepted zero revision")
	}
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT invalid_revision`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workspace_mcp_server (name, config, revision) VALUES ('bad', '{}', NULL)`); err == nil {
		t.Fatal("accepted null revision")
	}
}

func TestMcpEditRevisionAndAtomicity(t *testing.T) {
	id := dbfx.Insert(t, "workspace_mcp_server", testutil.Cols{"workspace_id": testWorkspaceID, "name": "safe-atomic", "config": `{"url":"keep-secret","env":{"KEEP":"keep-secret"},"custom":9007199254740993}`})
	get := func() WorkspaceMcpEditableConfig {
		var view WorkspaceMcpEditableConfig
		testutil.Call(t, testHandler.GetWorkspaceMcpEditableConfig, mcpEditRequest("GET", id, testUserID, "", "")).Want(200).JSON(&view)
		return view
	}
	if get().Revision != "1" {
		t.Fatal("new entries must start at revision 1")
	}
	for _, tag := range []string{"", `1`, `"01"`, `W/"1"`, `*`, `"0"`, `"1", "2"`} {
		want := 412
		if tag == "" {
			want = 428
		}
		testutil.Call(t, testHandler.PatchWorkspaceMcpConfig, mcpEditRequest("PATCH", id, testUserID, `{"operations":[]}`, tag)).Want(want)
	}
	// A failed second operation must leave both name and config untouched.
	testutil.Call(t, testHandler.PatchWorkspaceMcpConfig, mcpEditRequest("PATCH", id, testUserID, `{"name":"should-not-save","operations":[{"op":"set","path":"/url","value":"changed"},{"op":"remove","path":"/missing"}]}`, `"1"`)).Want(400)
	if view := get(); view.Revision != "1" || view.Name != "safe-atomic" {
		t.Fatal("failed patch partially committed")
	}
	testutil.Call(t, testHandler.PatchWorkspaceMcpConfig, mcpEditRequest("PATCH", id, testUserID, `{"name":"safe-renamed","operations":[{"op":"set","path":"/env/NEW","value":"new"}]}`, `"1"`)).Want(200)
	if get().Revision != "2" {
		t.Fatal("PATCH did not advance revision")
	}
	testutil.Call(t, testHandler.PatchWorkspaceMcpConfig, mcpEditRequest("PATCH", id, testUserID, `{"operations":[]}`, `"1"`)).Want(412)
	row, err := testHandler.Queries.GetWorkspaceMcpServer(context.Background(), db.GetWorkspaceMcpServerParams{ID: parseUUID(id), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(row.Config), "keep-secret") || !strings.Contains(string(row.Config), "9007199254740993") {
		t.Fatal("lost untouched fields")
	}
	testutil.Call(t, testHandler.UpdateWorkspaceMcpServer, mcpEditRequest("PUT", id, testUserID, `{"name":"legacy-renamed"}`, "")).Want(200)
	if get().Revision != "3" {
		t.Fatal("legacy PUT did not advance revision")
	}
	testutil.Call(t, testHandler.PatchWorkspaceMcpConfig, mcpEditRequest("PATCH", id, testUserID, `{"operations":[]}`, `"2"`)).Want(412)
	// Other-workspace IDs never bypass the row scope, for reads or writes.
	other := dbfx.Workspace(t, "MCP other", "mcp-other-safe-edit")
	foreign := dbfx.Insert(t, "workspace_mcp_server", testutil.Cols{"workspace_id": other, "name": "foreign", "config": `{"url":"foreign-secret"}`})
	testutil.Call(t, testHandler.GetWorkspaceMcpEditableConfig, mcpEditRequest("GET", foreign, testUserID, "", "")).Want(404)
	testutil.Call(t, testHandler.PatchWorkspaceMcpConfig, mcpEditRequest("PATCH", foreign, testUserID, `{"operations":[]}`, `"1"`)).Want(404)
}
