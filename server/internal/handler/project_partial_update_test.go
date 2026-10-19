package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type projectReadPauseDB struct {
	db.DBTX
	paused, release chan struct{}
}

func (d projectReadPauseDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	row := d.DBTX.QueryRow(ctx, query, args...)
	if strings.HasPrefix(query, "-- name: GetProjectInWorkspace :one") {
		return &contextLockPauseRow{Row: row, paused: d.paused, release: d.release}
	}
	return row
}

func projectNullableValues(p ProjectResponse) map[string]*string {
	return map[string]*string{
		"description": p.Description, "icon": p.Icon,
		"lead_type": p.LeadType, "lead_id": p.LeadID,
		"start_date": p.StartDate, "due_date": p.DueDate,
	}
}

func TestUpdateProjectConcurrentPartialUpdate(t *testing.T) {
	for _, clear := range []bool{false, true} {
		name := "set"
		if clear {
			name = "clear"
		}
		t.Run(name, func(t *testing.T) {
			projectID := dbfx.Project(t, "original title", testutil.Cols{
				"description": "original description", "icon": "original icon",
				"lead_type": "member", "lead_id": testUserID,
				"start_date": "2026-03-01", "due_date": "2026-03-31",
			})
			paused, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			resume := func() { once.Do(func() { close(release) }) }
			defer resume()
			h := *testHandler
			h.Queries = db.New(projectReadPauseDB{DBTX: testPool, paused: paused, release: release})
			req := withURLParam(newRequest("PUT", "/api/projects/"+projectID, map[string]any{
				"title": "updated title",
			}), "id", projectID)
			ctx, cancel := context.WithTimeout(req.Context(), 15*time.Second)
			defer cancel()
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()
			done := make(chan error, 1)
			go func() {
				h.UpdateProject(rec, req)
				done <- nil
			}()
			waitContextLockSignal(t, paused, "title update did not read project")
			changes := map[string]any{
				"description": "new description", "icon": "new icon",
				"lead_type": "agent", "lead_id": dbfx.Agent(t, "project lead", testRuntimeID),
				"start_date": "2026-04-01", "due_date": "2026-04-30",
			}
			if clear {
				for field := range changes {
					changes[field] = nil
				}
			}
			var competing ProjectResponse
			testutil.Call(t, testHandler.UpdateProject, withURLParam(newRequest("PUT", "/api/projects/"+projectID, changes), "id", projectID)).Want(http.StatusOK).JSON(&competing)
			resume()
			waitContextLockResult(t, done, "title-only update")
			updated := decodeProject(t, rec, http.StatusOK)
			stored, err := testHandler.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
				ID: parseUUID(projectID), WorkspaceID: parseUUID(testWorkspaceID),
			})
			if err != nil {
				t.Fatal(err)
			}
			for label, p := range map[string]ProjectResponse{"response": updated, "database": projectToResponse(stored)} {
				if p.Title != "updated title" {
					t.Errorf("%s title = %q, want updated title", label, p.Title)
				}
				for field, want := range projectNullableValues(competing) {
					got := projectNullableValues(p)[field]
					if !reflect.DeepEqual(got, want) {
						t.Errorf("%s lost concurrent %s update: got %v, want %v", label, field, got, want)
					}
				}
			}
		})
	}
}

func TestUpdateProjectNullableFieldPresence(t *testing.T) {
	agentID := dbfx.Agent(t, "project lead", testRuntimeID)
	initial := map[string]string{
		"description": "original description", "icon": "original icon",
		"lead_type": "member", "lead_id": testUserID,
		"start_date": "2026-03-01", "due_date": "2026-03-31",
	}
	values := map[string]string{
		"description": "new description", "icon": "new icon",
		"lead_type": "agent", "lead_id": agentID,
		"start_date": "2026-04-01", "due_date": "2026-04-30",
	}
	for field, value := range values {
		modes := []string{"omitted", "set", "null"}
		if field == "start_date" || field == "due_date" {
			modes = append(modes, "empty")
		}
		for _, mode := range modes {
			t.Run(field+"/"+mode, func(t *testing.T) {
				cols := testutil.Cols{}
				for key, value := range initial {
					cols[key] = value
				}
				projectID := dbfx.Project(t, "original title", cols)
				body := map[string]any{"title": "updated title", "status": nil, "priority": nil}
				switch mode {
				case "set":
					body[field] = value
				case "null":
					body[field] = nil
				case "empty":
					body[field] = ""
				}
				var updated ProjectResponse
				testutil.Call(t, testHandler.UpdateProject, withURLParam(newRequest("PUT", "/api/projects/"+projectID, body), "id", projectID)).Want(http.StatusOK).JSON(&updated)
				stored, err := testHandler.Queries.GetProjectInWorkspace(context.Background(), db.GetProjectInWorkspaceParams{
					ID: parseUUID(projectID), WorkspaceID: parseUUID(testWorkspaceID),
				})
				if err != nil {
					t.Fatal(err)
				}
				for label, p := range map[string]ProjectResponse{"response": updated, "database": projectToResponse(stored)} {
					if p.Title != "updated title" || p.Status != "planned" || p.Priority != "none" {
						t.Errorf("%s nonnullable fields = (%q, %q, %q)", label, p.Title, p.Status, p.Priority)
					}
					for key, got := range projectNullableValues(p) {
						want := initial[key]
						if key == field {
							if mode == "null" || mode == "empty" {
								if got != nil {
									t.Errorf("%s %s = %q, want null", label, key, *got)
								}
								continue
							}
							if mode == "set" {
								want = value
							}
						}
						if got == nil || *got != want {
							t.Errorf("%s %s = %v, want %q", label, key, got, want)
						}
					}
				}
			})
		}
	}
}
