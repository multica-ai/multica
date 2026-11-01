package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── helpers ─────────────────────────────────────────────────────────────────

// resetOffice empties the shared test workspace's floors and seats now and
// again on cleanup. Squads created by other tests auto-seat, so a test that
// asserts on exact floors and slots starts from a clean office.
func resetOffice(t *testing.T) {
	t.Helper()
	clean := func() {
		ctx := context.Background()
		testPool.Exec(ctx, `UPDATE squad SET floor_id = NULL, office_slot = NULL WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM office_floor WHERE workspace_id = $1`, testWorkspaceID)
	}
	clean()
	t.Cleanup(clean)
}

// officeReq builds a request as userID (empty = the workspace owner) against
// the given workspace with chi URL params.
func officeReq(userID, wsID, method, path string, body any, params map[string]string) *http.Request {
	var req *http.Request
	if userID == "" {
		req = newRequest(method, path, body)
	} else {
		req = newRequestAs(userID, method, path, body)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceId", wsID)
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func officeCall(h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return out
}

func wantStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("expected %d, got %d: %s", want, w.Code, w.Body.String())
	}
}

func createFloor(t *testing.T, userID, wsID, name string) OfficeFloorResponse {
	t.Helper()
	w := officeCall(testHandler.CreateOfficeFloor, officeReq(userID, wsID, "POST", "/api/office/floors", map[string]any{"name": name}, nil))
	wantStatus(t, w, http.StatusCreated)
	return decodeBody[OfficeFloorResponse](t, w)
}

func listFloors(t *testing.T, wsID string) []OfficeFloorResponse {
	t.Helper()
	w := officeCall(testHandler.ListOfficeFloors, officeReq("", wsID, "GET", "/api/office/floors", nil, nil))
	wantStatus(t, w, http.StatusOK)
	return decodeBody[[]OfficeFloorResponse](t, w)
}

// seedSquad inserts an unseated squad led by leaderID (also its member).
func seedOfficeSquad(t *testing.T, name, leaderID string) string {
	t.Helper()
	squadID := dbfx.Squad(t, name, leaderID)
	dbfx.SquadMember(t, squadID, "agent", leaderID, map[string]any{"role": "leader"})
	return squadID
}

func putSeats(userID, wsID string, items any) *httptest.ResponseRecorder {
	return officeCall(testHandler.PutOfficeSeats, officeReq(userID, wsID, "PUT", "/api/office/seats", items, nil))
}

func seatOf(t *testing.T, squadID string) (floorID string, slot int) {
	t.Helper()
	var f *string
	var s *int16
	if err := testPool.QueryRow(context.Background(),
		`SELECT floor_id::text, office_slot FROM squad WHERE id = $1`, squadID).Scan(&f, &s); err != nil {
		t.Fatalf("read seat: %v", err)
	}
	if f == nil || s == nil {
		return "", -1
	}
	return *f, int(*s)
}

