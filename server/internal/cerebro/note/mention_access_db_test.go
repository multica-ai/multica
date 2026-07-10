package note

// FIR-2595 Stage 2 — the mention/access flow. Proves, against a real DB:
//   * a member @mentioned in a FOLDERED note they cannot open is NOT notified
//     (the silent auto-share that produced an un-openable notification is gone),
//   * once granted access they ARE notified,
//   * a member @mentioned in a ROOT (folderless) note is still shared + notified
//     (unchanged — nothing gates a root note),
//   * the GrantMentionAccess endpoint opens a foldered note for a tagged member,
//   * the MentionAccessCheck endpoint reports exactly who lacks access.
// Skips cleanly when no DB is reachable.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func memberMention(userID string) string {
	return "hey [@x](mention://member/" + userID + ")"
}

// A tagged member who cannot open a foldered note is not notified; after a grant
// they are.
func TestFolderedNoteMentionSkipsNoAccessUntilGranted(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()
	folder := makeFolder(t, ctx, "ma-skip", pgtype.UUID{})
	note := makeNoteInFolder(t, ctx, folder)
	h, captured := busHandler()

	body := memberMention(uuidStr(w3UserB))
	// userA (owner) tags userB, who has no access to the restricted folder.
	h.notifyNoteMentions(ctx, w3WsID, note, w3UserA, "t", "", body, "private", "", "")
	if len(*captured) != 0 {
		t.Fatalf("expected NO notification for a no-access member, got %d", len(*captured))
	}

	// Grant userB folder access, then a fresh mention notifies them.
	grantFolder(t, ctx, folder, "member", w3UserB, "viewer")
	h.notifyNoteMentions(ctx, w3WsID, note, w3UserA, "t", "", body, "private", "", "")
	if len(*captured) != 1 {
		t.Fatalf("expected 1 notification after grant, got %d", len(*captured))
	}
	ids, _ := (*captured)[0].Payload.(map[string]any)["member_ids"].([]string)
	if len(ids) != 1 || ids[0] != uuidStr(w3UserB) {
		t.Fatalf("expected member_ids [%s], got %v", uuidStr(w3UserB), ids)
	}
}

// A root (folderless) note keeps the original share + notify behaviour.
func TestRootNoteMentionStillSharesAndNotifies(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()
	note := makeNote(t, ctx, "root", "body") // makeNote creates a root, private note
	h, captured := busHandler()

	body := memberMention(uuidStr(w3UserB))
	h.notifyNoteMentions(ctx, w3WsID, note, w3UserA, "t", "", body, "private", "", "")
	if len(*captured) != 1 {
		t.Fatalf("root note: expected 1 notification, got %d", len(*captured))
	}
	if !canSee(t, ctx, note, w3UserB) {
		t.Fatal("root note: userB should be able to see it after the mention share")
	}
}

// membersWithoutNoteAccess reports exactly the members who cannot open the note.
func TestMembersWithoutNoteAccess(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()
	folder := makeFolder(t, ctx, "ma-check", pgtype.UUID{})
	note := makeNoteInFolder(t, ctx, folder)

	got := w3H.membersWithoutNoteAccess(ctx, note, []string{uuidStr(w3UserA), uuidStr(w3UserB)})
	// userA owns the note (can see); userB cannot.
	if len(got) != 1 || got[0] != uuidStr(w3UserB) {
		t.Fatalf("expected only userB to lack access, got %v", got)
	}
	grantFolder(t, ctx, folder, "member", w3UserB, "viewer")
	if got := w3H.membersWithoutNoteAccess(ctx, note, []string{uuidStr(w3UserB)}); len(got) != 0 {
		t.Fatalf("after grant, expected nobody to lack access, got %v", got)
	}
}

// The GrantMentionAccess endpoint opens a foldered note for a tagged member.
func TestGrantMentionAccessEndpoint(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()
	folder := makeFolder(t, ctx, "ma-grant", pgtype.UUID{})
	note := makeNoteInFolder(t, ctx, folder)
	if canSee(t, ctx, note, w3UserB) {
		t.Fatal("baseline: userB should not see the note")
	}

	body := `{"member_ids":["` + uuidStr(w3UserB) + `"]}`
	w := doMentionAccess(t, http.MethodPost, note, "", body)
	if w.Code != http.StatusOK {
		t.Fatalf("grant: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !canSee(t, ctx, note, w3UserB) {
		t.Fatal("after grant endpoint: userB should see the note")
	}
}

// A viewer/commenter can ask who lacks access, but cannot expand the note's
// audience. Only the note owner may give access from the prompt.
func TestGrantMentionAccessEndpointRequiresOwner(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()
	folder := makeFolder(t, ctx, "ma-grant-owner", pgtype.UUID{})
	note := makeNoteInFolder(t, ctx, folder)
	grantFolder(t, ctx, folder, "member", w3UserB, "viewer")

	body := `{"member_ids":["` + uuidStr(w3UserB) + `"]}`
	w := doMentionAccessAs(t, http.MethodPost, note, uuidStr(w3UserB), "", body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer grant: expected 403, got %d (%s)", w.Code, w.Body.String())
	}
}

// The MentionAccessCheck endpoint reports who lacks access.
func TestMentionAccessCheckEndpoint(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()
	folder := makeFolder(t, ctx, "ma-checkep", pgtype.UUID{})
	note := makeNoteInFolder(t, ctx, folder)

	w := doMentionAccess(t, http.MethodGet, note, "members="+uuidStr(w3UserB), "")
	if w.Code != http.StatusOK {
		t.Fatalf("check: expected 200, got %d", w.Code)
	}
	var resp struct {
		NoAccess []string `json:"no_access"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.NoAccess) != 1 || resp.NoAccess[0] != uuidStr(w3UserB) {
		t.Fatalf("expected no_access [%s], got %v", uuidStr(w3UserB), resp.NoAccess)
	}
}

// doMentionAccess drives the mention-access handlers with the workspace + user +
// chi {id} they read, mirroring the other handler DB tests. userA (owner) is the
// caller.
func doMentionAccess(t *testing.T, method string, noteID pgtype.UUID, query, body string) *httptest.ResponseRecorder {
	return doMentionAccessAs(t, method, noteID, uuidStr(w3UserA), query, body)
}

func doMentionAccessAs(t *testing.T, method string, noteID pgtype.UUID, userID, query, body string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/notes/" + uuidStr(noteID) + "/mention-access"
	if query != "" {
		url += "?" + query
	}
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	r := httptest.NewRequest(method, url, reader)
	r.Header.Set("X-User-ID", userID)
	ctx := middleware.SetMemberContext(r.Context(), uuidStr(w3WsID), db.Member{})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuidStr(noteID))
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	w := httptest.NewRecorder()
	if method == http.MethodGet {
		w3H.MentionAccessCheck(w, r.WithContext(ctx))
	} else {
		w3H.GrantMentionAccess(w, r.WithContext(ctx))
	}
	return w
}
