package servicetokenapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/servicetoken"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type handlerStore struct {
	enabled bool
	creates int
	audits  []servicetoken.AuditEvent
}

func (s *handlerStore) Create(_ context.Context, p servicetoken.CreateParams) (servicetoken.Token, error) {
	s.creates++
	token := servicetoken.Token{
		ID:          "token-1",
		WorkspaceID: p.WorkspaceID,
		Name:        p.Name,
		TokenPrefix: p.TokenPrefix,
		Scopes:      p.Scopes,
		ExpiresAt:   p.ExpiresAt,
		CreatedAt:   time.Now(),
	}
	s.audits = append(s.audits, servicetoken.AuditEvent{
		TokenID:     token.ID,
		WorkspaceID: token.WorkspaceID,
		Event:       "issued",
		ActorUserID: p.CreatedBy,
		Detail:      p.AuditDetail,
	})
	return token, nil
}

func (s *handlerStore) GetByHash(context.Context, string) (servicetoken.Token, error) {
	panic("not used")
}

func (s *handlerStore) FeatureEnabled(context.Context, string) (bool, error) {
	return s.enabled, nil
}

func (s *handlerStore) Touch(context.Context, string) error { panic("not used") }

func (s *handlerStore) ListByWorkspace(context.Context, string) ([]servicetoken.Token, error) {
	panic("not used")
}

func (s *handlerStore) Revoke(context.Context, string, string, string) (servicetoken.Token, error) {
	panic("not used")
}

func (s *handlerStore) AppendAudit(_ context.Context, event servicetoken.AuditEvent) error {
	s.audits = append(s.audits, event)
	return nil
}

func createRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/service-tokens", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	member := db.Member{UserID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}}
	return req.WithContext(middleware.SetMemberContext(
		req.Context(),
		"11111111-1111-1111-1111-111111111111",
		member,
	))
}

func TestCreateTokenRequiresBoundedExpiry(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing", body: `{"name":"reader","scopes":["skills:read"]}`},
		{name: "zero", body: `{"name":"reader","scopes":["skills:read"],"expires_in_days":0}`},
		{name: "negative", body: `{"name":"reader","scopes":["skills:read"],"expires_in_days":-1}`},
		{name: "beyond maximum", body: `{"name":"reader","scopes":["skills:read"],"expires_in_days":366}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &handlerStore{enabled: true}
			handler := NewHandler(servicetoken.NewTokenService(store), nil)
			recorder := httptest.NewRecorder()
			handler.CreateToken(recorder, createRequest(tt.body))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
			if store.creates != 0 {
				t.Fatal("invalid expiry persisted a token")
			}
		})
	}
}

func TestCreateTokenRejectsWriteScope(t *testing.T) {
	store := &handlerStore{enabled: true}
	handler := NewHandler(servicetoken.NewTokenService(store), nil)
	recorder := httptest.NewRecorder()
	handler.CreateToken(recorder, createRequest(
		`{"name":"writer","scopes":["issues:write"],"expires_in_days":90}`,
	))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if store.creates != 0 {
		t.Fatal("write scope persisted a token")
	}
}

func TestCreateTokenFeatureOffRemovesManagementPath(t *testing.T) {
	store := &handlerStore{enabled: false}
	handler := NewHandler(servicetoken.NewTokenService(store), nil)
	recorder := httptest.NewRecorder()
	handler.CreateToken(recorder, createRequest(
		`{"name":"reader","scopes":["skills:read"],"expires_in_days":90}`,
	))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", recorder.Code, recorder.Body.String())
	}
	if store.creates != 0 {
		t.Fatal("disabled feature persisted a token")
	}
}

func TestCreateTokenReturnsOneTimeSecret(t *testing.T) {
	store := &handlerStore{enabled: true}
	handler := NewHandler(servicetoken.NewTokenService(store), nil)
	recorder := httptest.NewRecorder()
	handler.CreateToken(recorder, createRequest(
		`{"name":"reader","scopes":["issues:read"],"expires_in_days":90}`,
	))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", recorder.Code, recorder.Body.String())
	}
	if store.creates != 1 || len(store.audits) != 1 || store.audits[0].Event != "issued" {
		t.Fatalf("creates=%d audits=%#v", store.creates, store.audits)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"token":"msv_`)) {
		t.Fatalf("response omitted one-time secret: %s", recorder.Body.String())
	}
}
