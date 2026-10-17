package handler

// channel_binding_notification_test.go — SHAR-146 B1. The set endpoint is
// the security-sensitive one (over-permission + body validation), so the
// tests drive the branches that can fail without a real DB: the owner
// check, the body contract, and the missing-binding 404. The query layer's
// "explicit target wins / oldest fallback / new binding does not hijack"
// behavior is enforced by the partial unique index and covered by the
// functional verification, not here.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// mockBindingDB routes QueryRow by the sqlc query name so one mock answers
// every query the handler can issue for this surface. ListWecomBindings is
// the only :many read; the Set path reads GetChannelUserBindingByID, then
// writes via Exec inside the tx.
type mockBindingDB struct {
	db.DBTX
	getBindingRow db.ChannelUserBinding
	getBindingErr error
	listRows      []db.ListChannelUserBindingsForMemberRow
	listErr       error
	execErr       error
	listCalls     int
	execCalls     int
}

func (m *mockBindingDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	const getQ = "-- name: GetChannelUserBindingByID :one\n"
	if len(sql) >= len(getQ) && sql[:len(getQ)] == getQ {
		return &mockBindingRow{row: m.getBindingRow, err: m.getBindingErr}
	}
	return &mockBindingRow{err: pgx.ErrNoRows}
}

func (m *mockBindingDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	m.listCalls++
	return &mockBindingRows{rows: m.listRows, err: m.listErr}, nil
}

func (m *mockBindingDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	m.execCalls++
	return pgconn.NewCommandTag("UPDATE 1"), m.execErr
}

type mockBindingRow struct {
	pgx.Row
	row db.ChannelUserBinding
	err error
}

func (r *mockBindingRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	// ChannelUserBinding's generated Scan passes one pointer per column in
	// schema order. The tests only care about the first nine columns (id,
	// workspace_id, multica_user_id, installation_id, channel_type,
	// channel_user_id, config, bound_at, is_notification_target); the rest
	// are filled with zero values via reflection-free best effort.
	columns := []interface{}{
		r.row.ID, r.row.WorkspaceID, r.row.MulticaUserID, r.row.InstallationID,
		r.row.ChannelType, r.row.ChannelUserID, r.row.Config, r.row.BoundAt,
		r.row.IsNotificationTarget,
	}
	for i := 0; i < len(dest) && i < len(columns); i++ {
		assignScanDest(dest[i], columns[i])
	}
	return nil
}

// assignScanDest copies src into the pointer dst when the types line up.
// Column order in the generated Scan is fixed, so a typed switch over the
// handful of types this test needs is enough.
func assignScanDest(dst, src interface{}) {
	switch d := dst.(type) {
	case *pgtype.UUID:
		if s, ok := src.(pgtype.UUID); ok {
			*d = s
		}
	case *string:
		if s, ok := src.(string); ok {
			*d = s
		}
	case *[]byte:
		if b, ok := src.([]byte); ok {
			*d = b
		}
	case *pgtype.Timestamptz:
		if t, ok := src.(pgtype.Timestamptz); ok {
			*d = t
		}
	case *bool:
		if b, ok := src.(bool); ok {
			*d = b
		}
	}
}

type mockBindingRows struct {
	pgx.Rows
	rows []db.ListChannelUserBindingsForMemberRow
	err  error
	pos  int
}

func (r *mockBindingRows) Next() bool { r.pos++; return r.pos <= len(r.rows) }
func (r *mockBindingRows) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	if r.pos < 1 || r.pos > len(r.rows) {
		return pgx.ErrNoRows
	}
	row := r.rows[r.pos-1]
	columns := []interface{}{
		row.ID, row.WorkspaceID, row.MulticaUserID, row.InstallationID,
		row.ChannelType, row.ChannelUserID, row.Config, row.BoundAt,
		row.IsNotificationTarget, row.AgentID, row.InstallationStatus,
	}
	for i := 0; i < len(dest) && i < len(columns); i++ {
		assignScanDest(dest[i], columns[i])
	}
	return nil
}
func (r *mockBindingRows) Close() {}
func (r *mockBindingRows) Err() error   { return r.err }

// fakeBindingTxStarter hands back a tx that routes Exec to the same mock, so
// the Set path's Clear+Set writes are observable without a real connection.
type fakeBindingTxStarter struct {
	mock *mockBindingDB
}

func (f *fakeBindingTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	return &fakeBindingTx{mock: f.mock}, nil
}

type fakeBindingTx struct {
	pgx.Tx
	mock   *mockBindingDB
	rolled bool
}

func (t *fakeBindingTx) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return t.mock.Exec(ctx, sql, args...)
}
func (t *fakeBindingTx) Rollback(ctx context.Context) error { t.rolled = true; return nil }
func (t *fakeBindingTx) Commit(ctx context.Context) error   { return nil }

var _ pgx.Tx = (*fakeBindingTx)(nil)

func setTargetRequest(workspaceID, bindingID, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPut,
		"/api/workspaces/"+workspaceID+"/wecom/bindings/"+bindingID+"/notification-target",
		strings.NewReader(body))
	r.Header.Set("X-User-ID", "00000000-0000-0000-0000-000000000001")
	return r
}

func listBindingsRequest(workspaceID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet,
		"/api/workspaces/"+workspaceID+"/wecom/bindings", nil)
	r.Header.Set("X-User-ID", "00000000-0000-0000-0000-000000000001")
	return r
}

// ownerID is the calling member; foreignID is another member's id, used to
// exercise the over-permission path.
var (
	ownerID   = pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	foreignID = pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	wsID      = pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
	bindingRow = db.ChannelUserBinding{
		ID:             pgtype.UUID{Bytes: [16]byte{3}, Valid: true},
		WorkspaceID:    wsID,
		MulticaUserID:  ownerID,
		InstallationID: pgtype.UUID{Bytes: [16]byte{4}, Valid: true},
		ChannelType:    wecomNotificationTargetChannelType,
	}
)