func homeOf(t *testing.T, agentID string) string {
	t.Helper()
	var home *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT home_squad_id::text FROM agent WHERE id = $1`, agentID).Scan(&home); err != nil {
		t.Fatalf("read home squad: %v", err)
	}
	if home == nil {
		return ""
	}
	return *home
}

func requireOfficeDB(t *testing.T) {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
}

// ── floors ──────────────────────────────────────────────────────────────────

func TestOfficeFloors_CRUDAndOrder(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)

	a := createFloor(t, "", testWorkspaceID, "  Research  ")
	if a.Name != "Research" {
		t.Fatalf("name should be trimmed, got %q", a.Name)
	}
	b := createFloor(t, "", testWorkspaceID, "Product")
	c := createFloor(t, "", testWorkspaceID, "Build")
	if a.Position != 0 || b.Position != 1 || c.Position != 2 {
		t.Fatalf("floors should append: positions %d %d %d", a.Position, b.Position, c.Position)
	}

	names := func() string {
		var out []string
		for _, f := range listFloors(t, testWorkspaceID) {
			out = append(out, fmt.Sprintf("%s@%d", f.Name, f.Position))
		}
		return strings.Join(out, ",")
	}
	if got := names(); got != "Research@0,Product@1,Build@2" {
		t.Fatalf("list order: %s", got)
	}

	// Rename only.
	w := officeCall(testHandler.UpdateOfficeFloor, officeReq("", testWorkspaceID, "PUT", "/api/office/floors/"+b.ID, map[string]any{"name": "Roadmap"}, map[string]string{"id": b.ID}))
	wantStatus(t, w, http.StatusOK)
	if got := decodeBody[OfficeFloorResponse](t, w).Name; got != "Roadmap" {
		t.Fatalf("rename: %q", got)
	}

	// Move the last floor to the front: the others are renumbered.
	w = officeCall(testHandler.UpdateOfficeFloor, officeReq("", testWorkspaceID, "PUT", "/api/office/floors/"+c.ID, map[string]any{"position": 0}, map[string]string{"id": c.ID}))
	wantStatus(t, w, http.StatusOK)
	if got := names(); got != "Build@0,Research@1,Roadmap@2" {
		t.Fatalf("after move to front: %s", got)
	}

	// A position past the end clamps to the last index.
	w = officeCall(testHandler.UpdateOfficeFloor, officeReq("", testWorkspaceID, "PUT", "/api/office/floors/"+c.ID, map[string]any{"position": 99}, map[string]string{"id": c.ID}))
	wantStatus(t, w, http.StatusOK)
	if got := names(); got != "Research@0,Roadmap@1,Build@2" {
		t.Fatalf("after move past the end: %s", got)
	}

	// Rename and move in one request.
	w = officeCall(testHandler.UpdateOfficeFloor, officeReq("", testWorkspaceID, "PUT", "/api/office/floors/"+c.ID, map[string]any{"name": "Launch", "position": 1}, map[string]string{"id": c.ID}))
	wantStatus(t, w, http.StatusOK)
	if got := names(); got != "Research@0,Launch@1,Roadmap@2" {
		t.Fatalf("after rename+move: %s", got)
	}

	// Delete an empty floor.
	w = officeCall(testHandler.DeleteOfficeFloor, officeReq("", testWorkspaceID, "DELETE", "/api/office/floors/"+a.ID, nil, map[string]string{"id": a.ID}))
	wantStatus(t, w, http.StatusNoContent)
	if got := names(); got != "Launch@1,Roadmap@2" {
		t.Fatalf("after delete: %s", got)
	}
}

func TestOfficeFloors_Validation(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	floor := createFloor(t, "", testWorkspaceID, "Valid")

	for _, name := range []string{"", "   ", strings.Repeat("x", 65)} {
		w := officeCall(testHandler.CreateOfficeFloor, officeReq("", testWorkspaceID, "POST", "/api/office/floors", map[string]any{"name": name}, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("create name %q: expected 400, got %d", name, w.Code)
		}
	}
	// 64 characters, counted as code points, is the longest accepted name.
	createFloor(t, "", testWorkspaceID, strings.Repeat("é", 64))

	cases := []struct {
		name   string
		id     string
		body   any
		status int
	}{
		{"empty update", floor.ID, map[string]any{}, http.StatusBadRequest},
		{"empty name", floor.ID, map[string]any{"name": " "}, http.StatusBadRequest},
		{"negative position", floor.ID, map[string]any{"position": -1}, http.StatusBadRequest},
		{"bad id", "not-a-uuid", map[string]any{"name": "x"}, http.StatusBadRequest},
		{"unknown floor", util.UUIDToString(pgtype.UUID{Bytes: [16]byte{1}, Valid: true}), map[string]any{"name": "x"}, http.StatusNotFound},
	}
	for _, tc := range cases {
		w := officeCall(testHandler.UpdateOfficeFloor, officeReq("", testWorkspaceID, "PUT", "/api/office/floors/"+tc.id, tc.body, map[string]string{"id": tc.id}))
		if w.Code != tc.status {
			t.Fatalf("%s: expected %d, got %d: %s", tc.name, tc.status, w.Code, w.Body.String())
		}
	}
	w := officeCall(testHandler.DeleteOfficeFloor, officeReq("", testWorkspaceID, "DELETE", "/api/office/floors/bad", nil, map[string]string{"id": "bad"}))
	wantStatus(t, w, http.StatusBadRequest)
}

func TestOfficeFloors_CapOfSixteen(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)

	for i := 0; i < officeMaxFloors; i++ {
		createFloor(t, "", testWorkspaceID, fmt.Sprintf("F%d", i))
	}
	w := officeCall(testHandler.CreateOfficeFloor, officeReq("", testWorkspaceID, "POST", "/api/office/floors", map[string]any{"name": "one too many"}, nil))
	wantStatus(t, w, http.StatusConflict)
	if code := decodeBody[map[string]any](t, w)["code"]; code != "floor_limit" {
		t.Fatalf("code = %v, want floor_limit", code)
	}
	if got := len(listFloors(t, testWorkspaceID)); got != officeMaxFloors {
		t.Fatalf("floor count = %d, want %d", got, officeMaxFloors)
	}
}

func TestOfficeFloors_OnlyOwnersAndAdminsManage(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	memberID := createPlainMember(t, "office-floors-member@multica.test")
	floor := createFloor(t, "", testWorkspaceID, "Owner floor")

	w := officeCall(testHandler.CreateOfficeFloor, officeReq(memberID, testWorkspaceID, "POST", "/api/office/floors", map[string]any{"name": "Nope"}, nil))
	wantStatus(t, w, http.StatusForbidden)
	w = officeCall(testHandler.UpdateOfficeFloor, officeReq(memberID, testWorkspaceID, "PUT", "/api/office/floors/"+floor.ID, map[string]any{"name": "Nope"}, map[string]string{"id": floor.ID}))
	wantStatus(t, w, http.StatusForbidden)
	w = officeCall(testHandler.DeleteOfficeFloor, officeReq(memberID, testWorkspaceID, "DELETE", "/api/office/floors/"+floor.ID, nil, map[string]string{"id": floor.ID}))
	wantStatus(t, w, http.StatusForbidden)

	// Reading is open to every member.
	w = officeCall(testHandler.ListOfficeFloors, officeReq(memberID, testWorkspaceID, "GET", "/api/office/floors", nil, nil))
	wantStatus(t, w, http.StatusOK)
	if got := len(decodeBody[[]OfficeFloorResponse](t, w)); got != 1 {
		t.Fatalf("member should see the floor, got %d", got)
	}

	// An admin may manage.
	adminID := createPlainMember(t, "office-floors-admin@multica.test")
	if _, err := testPool.Exec(context.Background(), `UPDATE member SET role = 'admin' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, adminID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	createFloor(t, adminID, testWorkspaceID, "Admin floor")
}

func TestOfficeFloors_DeleteRefusedWhileOccupied(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-delete-leader", nil)
	floor := createFloor(t, "", testWorkspaceID, "Busy")
	squadID := seedOfficeSquad(t, "Office delete squad", leader)

	w := putSeats("", testWorkspaceID, []map[string]any{{"squad_id": squadID, "floor_id": floor.ID, "slot": 3}})
	wantStatus(t, w, http.StatusOK)

	del := func() *httptest.ResponseRecorder {
		return officeCall(testHandler.DeleteOfficeFloor, officeReq("", testWorkspaceID, "DELETE", "/api/office/floors/"+floor.ID, nil, map[string]string{"id": floor.ID}))
	}
	w = del()
	wantStatus(t, w, http.StatusConflict)
	if code := decodeBody[map[string]any](t, w)["code"]; code != "floor_not_empty" {
		t.Fatalf("code = %v, want floor_not_empty", code)
	}
	if len(listFloors(t, testWorkspaceID)) != 1 {
		t.Fatal("the occupied floor must survive")
	}

	// Unseating the squad empties the floor.
	w = putSeats("", testWorkspaceID, []map[string]any{{"squad_id": squadID, "floor_id": nil, "slot": nil}})
	wantStatus(t, w, http.StatusOK)
	wantStatus(t, del(), http.StatusNoContent)
}

func TestOfficeFloors_ArchivedSquadDoesNotBlockDelete(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-archived-leader", nil)

	squad := createSquadAs(t, "", "Office archived squad", leader)
	if squad.FloorID == nil {
		t.Fatal("a new squad should be auto-seated")
	}
	floorID := *squad.FloorID

	w := officeCall(testHandler.DeleteSquad, squadScopeReq("", "DELETE", "/api/squads", nil, map[string]string{"id": squad.ID}))
	wantStatus(t, w, http.StatusNoContent)
	if f, s := seatOf(t, squad.ID); f != "" || s != -1 {
		t.Fatalf("archiving should free the seat, got floor=%q slot=%d", f, s)
	}

	w = officeCall(testHandler.DeleteOfficeFloor, officeReq("", testWorkspaceID, "DELETE", "/api/office/floors/"+floorID, nil, map[string]string{"id": floorID}))
	wantStatus(t, w, http.StatusNoContent)
}

// ── seats ───────────────────────────────────────────────────────────────────

