package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func legacyInvitationFixture(t *testing.T, memberRole, inviteRole string) (string, string, string, string) {
	t.Helper()
	email := "legacy-" + uuid.NewString() + "@example.invalid"
	user := dbfx.User(t, "Legacy invitee", email)
	workspace := dbfx.Workspace(t, "Legacy workspace", "legacy-"+uuid.NewString())
	member := dbfx.Member(t, workspace, user, memberRole)
	invitation := dbfx.Insert(t, "workspace_invitation", testutil.Cols{
		"workspace_id": workspace, "inviter_id": testUserID, "invitee_email": email,
		"invitee_user_id": user, "role": inviteRole, "status": "pending",
		"created_at": testutil.Raw("now() - interval '2 days'"),
		"expires_at": testutil.Raw("now() + interval '5 days'"),
	})
	t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM seat_capacity_outbox WHERE workspace_id=$1", parseUUID(workspace)) })
	return user, workspace, member, invitation
}

func legacyAccept(h *Handler, user, invitation string) *httptest.ResponseRecorder {
	req := newRequest("POST", "/api/invitations/"+invitation+"/accept", nil)
	req.Header.Set("X-User-ID", user)
	req = withURLParam(req, "id", invitation)
	w := httptest.NewRecorder()
	h.AcceptInvitation(w, req)
	return w
}

func TestAcceptInvitation_LegacyMemberPreservesMembership(t *testing.T) {
	for _, roles := range [][2]string{{"admin", "member"}, {"member", "admin"}, {"owner", "admin"}} {
		t.Run(roles[0]+"_invited_as_"+roles[1], func(t *testing.T) {
			user, workspace, member, invitation := legacyInvitationFixture(t, roles[0], roles[1])
			h := *testHandler
			h.SeatCapacity = nil
			h.Bus = events.New()
			accepted, added := 0, 0
			h.Bus.Subscribe(protocol.EventInvitationAccepted, func(events.Event) { accepted++ })
			h.Bus.Subscribe(protocol.EventMemberAdded, func(events.Event) { added++ })
			for i := 0; i < 2; i++ {
				w := legacyAccept(&h, user, invitation)
				if w.Code != http.StatusOK {
					t.Fatalf("accept %d: %d %s", i, w.Code, w.Body)
				}
				var got MemberWithUserResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.ID != member || got.Role != roles[0] {
					t.Fatalf("membership changed: %+v", got)
				}
			}
			if accepted != 1 || added != 0 {
				t.Fatalf("events: accepted=%d added=%d", accepted, added)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM member WHERE workspace_id=$1 AND user_id=$2", parseUUID(workspace), parseUUID(user)); n != 1 {
				t.Fatalf("members=%d", n)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM workspace_invitation WHERE id=$1 AND status='accepted'", parseUUID(invitation)); n != 1 {
				t.Fatal("invitation not settled")
			}
			rows, err := h.Queries.ListPendingInvitationsForUser(context.Background(), db.ListPendingInvitationsForUserParams{InviteeUserID: parseUUID(user), InviteeEmail: ""})
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if uuidToString(row.ID) == invitation {
					t.Fatal("still in account pending list")
				}
			}
		})
	}
}

func TestAcceptInvitation_LegacyMemberOwnershipExpiryAndLeave(t *testing.T) {
	user, workspace, member, invitation := legacyInvitationFixture(t, "admin", "member")
	h := *testHandler
	h.SeatCapacity = nil
	if w := legacyAccept(&h, testUserID, invitation); w.Code != http.StatusForbidden {
		t.Fatalf("foreign pending: %d", w.Code)
	}
	dbfx.Exec(t, "UPDATE workspace_invitation SET expires_at=now()-interval '1 second' WHERE id=$1", parseUUID(invitation))
	if w := legacyAccept(&h, user, invitation); w.Code != http.StatusGone {
		t.Fatalf("expired: %d", w.Code)
	}
	dbfx.Exec(t, "UPDATE workspace_invitation SET expires_at=now()+interval '1 day' WHERE id=$1", parseUUID(invitation))
	if w := legacyAccept(&h, user, invitation); w.Code != http.StatusOK {
		t.Fatalf("settle: %d %s", w.Code, w.Body)
	}
	if w := legacyAccept(&h, testUserID, invitation); w.Code != http.StatusForbidden {
		t.Fatalf("foreign accepted: %d", w.Code)
	}
	if _, err := h.revokeAndRemoveMember(context.Background(), parseUUID(workspace), parseUUID(user), parseUUID(member), parseUUID(user)); err != nil {
		t.Fatal(err)
	}
	if w := legacyAccept(&h, user, invitation); w.Code != http.StatusBadRequest {
		t.Fatalf("reaccept after leave: %d", w.Code)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM member WHERE workspace_id=$1 AND user_id=$2", parseUUID(workspace), parseUUID(user)); n != 0 {
		t.Fatal("member resurrected")
	}
}

