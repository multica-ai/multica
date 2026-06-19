package handler

// CEREBRO-PATCH(channel-message-search): FIR-407 — tests for per-channel/DM message search.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedChannelWithMessages creates a channel owned by testUserID and inserts the
// given message contents in order (one minute apart so chronological ordering
// is deterministic). Returns the channel ID.
func seedChannelWithMessages(t *testing.T, name string, contents []string) string {
	t.Helper()
	ctx := context.Background()

	peerID, cleanup := createSecondTestUser(t, "msgsearch-peer-"+name)
	t.Cleanup(cleanup)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"kind":       "channel",
		"name":       name,
		"member_ids": []string{peerID},
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateChannel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var ch ChannelResponse
	if err := json.NewDecoder(w.Body).Decode(&ch); err != nil {
		t.Fatalf("decode channel: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, ch.ID)
	})

	base := time.Now().Add(-time.Hour)
	for i, content := range contents {
		if _, err := testPool.Exec(ctx,
			`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type, created_at)
			 VALUES ($1, $2, 'member', $3, $4, 'comment', $5)`,
			testWorkspaceID, ch.ID, testUserID, content, base.Add(time.Duration(i)*time.Minute),
		); err != nil {
			t.Fatalf("seed message %d: %v", i, err)
		}
	}
	return ch.ID
}

func searchChannel(t *testing.T, channelID, query string) (*httptest.ResponseRecorder, ChannelMessageSearchResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("GET", "/api/channels/"+channelID+"/messages/search?q="+query, nil),
		"id", channelID,
	)
	testHandler.SearchChannelMessages(w, req)
	var resp ChannelMessageSearchResponse
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return w, resp
}

func TestSearchChannelMessages_FindsMatchesChronologically(t *testing.T) {
	channelID := seedChannelWithMessages(t, "search-find", []string{
		"Deploy til staging er ude",
		"Her er linket til Purchase product selector: https://portal.firtal.dk/x",
		"Tak, er inde nu",
		"Opdateret dokumentation for Purchase product selector",
	})

	w, resp := searchChannel(t, channelID, "purchase+product+selector")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.Total != 2 {
		t.Fatalf("expected total 2, got %d", resp.Total)
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(resp.Messages))
	}
	// Chronological: the "linket til" message was seeded before the "Opdateret" one.
	if resp.Messages[0].CreatedAt >= resp.Messages[1].CreatedAt {
		t.Fatalf("expected chronological order, got %s then %s", resp.Messages[0].CreatedAt, resp.Messages[1].CreatedAt)
	}
	for _, m := range resp.Messages {
		if m.Snippet == "" {
			t.Fatalf("expected a snippet for message %s", m.ID)
		}
	}
}

func TestSearchChannelMessages_SubstringFallbackMatchesUrl(t *testing.T) {
	channelID := seedChannelWithMessages(t, "search-url", []string{
		"intet link her",
		"se https://portal.firtal.dk/products/selector for detaljer",
	})
	// A URL fragment the tsvector tokenizer would split — the substring
	// fallback must still match it.
	w, resp := searchChannel(t, channelID, "portal.firtal.dk%2Fproducts")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.Total != 1 {
		t.Fatalf("expected total 1, got %d", resp.Total)
	}
}

func TestSearchChannelMessages_RequiresQuery(t *testing.T) {
	channelID := seedChannelWithMessages(t, "search-noq", []string{"hej"})
	w, _ := searchChannel(t, channelID, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty query, got %d", w.Code)
	}
}

func TestSearchChannelMessages_NonParticipantForbidden(t *testing.T) {
	channelID := seedChannelWithMessages(t, "search-acl", []string{
		"hemmelig besked om Purchase product selector",
	})

	// A workspace member who is NOT subscribed to this channel must not be able
	// to search it — the access rule that keeps private DMs private.
	outsiderID, cleanup := createSecondTestUser(t, "outsider")
	defer cleanup()

	w := httptest.NewRecorder()
	req := withURLParam(
		newRequestAs(outsiderID, "GET", "/api/channels/"+channelID+"/messages/search?q=purchase", nil),
		"id", channelID,
	)
	testHandler.SearchChannelMessages(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-participant, got %d: %s", w.Code, w.Body.String())
	}
}

// adminSearchAll runs the workspace-wide admin search with the given member role
// injected into context.
func adminSearchAll(t *testing.T, role, query string) (*httptest.ResponseRecorder, AdminChannelMessageSearchResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/cerebro/channel-messages/search?q="+query, nil)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: role}))
	testHandler.SearchAllChannelMessages(w, req)
	var resp AdminChannelMessageSearchResponse
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return w, resp
}