func TestOfficeSeats_PlaceMoveAndUnseat(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-seat-leader", nil)
	floor := createFloor(t, "", testWorkspaceID, "Seats")
	squadID := seedOfficeSquad(t, "Seat squad", leader)

	w := putSeats("", testWorkspaceID, []map[string]any{{"squad_id": squadID, "floor_id": floor.ID, "slot": 5}})
	wantStatus(t, w, http.StatusOK)
	resp := decodeBody[OfficeSeatsResponse](t, w)
	if len(resp.Squads) != 1 || resp.Squads[0].FloorID == nil || *resp.Squads[0].FloorID != floor.ID || resp.Squads[0].OfficeSlot == nil || *resp.Squads[0].OfficeSlot != 5 {
		t.Fatalf("response should carry the new seat: %+v", resp.Squads)
	}
	if f, s := seatOf(t, squadID); f != floor.ID || s != 5 {
		t.Fatalf("seat = %s/%d", f, s)
	}

	// Re-sending the same seat is a no-op, not a conflict with itself.
	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{{"squad_id": squadID, "floor_id": floor.ID, "slot": 5}}), http.StatusOK)

	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{{"squad_id": squadID, "floor_id": floor.ID, "slot": 0}}), http.StatusOK)
	if _, s := seatOf(t, squadID); s != 0 {
		t.Fatalf("slot = %d, want 0", s)
	}

	w = putSeats("", testWorkspaceID, []map[string]any{{"squad_id": squadID, "floor_id": nil, "slot": nil}})
	wantStatus(t, w, http.StatusOK)
	resp = decodeBody[OfficeSeatsResponse](t, w)
	if resp.Squads[0].FloorID != nil || resp.Squads[0].OfficeSlot != nil {
		t.Fatalf("unseated squad should report null seat: %+v", resp.Squads[0])
	}
	if f, s := seatOf(t, squadID); f != "" || s != -1 {
		t.Fatalf("seat should be cleared, got %s/%d", f, s)
	}
}

func TestOfficeSeats_ConflictNamesTheHolder(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-conflict-leader", nil)
	floor := createFloor(t, "", testWorkspaceID, "Conflict")
	holder := seedOfficeSquad(t, "Holder squad", leader)
	other := seedOfficeSquad(t, "Other squad", leader)

	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{{"squad_id": holder, "floor_id": floor.ID, "slot": 2}}), http.StatusOK)

	w := putSeats("", testWorkspaceID, []map[string]any{{"squad_id": other, "floor_id": floor.ID, "slot": 2}})
	wantStatus(t, w, http.StatusConflict)
	body := decodeBody[map[string]any](t, w)
	if body["code"] != "seat_conflict" {
		t.Fatalf("code = %v", body["code"])
	}
	conflict, _ := body["conflict"].(map[string]any)
	if conflict["squad_id"] != holder || conflict["squad_name"] != "Holder squad" || conflict["floor_id"] != floor.ID || conflict["slot"] != float64(2) {
		t.Fatalf("conflict should name the holder: %v", conflict)
	}
	if f, _ := seatOf(t, other); f != "" {
		t.Fatal("the refused squad must stay unseated")
	}
}

func TestOfficeSeats_UniqueIndexBacksUpTheCheck(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-uidx-leader", nil)
	floor := createFloor(t, "", testWorkspaceID, "Index")
	a := seedOfficeSquad(t, "Index A", leader)
	b := seedOfficeSquad(t, "Index B", leader)
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `UPDATE squad SET floor_id = $2, office_slot = 1 WHERE id = $1`, a, floor.ID); err != nil {
		t.Fatalf("seat a: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE squad SET floor_id = $2, office_slot = 1 WHERE id = $1`, b, floor.ID); !isUniqueViolation(err) {
		t.Fatalf("a second live squad on the same seat must violate the unique index, got %v", err)
	}
	// An archived squad releases the seat.
	if _, err := testPool.Exec(ctx, `UPDATE squad SET archived_at = now() WHERE id = $1`, a); err != nil {
		t.Fatalf("archive a: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE squad SET floor_id = $2, office_slot = 1 WHERE id = $1`, b, floor.ID); err != nil {
		t.Fatalf("seat b after a was archived: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE squad SET office_slot = 6 WHERE id = $1`, b); err == nil {
		t.Fatal("slot 6 must violate the check constraint")
	}
}

func TestOfficeSeats_SwapIsOneTransaction(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-swap-leader", nil)
	floor := createFloor(t, "", testWorkspaceID, "Swap")
	floor2 := createFloor(t, "", testWorkspaceID, "Swap 2")
	a := seedOfficeSquad(t, "Swap A", leader)
	b := seedOfficeSquad(t, "Swap B", leader)
	c := seedOfficeSquad(t, "Swap C", leader)

	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{
		{"squad_id": a, "floor_id": floor.ID, "slot": 0},
		{"squad_id": b, "floor_id": floor.ID, "slot": 1},
		{"squad_id": c, "floor_id": floor2.ID, "slot": 4},
	}), http.StatusOK)

	// Swap A and B in one request, across the unique index.
	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{
		{"squad_id": a, "floor_id": floor.ID, "slot": 1},
		{"squad_id": b, "floor_id": floor.ID, "slot": 0},
	}), http.StatusOK)
	if _, s := seatOf(t, a); s != 1 {
		t.Fatalf("a slot = %d, want 1", s)
	}
	if _, s := seatOf(t, b); s != 0 {
		t.Fatalf("b slot = %d, want 0", s)
	}

	// A three-way rotation across floors.
	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{
		{"squad_id": a, "floor_id": floor2.ID, "slot": 4},
		{"squad_id": c, "floor_id": floor.ID, "slot": 0},
		{"squad_id": b, "floor_id": floor.ID, "slot": 1},
	}), http.StatusOK)
	if f, s := seatOf(t, a); f != floor2.ID || s != 4 {
		t.Fatalf("a seat = %s/%d", f, s)
	}
	if f, s := seatOf(t, c); f != floor.ID || s != 0 {
		t.Fatalf("c seat = %s/%d", f, s)
	}
	if f, s := seatOf(t, b); f != floor.ID || s != 1 {
		t.Fatalf("b seat = %s/%d", f, s)
	}
}

func TestOfficeSeats_FailedRequestChangesNothing(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-atomic-leader", nil)
	floor := createFloor(t, "", testWorkspaceID, "Atomic")
	a := seedOfficeSquad(t, "Atomic A", leader)
	b := seedOfficeSquad(t, "Atomic B", leader)
	blocker := seedOfficeSquad(t, "Atomic blocker", leader)

	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{
		{"squad_id": a, "floor_id": floor.ID, "slot": 0},
		{"squad_id": b, "floor_id": floor.ID, "slot": 1},
		{"squad_id": blocker, "floor_id": floor.ID, "slot": 2},
	}), http.StatusOK)

	// The swap itself is fine, but b also wants the blocker's seat: all or nothing.
	w := putSeats("", testWorkspaceID, []map[string]any{
		{"squad_id": a, "floor_id": floor.ID, "slot": 1},
		{"squad_id": b, "floor_id": floor.ID, "slot": 2},
	})
	wantStatus(t, w, http.StatusConflict)
	if _, s := seatOf(t, a); s != 0 {
		t.Fatalf("a must stay on slot 0 after a refused request, got %d", s)
	}
	if _, s := seatOf(t, b); s != 1 {
		t.Fatalf("b must stay on slot 1 after a refused request, got %d", s)
	}
}

