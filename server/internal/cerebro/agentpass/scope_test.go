package agentpass

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// ── ScopeNarrows ─────────────────────────────────────────────────────────────

func TestScopeNarrows_EmptyParent_AnyChildValid(t *testing.T) {
	t.Parallel()
	child := []byte(`{"allowed_tools":["read","write"]}`)
	cases := []struct {
		name   string
		parent []byte
	}{
		{"nil", nil},
		{"empty bytes", []byte{}},
		{"empty object", []byte(`{}`)},
		{"null", []byte(`null`)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, err := ScopeNarrows(tc.parent, child)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ok {
				t.Fatalf("empty parent must allow any child scope")
			}
		})
	}
}

func TestScopeNarrows_EmptyChild_AlwaysValid(t *testing.T) {
	t.Parallel()
	parent := []byte(`{"allowed_tools":["read"]}`)
	cases := []struct {
		name  string
		child []byte
	}{
		{"nil", nil},
		{"empty bytes", []byte{}},
		{"empty object", []byte(`{}`)},
		{"null", []byte(`null`)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, err := ScopeNarrows(parent, tc.child)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ok {
				t.Fatalf("empty child scope must always be valid (inherits parent)")
			}
		})
	}
}

func TestScopeNarrows_ArraySubset_Valid(t *testing.T) {
	t.Parallel()
	parent := []byte(`{"allowed_tools":["read","write","delete"]}`)
	child := []byte(`{"allowed_tools":["read","write"]}`)
	ok, err := ScopeNarrows(parent, child)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("child subset of parent must be valid narrowing")
	}
}

func TestScopeNarrows_ArrayEqual_Valid(t *testing.T) {
	t.Parallel()
	scope := []byte(`{"allowed_tools":["read","write"]}`)
	ok, err := ScopeNarrows(scope, scope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("equal scopes must be valid (not widening)")
	}
}

func TestScopeNarrows_ArrayWiden_Invalid(t *testing.T) {
	t.Parallel()
	parent := []byte(`{"allowed_tools":["read"]}`)
	child := []byte(`{"allowed_tools":["read","write"]}`)
	ok, err := ScopeNarrows(parent, child)
	if err == nil || !errors.Is(err, ErrScopeWiden) {
		t.Fatalf("array widening must return ErrScopeWiden, got ok=%v err=%v", ok, err)
	}
	if ok {
		t.Fatalf("array widening must return ok=false")
	}
}

func TestScopeNarrows_ChildAddsNewKey_Valid(t *testing.T) {
	t.Parallel()
	// Parent restricts tools; child also restricts models (adding a restriction).
	parent := []byte(`{"allowed_tools":["read"]}`)
	child := []byte(`{"allowed_tools":["read"],"allowed_models":["claude-sonnet"]}`)
	ok, err := ScopeNarrows(parent, child)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("child adding a new restriction (new key) is narrowing — must be valid")
	}
}

func TestScopeNarrows_ScalarEqual_Valid(t *testing.T) {
	t.Parallel()
	scope := []byte(`{"max_tokens":1000}`)
	ok, err := ScopeNarrows(scope, scope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("identical scalar values must be valid")
	}
}

func TestScopeNarrows_ScalarWiden_Invalid(t *testing.T) {
	t.Parallel()
	parent := []byte(`{"max_tokens":1000}`)
	child := []byte(`{"max_tokens":9999}`)
	ok, err := ScopeNarrows(parent, child)
	if err == nil || !errors.Is(err, ErrScopeWiden) {
		t.Fatalf("scalar change must return ErrScopeWiden, got ok=%v err=%v", ok, err)
	}
	if ok {
		t.Fatalf("scalar change must return ok=false")
	}
}

func TestScopeNarrows_MultipleKeys_OnlyOneWidens_Invalid(t *testing.T) {
	t.Parallel()
	parent := []byte(`{"allowed_tools":["read","write"],"allowed_models":["claude-sonnet"]}`)
	// Child narrows tools (OK) but widens models (not OK).
	child := []byte(`{"allowed_tools":["read"],"allowed_models":["claude-sonnet","claude-opus"]}`)
	ok, err := ScopeNarrows(parent, child)
	if err == nil || !errors.Is(err, ErrScopeWiden) {
		t.Fatalf("widening on any key must return ErrScopeWiden, got ok=%v err=%v", ok, err)
	}
	if ok {
		t.Fatalf("widening on any key must return ok=false")
	}
}

func TestScopeNarrows_EmptyArray_NarrowsAny(t *testing.T) {
	t.Parallel()
	// Child with empty array (no tools allowed) is a strict subset of any parent array.
	parent := []byte(`{"allowed_tools":["read","write"]}`)
	child := []byte(`{"allowed_tools":[]}`)
	ok, err := ScopeNarrows(parent, child)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("empty array child must be valid (empty set ⊆ any set)")
	}
}

func TestScopeNarrows_BadParentJSON_Error(t *testing.T) {
	t.Parallel()
	_, err := ScopeNarrows([]byte(`{invalid`), []byte(`{"k":["v"]}`))
	if err == nil {
		t.Fatalf("malformed parent JSON must return error")
	}
	if errors.Is(err, ErrScopeWiden) {
		t.Fatalf("parse error must NOT be ErrScopeWiden")
	}
}

