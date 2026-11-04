package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// budgetWorkspace is a workspace with an owner and a plain member, with the
// company budget the write tests change.
type budgetWorkspace struct {
	ID     string
	Owner  string
	Member string
}

func newBudgetWorkspace(t *testing.T) budgetWorkspace {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	base := newCompanyWorkspace(t)
	// Budget rows have no foreign key to the workspace, so they are removed here.
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM budget_hold WHERE workspace_id = $1`, base.ID)
		testPool.Exec(context.Background(), `DELETE FROM budget_period WHERE workspace_id = $1`, base.ID)
		testPool.Exec(context.Background(), `DELETE FROM budget WHERE workspace_id = $1`, base.ID)
	})
	return budgetWorkspace{ID: base.ID, Owner: base.Owner, Member: base.Member}
}

func budgetReq(userID, wsID, method, path string, body any) *http.Request {
	return companyReq(userID, wsID, method, path, body)
}

type budgetJSON struct {
	ID           string `json:"id"`
	ScopeType    string `json:"scope_type"`
	Period       string `json:"period"`
	AmountMicros int64  `json:"amount_micros"`
	Mode         string `json:"mode"`
	State        string `json:"state"`
	SpentMicros  int64  `json:"spent_micros"`
}

// An omitted mode creates a hold budget (spec 4.2), not a warn one.
func TestBudgetAPI_OmittedModeCreatesAHoldBudget(t *testing.T) {
	ws := newBudgetWorkspace(t)
	create := map[string]any{"scope_type": "company", "period": "month", "amount_micros": 50_000_000}
	var created budgetJSON
	testutil.Call(t, testHandler.CreateBudget, budgetReq(ws.Owner, ws.ID, http.MethodPost, "/api/budgets", create)).
		Want(http.StatusCreated).JSON(&created)
	if created.Mode != "hold" {
		t.Fatalf("budget created without a mode = %q, want hold", created.Mode)
	}
}

func TestBudgetAPI_OnlyOwnersAndAdminsWriteAndTheWriteIsValidated(t *testing.T) {
	ws := newBudgetWorkspace(t)
	create := map[string]any{"scope_type": "company", "period": "month", "amount_micros": 50_000_000, "mode": "warn"}

	testutil.Call(t, testHandler.CreateBudget, budgetReq(ws.Member, ws.ID, http.MethodPost, "/api/budgets", create)).
		Want(http.StatusForbidden)

	var created budgetJSON
	testutil.Call(t, testHandler.CreateBudget, budgetReq(ws.Owner, ws.ID, http.MethodPost, "/api/budgets", create)).
		Want(http.StatusCreated).JSON(&created)
	if created.ID == "" || created.AmountMicros != 50_000_000 || created.Mode != "warn" {
		t.Fatalf("created budget = %+v", created)
	}

	// A second active budget for the same company and month is a conflict.
	testutil.Call(t, testHandler.CreateBudget, budgetReq(ws.Owner, ws.ID, http.MethodPost, "/api/budgets", create)).
		Want(http.StatusConflict)

	bad := map[string]any{"scope_type": "company", "period": "day", "amount_micros": 1, "mode": "warn"}
	testutil.Call(t, testHandler.CreateBudget, budgetReq(ws.Owner, ws.ID, http.MethodPost, "/api/budgets", bad)).
		Want(http.StatusBadRequest)

	human := map[string]any{"scope_type": "company", "period": "month", "amount_micros": 1, "mode": "hold", "classes": []string{"human"}}
	testutil.Call(t, testHandler.CreateBudget, budgetReq(ws.Owner, ws.ID, http.MethodPost, "/api/budgets", human)).
		Want(http.StatusBadRequest)

	squadScoped := map[string]any{"scope_type": "squad", "period": "month", "amount_micros": 1, "mode": "hold"}
	testutil.Call(t, testHandler.CreateBudget, budgetReq(ws.Owner, ws.ID, http.MethodPost, "/api/budgets", squadScoped)).
		Want(http.StatusBadRequest)

	// Listing shows the budget with its state, to every member.
	var list struct {
		Budgets []budgetJSON `json:"budgets"`
	}
	testutil.Call(t, testHandler.ListBudgets, budgetReq(ws.Member, ws.ID, http.MethodGet, "/api/budgets", nil)).
		Want(http.StatusOK).JSON(&list)
	if len(list.Budgets) != 1 || list.Budgets[0].State != "ok" {
		t.Fatalf("list = %+v, want the one budget in state ok", list.Budgets)
	}

	// Raising the amount is an owner's change; a member cannot.
	raise := map[string]any{"amount_micros": 80_000_000}
	withID := func(req *http.Request) *http.Request { return withURLParam(req, "id", created.ID) }
	testutil.Call(t, testHandler.UpdateBudget, withID(budgetReq(ws.Member, ws.ID, http.MethodPatch, "/api/budgets/"+created.ID, raise))).
		Want(http.StatusForbidden)
	var updated budgetJSON
	testutil.Call(t, testHandler.UpdateBudget, withID(budgetReq(ws.Owner, ws.ID, http.MethodPatch, "/api/budgets/"+created.ID, raise))).
		Want(http.StatusOK).JSON(&updated)
	if updated.AmountMicros != 80_000_000 {
		t.Fatalf("raised amount = %d, want 80000000", updated.AmountMicros)
	}

	// Extending takes a positive amount; a zero is refused.
	extend := func(body map[string]any) *http.Request {
		return withURLParam(budgetReq(ws.Owner, ws.ID, http.MethodPost, "/api/budgets/"+created.ID+"/extend", body), "id", created.ID)
	}
	testutil.Call(t, testHandler.ExtendBudget, extend(map[string]any{"amount_micros": 0})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.ExtendBudget, extend(map[string]any{"amount_micros": 5_000_000})).Want(http.StatusOK)

	// Holds in force and the release count are readable and well-formed.
	var holds struct {
		Holds []json.RawMessage `json:"holds"`
	}
	testutil.Call(t, testHandler.ListBudgetHolds, budgetReq(ws.Member, ws.ID, http.MethodGet, "/api/budget-holds?status=in_force", nil)).
		Want(http.StatusOK).JSON(&holds)
	if holds.Holds == nil {
		t.Fatal("holds must be an array, even when empty")
	}
	testutil.Call(t, testHandler.ReleaseBudgetHolds, withURLParam(budgetReq(ws.Owner, ws.ID, http.MethodPost, "/api/budgets/"+created.ID+"/release-holds", nil), "id", created.ID)).
		Want(http.StatusOK)

	// A budget of another workspace is not found from this one.
	other := newBudgetWorkspace(t)
	var foreign budgetJSON
	testutil.Call(t, testHandler.CreateBudget, budgetReq(other.Owner, other.ID, http.MethodPost, "/api/budgets", create)).
		Want(http.StatusCreated).JSON(&foreign)
	testutil.Call(t, testHandler.ExtendBudget, withURLParam(budgetReq(ws.Owner, ws.ID, http.MethodPost, "/api/budgets/"+foreign.ID+"/extend", map[string]any{"amount_micros": 1}), "id", foreign.ID)).
		Want(http.StatusNotFound)
}
