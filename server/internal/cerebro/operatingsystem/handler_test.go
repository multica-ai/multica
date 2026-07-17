package operatingsystem

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	testWorkspaceID = "550e8400-e29b-41d4-a716-446655440000"
	testMemberID    = "550e8400-e29b-41d4-a716-446655440001"
)

func TestHandlerRejectsNonMemberAccess(t *testing.T) {
	h := NewHandler(&fakeHandlerService{})
	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/strategy-items", nil)
	rec := httptest.NewRecorder()

	h.ListStrategyItems(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestHandlerRejectsMalformedStrategyBody(t *testing.T) {
	called := false
	h := NewHandler(&fakeHandlerService{createStrategy: func(context.Context, pgtype.UUID, StrategyItemInput) (StrategyItemResponse, error) {
		called = true
		return StrategyItemResponse{}, nil
	}})
	req := memberRequest(http.MethodPost, "/api/cerebro/strategy-items", `{`)
	rec := httptest.NewRecorder()

	h.CreateStrategyItem(rec, req)

	if rec.Code != http.StatusBadRequest || called {
		t.Fatalf("status = %d, called = %v", rec.Code, called)
	}
}

func TestHandlerRejectsInvalidStrategyUUID(t *testing.T) {
	h := NewHandler(&fakeHandlerService{})
	req := memberRequest(http.MethodPut, "/api/cerebro/strategy-items/not-a-uuid", `{"kind":"core_value","title":"Care"}`)
	req = withURLParam(req, "id", "not-a-uuid")
	rec := httptest.NewRecorder()

	h.UpdateStrategyItem(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandlerListsStrategyItems(t *testing.T) {
	h := NewHandler(&fakeHandlerService{listStrategy: func(context.Context, pgtype.UUID) ([]StrategyItemResponse, error) {
		return []StrategyItemResponse{{ID: "strategy-1", Title: "Care deeply"}}, nil
	}})
	req := memberRequest(http.MethodGet, "/api/cerebro/strategy-items", "")
	rec := httptest.NewRecorder()

	h.ListStrategyItems(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Care deeply") {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerMapsCrossWorkspaceRockToNotFound(t *testing.T) {
	h := NewHandler(&fakeHandlerService{upsertRock: func(context.Context, pgtype.UUID, RockInput) error {
		return ErrProjectNotInWorkspace
	}})
	req := memberRequest(http.MethodPost, "/api/cerebro/rocks", `{"project_id":"550e8400-e29b-41d4-a716-446655440099","period_start":"2026-07-01","period_end":"2026-09-30","confidence":70,"reported_health":"on_track"}`)
	rec := httptest.NewRecorder()

	h.UpsertRock(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerRejectsMalformedConnectionQuery(t *testing.T) {
	h := NewHandler(&fakeHandlerService{})
	req := memberRequest(http.MethodGet, "/api/cerebro/object-connections?object_type=rock&object_id=nope", "")
	rec := httptest.NewRecorder()

	h.ListConnections(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandlerListsElements(t *testing.T) {
	h := NewHandler(&fakeHandlerService{listElements: func(context.Context, pgtype.UUID) ([]OsElementResponse, error) {
		return []OsElementResponse{{Key: "goals", Enabled: true, DefaultEnabled: true}}, nil
	}})
	req := memberRequest(http.MethodGet, "/api/cerebro/operating-system/elements", "")
	rec := httptest.NewRecorder()

	h.ListElements(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"goals"`) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerMapsUnknownElementToBadRequest(t *testing.T) {
	h := NewHandler(&fakeHandlerService{updateElement: func(_ context.Context, _ pgtype.UUID, key string, _ bool) (OsElementResponse, error) {
		return OsElementResponse{}, errors.New(`invalid element "` + key + `"`)
	}})
	req := memberRequest(http.MethodPut, "/api/cerebro/operating-system/elements/nope", `{"enabled":true}`)
	req = withURLParam(req, "key", "nope")
	rec := httptest.NewRecorder()

	h.UpdateElement(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerCreatesGoalType(t *testing.T) {
	h := NewHandler(&fakeHandlerService{createGoalType: func(_ context.Context, _ pgtype.UUID, input GoalTypeInput) (GoalTypeResponse, error) {
		return GoalTypeResponse{ID: "goal-type-1", Name: input.Name}, nil
	}})
	req := memberRequest(http.MethodPost, "/api/cerebro/goal-types", `{"name":"Company","color":"#22C55E","scope_label":"company-wide"}`)
	rec := httptest.NewRecorder()

	h.CreateGoalType(rec, req)

	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "Company") {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerMapsDuplicateGoalTypeToConflict(t *testing.T) {
	h := NewHandler(&fakeHandlerService{createGoalType: func(context.Context, pgtype.UUID, GoalTypeInput) (GoalTypeResponse, error) {
		return GoalTypeResponse{}, errors.New("a goal type with this name already exists")
	}})
	req := memberRequest(http.MethodPost, "/api/cerebro/goal-types", `{"name":"Company"}`)
	rec := httptest.NewRecorder()

	h.CreateGoalType(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerCreatesPeriod(t *testing.T) {
	h := NewHandler(&fakeHandlerService{createPeriod: func(_ context.Context, _ pgtype.UUID, input OperatingPeriodInput) (OperatingPeriodResponse, error) {
		return OperatingPeriodResponse{ID: "period-1", Name: "August 2026", Unit: input.Unit}, nil
	}})
	req := memberRequest(http.MethodPost, "/api/cerebro/operating-periods", `{"unit":"month","starts_on":"2026-08-01"}`)
	rec := httptest.NewRecorder()

	h.CreatePeriod(rec, req)

	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "August 2026") {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerRejectsInvalidPeriodUUIDOnDelete(t *testing.T) {
	h := NewHandler(&fakeHandlerService{})
	req := memberRequest(http.MethodDelete, "/api/cerebro/operating-periods/not-a-uuid", "")
	req = withURLParam(req, "id", "not-a-uuid")
	rec := httptest.NewRecorder()

	h.DeletePeriod(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func memberRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	memberID := pgtype.UUID{Bytes: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, Valid: true}
	ctx := middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{ID: memberID})
	return req.WithContext(ctx)
}

func withURLParam(req *http.Request, key, value string) *http.Request {
	route := chi.NewRouteContext()
	route.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
}

type fakeHandlerService struct {
	createStrategy func(context.Context, pgtype.UUID, StrategyItemInput) (StrategyItemResponse, error)
	listStrategy   func(context.Context, pgtype.UUID) ([]StrategyItemResponse, error)
	upsertRock     func(context.Context, pgtype.UUID, RockInput) error
	listElements   func(context.Context, pgtype.UUID) ([]OsElementResponse, error)
	updateElement  func(context.Context, pgtype.UUID, string, bool) (OsElementResponse, error)
	createGoalType func(context.Context, pgtype.UUID, GoalTypeInput) (GoalTypeResponse, error)
	createPeriod   func(context.Context, pgtype.UUID, OperatingPeriodInput) (OperatingPeriodResponse, error)
}

func (f *fakeHandlerService) ListElements(ctx context.Context, ws pgtype.UUID) ([]OsElementResponse, error) {
	if f.listElements == nil {
		return []OsElementResponse{}, nil
	}
	return f.listElements(ctx, ws)
}
func (f *fakeHandlerService) UpdateElement(ctx context.Context, ws pgtype.UUID, key string, enabled bool) (OsElementResponse, error) {
	if f.updateElement == nil {
		return OsElementResponse{}, nil
	}
	return f.updateElement(ctx, ws, key, enabled)
}
func (f *fakeHandlerService) CreateGoalType(ctx context.Context, ws pgtype.UUID, input GoalTypeInput) (GoalTypeResponse, error) {
	if f.createGoalType == nil {
		return GoalTypeResponse{}, nil
	}
	return f.createGoalType(ctx, ws, input)
}
func (f *fakeHandlerService) ListGoalTypes(context.Context, pgtype.UUID) ([]GoalTypeResponse, error) {
	return []GoalTypeResponse{}, nil
}
func (f *fakeHandlerService) UpdateGoalType(context.Context, pgtype.UUID, pgtype.UUID, GoalTypeInput) (GoalTypeResponse, error) {
	return GoalTypeResponse{}, nil
}
func (f *fakeHandlerService) DeleteGoalType(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) {
	return true, nil
}
func (f *fakeHandlerService) CreatePeriod(ctx context.Context, ws pgtype.UUID, input OperatingPeriodInput) (OperatingPeriodResponse, error) {
	if f.createPeriod == nil {
		return OperatingPeriodResponse{}, nil
	}
	return f.createPeriod(ctx, ws, input)
}
func (f *fakeHandlerService) UpdatePeriod(context.Context, pgtype.UUID, pgtype.UUID, OperatingPeriodInput) (OperatingPeriodResponse, error) {
	return OperatingPeriodResponse{}, nil
}
func (f *fakeHandlerService) DeletePeriod(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) {
	return true, nil
}

func (f *fakeHandlerService) GetSettings(context.Context, pgtype.UUID) (SettingsResponse, error) {
	return SettingsResponse{}, nil
}
func (f *fakeHandlerService) UpdateSettings(context.Context, pgtype.UUID, Terminology) (SettingsResponse, error) {
	return SettingsResponse{}, nil
}
func (f *fakeHandlerService) CreateStrategyItem(ctx context.Context, ws pgtype.UUID, input StrategyItemInput) (StrategyItemResponse, error) {
	if f.createStrategy == nil {
		return StrategyItemResponse{}, errors.New("unexpected call")
	}
	return f.createStrategy(ctx, ws, input)
}
func (f *fakeHandlerService) ListStrategyItems(ctx context.Context, ws pgtype.UUID) ([]StrategyItemResponse, error) {
	if f.listStrategy == nil {
		return nil, errors.New("unexpected call")
	}
	return f.listStrategy(ctx, ws)
}
func (f *fakeHandlerService) ListStrategyHistory(context.Context, pgtype.UUID) ([]StrategyHistoryResponse, error) {
	return []StrategyHistoryResponse{}, nil
}
func (f *fakeHandlerService) UpdateStrategyItem(context.Context, pgtype.UUID, pgtype.UUID, StrategyItemInput) (StrategyItemResponse, error) {
	return StrategyItemResponse{}, nil
}
func (f *fakeHandlerService) DeleteStrategyItem(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) {
	return true, nil
}
func (f *fakeHandlerService) ListPeriods(context.Context, pgtype.UUID) ([]OperatingPeriodResponse, error) {
	return []OperatingPeriodResponse{}, nil
}
func (f *fakeHandlerService) UpsertRock(ctx context.Context, ws pgtype.UUID, input RockInput) error {
	if f.upsertRock == nil {
		return errors.New("unexpected call")
	}
	return f.upsertRock(ctx, ws, input)
}
func (f *fakeHandlerService) ListRocks(context.Context, pgtype.UUID) ([]RockResponse, error) {
	return []RockResponse{}, nil
}
func (f *fakeHandlerService) SaveRock(context.Context, pgtype.UUID, string, pgtype.UUID, *pgtype.UUID, RockInput) (RockResponse, error) {
	return RockResponse{}, nil
}
func (f *fakeHandlerService) AddRockCheckIn(context.Context, pgtype.UUID, pgtype.UUID, string, pgtype.UUID, RockCheckInInput) (RockCheckIn, error) {
	return RockCheckIn{}, nil
}
func (f *fakeHandlerService) DeleteRock(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) {
	return true, nil
}
func (f *fakeHandlerService) CreateConnection(context.Context, pgtype.UUID, string, pgtype.UUID, ObjectConnectionInput) (ObjectConnectionResponse, error) {
	return ObjectConnectionResponse{}, nil
}
func (f *fakeHandlerService) ListConnections(context.Context, pgtype.UUID, string, string) ([]ObjectConnectionResponse, error) {
	return []ObjectConnectionResponse{}, nil
}
func (f *fakeHandlerService) DeleteConnection(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) {
	return true, nil
}
