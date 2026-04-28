package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/documents"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Read-only tests (MUL-16)
// ---------------------------------------------------------------------------

func TestWorkspacePKMPathExtraction(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"nil", nil, ""},
		{"empty", []byte(``), ""},
		{"missing key", []byte(`{"other":"x"}`), ""},
		{"non-string", []byte(`{"pkm_path": 42}`), ""},
		{"happy", []byte(`{"pkm_path":"workspace1"}`), "workspace1"},
		{"trim", []byte(`{"pkm_path":"  workspace1  "}`), "workspace1"},
		{"invalid json", []byte(`{`), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := workspacePKMPath(db.Workspace{Settings: c.in})
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestMapDocumentsErrorStatuses(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{documents.ErrNotConfigured, http.StatusServiceUnavailable},
		{documents.ErrInvalidPath, http.StatusBadRequest},
		{documents.ErrOutsideRoot, http.StatusForbidden},
		{documents.ErrSymlinkEscape, http.StatusForbidden},
		{documents.ErrExtNotAllowed, http.StatusUnsupportedMediaType},
		{documents.ErrNotFound, http.StatusNotFound},
		{documents.ErrNotRegular, http.StatusBadRequest},
		{documents.ErrNotDirectory, http.StatusBadRequest},
		{documents.ErrTooLarge, http.StatusRequestEntityTooLarge},
		{os.ErrNotExist, http.StatusNotFound},
		{os.ErrPermission, http.StatusForbidden},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		mapDocumentsError(w, c.err)
		if w.Code != c.code {
			t.Errorf("err=%v: got status %d, want %d", c.err, w.Code, c.code)
		}
	}
}

// TestDocumentsDisabledReturns503 covers the case where the server has not
// configured an allowlist root: every endpoint must respond 503 without
// touching the DB or filesystem.
func TestDocumentsDisabledReturns503(t *testing.T) {
	h := &Handler{} // h.Documents = nil
	for _, path := range []string{"/documents/tree", "/documents/file", "/documents/image"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		var fn func(http.ResponseWriter, *http.Request)
		switch {
		case strings.HasSuffix(path, "/tree"):
			fn = h.GetDocumentsTree
		case strings.HasSuffix(path, "/file"):
			fn = h.GetDocumentsFile
		default:
			fn = h.GetDocumentsImage
		}
		fn(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: expected 503 with nil Documents, got %d", path, w.Code)
		}
	}
}

// TestResolveQueryDecoding documents the round-trip from a URL-encoded query
// string to the documents.Resolver. The HTTP layer normally URL-decodes the
// query for us, so by the time Resolve runs the bytes are the literal
// traversal sequence ".." and the resolver rejects them. This is the sanity
// check that the existing rejection set actually blocks what an attacker
// would send over the wire.
func TestResolveQueryDecoding(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	if err := os.MkdirAll(filepath.Join(root, "ws"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "outside.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed outside: %v", err)
	}
	r, err := documents.NewResolver(root)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	// Things a hostile client might put in ?path= over the wire. After the
	// HTTP server URL-decodes the query, these are the literal strings the
	// handler would pass to Resolve.
	encoded := []string{
		"%2E%2E%2Foutside.md",  // ../outside.md
		"%2e%2e%2foutside.md",  // ../outside.md (lowercase)
		"%2f..%2f..%2foutside", // /../../outside
		"foo%00bar",            // foo<NUL>bar
	}
	for _, raw := range encoded {
		dec, err := url.QueryUnescape(raw)
		if err != nil {
			t.Fatalf("decode %q: %v", raw, err)
		}
		if _, err := r.Resolve("ws", dec); err == nil {
			t.Errorf("decoded %q (%q): expected rejection, got nil", raw, dec)
		}
	}
}

// ---------------------------------------------------------------------------
// Write tests (MUL-18) — require database fixtures (testHandler / testPool)
// ---------------------------------------------------------------------------

// setupPKMTest carves out an allowlist root + workspace base inside
// t.TempDir(), points MULTICA_PKM_ROOT at it, and patches the test workspace
// settings so pkm_path resolves to the base. Returns (allowlistRoot, base).
func setupPKMTest(t *testing.T) (string, string) {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DATABASE_URL)")
	}
	tmp := t.TempDir()
	allowRoot := filepath.Join(tmp, "pkm")
	base := "PKM/PROJECTS"
	if err := os.MkdirAll(filepath.Join(allowRoot, base), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(pkmAllowlistRootEnv, allowRoot)

	// Patch workspace settings to set pkm_path. Restore on cleanup.
	prev, err := snapshotWorkspaceSettings(testWorkspaceID)
	if err != nil {
		t.Fatalf("snapshot settings: %v", err)
	}
	t.Cleanup(func() { _ = restoreWorkspaceSettings(testWorkspaceID, prev) })

	settings, _ := json.Marshal(map[string]any{"pkm_path": base})
	if _, err := testPool.Exec(context.Background(),
		`UPDATE workspace SET settings = $2 WHERE id = $1`,
		testWorkspaceID, settings,
	); err != nil {
		t.Fatalf("set settings: %v", err)
	}
	return allowRoot, base
}

