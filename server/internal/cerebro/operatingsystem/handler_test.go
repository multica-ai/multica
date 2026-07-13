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
func (f *fakeHandlerService) UpdateStrategyItem(context.Context, pgtype.UUID, pgtype.UUID, StrategyItemInput) (StrategyItemResponse, error) {
	return StrategyItemResponse{}, nil
}
func (f *fakeHandlerService) DeleteStrategyItem(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) {
	return true, nil
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
