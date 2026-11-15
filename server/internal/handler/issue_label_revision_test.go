package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueLabelRevisionPrecondition(t *testing.T) {
	labelID := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": testWorkspaceID, "name": "Conditional label", "color": "#aabbcc", "resource_type": "issue"})
	for _, tc := range []struct {
		name, method string
		expected     int64
		seed         bool
		wantStatus   int
		wantRevision int64
		wantAttached bool
	}{
		{"attach matching", http.MethodPost, 7, false, 200, 8, true},
		{"attach stale", http.MethodPost, 6, false, 409, 7, false},
		{"attach matching already attached", http.MethodPost, 7, true, 200, 7, true},
		{"attach stale already attached", http.MethodPost, 6, true, 409, 7, true},
		{"detach matching", http.MethodDelete, 7, true, 200, 8, false},
		{"detach stale", http.MethodDelete, 6, true, 409, 7, true},
		{"detach matching absent", http.MethodDelete, 7, false, 200, 7, false},
		{"detach stale absent", http.MethodDelete, 6, false, 409, 7, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := dbfx.Issue(t, "conditional label", testutil.Cols{"revision": 7})
			t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM issue_to_label WHERE issue_id=$1`, id) })
			if tc.seed {
				dbfx.Exec(t, `INSERT INTO issue_to_label(issue_id,label_id) VALUES($1,$2)`, id, labelID)
			}
			var body any
			if tc.method == http.MethodPost {
				body = map[string]any{"label_id": labelID}
			}
			req := testutil.WithURLParams(newRequest(tc.method, "/api/issues/"+id+"/labels", body), "id", id, "labelId", labelID)
			req.URL.RawQuery = fmt.Sprintf("expected_revision=%d", tc.expected)
			response := httptest.NewRecorder()
			if tc.method == http.MethodPost {
				testHandler.AttachLabel(response, req)
			} else {
				testHandler.DetachLabel(response, req)
			}
			if response.Code != tc.wantStatus {
				t.Errorf("status=%d, want %d; body=%s", response.Code, tc.wantStatus, response.Body)
			}
			var revision int64
			var attached bool
			dbfx.QueryRow(t, `SELECT revision,EXISTS(SELECT 1 FROM issue_to_label WHERE issue_id=$1 AND label_id=$2) FROM issue WHERE id=$1`, id, labelID).Scan(&revision, &attached)
			if revision != tc.wantRevision || attached != tc.wantAttached {
				t.Errorf("state=(revision %d, attached %t), want (%d,%t)", revision, attached, tc.wantRevision, tc.wantAttached)
			}
			var result struct {
				Code           string `json:"code"`
				ActualRevision int64  `json:"actual_revision"`
				IssueRevision  int64  `json:"issue_revision"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if tc.wantStatus == 409 && (result.Code != "revision_conflict" || result.ActualRevision != 7) {
				t.Errorf("conflict=%+v", result)
			}
			if tc.wantStatus == 200 && result.IssueRevision != tc.wantRevision {
				t.Errorf("response revision=%d, want %d", result.IssueRevision, tc.wantRevision)
			}
		})
	}
}

type failIssueLabelReadback struct{ db.DBTX }

func (f failIssueLabelReadback) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "ListLabelsByIssue") {
		return nil, errors.New("injected label readback failure")
	}
	return f.DBTX.Query(ctx, sql, args...)
}

func TestConditionalLabelWriteReturnsRevisionWhenReadbackFails(t *testing.T) {
	labelID := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": testWorkspaceID, "name": "Readback failure label", "color": "#aabbcc", "resource_type": "issue"})
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			id := dbfx.Issue(t, "conditional label readback", testutil.Cols{"revision": 7})
			t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM issue_to_label WHERE issue_id=$1`, id) })
			if method == http.MethodDelete {
				dbfx.Exec(t, `INSERT INTO issue_to_label(issue_id,label_id) VALUES($1,$2)`, id, labelID)
			}
			h := *testHandler
			h.Queries = db.New(failIssueLabelReadback{DBTX: testPool})
			req := testutil.WithURLParams(newRequest(method, "/api/issues/"+id+"/labels", map[string]any{"label_id": labelID}), "id", id, "labelId", labelID)
			req.URL.RawQuery = "expected_revision=7"
			response := httptest.NewRecorder()
			if method == http.MethodPost {
				h.AttachLabel(response, req)
			} else {
				h.DetachLabel(response, req)
			}
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			var result struct {
				IssueRevision int64 `json:"issue_revision"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.IssueRevision != 8 {
				t.Fatalf("committed write lost its revision: %s", response.Body)
			}
		})
	}
}