type legacyBeginHook func(context.Context) (pgx.Tx, error)

func (f legacyBeginHook) Begin(ctx context.Context) (pgx.Tx, error) { return f(ctx) }

func TestAcceptInvitation_LegacyMemberLeaveDuringAccept(t *testing.T) {
	user, workspace, member, invitation := legacyInvitationFixture(t, "admin", "member")
	h := *testHandler
	h.SeatCapacity = nil
	reached, resume := make(chan struct{}), make(chan struct{})
	h.TxStarter = legacyBeginHook(func(ctx context.Context) (pgx.Tx, error) {
		close(reached)
		select {
		case <-resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return testPool.Begin(ctx)
	})
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- legacyAccept(&h, user, invitation) }()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		close(resume)
		t.Fatal("accept did not reach transaction")
	}
	_, err := testHandler.revokeAndRemoveMember(context.Background(), parseUUID(workspace), parseUUID(user), parseUUID(member), parseUUID(user))
	close(resume)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-result:
		if w.Code != http.StatusBadRequest {
			t.Fatalf("leave race: %d %s", w.Code, w.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("accept hung")
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM member WHERE workspace_id=$1 AND user_id=$2", parseUUID(workspace), parseUUID(user)); n != 0 {
		t.Fatal("member recreated after leave")
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM workspace_invitation WHERE id=$1 AND status='pending'", parseUUID(invitation)); n != 1 {
		t.Fatal("failed acceptance changed invitation")
	}
}

func TestAcceptInvitation_LegacyMemberConcurrentAccepts(t *testing.T) {
	user, workspace, member, invitation := legacyInvitationFixture(t, "admin", "member")
	h := *testHandler
	h.SeatCapacity = nil
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 4)
	for range 4 {
		wg.Go(func() { results <- legacyAccept(&h, user, invitation) })
	}
	wg.Wait()
	close(results)
	for w := range results {
		var got MemberWithUserResponse
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.ID != member || got.Role != "admin" {
			t.Fatalf("concurrent accept: %d %s", w.Code, w.Body)
		}
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM member WHERE workspace_id=$1 AND user_id=$2", parseUUID(workspace), parseUUID(user)); n != 1 {
		t.Fatal("duplicate membership")
	}
}

type legacyReadFailure struct{ db.DBTX }
type legacyErrorRow struct{}

func (legacyErrorRow) Scan(...any) error { return errors.New("temporary membership read failure") }
func (q legacyReadFailure) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "-- name: GetMemberByUserAndWorkspace :one") {
		return legacyErrorRow{}
	}
	return q.DBTX.QueryRow(ctx, sql, args...)
}

type legacyCommitFailure struct{ pgx.Tx }

func (tx legacyCommitFailure) Commit(ctx context.Context) error {
	_ = tx.Tx.Rollback(ctx)
	return errors.New("injected commit failure")
}

type legacyTxReadFailure struct{ pgx.Tx }

