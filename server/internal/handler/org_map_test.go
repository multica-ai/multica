package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// A floor's chief, a squad on that floor with an online leader and a charter-less
// project, and a squad with no floor, read back through GET /api/company/org-map.
func TestGetCompanyOrgMap_FloorFilterAndRows(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)
	chief := dbfx.Agent(t, "Chief "+uuid.NewString(), runtimeID, testutil.Cols{"workspace_id": testWorkspaceID})
	leader := dbfx.Agent(t, "Leader "+uuid.NewString(), runtimeID, testutil.Cols{"workspace_id": testWorkspaceID})
	floor := dbfx.Insert(t, "company_floor", testutil.Cols{"workspace_id": testWorkspaceID, "name": "Floor " + uuid.NewString(), "position": 0})
	dbfx.Exec(t, `INSERT INTO floor_chief (workspace_id, floor_id, agent_id) VALUES ($1, $2, $3)`, testWorkspaceID, floor, chief)
	onFloor := dbfx.Squad(t, "On floor "+uuid.NewString(), leader, testutil.Cols{"floor_id": floor})
	dbfx.SquadMember(t, onFloor, "agent", leader)
	loose := dbfx.Squad(t, "Loose "+uuid.NewString(), leader)

	var all service.OrgMap
	testutil.Call(t, testHandler.GetCompanyOrgMap, newRequest("GET", "/api/company/org-map", nil)).Want(http.StatusOK).JSON(&all)
	if all.SquadLimit != service.OrgMapMaxSquads {
		t.Fatalf("squad_limit = %d, want %d", all.SquadLimit, service.OrgMapMaxSquads)
	}
	var sawChief bool
	for _, f := range all.Floors {
		if f.ID == floor && f.Chief != nil && f.Chief.ID == chief {
			sawChief = true
		}
	}
	if !sawChief {
		t.Fatalf("floor %s has no chief in the org map", floor)
	}

	var narrowed service.OrgMap
	req := newRequest("GET", "/api/company/org-map?floor_id="+floor, nil)
	testutil.Call(t, testHandler.GetCompanyOrgMap, req).Want(http.StatusOK).JSON(&narrowed)
	for _, s := range narrowed.Squads {
		if s.ID == loose {
			t.Fatalf("floor filter returned a squad with no floor")
		}
	}
	if len(narrowed.Floors) != 1 || narrowed.Floors[0].ID != floor {
		t.Fatalf("floor filter returned floors %+v", narrowed.Floors)
	}
}