// TestSetWecomBindingNotificationTargetRejectsForeignBinding: the over-
// permission path. A caller pointing Set at ANOTHER member's binding id is
// refused; the 404 (not 403) does not leak that the binding exists.
func TestSetWecomBindingNotificationTargetRejectsForeignBinding(t *testing.T) {
	mock := &mockBindingDB{
		getBindingRow: db.ChannelUserBinding{
			ID:            bindingRow.ID,
			WorkspaceID:   wsID,
			MulticaUserID: foreignID, // belongs to a different member
			ChannelType:   wecomNotificationTargetChannelType,
		},
	}
	h := &Handler{
		Queries:   db.New(mock),
		TxStarter: &fakeBindingTxStarter{mock: mock},
	}

	w := httptest.NewRecorder()
	h.SetWecomBindingNotificationTarget(w, setTargetRequest(
		uuidToString(wsID),
		uuidToString(bindingRow.ID),
		`{"is_notification_target":true}`,
	))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a foreign binding (body: %s)", w.Code, w.Body.String())
	}
	if mock.execCalls != 0 {
		t.Errorf("writes ran %d time(s), want 0 on the over-permission path", mock.execCalls)
	}
}

// TestSetWecomBindingNotificationTargetSetAndUnset exercises both branches
// of the radio: set clears then sets; unset clears only.
func TestSetWecomBindingNotificationTargetSetAndUnset(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		mock := &mockBindingDB{getBindingRow: bindingRow}
		h := &Handler{Queries: db.New(mock), TxStarter: &fakeBindingTxStarter{mock: mock}}

		w := httptest.NewRecorder()
		h.SetWecomBindingNotificationTarget(w, setTargetRequest(
			uuidToString(wsID), uuidToString(bindingRow.ID),
			`{"is_notification_target":true}`,
		))
		if w.Code != http.StatusOK {
			t.Fatalf("set status = %d, want 200 (body: %s)", w.Code, w.Body.String())
		}
		// Clear + Set = two execs.
		if mock.execCalls != 2 {
			t.Errorf("set ran %d write(s), want 2 (clear + set)", mock.execCalls)
		}
	})
	t.Run("unset", func(t *testing.T) {
		mock := &mockBindingDB{getBindingRow: bindingRow}
		h := &Handler{Queries: db.New(mock), TxStarter: &fakeBindingTxStarter{mock: mock}}

		w := httptest.NewRecorder()
		h.SetWecomBindingNotificationTarget(w, setTargetRequest(
			uuidToString(wsID), uuidToString(bindingRow.ID),
			`{"is_notification_target":false}`,
		))
		if w.Code != http.StatusOK {
			t.Fatalf("unset status = %d, want 200 (body: %s)", w.Code, w.Body.String())
		}
		// Unset = one exec.
		if mock.execCalls != 1 {
			t.Errorf("unset ran %d write(s), want 1", mock.execCalls)
		}
	})
}

// TestSetWecomBindingNotificationTargetRequiresField: a body that omits the
// field is a 400, not a silent no-op.
func TestSetWecomBindingNotificationTargetRequiresField(t *testing.T) {
	mock := &mockBindingDB{getBindingRow: bindingRow}
	h := &Handler{Queries: db.New(mock), TxStarter: &fakeBindingTxStarter{mock: mock}}

	w := httptest.NewRecorder()
	h.SetWecomBindingNotificationTarget(w, setTargetRequest(
		uuidToString(wsID), uuidToString(bindingRow.ID),
		`{}`,
	))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a missing field", w.Code)
	}
}

// TestSetWecomBindingNotificationTargetMissingBinding: a binding id that does
// not exist in this workspace is a 404, with no writes issued.
func TestSetWecomBindingNotificationTargetMissingBinding(t *testing.T) {
	mock := &mockBindingDB{getBindingErr: pgx.ErrNoRows}
	h := &Handler{Queries: db.New(mock), TxStarter: &fakeBindingTxStarter{mock: mock}}

	w := httptest.NewRecorder()
	h.SetWecomBindingNotificationTarget(w, setTargetRequest(
		uuidToString(wsID), "00000000-0000-0000-0000-000000000099",
		`{"is_notification_target":true}`,
	))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a missing binding", w.Code)
	}
	if mock.execCalls != 0 {
		t.Errorf("writes ran %d time(s), want 0 for a missing binding", mock.execCalls)
	}
}

// TestListWecomBindingsReturnsTheMemberBindings: the list endpoint returns
// the caller's bindings, whose order is bound_at ASC — also the fallback
// order the notification lookup uses.
func TestListWecomBindingsReturnsTheMemberBindings(t *testing.T) {
	mock := &mockBindingDB{
		listRows: []db.ListChannelUserBindingsForMemberRow{
			{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, InstallationStatus: "active", ChannelUserID: "T-1", IsNotificationTarget: true},
			{ID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, InstallationStatus: "active", ChannelUserID: "T-2", IsNotificationTarget: false},
		},
	}
	h := &Handler{Queries: db.New(mock)}

	w := httptest.NewRecorder()
	h.ListWecomBindings(w, listBindingsRequest(uuidToString(wsID)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if mock.listCalls != 1 {
		t.Errorf("list query ran %d time(s), want 1", mock.listCalls)
	}
	if !strings.Contains(w.Body.String(), `"channel_type":"wecom"`) {
		t.Errorf("response missing channel_type envelope: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"is_notification_target":true`) {
		t.Errorf("response missing the target binding: %s", w.Body.String())
	}
}