func TestOfficeSeats_TwoSquadsOnOneSeatInOneRequest(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-dupseat-leader", nil)
	floor := createFloor(t, "", testWorkspaceID, "Dup")
	a := seedOfficeSquad(t, "Dup A", leader)
	b := seedOfficeSquad(t, "Dup B", leader)

	w := putSeats("", testWorkspaceID, []map[string]any{
		{"squad_id": a, "floor_id": floor.ID, "slot": 0},
		{"squad_id": b, "floor_id": floor.ID, "slot": 0},
	})
	wantStatus(t, w, http.StatusConflict)
	if f, _ := seatOf(t, a); f != "" {
		t.Fatal("nothing may be applied when two squads ask for one seat")
	}
}

func TestOfficeSeats_Validation(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-valid-leader", nil)
	floor := createFloor(t, "", testWorkspaceID, "Valid")
	squadID := seedOfficeSquad(t, "Valid squad", leader)
	archived := seedOfficeSquad(t, "Archived squad", leader)
	if _, err := testPool.Exec(context.Background(), `UPDATE squad SET archived_at = now() WHERE id = $1`, archived); err != nil {
		t.Fatalf("archive: %v", err)
	}
	unknown := util.UUIDToString(pgtype.UUID{Bytes: [16]byte{9}, Valid: true})

	cases := []struct {
		name string
		body any
	}{
		{"not an array", map[string]any{"squad_id": squadID}},
		{"empty array", []any{}},
		{"malformed json", "{"},
		{"bad squad id", []map[string]any{{"squad_id": "nope", "floor_id": nil, "slot": nil}}},
		{"floor without slot", []map[string]any{{"squad_id": squadID, "floor_id": floor.ID}}},
		{"slot without floor", []map[string]any{{"squad_id": squadID, "slot": 1}}},
		{"slot too high", []map[string]any{{"squad_id": squadID, "floor_id": floor.ID, "slot": 6}}},
		{"slot negative", []map[string]any{{"squad_id": squadID, "floor_id": floor.ID, "slot": -1}}},
		{"bad floor id", []map[string]any{{"squad_id": squadID, "floor_id": "nope", "slot": 1}}},
		{"unknown floor", []map[string]any{{"squad_id": squadID, "floor_id": unknown, "slot": 1}}},
		{"unknown squad", []map[string]any{{"squad_id": unknown, "floor_id": floor.ID, "slot": 1}}},
		{"archived squad", []map[string]any{{"squad_id": archived, "floor_id": floor.ID, "slot": 1}}},
		{"same squad twice", []map[string]any{
			{"squad_id": squadID, "floor_id": floor.ID, "slot": 1},
			{"squad_id": squadID, "floor_id": floor.ID, "slot": 2},
		}},
	}
	for _, tc := range cases {
		w := putSeats("", testWorkspaceID, tc.body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", tc.name, w.Code, w.Body.String())
		}
	}
	if f, _ := seatOf(t, squadID); f != "" {
		t.Fatal("rejected requests must not seat anything")
	}
}

func TestOfficeSeats_NonAdminNeedsToBeTheCreator(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	creatorID := createPlainMember(t, "office-seat-creator@multica.test")
	strangerID := createPlainMember(t, "office-seat-stranger@multica.test")
	leader := createHandlerTestAgent(t, "office-perm-leader", nil)
	floor := createFloor(t, "", testWorkspaceID, "Perms")

	mine := createSquadAs(t, creatorID, "Mine", leader)
	theirs := seedOfficeSquad(t, "Not mine", leader) // created by the owner fixture user
	// Free every auto-seat so the test chooses seats itself.
	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{{"squad_id": mine.ID, "floor_id": nil, "slot": nil}}), http.StatusOK)

	// The creator moves their own squad.
	wantStatus(t, putSeats(creatorID, testWorkspaceID, []map[string]any{{"squad_id": mine.ID, "floor_id": floor.ID, "slot": 3}}), http.StatusOK)

	// A stranger cannot.
	wantStatus(t, putSeats(strangerID, testWorkspaceID, []map[string]any{{"squad_id": mine.ID, "floor_id": floor.ID, "slot": 4}}), http.StatusForbidden)
	if _, s := seatOf(t, mine.ID); s != 3 {
		t.Fatalf("a refused request must not move the squad, slot=%d", s)
	}

	// The creator cannot touch another creator's squad, and a mixed request
	// applies nothing.
	w := putSeats(creatorID, testWorkspaceID, []map[string]any{
		{"squad_id": mine.ID, "floor_id": floor.ID, "slot": 5},
		{"squad_id": theirs, "floor_id": floor.ID, "slot": 4},
	})
	wantStatus(t, w, http.StatusForbidden)
	if _, s := seatOf(t, mine.ID); s != 3 {
		t.Fatalf("a mixed request must not partially apply, slot=%d", s)
	}

	// An owner may seat anything.
	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{{"squad_id": theirs, "floor_id": floor.ID, "slot": 4}}), http.StatusOK)
}

// ── auto-seat on squad create ───────────────────────────────────────────────

func TestCreateSquad_AutoSeatsLowestFreeSlotAndOverflowsToNewFloor(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-auto-leader", nil)

	var squads []SquadResponse
	for i := 0; i < officeSlotsPerFloor; i++ {
		s := createSquadAs(t, "", fmt.Sprintf("Auto %d", i), leader)
		squads = append(squads, s)
		if s.FloorID == nil || s.OfficeSlot == nil || int(*s.OfficeSlot) != i {
			t.Fatalf("squad %d should take slot %d of the first floor, got %v/%v", i, i, s.FloorID, s.OfficeSlot)
		}
		if *s.FloorID != *squads[0].FloorID {
			t.Fatalf("squad %d should share the first floor", i)
		}
	}
	floors := listFloors(t, testWorkspaceID)
	if len(floors) != 1 || floors[0].Name != "Floor 1" || floors[0].ID != *squads[0].FloorID {
		t.Fatalf("expected one auto-created Floor 1, got %+v", floors)
	}

	// The seventh squad overflows onto a new floor.
	seventh := createSquadAs(t, "", "Auto 6", leader)
	floors = listFloors(t, testWorkspaceID)
	if len(floors) != 2 || floors[1].Name != "Floor 2" {
		t.Fatalf("expected a new Floor 2, got %+v", floors)
	}
	if seventh.FloorID == nil || *seventh.FloorID != floors[1].ID || *seventh.OfficeSlot != 0 {
		t.Fatalf("seventh squad should take slot 0 of Floor 2, got %v/%v", seventh.FloorID, seventh.OfficeSlot)
	}

	// A freed seat is reused before anything on a later floor.
	w := officeCall(testHandler.DeleteSquad, squadScopeReq("", "DELETE", "/api/squads", nil, map[string]string{"id": squads[2].ID}))
	wantStatus(t, w, http.StatusNoContent)
	reuse := createSquadAs(t, "", "Auto reuse", leader)
	if *reuse.FloorID != *squads[0].FloorID || *reuse.OfficeSlot != 2 {
		t.Fatalf("freed slot 2 of floor 1 should be reused, got %v/%v", *reuse.FloorID, *reuse.OfficeSlot)
	}
}

