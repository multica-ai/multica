package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Inject faults at an actual SQL call, after the preceding access checks have
// used the real database. Successful scans can also cancel just before return.
type requestCancellationDB struct {
	db.DBTX
	query  string
	err    error
	cancel context.CancelFunc
	after  bool
}

func (d requestCancellationDB) matches(sql string) bool {
	return strings.HasPrefix(sql, "-- name: "+d.query+" :")
}

func (d requestCancellationDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if !d.matches(sql) {
		return d.DBTX.Query(ctx, sql, args...)
	}
	if !d.after {
		if d.cancel != nil {
			d.cancel()
		}
		return nil, d.err
	}
	rows, err := d.DBTX.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return searchCancelAfterCloseRows{Rows: rows, cancel: d.cancel}, nil
}

func (d requestCancellationDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if !d.matches(sql) {
		return d.DBTX.QueryRow(ctx, sql, args...)
	}
	if !d.after {
		if d.cancel != nil {
			d.cancel()
		}
		return requestCancellationRow{err: d.err}
	}
	return searchCancelRow{Row: d.DBTX.QueryRow(ctx, sql, args...), cancel: d.cancel}
}

type requestCancellationRow struct{ err error }

func (r requestCancellationRow) Scan(...any) error { return r.err }

type requestCancellationCase struct {
	name   string
	err    error
	cancel bool
	after  bool
	status int
}

func requestCancellationCases(failureStatus int) []requestCancellationCase {
	return []requestCancellationCase{
		{"cancelled operation", fmt.Errorf("query: %w", context.Canceled), true, false, 499},
		{"postgres cancellation", &pgconn.PgError{Code: "57014"}, true, false, 499},
		{"cancelled after successful read", nil, true, true, 499},
		{"independent cancellation", context.Canceled, false, false, failureStatus},
		{"independent deadline", context.DeadlineExceeded, false, false, failureStatus},
		{"deadline concurrent with disconnect", context.DeadlineExceeded, true, false, failureStatus},
		{"failure concurrent with disconnect", errors.New("database unavailable"), true, false, failureStatus},
	}
}

func assertRequestCancellationStatus(t *testing.T, call http.HandlerFunc, req *http.Request, status int) {
	t.Helper()
	response := testutil.Call(t, call, req).Want(status)
	if status == 499 && (response.Body.Len() != 0 || response.Header().Get("Retry-After") != "") {
		t.Fatalf("cancelled request returned a body or retry instruction: %s", response.Body.String())
	}
}

