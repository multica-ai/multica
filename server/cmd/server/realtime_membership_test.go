package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type membershipFailCommitPool struct{ pool *pgxpool.Pool }
type membershipFailCommitTx struct{ pgx.Tx }

func (p membershipFailCommitPool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return membershipFailCommitTx{tx}, nil
}

func (membershipFailCommitTx) Commit(context.Context) error {
	return errors.New("injected membership commit failure")
}

func realtimeMembershipConn(t *testing.T, server *httptest.Server, userID, workspaceID string) (*websocket.Conn, string) {
	t.Helper()
	token, err := generateTestJWT(userID, "realtime@example.test", "Realtime")
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"Cookie": []string{auth.AuthCookieName + "=" + token}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws?workspace_id="+workspaceID, headers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.WriteJSON(map[string]string{"type": "ping"}); err != nil {
		t.Fatal(err)
	}
	wantRealtimeMembershipFrame(t, conn, "pong")
	return conn, token
}

func wantRealtimeMembershipFrame(t *testing.T, conn *websocket.Conn, typ string) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	var frame struct{ Type string }
	if err != nil || json.Unmarshal(raw, &frame) != nil || frame.Type != typ {
		t.Fatalf("want %s, got %s, err=%v", typ, raw, err)
	}
}

func wantRealtimeMembershipClosed(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				t.Fatalf("revoked socket stayed open: %v", err)
			}
			return
		}
		var frame struct{ Type string }
		if json.Unmarshal(raw, &frame) != nil || frame.Type != protocol.EventMemberRemoved {
			t.Fatalf("revoked socket received workspace content: %s", raw)
		}
	}
}

func TestRealtimeMemberRevocation(t *testing.T) {
	for _, method := range []string{"delete", "leave"} {
		for _, failCommit := range []bool{false, true} {
			name := method
			if failCommit {
				name += "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				fx := workspaceFixture(t)
				wsID := fx.Workspace(t, "Realtime revocation", "realtime-revocation")
				otherWS := fx.Workspace(t, "Other realtime workspace", "realtime-other")
				userID := fx.User(t, "Leaver", "realtime-leaver@example.test")
				fx.Member(t, wsID, testUserID, "owner")
				memberID := fx.Member(t, wsID, userID, "member")
				fx.Member(t, otherWS, userID, "owner")
				hub := realtime.NewHub()
				go hub.Run()
				bus := events.New()
				registerListeners(bus, hub)
				router, h := NewRouterWithOptions(testPool, hub, bus, analytics.NoopClient{}, nil, RouterOptions{})
				server := httptest.NewServer(router)
				t.Cleanup(server.Close)
				old, token := realtimeMembershipConn(t, server, userID, wsID)
				second, _ := realtimeMembershipConn(t, server, userID, wsID)
				other, _ := realtimeMembershipConn(t, server, userID, otherWS)
				current, _ := realtimeMembershipConn(t, server, testUserID, wsID)
				if failCommit {
					h.TxStarter = membershipFailCommitPool{testPool}
				}
				request := httptest.NewRequest(http.MethodDelete, "/", nil)
				actor := testUserID
				if method == "leave" {
					actor = userID
				}
				request.Header.Set("X-User-ID", actor)
				route := chi.NewRouteContext()
				route.URLParams.Add("id", wsID)
				route.URLParams.Add("memberId", memberID)
				request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
				endpoint := h.DeleteMember
				if method == "leave" {
					endpoint = h.LeaveWorkspace
				}
				status := http.StatusNoContent
				if failCommit {
					status = http.StatusInternalServerError
				}
				testutil.Call(t, endpoint, request).Want(status)
				if failCommit {
					if fx.Count(t, `SELECT count(*) FROM member WHERE id=$1`, memberID) != 1 {
						t.Fatal("failed revocation did not restore membership")
					}
				} else {
					wantRealtimeMembershipClosed(t, old)
					wantRealtimeMembershipClosed(t, second)
					wantRealtimeMembershipFrame(t, current, protocol.EventMemberRemoved)
					fresh, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws?workspace_id="+wsID,
						http.Header{"Cookie": []string{auth.AuthCookieName + "=" + token}})
					if fresh != nil {
						fresh.Close()
					}
					if resp != nil && resp.Body != nil {
						defer resp.Body.Close()
					}
					if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
						t.Fatalf("revoked reconnect accepted: response=%v, err=%v", resp, err)
					}
				}
				// Use the real event listeners after the removal API returned.
				for _, typ := range []string{protocol.EventCommentCreated, protocol.EventIssueUpdated} {
					bus.Publish(events.Event{Type: typ, WorkspaceID: wsID, Payload: map[string]string{"body": "new-workspace-content"}})
					wantRealtimeMembershipFrame(t, current, typ)
					if failCommit {
						wantRealtimeMembershipFrame(t, old, typ)
						wantRealtimeMembershipFrame(t, second, typ)
					}
				}
				bus.Publish(events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: otherWS, Payload: map[string]string{"title": "other-workspace"}})
				wantRealtimeMembershipFrame(t, other, protocol.EventIssueUpdated)
			})
		}
	}
}

func TestRealtimeBatchMembershipPairs(t *testing.T) {
	fx := workspaceFixture(t)
	firstWS := fx.Workspace(t, "Batch one", "realtime-batch-one")
	secondWS := fx.Workspace(t, "Batch two", "realtime-batch-two")
	firstUser := fx.User(t, "Batch A", "realtime-batch-a@example.test")
	secondUser := fx.User(t, "Batch B", "realtime-batch-b@example.test")
	fx.Member(t, firstWS, firstUser, "owner")
	fx.Member(t, secondWS, secondUser, "owner")
	checker := &membershipChecker{queries: db.New(testPool)}
	// Both users and both workspaces exist, but cross-pair identities must
	// never inherit another pair's membership through a Cartesian product.
	requested := []realtime.MembershipIdentity{
		{WorkspaceID: firstWS, UserID: secondUser},
		{WorkspaceID: secondWS, UserID: firstUser},
		{WorkspaceID: firstWS, UserID: firstUser},
		{WorkspaceID: secondWS, UserID: secondUser},
		{WorkspaceID: strings.ToUpper(firstWS), UserID: strings.ToUpper(firstUser)},
	}
	members, err := checker.Members(context.Background(), requested)
	if err != nil {
		t.Fatal(err)
	}
	for i, identity := range requested {
		if members[identity] != (i >= 2) {
			t.Fatalf("incorrect membership for pair %+v", identity)
		}
	}
}