func TestCreateSquad_FloorNameOnlyWhenANewFloorIsNeeded(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-floorname-leader", nil)

	create := func(name, floorName string) (*httptest.ResponseRecorder, SquadResponse) {
		w := officeCall(testHandler.CreateSquad, squadScopeReq("", "POST", "/api/squads", map[string]any{
			"name": name, "leader_id": leader, "floor_name": floorName,
		}, nil))
		if w.Code != http.StatusCreated {
			return w, SquadResponse{}
		}
		resp := decodeBody[SquadResponse](t, w)
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, resp.ID)
			testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, resp.ID)
		})
		return w, resp
	}

	// No floor exists yet, so the template's name is used.
	w, first := create("Named 1", "Research")
	wantStatus(t, w, http.StatusCreated)
	floors := listFloors(t, testWorkspaceID)
	if len(floors) != 1 || floors[0].Name != "Research" || first.FloorID == nil || *first.FloorID != floors[0].ID {
		t.Fatalf("the new floor should carry the template name: %+v", floors)
	}

	// A free slot exists: the name is ignored and no floor is created.
	w, _ = create("Named 2", "Ignored")
	wantStatus(t, w, http.StatusCreated)
	if got := len(listFloors(t, testWorkspaceID)); got != 1 {
		t.Fatalf("no new floor expected, got %d", got)
	}

	// An invalid name is rejected even when it would not be used.
	w, _ = create("Named 3", strings.Repeat("x", 65))
	wantStatus(t, w, http.StatusBadRequest)
}

func TestCreateSquad_StaysUnseatedWhenEveryFloorIsFull(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-full-leader", nil)
	ctx := context.Background()

	for i := 0; i < officeMaxFloors; i++ {
		createFloor(t, "", testWorkspaceID, fmt.Sprintf("Full %d", i))
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id, floor_id, office_slot)
		SELECT $1, 'seedfull-' || f.position || '-' || s, '', $2, $3, f.id, s
		FROM office_floor f, generate_series(0, 5) AS s
		WHERE f.workspace_id = $1`, testWorkspaceID, leader, testUserID); err != nil {
		t.Fatalf("fill the office: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM squad WHERE workspace_id = $1 AND name LIKE 'seedfull-%'`, testWorkspaceID)
	})

	squad := createSquadAs(t, "", "Overflow of overflow", leader)
	if squad.FloorID != nil || squad.OfficeSlot != nil {
		t.Fatalf("a full office leaves the squad unseated, got %v/%v", squad.FloorID, squad.OfficeSlot)
	}
	if got := len(listFloors(t, testWorkspaceID)); got != officeMaxFloors {
		t.Fatalf("the floor cap must hold, got %d floors", got)
	}
}

func TestSquadResponsesCarryTheSeat(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-resp-leader", nil)
	squad := createSquadAs(t, "", "Seat in responses", leader)
	if squad.FloorID == nil {
		t.Fatal("create response should carry floor_id")
	}

	w := officeCall(testHandler.GetSquad, squadScopeReq("", "GET", "/api/squads/"+squad.ID, nil, map[string]string{"id": squad.ID}))
	wantStatus(t, w, http.StatusOK)
	got := decodeBody[SquadResponse](t, w)
	if got.FloorID == nil || *got.FloorID != *squad.FloorID || got.OfficeSlot == nil || *got.OfficeSlot != *squad.OfficeSlot {
		t.Fatalf("get response seat mismatch: %+v", got)
	}

	w = officeCall(testHandler.UpdateSquad, squadScopeReq("", "PUT", "/api/squads/"+squad.ID, map[string]any{"name": "Renamed"}, map[string]string{"id": squad.ID}))
	wantStatus(t, w, http.StatusOK)
	got = decodeBody[SquadResponse](t, w)
	if got.FloorID == nil || *got.FloorID != *squad.FloorID {
		t.Fatalf("update response should keep the seat: %+v", got)
	}

	w = officeCall(testHandler.ListSquads, squadScopeReq("", "GET", "/api/squads", nil, nil))
	wantStatus(t, w, http.StatusOK)
	found := false
	for _, s := range decodeBody[[]SquadResponse](t, w) {
		if s.ID == squad.ID {
			found = true
			if s.FloorID == nil || *s.FloorID != *squad.FloorID || s.OfficeSlot == nil {
				t.Fatalf("list response seat mismatch: %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("squad missing from list")
	}

	// The wire keys exist (as null) for an unseated squad.
	unseated := seedOfficeSquad(t, "Unseated wire", leader)
	w = officeCall(testHandler.GetSquad, squadScopeReq("", "GET", "/api/squads/"+unseated, nil, map[string]string{"id": unseated}))
	wantStatus(t, w, http.StatusOK)
	raw := decodeBody[map[string]any](t, w)
	if v, ok := raw["floor_id"]; !ok || v != nil {
		t.Fatalf("floor_id should be present and null, got %v (present=%v)", v, ok)
	}
	if v, ok := raw["office_slot"]; !ok || v != nil {
		t.Fatalf("office_slot should be present and null, got %v (present=%v)", v, ok)
	}
}

// ── home squad ──────────────────────────────────────────────────────────────

func addMemberAs(t *testing.T, squadID, agentID string) {
	t.Helper()
	w := officeCall(testHandler.AddSquadMember, squadScopeReq("", "POST", "/api/squads/members", map[string]any{
		"member_type": "agent", "member_id": agentID,
	}, map[string]string{"id": squadID}))
	wantStatus(t, w, http.StatusCreated)
}

func removeMemberAs(t *testing.T, squadID, agentID string) {
	t.Helper()
	w := officeCall(testHandler.RemoveSquadMember, squadScopeReq("", "DELETE", "/api/squads/members", map[string]any{
		"member_type": "agent", "member_id": agentID,
	}, map[string]string{"id": squadID}))
	wantStatus(t, w, http.StatusNoContent)
}

func TestHomeSquad_SetOnFirstMembership(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "home-first-leader", nil)
	worker := createHandlerTestAgent(t, "home-first-worker", nil)

	a := createSquadAs(t, "", "Home A", leader)
	if got := homeOf(t, leader); got != a.ID {
		t.Fatalf("a leader's first squad is its home, got %q want %q", got, a.ID)
	}

	addMemberAs(t, a.ID, worker)
	if got := homeOf(t, worker); got != a.ID {
		t.Fatalf("a worker's first squad is its home, got %q", got)
	}

	// A second squad does not move the home.
	b := createSquadAs(t, "", "Home B", leader)
	addMemberAs(t, b.ID, worker)
	if got := homeOf(t, worker); got != a.ID {
		t.Fatalf("the home must not move when joining another squad, got %q", got)
	}
	if got := homeOf(t, leader); got != a.ID {
		t.Fatalf("leading a second squad must not move the home, got %q", got)
	}
}

func TestHomeSquad_NotClaimedByAgentsAlreadyInASquad(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "home-legacy-leader", nil)
	legacy := createHandlerTestAgent(t, "home-legacy-worker", nil)

	// The legacy agent was in a squad before homes existed: no home set.
	old := seedOfficeSquad(t, "Legacy squad", leader)
	dbfx.SquadMember(t, old, "agent", legacy)

	fresh := createSquadAs(t, "", "Fresh squad", leader)
	addMemberAs(t, fresh.ID, legacy)
	if got := homeOf(t, legacy); got != "" {
		t.Fatalf("only a first membership claims a home; got %q", got)
	}
}

func TestHomeSquad_MovesOrClearsWhenRemovedFromHome(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "home-move-leader", nil)
	worker := createHandlerTestAgent(t, "home-move-worker", nil)

	a := createSquadAs(t, "", "Move A", leader)
	b := createSquadAs(t, "", "Move B", leader)
	c := createSquadAs(t, "", "Move C", leader)
	addMemberAs(t, a.ID, worker)
	addMemberAs(t, b.ID, worker)
	addMemberAs(t, c.ID, worker)

	// Leaving a non-home squad changes nothing.
	removeMemberAs(t, c.ID, worker)
	if got := homeOf(t, worker); got != a.ID {
		t.Fatalf("home = %q, want %q", got, a.ID)
	}

	// Leaving the home squad prefers another squad the agent still belongs to.
	removeMemberAs(t, a.ID, worker)
	if got := homeOf(t, worker); got != b.ID {
		t.Fatalf("home should move to the remaining squad %q, got %q", b.ID, got)
	}

	// Leaving the last squad clears it.
	removeMemberAs(t, b.ID, worker)
	if got := homeOf(t, worker); got != "" {
		t.Fatalf("home should be cleared, got %q", got)
	}
}