func TestCancellableReadEndpoints(t *testing.T) {
	issueID := dbfx.Issue(t, "Cancelled detail")
	issue, err := testHandler.Queries.GetIssue(t.Context(), parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	identifier := fmt.Sprintf("%s-%d", testHandler.getIssuePrefix(t.Context(), issue.WorkspaceID), issue.Number)
	runtimeID := dbfx.Runtime(t, "Cancelled discovery", testutil.Cols{"status": "online", "owner_id": testUserID, "visibility": "public"})
	agentID := dbfx.Agent(t, "Cancelled history", runtimeID)
	for _, endpoint := range []struct {
		name, query string
		status      int
		request     *http.Request
		call        func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{"issue lookup", "GetIssueInWorkspace", 404, withURLParam(newRequest(http.MethodGet, "/issues/"+issueID, nil), "id", issueID), (*Handler).GetIssue},
		{"identifier workspace", "GetWorkspace", 404, withURLParam(newRequest(http.MethodGet, "/issues/"+identifier, nil), "id", identifier), (*Handler).GetIssue},
		{"identifier issue", "GetIssueByNumber", 404, withURLParam(newRequest(http.MethodGet, "/issues/"+identifier, nil), "id", identifier), (*Handler).GetIssue},
		{"issue attachments", "ListAttachmentsByIssue", 200, withURLParam(newRequest(http.MethodGet, "/issues/"+issueID, nil), "id", issueID), (*Handler).GetIssue},
		{"agent lookup", "GetAgentInWorkspace", 404, withURLParam(newRequest(http.MethodGet, "/agents/"+agentID+"/tasks", nil), "id", agentID), (*Handler).ListAgentTasks},
		{"agent history", "ListAgentTasks", 500, withURLParam(newRequest(http.MethodGet, "/agents/"+agentID+"/tasks", nil), "id", agentID), (*Handler).ListAgentTasks},
		{"runtime lookup", "GetAgentRuntime", 404, withURLParam(newRequest(http.MethodPost, "/runtimes/"+runtimeID+"/local-skills", nil), "runtimeId", runtimeID), (*Handler).InitiateListLocalSkills},
		{"workspace membership", "GetMemberByUserAndWorkspace", 404, withURLParam(newRequest(http.MethodPost, "/runtimes/"+runtimeID+"/local-skills", nil), "runtimeId", runtimeID), (*Handler).InitiateListLocalSkills},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			cases := requestCancellationCases(endpoint.status)
			if endpoint.status == 404 {
				cases = append(cases, requestCancellationCase{"missing row after disconnect", pgx.ErrNoRows, true, false, 404})
			}
			for _, tc := range cases {
				if endpoint.query == "ListAttachmentsByIssue" && (!tc.cancel || tc.err != nil && !errors.Is(tc.err, context.Canceled) && !tc.after) {
					continue
				}
				t.Run(tc.name, func(t *testing.T) {
					ctx, cancel := context.WithCancel(endpoint.request.Context())
					defer cancel()
					h := *testHandler
					fault := requestCancellationDB{DBTX: testPool, query: endpoint.query, err: tc.err, after: tc.after}
					if tc.cancel {
						fault.cancel = cancel
					}
					h.Queries = db.New(fault)
					assertRequestCancellationStatus(t, func(w http.ResponseWriter, r *http.Request) { endpoint.call(&h, w, r) }, endpoint.request.WithContext(ctx), tc.status)
					if tc.cancel && ctx.Err() != context.Canceled {
						t.Fatal("request did not reach the named SQL cancellation")
					}
				})
			}
		})
	}
}

type requestCancellationTxStarter struct {
	txStarter
	fault       requestCancellationDB
	afterCommit context.CancelFunc
}

func (s requestCancellationTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	s.fault.DBTX = tx
	return requestCancellationTx{Tx: tx, fault: s.fault, afterCommit: s.afterCommit}, nil
}

type requestCancellationTx struct {
	pgx.Tx
	fault       requestCancellationDB
	afterCommit context.CancelFunc
}

func (tx requestCancellationTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return tx.fault.Query(ctx, sql, args...)
}

func (tx requestCancellationTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return tx.fault.QueryRow(ctx, sql, args...)
}

func (tx requestCancellationTx) Commit(ctx context.Context) error {
	err := tx.Tx.Commit(ctx)
	if err == nil && tx.afterCommit != nil {
		tx.afterCommit()
	}
	return err
}

func TestSearchIndexRequestCancellation(t *testing.T) {
	cursor := searchIndexManifest(t, testWorkspaceID).Cursor
	for _, endpoint := range []struct {
		name  string
		query string
		call  func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{"manifest", "GetSearchIndexManifest", (*Handler).GetSearchIndexManifest},
		{"snapshot", "ListSearchIndexIssuesPage", (*Handler).GetSearchIndexSnapshot},
		{"changes", "SearchIndexSinceIsPruned", (*Handler).ListSearchIndexChanges},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			cases := append(requestCancellationCases(500), requestCancellationCase{"live statement timeout", &pgconn.PgError{Code: "57014"}, false, false, 503})
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					h := *testHandler
					fault := requestCancellationDB{query: endpoint.query, err: tc.err}
					starter := requestCancellationTxStarter{txStarter: testPool, fault: fault}
					if tc.after {
						starter.fault.query = ""
						starter.afterCommit = cancel
					} else if tc.cancel {
						starter.fault.cancel = cancel
					}
					h.TxStarter = starter
					req := newRequest(http.MethodGet, "/search-index/"+endpoint.name, nil)
					if endpoint.name == "changes" {
						req = newRequest(http.MethodPost, "/search-index/changes", map[string]any{"cursor": cursor})
					}
					assertRequestCancellationStatus(t, func(w http.ResponseWriter, r *http.Request) { endpoint.call(&h, w, r) }, req.WithContext(ctx), tc.status)
				})
			}
		})
	}
}