func (tx legacyTxReadFailure) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "-- name: GetMemberByUserAndWorkspaceForUpdate :one") {
		return legacyErrorRow{}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestAcceptInvitation_LegacyMemberDatabaseFailures(t *testing.T) {
	for _, stage := range []string{"membership_read", "locked_membership_read", "begin", "commit"} {
		t.Run(stage, func(t *testing.T) {
			user, _, _, invitation := legacyInvitationFixture(t, "admin", "member")
			h := *testHandler
			h.SeatCapacity = nil
			switch stage {
			case "membership_read":
				h.Queries = db.New(legacyReadFailure{testPool})
			case "locked_membership_read":
				h.TxStarter = legacyBeginHook(func(ctx context.Context) (pgx.Tx, error) {
					tx, e := testPool.Begin(ctx)
					return legacyTxReadFailure{tx}, e
				})
			case "begin":
				h.TxStarter = legacyBeginHook(func(context.Context) (pgx.Tx, error) { return nil, errors.New("temporary begin failure") })
			case "commit":
				h.TxStarter = legacyBeginHook(func(ctx context.Context) (pgx.Tx, error) {
					tx, e := testPool.Begin(ctx)
					return legacyCommitFailure{tx}, e
				})
			}
			if w := legacyAccept(&h, user, invitation); w.Code != 500 {
				t.Fatalf("%s: %d %s", stage, w.Code, w.Body)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM workspace_invitation WHERE id=$1 AND status='pending'", parseUUID(invitation)); n != 1 {
				t.Fatal("failure did not roll back invitation")
			}
			if w := legacyAccept(testHandler, user, invitation); w.Code != 200 {
				t.Fatalf("retry: %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestAcceptInvitation_LegacyMemberManagedCapacityRecovery(t *testing.T) {
	for _, mode := range []string{"managed", "unmanaged", "release_retry", "rollback", "concurrent", "confirm_guard", "interrupted_consume"} {
		t.Run(mode, func(t *testing.T) {
			user, workspace, member, invitation := legacyInvitationFixture(t, "admin", "member")
			var mu sync.Mutex
			state := "reserved"
			releases := 0
			forbidden := []string{}
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				var payload struct {
					Token string `json:"token"`
				}
				_ = json.NewDecoder(r.Body).Decode(&payload)
				if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/capacity/release") || payload.Token != invitation {
					forbidden = append(forbidden, r.Method+" "+r.URL.Path)
					http.Error(w, "unexpected capacity action", 500)
					return
				}
				releases++
				if mode == "release_retry" && releases == 1 {
					http.Error(w, "temporary capacity failure", 503)
					return
				}
				if state != "reserved" && state != "released" && state != "consuming" {
					http.Error(w, "cannot release member seat", 409)
					return
				}
				state = "released"
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(seatcapacity.Decision{Managed: mode != "unmanaged", Allowed: true, Reason: "released"})
			}))
			defer cloud.Close()
			executor, err := seatcapacity.New(seatcapacity.Config{BaseURL: cloud.URL})
			if err != nil {
				t.Fatal(err)
			}
			h := *testHandler
			h.SeatCapacity = executor
			h.SeatCapacityLocker = seatcapacity.NewWorkspaceLocker(testPool)
			if mode == "interrupted_consume" {
				state = "consuming"
				params := capacityIntentParams(uuid.MustParse(workspace), uuid.MustParse(invitation), seatcapacity.ActionConsumeInvitation, time.Now())
				params.SubjectID = parseUUID(invitation)
				params.InvitationID = parseUUID(invitation)
				params.UserID = parseUUID(user)
				if _, err := h.Queries.UpsertSeatCapacityIntent(context.Background(), params); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "confirm_guard" {
				params := capacityIntentParams(uuid.MustParse(workspace), uuid.MustParse(invitation), seatcapacity.ActionConfirm, time.Now())
				params.MemberID = parseUUID(member)
				if _, err := h.Queries.UpsertSeatCapacityIntent(context.Background(), params); err != nil {
					t.Fatal(err)
				}
				w := legacyAccept(&h, user, invitation)
				if w.Code != 500 {
					t.Fatalf("confirm guard: %d %s", w.Code, w.Body)
				}
				if n := dbfx.Count(t, "SELECT count(*) FROM workspace_invitation WHERE id=$1 AND status='pending'", parseUUID(invitation)); n != 1 {
					t.Fatal("protected confirm changed invitation")
				}
				mu.Lock()
				defer mu.Unlock()
				if releases != 0 || len(forbidden) != 0 {
					t.Fatal("released protected member capacity")
				}
				return
			}
			if mode == "rollback" {
				h.TxStarter = legacyBeginHook(func(ctx context.Context) (pgx.Tx, error) {
					tx, e := testPool.Begin(ctx)
					return legacyCommitFailure{tx}, e
				})
			}
			w := legacyAccept(&h, user, invitation)
			if mode == "concurrent" {
				// Reopen only the synthetic fixture, then race requests through
				// the real workspace locker, transaction and capacity adapter.
				dbfx.Exec(t, "UPDATE workspace_invitation SET status='pending' WHERE id=$1", parseUUID(invitation))
				mu.Lock()
				releases = 0
				state = "reserved"
				mu.Unlock()
				results := make(chan *httptest.ResponseRecorder, 4)
				var wg sync.WaitGroup
				for range 4 {
					wg.Go(func() { results <- legacyAccept(&h, user, invitation) })
				}
				wg.Wait()
				close(results)
				for result := range results {
					if result.Code != 200 {
						t.Fatalf("concurrent managed: %d %s", result.Code, result.Body)
					}
				}
			}
			if mode == "rollback" {
				if w.Code != 500 {
					t.Fatalf("rollback: %d %s", w.Code, w.Body)
				}
				if n := dbfx.Count(t, "SELECT count(*) FROM seat_capacity_outbox WHERE operation_token=$1", parseUUID(invitation)); n != 0 {
					t.Fatal("outbox escaped rollback")
				}
				if n := dbfx.Count(t, "SELECT count(*) FROM workspace_invitation WHERE id=$1 AND status='pending'", parseUUID(invitation)); n != 1 {
					t.Fatal("invitation escaped rollback")
				}
				mu.Lock()
				defer mu.Unlock()
				if releases != 0 || state != "reserved" {
					t.Fatal("released before commit")
				}
				return
			}
			if w.Code != 200 {
				t.Fatalf("settle: %d %s", w.Code, w.Body)
			}
			if mode == "release_retry" {
				if n := dbfx.Count(t, "SELECT count(*) FROM seat_capacity_outbox WHERE operation_token=$1 AND action='release'", parseUUID(invitation)); n != 1 {
					t.Fatal("missing durable release")
				}
				dbfx.Exec(t, "UPDATE seat_capacity_outbox SET next_attempt_at=now() WHERE operation_token=$1", parseUUID(invitation))
				worker := seatcapacity.NewWorker(testHandler.Queries, executor, h.SeatCapacityLocker, seatcapacity.WorkerConfig{})
				if err := worker.ReconcileOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := worker.ReconcileOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if w := legacyAccept(&h, user, invitation); w.Code != 200 {
				t.Fatalf("retry accept: %d %s", w.Code, w.Body)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM seat_capacity_outbox WHERE operation_token=$1", parseUUID(invitation)); n != 0 {
				t.Fatal("release not settled")
			}
			var got MemberWithUserResponse
			if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.ID != member || got.Role != "admin" {
				t.Fatalf("member changed: %s", w.Body)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM member WHERE workspace_id=$1 AND user_id=$2", parseUUID(workspace), parseUUID(user)); n != 1 {
				t.Fatal("duplicate member")
			}
			mu.Lock()
			defer mu.Unlock()
			want := 1
			if mode == "release_retry" {
				want = 2
			}
			if releases != want || state != "released" || len(forbidden) != 0 {
				t.Fatalf("capacity: releases=%d state=%s forbidden=%v", releases, state, forbidden)
			}
		})
	}
}

func TestAcceptInvitation_NormalInvitationCapacityContract(t *testing.T) {
	for _, managed := range []bool{true, false} {
		name := "unmanaged"
		if managed {
			name = "managed"
		}
		t.Run(name, func(t *testing.T) {
			user, workspace, oldMember, invitation := legacyInvitationFixture(t, "admin", "member")
			// This is the ordinary non-member control, using a synthetic already
			// issued invitation. No production leave or share-link path is replaced.
			dbfx.Exec(t, "DELETE FROM member WHERE id=$1", parseUUID(oldMember))
			var mu sync.Mutex
			var actions []string
			state, confirmedMember := "reserved", ""
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				var payload struct {
					Token    string `json:"token"`
					MemberID string `json:"member_id"`
				}
				if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Token != invitation || r.Method != "POST" {
					http.Error(w, "invalid operation", 500)
					return
				}
				action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				actions = append(actions, action)
				switch {
				case action == "consume" && state == "reserved":
					if managed {
						state = "consuming"
					}
				case action == "confirm" && managed && state == "consuming" && payload.MemberID != "":
					state = "used"
					confirmedMember = payload.MemberID
				default:
					http.Error(w, "unexpected capacity transition", 500)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(seatcapacity.Decision{Managed: managed, Allowed: true})
			}))
			defer cloud.Close()
			executor, err := seatcapacity.New(seatcapacity.Config{BaseURL: cloud.URL})
			if err != nil {
				t.Fatal(err)
			}
			h := *testHandler
			h.SeatCapacity = executor
			h.SeatCapacityLocker = seatcapacity.NewWorkspaceLocker(testPool)
			w := legacyAccept(&h, user, invitation)
			var got MemberWithUserResponse
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.ID == oldMember || got.Role != "member" {
				t.Fatalf("normal accept: %d %s", w.Code, w.Body)
			}
			t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM member WHERE id=$1", parseUUID(got.ID)) })
			if w := legacyAccept(&h, user, invitation); w.Code != 200 {
				t.Fatalf("repeat: %d %s", w.Code, w.Body)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM member WHERE workspace_id=$1 AND user_id=$2", parseUUID(workspace), parseUUID(user)); n != 1 {
				t.Fatal("wrong member count")
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM \"user\" WHERE id=$1 AND onboarded_at IS NOT NULL", parseUUID(user)); n != 1 {
				t.Fatal("normal onboarding lost")
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM seat_capacity_outbox WHERE operation_token=$1", parseUUID(invitation)); n != 0 {
				t.Fatal("normal outbox not settled")
			}
			mu.Lock()
			defer mu.Unlock()
			if managed {
				if strings.Join(actions, ",") != "consume,confirm" || state != "used" || confirmedMember != got.ID {
					t.Fatalf("managed actions=%v state=%s member=%s", actions, state, confirmedMember)
				}
			} else if strings.Join(actions, ",") != "consume" {
				t.Fatalf("unmanaged actions=%v", actions)
			}
		})
	}
}