func TestSearchAllChannelMessages_AdminFindsAcrossChannels(t *testing.T) {
	term := "Quarterlyplanningdoc"
	seedChannelWithMessages(t, "admin-a", []string{"intro", "se " + term + " her"})
	seedChannelWithMessages(t, "admin-b", []string{"hej", "andet " + term + " link"})

	w, resp := adminSearchAll(t, "owner", term)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.Total < 2 {
		t.Fatalf("expected at least 2 matches across channels, got %d", resp.Total)
	}
	channels := map[string]bool{}
	for _, m := range resp.Messages {
		channels[m.ChannelID] = true
		if m.Snippet == "" {
			t.Fatalf("expected snippet for %s", m.ID)
		}
	}
	if len(channels) < 2 {
		t.Fatalf("expected matches from >=2 distinct channels, got %d", len(channels))
	}
}

func TestSearchAllChannelMessages_AdminFindsPrivateDMNotMember(t *testing.T) {
	// A DM between two OTHER users — the admin (testUserID) is not a participant.
	userA, cleanupA := createSecondTestUser(t, "dm-a")
	defer cleanupA()
	userB, cleanupB := createSecondTestUser(t, "dm-b")
	defer cleanupB()

	ctx := context.Background()
	var dmID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, kind, title, status, priority, creator_type, creator_id)
		 VALUES ($1, 'dm', '', 'todo', 'none', 'member', $2) RETURNING id`,
		testWorkspaceID, userA,
	).Scan(&dmID); err != nil {
		t.Fatalf("insert dm: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, dmID) })

	term := "Secretcoachingphrase"
	if _, err := testPool.Exec(ctx,
		`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type)
		 VALUES ($1, $2, 'member', $3, $4, 'comment')`,
		testWorkspaceID, dmID, userB, "privat: "+term,
	); err != nil {
		t.Fatalf("insert dm message: %v", err)
	}

	// Admin (owner) can find it even though they're not in the DM.
	w, resp := adminSearchAll(t, "owner", term)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 match in foreign DM, got %d", resp.Total)
	}
	if resp.Messages[0].ChannelKind != "dm" {
		t.Fatalf("expected dm kind, got %q", resp.Messages[0].ChannelKind)
	}
}

func TestSearchAllChannelMessages_NonAdminForbidden(t *testing.T) {
	w, _ := adminSearchAll(t, "member", "anything")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSearchAllChannelMessages_RequiresQuery(t *testing.T) {
	w, _ := adminSearchAll(t, "owner", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty query, got %d", w.Code)
	}
}

// mySearch runs the per-user global message search as the given user (empty
// userID = default test user).
func mySearch(t *testing.T, userID, query string) (*httptest.ResponseRecorder, AdminChannelMessageSearchResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	var req *http.Request
	if userID == "" {
		req = newRequest("GET", "/api/cerebro/my/channel-messages/search?q="+query, nil)
	} else {
		req = newRequestAs(userID, "GET", "/api/cerebro/my/channel-messages/search?q="+query, nil)
	}
	testHandler.SearchMyChannelMessages(w, req)
	var resp AdminChannelMessageSearchResponse
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return w, resp
}

func TestSearchMyChannelMessages_FindsOwnConversations(t *testing.T) {
	term := "Mineownsearchterm"
	// seedChannelWithMessages creates the channel owned by testUserID, so the
	// default test user is a subscriber and must find its messages.
	channelID := seedChannelWithMessages(t, "mine-find", []string{"intro", "se " + term + " her"})

	w, resp := mySearch(t, "", term)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 match in own channel, got %d", resp.Total)
	}
	if resp.Messages[0].ChannelID != channelID {
		t.Fatalf("expected channel %s, got %s", channelID, resp.Messages[0].ChannelID)
	}
	if resp.Messages[0].ChannelTitle == "" || resp.Messages[0].Snippet == "" {
		t.Fatalf("expected channel title + snippet, got title=%q snippet=%q", resp.Messages[0].ChannelTitle, resp.Messages[0].Snippet)
	}
}

func TestSearchMyChannelMessages_ExcludesNonSubscribed(t *testing.T) {
	term := "Notmineterm"
	seedChannelWithMessages(t, "mine-acl", []string{"hemmelig " + term})

	// A workspace member who is NOT subscribed to that channel must get zero
	// results — the personal scope is the access boundary (not a 403).
	outsiderID, cleanup := createSecondTestUser(t, "mine-outsider")
	defer cleanup()

	w, resp := mySearch(t, outsiderID, term)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.Total != 0 {
		t.Fatalf("expected 0 matches for non-subscriber, got %d", resp.Total)
	}
}

func TestSearchMyChannelMessages_RequiresQuery(t *testing.T) {
	w, _ := mySearch(t, "", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty query, got %d", w.Code)
	}
}