func TestHomeSquad_MovesOrClearsWhenSquadIsArchived(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "home-arch-leader", nil)
	other := createHandlerTestAgent(t, "home-arch-other", nil)
	worker := createHandlerTestAgent(t, "home-arch-worker", nil)
	solo := createHandlerTestAgent(t, "home-arch-solo", nil)

	a := createSquadAs(t, "", "Arch A", leader)
	b := createSquadAs(t, "", "Arch B", other)
	addMemberAs(t, a.ID, worker)
	addMemberAs(t, b.ID, worker)
	addMemberAs(t, a.ID, solo)

	w := officeCall(testHandler.DeleteSquad, squadScopeReq("", "DELETE", "/api/squads", nil, map[string]string{"id": a.ID}))
	wantStatus(t, w, http.StatusNoContent)

	if got := homeOf(t, worker); got != b.ID {
		t.Fatalf("a member of another live squad moves its home there, got %q", got)
	}
	if got := homeOf(t, solo); got != "" {
		t.Fatalf("an agent with no other squad is unassigned, got %q", got)
	}
	if got := homeOf(t, leader); got != "" {
		t.Fatalf("the archived squad's leader is unassigned, got %q", got)
	}
	if got := homeOf(t, other); got != b.ID {
		t.Fatalf("unrelated homes must not change, got %q", got)
	}
}

func TestHomeSquad_NewLeaderClaimsHome(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "home-lead-old", nil)
	next := createHandlerTestAgent(t, "home-lead-new", nil)

	squad := createSquadAs(t, "", "Leader swap", leader)
	w := officeCall(testHandler.UpdateSquad, squadScopeReq("", "PUT", "/api/squads/"+squad.ID, map[string]any{"leader_id": next}, map[string]string{"id": squad.ID}))
	wantStatus(t, w, http.StatusOK)
	if got := homeOf(t, next); got != squad.ID {
		t.Fatalf("a new leader added to its first squad takes it as home, got %q", got)
	}
}

func updateAgentHome(t *testing.T, agentID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, body), "id", agentID))
	return w
}

func TestUpdateAgent_HomeSquad(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "home-upd-leader", nil)
	worker := createHandlerTestAgent(t, "home-upd-worker", nil)

	a := createSquadAs(t, "", "Upd A", leader)
	b := createSquadAs(t, "", "Upd B", leader)
	c := createSquadAs(t, "", "Upd C", leader) // the worker is not a member
	addMemberAs(t, a.ID, worker)
	addMemberAs(t, b.ID, worker)
	if got := homeOf(t, worker); got != a.ID {
		t.Fatalf("setup: home = %q", got)
	}

	// Move the home to another squad the agent belongs to.
	w := updateAgentHome(t, worker, map[string]any{"home_squad_id": b.ID})
	wantStatus(t, w, http.StatusOK)
	resp := decodeBody[AgentResponse](t, w)
	if resp.HomeSquadID == nil || *resp.HomeSquadID != b.ID {
		t.Fatalf("response home_squad_id = %v, want %s", resp.HomeSquadID, b.ID)
	}
	if got := homeOf(t, worker); got != b.ID {
		t.Fatalf("home = %q", got)
	}

	// An update that omits the field leaves it alone.
	wantStatus(t, updateAgentHome(t, worker, map[string]any{"description": "still here"}), http.StatusOK)
	if got := homeOf(t, worker); got != b.ID {
		t.Fatalf("omitting home_squad_id changed it to %q", got)
	}

	// Not a member, unknown squad, malformed id: 400 and nothing changes,
	// including the other fields of the same request.
	for name, id := range map[string]any{
		"not a member":  c.ID,
		"unknown squad": util.UUIDToString(pgtype.UUID{Bytes: [16]byte{7}, Valid: true}),
		"malformed":     "nope",
	} {
		w := updateAgentHome(t, worker, map[string]any{"home_squad_id": id, "description": "must not stick"})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", name, w.Code, w.Body.String())
		}
	}
	var description string
	if err := testPool.QueryRow(context.Background(), `SELECT description FROM agent WHERE id = $1`, worker).Scan(&description); err != nil {
		t.Fatalf("read description: %v", err)
	}
	if description != "still here" {
		t.Fatalf("a rejected request must not apply other fields, description = %q", description)
	}
	if got := homeOf(t, worker); got != b.ID {
		t.Fatalf("home = %q after rejected requests", got)
	}

	// An archived squad is not a valid home.
	wantStatus(t, officeCall(testHandler.DeleteSquad, squadScopeReq("", "DELETE", "/api/squads", nil, map[string]string{"id": a.ID})), http.StatusNoContent)
	if w := updateAgentHome(t, worker, map[string]any{"home_squad_id": a.ID}); w.Code != http.StatusBadRequest {
		t.Fatalf("archived squad: expected 400, got %d", w.Code)
	}

	// null clears the home.
	w = updateAgentHome(t, worker, map[string]any{"home_squad_id": nil})
	wantStatus(t, w, http.StatusOK)
	if resp := decodeBody[AgentResponse](t, w); resp.HomeSquadID != nil {
		t.Fatalf("response should report a cleared home, got %v", *resp.HomeSquadID)
	}
	if got := homeOf(t, worker); got != "" {
		t.Fatalf("home should be cleared, got %q", got)
	}
}