func snapshotWorkspaceSettings(workspaceID string) ([]byte, error) {
	var raw []byte
	err := testPool.QueryRow(context.Background(),
		`SELECT settings FROM workspace WHERE id = $1`,
		workspaceID,
	).Scan(&raw)
	return raw, err
}

func restoreWorkspaceSettings(workspaceID string, raw []byte) error {
	_, err := testPool.Exec(context.Background(),
		`UPDATE workspace SET settings = $2 WHERE id = $1`,
		workspaceID, raw,
	)
	return err
}

func docReq(method, query string, body []byte) *http.Request {
	u := "/api/workspaces/" + testWorkspaceID + "/documents/" + strings.TrimPrefix(query, "/")
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, u, nil)
	} else {
		r = httptest.NewRequest(method, u, bytes.NewReader(body))
	}
	r.Header.Set("X-User-ID", testUserID)
	r.Header.Set("X-Workspace-ID", testWorkspaceID)
	return withURLParam(r, "id", testWorkspaceID)
}

func TestDocuments_PutCreatesAndOverwrites(t *testing.T) {
	allowRoot, base := setupPKMTest(t)

	// Create via PUT.
	req := docReq("PUT", "file?path=notes.md", []byte("v1"))
	w := httptest.NewRecorder()
	testHandler.PutDocumentFile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT v1: %d %s", w.Code, w.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(allowRoot, base, "notes.md"))
	if err != nil || string(got) != "v1" {
		t.Fatalf("file content: %v %q", err, got)
	}

	// Overwrite via PUT.
	req = docReq("PUT", "file?path=notes.md", []byte("v2"))
	w = httptest.NewRecorder()
	testHandler.PutDocumentFile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT v2: %d %s", w.Code, w.Body.String())
	}
	got, _ = os.ReadFile(filepath.Join(allowRoot, base, "notes.md"))
	if string(got) != "v2" {
		t.Fatalf("overwrite failed: %q", got)
	}
}

func TestDocuments_PostFile_NewAndConflict(t *testing.T) {
	allowRoot, base := setupPKMTest(t)
	req := docReq("POST", "file?path=a.md", []byte("hello"))
	w := httptest.NewRecorder()
	testHandler.CreateDocumentFile(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST new: %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(allowRoot, base, "a.md")); string(got) != "hello" {
		t.Fatalf("contents %q", got)
	}
	// Conflict on second POST.
	req = docReq("POST", "file?path=a.md", []byte("again"))
	w = httptest.NewRecorder()
	testHandler.CreateDocumentFile(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("POST dup: expected 409, got %d %s", w.Code, w.Body.String())
	}
}

func TestDocuments_PostFile_ParentMissing(t *testing.T) {
	setupPKMTest(t)
	req := docReq("POST", "file?path=missing/sub/a.md", []byte("x"))
	w := httptest.NewRecorder()
	testHandler.CreateDocumentFile(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d %s", w.Code, w.Body.String())
	}
}

func TestDocuments_TraversalBlocked(t *testing.T) {
	allowRoot, _ := setupPKMTest(t)
	// Pre-create a sentinel outside the workspace base but inside allow root,
	// and a file outside allow root entirely.
	outsideAllow := filepath.Join(filepath.Dir(allowRoot), "outside.md")
	if err := os.WriteFile(outsideAllow, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"../../outside.md",
		"../../../outside.md",
		"sub/../../escape.md",
		"/etc/passwd.md",
	}
	for _, p := range cases {
		req := docReq("PUT", "file?path="+p, []byte("pwn"))
		w := httptest.NewRecorder()
		testHandler.PutDocumentFile(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("traversal %q: expected 400, got %d %s", p, w.Code, w.Body.String())
		}
	}

	// outside.md must be untouched.
	got, _ := os.ReadFile(outsideAllow)
	if string(got) != "untouched" {
		t.Fatalf("outside file mutated: %q", got)
	}
}

func TestDocuments_NonMarkdownRejected(t *testing.T) {
	setupPKMTest(t)
	req := docReq("PUT", "file?path=notes.txt", []byte("x"))
	w := httptest.NewRecorder()
	testHandler.PutDocumentFile(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", w.Code, w.Body.String())
	}
}

