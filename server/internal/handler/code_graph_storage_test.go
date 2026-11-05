package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCodeGraphMissingCheckoutInvalidatesReady(t *testing.T) {
	ctx := context.Background()
	code, repo, raw := createCodeGraphViaAPI(t, map[string]any{"repo_url": "https://github.com/multica-ai/multica-cli.git", "auth_mode": "anonymous", "branch": "main"})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, raw)
	}
	if _, err := testPool.Exec(ctx, "UPDATE code_graph_repository SET status = 'ready' WHERE id = $1", repo.ID); err != nil {
		t.Fatal(err)
	}
	req := withURLParams(newRequest(http.MethodGet, "/", nil), "id", testWorkspaceID, "codeGraphId", repo.ID)
	response := httptest.NewRecorder()
	testHandler.GetWorkspaceCodeGraph(response, req)
	var status, internal string
	if err := testPool.QueryRow(ctx, "SELECT status, COALESCE(internal_status, '') FROM code_graph_repository WHERE id = $1", repo.ID).Scan(&status, &internal); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || internal != "checkout_missing" {
		t.Fatalf("missing checkout retained status=%s internal=%s", status, internal)
	}
	dir := codeGraphCheckoutDir(testWorkspaceID, repo.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if _, err := testPool.Exec(ctx, "UPDATE code_graph_repository SET status = 'ready' WHERE id = $1", repo.ID); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	testHandler.GetWorkspaceCodeGraph(response, req)
	if err := testPool.QueryRow(ctx, "SELECT status FROM code_graph_repository WHERE id = $1", repo.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "ready" {
		t.Fatalf("existing checkout invalidated: %s", status)
	}
}