func TestIssueRevisionSerializesConcurrentLabelWriters(t *testing.T) {
	id := dbfx.Issue(t, "concurrent conditional labels", testutil.Cols{"revision": 7})
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM issue_to_label WHERE issue_id=$1`, id) })
	var labels []string
	for i := range 2 {
		labels = append(labels, dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": testWorkspaceID, "name": fmt.Sprintf("Concurrent label %d", i), "color": "#aabbcc", "resource_type": "issue"}))
	}
	start := make(chan struct{})
	result := make(chan int, 2)
	for _, labelID := range labels {
		go func() {
			<-start
			req := testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+id+"/labels", map[string]any{"label_id": labelID}), "id", id)
			req.URL.RawQuery = "expected_revision=7"
			response := httptest.NewRecorder()
			testHandler.AttachLabel(response, req)
			result <- response.Code
		}()
	}
	close(start)
	first, second := <-result, <-result
	if !((first == 200 && second == 409) || (first == 409 && second == 200)) {
		t.Fatalf("writers=(%d,%d), want one success and one conflict", first, second)
	}
	var revision int64
	var count int
	dbfx.QueryRow(t, `SELECT revision,(SELECT count(*) FROM issue_to_label WHERE issue_id=$1) FROM issue WHERE id=$1`, id).Scan(&revision, &count)
	if revision != 8 || count != 1 {
		t.Errorf("revision=%d labels=%d, want 8 and 1", revision, count)
	}
}

type mutateBeforeLabelRead struct {
	db.DBTX
	mutate func()
}

func (d mutateBeforeLabelRead) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if strings.Contains(query, "-- name: ListLabelsByIssue") {
		d.mutate()
	}
	return d.DBTX.Query(ctx, query, args...)
}

func TestConditionalLabelSnapshotMatchesMutationRevision(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			labelID := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": testWorkspaceID, "name": "snapshot label", "color": "#aabbcc", "resource_type": "issue"})
			id := dbfx.Issue(t, "label snapshot", testutil.Cols{"revision": 7})
			t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM issue_to_label WHERE issue_id=$1`, id) })
			if method == http.MethodDelete {
				dbfx.Exec(t, `INSERT INTO issue_to_label(issue_id,label_id) VALUES($1,$2)`, id, labelID)
			}
			h := *testHandler
			h.Queries = db.New(mutateBeforeLabelRead{DBTX: testPool, mutate: func() {
				if method == http.MethodPost {
					dbfx.Exec(t, `DELETE FROM issue_to_label WHERE issue_id=$1`, id)
				} else {
					dbfx.Exec(t, `INSERT INTO issue_to_label(issue_id,label_id) VALUES($1,$2)`, id, labelID)
				}
				dbfx.Exec(t, `UPDATE issue SET revision=9 WHERE id=$1`, id)
			}})
			req := testutil.WithURLParams(newRequest(method, "/api/issues/"+id+"/labels", map[string]any{"label_id": labelID}), "id", id, "labelId", labelID)
			req.URL.RawQuery = "expected_revision=7"
			response := httptest.NewRecorder()
			if method == http.MethodPost {
				h.AttachLabel(response, req)
			} else {
				h.DetachLabel(response, req)
			}
			var result struct {
				Labels   []LabelResponse `json:"labels"`
				Revision int64           `json:"issue_revision"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != 200 || result.Revision != 8 || (len(result.Labels) == 1) != (method == http.MethodPost) {
				t.Fatalf("inconsistent mutation snapshot: status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}