func TestDocuments_BodyCapEnforced(t *testing.T) {
	setupPKMTest(t)
	body := bytes.Repeat([]byte("A"), pkmMaxBodyBytes+1)
	req := docReq("PUT", "file?path=big.md", body)
	w := httptest.NewRecorder()
	testHandler.PutDocumentFile(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d %s", w.Code, w.Body.String())
	}
}

func TestDocuments_FolderCreateAndDelete(t *testing.T) {
	allowRoot, base := setupPKMTest(t)

	req := docReq("POST", "folder?path=notes/2026", nil)
	w := httptest.NewRecorder()
	testHandler.CreateDocumentFolder(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("nested without parent: expected 404, got %d %s", w.Code, w.Body.String())
	}

	req = docReq("POST", "folder?path=notes", nil)
	w = httptest.NewRecorder()
	testHandler.CreateDocumentFolder(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("folder create: %d %s", w.Code, w.Body.String())
	}
	req = docReq("POST", "folder?path=notes", nil)
	w = httptest.NewRecorder()
	testHandler.CreateDocumentFolder(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("folder dup: expected 409, got %d", w.Code)
	}

	// Delete empty folder.
	req = docReq("DELETE", "folder?path=notes", nil)
	w = httptest.NewRecorder()
	testHandler.DeleteDocumentFolder(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("folder delete empty: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(allowRoot, base, "notes")); !os.IsNotExist(err) {
		t.Fatalf("folder still present: %v", err)
	}
}

func TestDocuments_FolderForceDeleteRequiresHeader(t *testing.T) {
	allowRoot, base := setupPKMTest(t)
	if err := os.MkdirAll(filepath.Join(allowRoot, base, "tree"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(allowRoot, base, "tree", "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Plain DELETE on a non-empty folder -> 409.
	req := docReq("DELETE", "folder?path=tree", nil)
	w := httptest.NewRecorder()
	testHandler.DeleteDocumentFolder(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("non-empty without force: expected 409, got %d %s", w.Code, w.Body.String())
	}

	// force=true without confirmation header -> 428.
	req = docReq("DELETE", "folder?path=tree&force=true", nil)
	w = httptest.NewRecorder()
	testHandler.DeleteDocumentFolder(w, req)
	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("force without header: expected 428, got %d %s", w.Code, w.Body.String())
	}

	// force=true + confirmation header -> 204.
	req = docReq("DELETE", "folder?path=tree&force=true", nil)
	req.Header.Set(pkmConfirmHeader, pkmConfirmValue)
	w = httptest.NewRecorder()
	testHandler.DeleteDocumentFolder(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("force with header: expected 204, got %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(allowRoot, base, "tree")); !os.IsNotExist(err) {
		t.Fatalf("tree still present: %v", err)
	}
}

func TestDocuments_DeleteFile(t *testing.T) {
	allowRoot, base := setupPKMTest(t)
	target := filepath.Join(allowRoot, base, "doomed.md")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := docReq("DELETE", "file?path=doomed.md", nil)
	w := httptest.NewRecorder()
	testHandler.DeleteDocumentFile(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("file not removed: %v", err)
	}

	// Re-delete -> 404.
	req = docReq("DELETE", "file?path=doomed.md", nil)
	w = httptest.NewRecorder()
	testHandler.DeleteDocumentFile(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDocuments_RootNotConfigured(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB")
	}
	t.Setenv(pkmAllowlistRootEnv, "")
	req := docReq("PUT", "file?path=a.md", []byte("x"))
	w := httptest.NewRecorder()
	testHandler.PutDocumentFile(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d %s", w.Code, w.Body.String())
	}
}

func TestDocuments_PathRequired(t *testing.T) {
	setupPKMTest(t)
	req := docReq("PUT", "file", []byte("x"))
	w := httptest.NewRecorder()
	testHandler.PutDocumentFile(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ensure error path through writePKMFSError compiles for symlink leaf
// (mirror of pkmfs unit test, but at handler layer).
func TestDocuments_PutRefusesSymlinkLeaf(t *testing.T) {
	allowRoot, base := setupPKMTest(t)
	// Create a target inside allow root but outside base.
	target := filepath.Join(allowRoot, "secret.md")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowRoot, base, "trojan.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	req := docReq("PUT", fmt.Sprintf("file?path=%s", "trojan.md"), []byte("pwn"))
	w := httptest.NewRecorder()
	testHandler.PutDocumentFile(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d %s", w.Code, w.Body.String())
	}
	got, _ := os.ReadFile(target)
	if string(got) != "secret" {
		t.Fatalf("symlink target mutated: %q", got)
	}
}