func TestAgentResponseCarriesNullHomeSquad(t *testing.T) {
	requireOfficeDB(t)
	agentID := createHandlerTestAgent(t, "home-null-agent", nil)
	w := httptest.NewRecorder()
	testHandler.GetAgent(w, withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID, nil), "id", agentID))
	wantStatus(t, w, http.StatusOK)
	raw := decodeBody[map[string]any](t, w)
	if v, ok := raw["home_squad_id"]; !ok || v != nil {
		t.Fatalf("home_squad_id should be present and null, got %v (present=%v)", v, ok)
	}
}

// ── events ──────────────────────────────────────────────────────────────────

func TestOfficeEvents(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-events-leader", nil)

	var mu sync.Mutex
	got := map[string]int{}
	record := func(e events.Event) {
		if e.WorkspaceID != testWorkspaceID {
			return
		}
		mu.Lock()
		got[e.Type]++
		mu.Unlock()
	}
	for _, typ := range []string{
		protocol.EventOfficeFloorCreated, protocol.EventOfficeFloorUpdated, protocol.EventOfficeFloorDeleted,
		protocol.EventSquadCreated, protocol.EventSquadUpdated, protocol.EventAgentStatus,
	} {
		testHandler.Bus.Subscribe(typ, record)
	}
	count := func(typ string) int {
		mu.Lock()
		defer mu.Unlock()
		return got[typ]
	}

	floor := createFloor(t, "", testWorkspaceID, "Events")
	if count(protocol.EventOfficeFloorCreated) != 1 {
		t.Fatalf("floor create should publish office_floor:created")
	}
	wantStatus(t, officeCall(testHandler.UpdateOfficeFloor, officeReq("", testWorkspaceID, "PUT", "/api/office/floors/"+floor.ID, map[string]any{"name": "Events 2"}, map[string]string{"id": floor.ID})), http.StatusOK)
	if count(protocol.EventOfficeFloorUpdated) != 1 {
		t.Fatalf("floor update should publish office_floor:updated")
	}

	// Creating a squad on a full floor set creates a floor and says so.
	before := count(protocol.EventOfficeFloorCreated)
	squad := createSquadAs(t, "", "Events squad", leader)
	if count(protocol.EventSquadCreated) < 1 {
		t.Fatal("squad create should publish squad:created")
	}
	if count(protocol.EventOfficeFloorCreated) != before {
		t.Fatal("seating on an existing floor must not publish a floor event")
	}
	if count(protocol.EventAgentStatus) < 1 {
		t.Fatal("setting a home squad should publish agent:status")
	}

	// Only changed squads publish squad:updated.
	other := seedOfficeSquad(t, "Events other", leader)
	updatedBefore := count(protocol.EventSquadUpdated)
	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{
		{"squad_id": squad.ID, "floor_id": *squad.FloorID, "slot": *squad.OfficeSlot}, // unchanged
		{"squad_id": other, "floor_id": floor.ID, "slot": 5},
	}), http.StatusOK)
	if got := count(protocol.EventSquadUpdated) - updatedBefore; got != 1 {
		t.Fatalf("expected one squad:updated for the one moved squad, got %d", got)
	}

	// Unseating both squads empties the floor; deleting it publishes.
	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{
		{"squad_id": other, "floor_id": nil, "slot": nil},
		{"squad_id": squad.ID, "floor_id": nil, "slot": nil},
	}), http.StatusOK)
	wantStatus(t, officeCall(testHandler.DeleteOfficeFloor, officeReq("", testWorkspaceID, "DELETE", "/api/office/floors/"+floor.ID, nil, map[string]string{"id": floor.ID})), http.StatusNoContent)
	if count(protocol.EventOfficeFloorDeleted) != 1 {
		t.Fatalf("floor delete should publish office_floor:deleted")
	}
}

// ── workspace isolation ─────────────────────────────────────────────────────