func TestScopeNarrows_BadChildJSON_Error(t *testing.T) {
	t.Parallel()
	_, err := ScopeNarrows([]byte(`{"k":["v"]}`), []byte(`{invalid`))
	if err == nil {
		t.Fatalf("malformed child JSON must return error")
	}
	if errors.Is(err, ErrScopeWiden) {
		t.Fatalf("parse error must NOT be ErrScopeWiden")
	}
}

// ── ValidateChildScope ────────────────────────────────────────────────────────

func TestValidateChildScope_NoParentID_AllowsAny(t *testing.T) {
	t.Parallel()
	q := &fakeQueries{}
	s := mustService(t, q, nil)

	// parentPassID with Valid=false → root pass, nothing to narrow against.
	err := s.ValidateChildScope(context.Background(), pgtype.UUID{Valid: false},
		[]byte(`{"allowed_tools":["anything"]}`))
	if err != nil {
		t.Fatalf("no parent pass ID must allow any scope, got %v", err)
	}
}

func TestValidateChildScope_ParentNotFound_AllowsAny(t *testing.T) {
	t.Parallel()
	q := &fakeQueries{getByIDErr: pgx.ErrNoRows}
	s := mustService(t, q, nil)

	err := s.ValidateChildScope(context.Background(), uuidValue(0xAB),
		[]byte(`{"allowed_tools":["read","write"]}`))
	if err != nil {
		t.Fatalf("missing parent pass must allow (treat as no restriction), got %v", err)
	}
}

func TestValidateChildScope_ValidNarrowing_Allows(t *testing.T) {
	t.Parallel()
	q := &fakeQueries{
		getByID: cerebrodb.CerebroAgentPass{
			ID:    uuidValue(0xAB),
			Scope: []byte(`{"allowed_tools":["read","write"]}`),
		},
	}
	s := mustService(t, q, nil)

	err := s.ValidateChildScope(context.Background(), uuidValue(0xAB),
		[]byte(`{"allowed_tools":["read"]}`))
	if err != nil {
		t.Fatalf("valid narrowing must be allowed, got %v", err)
	}
}

func TestValidateChildScope_EqualScope_Allows(t *testing.T) {
	t.Parallel()
	scope := []byte(`{"allowed_tools":["read"]}`)
	q := &fakeQueries{
		getByID: cerebrodb.CerebroAgentPass{
			ID:    uuidValue(0xAB),
			Scope: scope,
		},
	}
	s := mustService(t, q, nil)

	err := s.ValidateChildScope(context.Background(), uuidValue(0xAB), scope)
	if err != nil {
		t.Fatalf("equal scope must be allowed (not widening), got %v", err)
	}
}

func TestValidateChildScope_Widening_RejectsWithErrScopeWiden(t *testing.T) {
	t.Parallel()
	q := &fakeQueries{
		getByID: cerebrodb.CerebroAgentPass{
			ID:    uuidValue(0xAB),
			Scope: []byte(`{"allowed_tools":["read"]}`),
		},
	}
	s := mustService(t, q, nil)

	err := s.ValidateChildScope(context.Background(), uuidValue(0xAB),
		[]byte(`{"allowed_tools":["read","write"]}`))
	if err == nil {
		t.Fatalf("widening scope must be rejected")
	}
	if !errors.Is(err, ErrScopeWiden) {
		t.Fatalf("rejection must be ErrScopeWiden, got %v", err)
	}
}

func TestValidateChildScope_EmptyChildScope_Allows(t *testing.T) {
	t.Parallel()
	// Child with {} inherits parent restrictions — always valid.
	q := &fakeQueries{
		getByID: cerebrodb.CerebroAgentPass{
			ID:    uuidValue(0xAB),
			Scope: []byte(`{"allowed_tools":["read"]}`),
		},
	}
	s := mustService(t, q, nil)

	err := s.ValidateChildScope(context.Background(), uuidValue(0xAB), []byte(`{}`))
	if err != nil {
		t.Fatalf("empty child scope must be allowed (inherits parent), got %v", err)
	}
}

func TestValidateChildScope_DBError_Propagates(t *testing.T) {
	t.Parallel()
	q := &fakeQueries{getByIDErr: errors.New("connection refused")}
	s := mustService(t, q, nil)

	err := s.ValidateChildScope(context.Background(), uuidValue(0xAB), []byte(`{}`))
	if err == nil {
		t.Fatalf("DB error must propagate")
	}
	if errors.Is(err, ErrScopeWiden) {
		t.Fatalf("DB error must NOT surface as ErrScopeWiden")
	}
}

func TestValidateChildScope_NilService_ReturnsErrNilQueries(t *testing.T) {
	t.Parallel()
	var s *Service
	err := s.ValidateChildScope(context.Background(), uuidValue(0xAB), []byte(`{}`))
	if !errors.Is(err, ErrNilQueries) {
		t.Fatalf("nil service must return ErrNilQueries, got %v", err)
	}
}