func TestSearchIndexCancellationWhileWaitingForSlot(t *testing.T) {
	previous := searchIndexBootstrapSlots
	searchIndexBootstrapSlots = make(chan struct{}, 1)
	searchIndexBootstrapSlots <- struct{}{}
	t.Cleanup(func() { searchIndexBootstrapSlots = previous })
	for _, call := range []http.HandlerFunc{testHandler.GetSearchIndexManifest, testHandler.GetSearchIndexSnapshot} {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		assertRequestCancellationStatus(t, call, newRequest(http.MethodGet, "/search-index", nil).WithContext(ctx), 499)
	}
}

type cancellationLocalSkillStore struct {
	LocalSkillListStore
	cancel context.CancelFunc
	err    error
	after  bool
}

func (s cancellationLocalSkillStore) Create(ctx context.Context, runtimeID string) (*RuntimeLocalSkillListRequest, error) {
	if !s.after {
		if s.cancel != nil {
			s.cancel()
		}
		return nil, s.err
	}
	req, err := s.LocalSkillListStore.Create(ctx, runtimeID)
	s.cancel()
	return req, err
}

func (s cancellationLocalSkillStore) Get(ctx context.Context, id string) (*RuntimeLocalSkillListRequest, error) {
	if !s.after {
		if s.cancel != nil {
			s.cancel()
		}
		return nil, s.err
	}
	req, err := s.LocalSkillListStore.Get(ctx, id)
	s.cancel()
	return req, err
}

func TestLocalSkillDiscoveryRequestCancellation(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "Cancellation native skills", testutil.Cols{"status": "online", "owner_id": testUserID, "visibility": "public"})
	for _, operation := range []string{"create", "get"} {
		for _, tc := range []requestCancellationCase{
			{"cancelled", context.Canceled, true, false, 499},
			{"independent cancellation", context.Canceled, false, false, 500},
			{"independent deadline", context.DeadlineExceeded, false, false, 500},
			{"unrelated failure", errors.New("store unavailable"), true, false, 500},
			{"cancelled after acceptance", nil, true, true, 499},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				store := NewInMemoryLocalSkillListStore()
				h := *testHandler
				recorder := &runtimeLocalSkillPendingWorkRecorder{}
				h.DaemonPendingWork = recorder
				fault := cancellationLocalSkillStore{LocalSkillListStore: store, err: tc.err, after: tc.after}
				if tc.cancel {
					fault.cancel = cancel
				}
				h.LocalSkillListStore = fault
				req := withURLParam(newRequest(http.MethodPost, "/skills", map[string]any{"requestId": uuid.NewString()}), "runtimeId", runtimeID)
				call := h.InitiateListLocalSkills
				if operation == "get" {
					created, err := store.Create(t.Context(), runtimeID)
					if err != nil {
						t.Fatal(err)
					}
					req = withURLParams(newRequest(http.MethodGet, "/skills/"+created.ID, nil), "runtimeId", runtimeID, "requestId", created.ID)
					call = h.GetLocalSkillListRequest
				}
				if operation == "get" {
					req = withURLParams(req.WithContext(ctx), "runtimeId", runtimeID, "requestId", strings.TrimPrefix(req.URL.Path, "/skills/"))
				} else {
					req = withURLParam(req.WithContext(ctx), "runtimeId", runtimeID)
				}
				assertRequestCancellationStatus(t, call, req, tc.status)
				if operation == "create" {
					if (len(recorder.hints) == 1) != tc.after {
						t.Fatal("accepted discovery did not retain its wakeup")
					}
					pending, err := store.HasPending(t.Context(), runtimeID)
					if err != nil || pending != tc.after {
						t.Fatalf("cancellation changed accepted discovery: pending=%v, err=%v", pending, err)
					}
				}
			})
		}
	}
}