func secondOfficeWorkspace(t *testing.T) string {
	t.Helper()
	wsID := dbfx.Workspace(t, "Office isolation", fmt.Sprintf("office-iso-%d", time.Now().UnixNano()))
	dbfx.Member(t, wsID, testUserID, "owner")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM office_floor WHERE workspace_id = $1`, wsID)
	})
	return wsID
}

func TestOfficeIsolation_Handlers(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	leader := createHandlerTestAgent(t, "office-iso-leader", nil)
	ws2 := secondOfficeWorkspace(t)

	floor1 := createFloor(t, "", testWorkspaceID, "WS1 floor")
	floor2 := createFloor(t, "", ws2, "WS2 floor")
	squad1 := seedOfficeSquad(t, "WS1 squad", leader)

	// Lists are scoped.
	if got := listFloors(t, ws2); len(got) != 1 || got[0].ID != floor2.ID {
		t.Fatalf("ws2 should see only its own floor: %+v", got)
	}
	if got := listFloors(t, testWorkspaceID); len(got) != 1 || got[0].ID != floor1.ID {
		t.Fatalf("ws1 should see only its own floor: %+v", got)
	}

	// Another workspace's floor cannot be renamed, moved or deleted through ws2.
	w := officeCall(testHandler.UpdateOfficeFloor, officeReq("", ws2, "PUT", "/api/office/floors/"+floor1.ID, map[string]any{"name": "hijack"}, map[string]string{"id": floor1.ID}))
	wantStatus(t, w, http.StatusNotFound)
	w = officeCall(testHandler.DeleteOfficeFloor, officeReq("", ws2, "DELETE", "/api/office/floors/"+floor1.ID, nil, map[string]string{"id": floor1.ID}))
	wantStatus(t, w, http.StatusNotFound)

	// A squad of ws1 cannot be seated through ws2, and a ws1 floor cannot be
	// used for a ws2 squad.
	wantStatus(t, putSeats("", ws2, []map[string]any{{"squad_id": squad1, "floor_id": floor2.ID, "slot": 0}}), http.StatusBadRequest)
	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{{"squad_id": squad1, "floor_id": floor2.ID, "slot": 0}}), http.StatusBadRequest)
	if f, _ := seatOf(t, squad1); f != "" {
		t.Fatal("cross-workspace requests must not seat anything")
	}

	// The 16-floor cap is per workspace.
	for i := 0; i < officeMaxFloors-1; i++ {
		createFloor(t, "", ws2, fmt.Sprintf("WS2 %d", i))
	}
	wantStatus(t, officeCall(testHandler.CreateOfficeFloor, officeReq("", ws2, "POST", "/api/office/floors", map[string]any{"name": "over"}, nil)), http.StatusConflict)
	createFloor(t, "", testWorkspaceID, "WS1 second") // ws1 is unaffected

	// A member of ws1 only gets 404 on ws2's floors.
	outsider := createPlainMember(t, "office-iso-outsider@multica.test")
	wantStatus(t, officeCall(testHandler.ListOfficeFloors, officeReq(outsider, ws2, "GET", "/api/office/floors", nil, nil)), http.StatusNotFound)
}

// TestOfficeIsolation_Queries asserts that every new query refuses to cross a
// workspace boundary even when handed valid ids from another workspace.
func TestOfficeIsolation_Queries(t *testing.T) {
	requireOfficeDB(t)
	resetOffice(t)
	ctx := context.Background()
	q := testHandler.Queries
	leader := createHandlerTestAgent(t, "office-isoq-leader", nil)
	ws2 := secondOfficeWorkspace(t)
	ws1UUID := mustUUID(t, testWorkspaceID)
	ws2UUID := mustUUID(t, ws2)

	floor := createFloor(t, "", testWorkspaceID, "Isolated")
	floorUUID := mustUUID(t, floor.ID)
	squadID := seedOfficeSquad(t, "Isolated squad", leader)
	squadUUID := mustUUID(t, squadID)
	leaderUUID := mustUUID(t, leader)
	wantStatus(t, putSeats("", testWorkspaceID, []map[string]any{{"squad_id": squadID, "floor_id": floor.ID, "slot": 1}}), http.StatusOK)
	slot := pgtype.Int2{Int16: 1, Valid: true}

	if _, err := q.GetOfficeFloorInWorkspace(ctx, db.GetOfficeFloorInWorkspaceParams{ID: floorUUID, WorkspaceID: ws2UUID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetOfficeFloorInWorkspace crossed workspaces: %v", err)
	}
	if _, err := q.RenameOfficeFloor(ctx, db.RenameOfficeFloorParams{ID: floorUUID, WorkspaceID: ws2UUID, Name: "x"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("RenameOfficeFloor crossed workspaces: %v", err)
	}
	if err := q.SetOfficeFloorPosition(ctx, db.SetOfficeFloorPositionParams{ID: floorUUID, WorkspaceID: ws2UUID, Position: 9}); err != nil {
		t.Fatalf("SetOfficeFloorPosition: %v", err)
	}
	if rows, err := q.DeleteOfficeFloor(ctx, db.DeleteOfficeFloorParams{ID: floorUUID, WorkspaceID: ws2UUID}); err != nil || rows != 0 {
		t.Fatalf("DeleteOfficeFloor crossed workspaces: rows=%d err=%v", rows, err)
	}
	if got := listFloors(t, testWorkspaceID); len(got) != 1 || got[0].Name != "Isolated" || got[0].Position != 0 {
		t.Fatalf("floor must be untouched by ws2 queries: %+v", got)
	}
	if n, _ := q.CountOfficeFloors(ctx, ws2UUID); n != 0 {
		t.Fatalf("CountOfficeFloors(ws2) = %d", n)
	}
	if floors, _ := q.ListOfficeFloors(ctx, ws2UUID); len(floors) != 0 {
		t.Fatalf("ListOfficeFloors(ws2) = %d", len(floors))
	}
	if n, _ := q.CountLiveSquadsOnFloor(ctx, db.CountLiveSquadsOnFloorParams{WorkspaceID: ws2UUID, FloorID: floorUUID}); n != 0 {
		t.Fatalf("CountLiveSquadsOnFloor crossed workspaces: %d", n)
	}
	if seats, _ := q.ListOfficeSeats(ctx, ws2UUID); len(seats) != 0 {
		t.Fatalf("ListOfficeSeats(ws2) = %d", len(seats))
	}
	if _, err := q.GetLiveSquadAtSeat(ctx, db.GetLiveSquadAtSeatParams{WorkspaceID: ws2UUID, FloorID: floorUUID, OfficeSlot: slot}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetLiveSquadAtSeat crossed workspaces: %v", err)
	}
	if _, err := q.SetSquadSeat(ctx, db.SetSquadSeatParams{ID: squadUUID, WorkspaceID: ws2UUID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetSquadSeat crossed workspaces: %v", err)
	}
	if _, s := seatOf(t, squadID); s != 1 {
		t.Fatalf("squad seat must be untouched, slot=%d", s)
	}

	// Agent home queries.
	if _, err := q.SetAgentHomeSquad(ctx, db.SetAgentHomeSquadParams{ID: leaderUUID, WorkspaceID: ws2UUID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetAgentHomeSquad crossed workspaces: %v", err)
	}
	if _, err := q.SetAgentHomeSquadOnFirstMembership(ctx, db.SetAgentHomeSquadOnFirstMembershipParams{ID: leaderUUID, WorkspaceID: ws2UUID, SquadID: squadUUID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetAgentHomeSquadOnFirstMembership crossed workspaces: %v", err)
	}
	// Give the leader a home in ws1, then try to release it from ws2.
	dbfx.Exec(t, `UPDATE agent SET home_squad_id = $2 WHERE id = $1`, leader, squadID)
	if rows, err := q.ReassignAgentHomeSquad(ctx, db.ReassignAgentHomeSquadParams{WorkspaceID: ws2UUID, SquadID: squadUUID}); err != nil || len(rows) != 0 {
		t.Fatalf("ReassignAgentHomeSquad crossed workspaces: rows=%d err=%v", len(rows), err)
	}
	if got := homeOf(t, leader); got != squadID {
		t.Fatalf("home must be untouched by ws2 queries, got %q", got)
	}
	// And a squad of ws2 is never a valid home for a ws1 agent.
	if _, err := q.SetAgentHomeSquad(ctx, db.SetAgentHomeSquadParams{ID: leaderUUID, WorkspaceID: ws1UUID, HomeSquadID: mustUUID(t, ws2)}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a non-squad id must not pass the home guard: %v", err)
	}
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}
