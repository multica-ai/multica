package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// workspaceTokenFixture is a user who belongs to two workspaces and holds a
// workspace token for the first: everything the token reaches in the second
// is a leak.
type workspaceTokenFixture struct {
	userID       string
	bound, other string
	token        string
	tokenID      string
}

func newWorkspaceTokenFixture(t *testing.T) workspaceTokenFixture {
	t.Helper()
	if testPool == nil {
		t.Skip("test database not available")
	}
	f := workspaceTokenFixture{
		userID: dbfx.User(t, "Workspace token user", "ws-token-"+uuid.NewString()+"@example.test"),
		bound:  dbfx.Workspace(t, "Workspace token bound", "ws-token-bound-"+uuid.NewString()),
		other:  dbfx.Workspace(t, "Workspace token other", "ws-token-other-"+uuid.NewString()),
	}
	dbfx.Member(t, f.bound, f.userID, "owner")
	dbfx.Member(t, f.other, f.userID, "owner")
	f.token, f.tokenID = mintWorkspaceToken(t, f.userID, f.bound)
	return f
}

// mintWorkspaceToken mints a workspace token through the endpoint, as the
// user would, and returns the raw token and its id.
func mintWorkspaceToken(t *testing.T, userID, workspaceID string) (raw, id string) {
	t.Helper()
	req := testutil.WithHeaders(testutil.JSONRequest(http.MethodPost, "/api/tokens", map[string]any{
		"name": "workspace token", "workspace_id": workspaceID,
	}), "X-User-ID", userID)
	var created CreatePATResponse
	testutil.Call(t, testHandler.CreatePersonalAccessToken, req).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM personal_access_token WHERE id = $1`, created.ID)
	})
	return created.Token, created.ID
}

// call runs h behind the real Auth middleware, authenticated by the fixture's
// token.
func (f workspaceTokenFixture) call(t *testing.T, h http.HandlerFunc, req *http.Request) *testutil.Response {
	t.Helper()
	req.Header.Set("Authorization", "Bearer "+f.token)
	return testutil.Call(t, middleware.Auth(testHandler.Queries, nil, nil, nil)(h).ServeHTTP, req)
}

func TestCreateWorkspaceToken(t *testing.T) {
	f := newWorkspaceTokenFixture(t)
	ctx := context.Background()

	if !strings.HasPrefix(f.token, auth.WorkspaceTokenPrefix) {
		t.Fatalf("token %q does not carry the workspace token prefix %q", f.token[:8], auth.WorkspaceTokenPrefix)
	}

	// The two kinds of token never resolve as each other.
	hash := auth.HashToken(f.token)
	row, err := testHandler.Queries.GetWorkspaceTokenByHash(ctx, hash)
	if err != nil {
		t.Fatalf("GetWorkspaceTokenByHash: %v", err)
	}
	if uuidToString(row.WorkspaceID) != f.bound || uuidToString(row.UserID) != f.userID {
		t.Fatalf("token row = user %s workspace %s, want %s / %s", uuidToString(row.UserID), uuidToString(row.WorkspaceID), f.userID, f.bound)
	}
	if _, err := testHandler.Queries.GetPersonalAccessTokenByHash(ctx, hash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a workspace token resolved as an account-wide token (err = %v)", err)
	}
	accountWide, _ := insertTestPAT(t, time.Time{})
	if _, err := testHandler.Queries.GetWorkspaceTokenByHash(ctx, auth.HashToken(accountWide)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("an account-wide token resolved as a workspace token (err = %v)", err)
	}

	// The list tells the two kinds apart.
	var listed []PersonalAccessTokenResponse
	req := testutil.WithHeaders(testutil.JSONRequest(http.MethodGet, "/api/tokens", nil), "X-User-ID", f.userID)
	testutil.Call(t, testHandler.ListPersonalAccessTokens, req).Want(http.StatusOK).JSON(&listed)
	if len(listed) != 1 || listed[0].ID != f.tokenID || listed[0].WorkspaceID == nil || *listed[0].WorkspaceID != f.bound {
		t.Fatalf("listed tokens = %+v, want the one token with workspace %s", listed, f.bound)
	}
}

// A token is only minted for a workspace its owner belongs to.
func TestCreateWorkspaceToken_RequiresMembership(t *testing.T) {
	f := newWorkspaceTokenFixture(t)
	stranger := dbfx.Workspace(t, "Workspace token stranger", "ws-token-stranger-"+uuid.NewString())

	mint := func(workspaceID string) *testutil.Response {
		req := testutil.WithHeaders(testutil.JSONRequest(http.MethodPost, "/api/tokens", map[string]any{
			"name": "refused", "workspace_id": workspaceID,
		}), "X-User-ID", f.userID)
		return testutil.Call(t, testHandler.CreatePersonalAccessToken, req)
	}
	mint(stranger).Want(http.StatusNotFound)
	mint(uuid.NewString()).Want(http.StatusNotFound)
	mint("not-a-uuid").Want(http.StatusBadRequest)

	if n := dbfx.Count(t, `SELECT count(*) FROM personal_access_token WHERE user_id = $1 AND name = 'refused'`, f.userID); n != 0 {
		t.Fatalf("%d token rows were written for refused requests", n)
	}
}

func TestListWorkspaces_WorkspaceTokenSeesOnlyBoundWorkspace(t *testing.T) {
	f := newWorkspaceTokenFixture(t)
	ids := func(got []WorkspaceResponse) []string {
		out := make([]string, len(got))
		for i, ws := range got {
			out[i] = ws.ID
		}
		sort.Strings(out)
		return out
	}

	// Control: the owner's own session lists both workspaces.
	var own []WorkspaceResponse
	req := testutil.WithHeaders(testutil.JSONRequest(http.MethodGet, "/api/workspaces", nil), "X-User-ID", f.userID)
	testutil.Call(t, testHandler.ListWorkspaces, req).Want(http.StatusOK).JSON(&own)
	if len(own) != 2 {
		t.Fatalf("owner list = %v, want both workspaces", ids(own))
	}

	var scoped []WorkspaceResponse
	f.call(t, testHandler.ListWorkspaces, testutil.JSONRequest(http.MethodGet, "/api/workspaces", nil)).Want(http.StatusOK).JSON(&scoped)
	if len(scoped) != 1 || scoped[0].ID != f.bound {
		t.Fatalf("workspace token list = %v, want only %s", ids(scoped), f.bound)
	}
}

// Handlers that take the workspace from an entity or from the request body
// check membership themselves. Under a workspace token that check sees no
// membership outside the bound workspace, although the owner has one.
func TestWorkspaceToken_HandlerMembershipIsConfined(t *testing.T) {
	f := newWorkspaceTokenFixture(t)

	check := func(workspaceID string) (status int) {
		rec := f.call(t, func(w http.ResponseWriter, r *http.Request) {
			if _, ok := testHandler.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); ok {
				w.WriteHeader(http.StatusOK)
			}
		}, testutil.JSONRequest(http.MethodGet, "/api/anything", nil))
		return rec.Code
	}
	if got := check(f.bound); got != http.StatusOK {
		t.Fatalf("bound workspace: status %d, want 200", got)
	}
	if got := check(f.other); got != http.StatusNotFound {
		t.Fatalf("other workspace: status %d, want 404", got)
	}
}

// A workspace token is bound to one workspace. Its owner also belongs to the
// attachment's workspace, but the token must not read files from there.
func TestAttachmentReads_WorkspaceTokenStaysInItsBoundWorkspace(t *testing.T) {
	f := newWorkspaceTokenFixture(t)
	store := useThumbnailTestStorage(t)

	key := "thumbs/workspace-token-other.jpg"
	store.put(key, testJPEG(t, 2000, 1500))
	attachment := func(workspaceID string) string {
		return dbfx.Insert(t, "attachment", testutil.Cols{
			"workspace_id": workspaceID, "uploader_type": "member", "uploader_id": f.userID,
			"filename": "workspace-token.jpg", "url": "https://s3.example.com/test-bucket/" + key,
			"content_type": "image/jpeg", "size_bytes": 600 << 10,
		})
	}
	inOther, inBound := attachment(f.other), attachment(f.bound)

	r := chi.NewRouter()
	r.Use(middleware.Auth(testHandler.Queries, nil, nil, nil))
	r.Get("/api/attachments/{id}/download", testHandler.DownloadAttachment)
	r.Get("/api/attachments/{id}/thumbnail", testHandler.GetAttachmentThumbnail)
	r.Get("/api/attachments/{id}/original", testHandler.GetAttachmentOriginal)
	r.Get("/api/attachments/{id}/download-metadata", testHandler.GetAttachmentForDownload)
	read := func(id, route string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/attachments/"+id+"/"+route, nil)
		req.Header.Set("Authorization", "Bearer "+f.token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	for _, route := range []string{"thumbnail", "original", "download", "download-metadata"} {
		if got := read(inOther, route); got != http.StatusNotFound {
			t.Fatalf("%s of an attachment in another workspace: status = %d, want 404", route, got)
		}
	}
	// Control: the same token reads its own workspace's attachment.
	if got := read(inBound, "download-metadata"); got != http.StatusOK {
		t.Fatalf("download-metadata in the bound workspace: status = %d, want 200", got)
	}
}

// Leaving a workspace takes the member's tokens for it along, so a later
// return does not bring an old token back to life. Tokens for other
// workspaces and account-wide tokens are untouched.
func TestRevokeMember_DeletesTheirWorkspaceTokens(t *testing.T) {
	f := newWorkspaceTokenFixture(t)
	ctx := context.Background()
	_, otherTokenID := mintWorkspaceToken(t, f.userID, f.other)
	accountWide := dbfx.Insert(t, "personal_access_token", testutil.Cols{
		"user_id": f.userID, "name": "account-wide", "token_hash": auth.HashToken(uuid.NewString()), "token_prefix": "mul_test",
	})
	// Another owner, so removing this one leaves the workspace owned.
	keeper := dbfx.User(t, "Workspace token keeper", "ws-token-keeper-"+uuid.NewString()+"@example.test")
	dbfx.Member(t, f.bound, keeper, "owner")

	var memberID string
	dbfx.QueryRow(t, `SELECT id::text FROM member WHERE workspace_id = $1 AND user_id = $2`, f.bound, f.userID).Scan(&memberID)
	if _, err := testHandler.revokeAndRemoveMember(ctx, parseUUID(f.bound), parseUUID(f.userID), parseUUID(memberID), parseUUID(keeper)); err != nil {
		t.Fatalf("revokeAndRemoveMember: %v", err)
	}

	remaining := func(id string) int {
		return dbfx.Count(t, `SELECT count(*) FROM personal_access_token WHERE id = $1`, id)
	}
	if remaining(f.tokenID) != 0 {
		t.Fatal("the member's token for the workspace they left is still there")
	}
	if remaining(otherTokenID) != 1 || remaining(accountWide) != 1 {
		t.Fatal("removing a member deleted a token for another workspace or an account-wide token")
	}
	if _, err := testHandler.Queries.GetWorkspaceTokenByHash(ctx, auth.HashToken(f.token)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the removed member's token still resolves (err = %v)", err)
	}
}

// Deleting a workspace removes the tokens bound to it in application code;
// there is no foreign key to do it.
func TestDeleteWorkspace_DeletesItsWorkspaceTokens(t *testing.T) {
	f := newWorkspaceTokenFixture(t)
	_, otherTokenID := mintWorkspaceToken(t, f.userID, f.other)

	if err := testHandler.Queries.DeleteWorkspaceAdministration(context.Background(), parseUUID(f.bound)); err != nil {
		t.Fatalf("DeleteWorkspaceAdministration: %v", err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM personal_access_token WHERE workspace_id = $1`, f.bound); n != 0 {
		t.Fatalf("%d tokens still bound to the deleted workspace", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM personal_access_token WHERE id = $1`, otherTokenID); n != 1 {
		t.Fatal("deleting a workspace removed a token bound to another one")
	}
}
