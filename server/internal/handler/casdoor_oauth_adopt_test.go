package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestFindOrCreateCasdoorUser_AdoptByEmail covers the case that locked users
// out in production: a Casdoor subject with no linked user, whose email already
// belongs to an existing account (NULL subject_id). The resolver must adopt the
// existing account instead of failing on the user_email_key unique constraint.
func TestFindOrCreateCasdoorUser_AdoptByEmail(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	email := "adopt-by-email@example.com"
	subject := "casdoor-subject-adopt-001"

	// Pre-existing account holding the email, not yet linked to any subject_id.
	existing, err := testHandler.Queries.CreateUser(ctx, db.CreateUserParams{
		Name:  "Pre-existing",
		Email: email,
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_user WHERE id = $1`, existing.ID)
	})

	req := httptest.NewRequest("GET", "/", nil)
	user, isNew, err := testHandler.findOrCreateCasdoorUser(req, &casdoorUserInfo{
		Sub:   subject,
		Name:  "Casdoor Name",
		Email: email,
	})
	if err != nil {
		t.Fatalf("findOrCreateCasdoorUser: %v", err)
	}
	if isNew {
		t.Fatal("expected existing user to be adopted, got a newly created user")
	}
	if user.ID != existing.ID {
		t.Fatalf("adopted wrong user: got %v want %v", user.ID, existing.ID)
	}

	// subject_id must now be bound so subsequent logins resolve by subject.
	bound, err := testHandler.Queries.GetUserBySubjectID(ctx, pgtype.Text{String: subject, Valid: true})
	if err != nil {
		t.Fatalf("GetUserBySubjectID after adoption: %v", err)
	}
	if bound.ID != existing.ID {
		t.Fatalf("subject_id bound to wrong user: got %v want %v", bound.ID, existing.ID)
	}
}

// TestFindOrCreateCasdoorUser_RefuseHijack ensures that when the email is owned
// by a user already carrying a *different* subject_id, the resolver refuses to
// re-bind it (no silent identity hijack) and surfaces an error.
func TestFindOrCreateCasdoorUser_RefuseHijack(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	email := "refuse-hijack@example.com"
	ownerSubject := "casdoor-subject-owner-001"
	intruderSubject := "casdoor-subject-intruder-001"

	owner, err := testHandler.Queries.CreateUser(ctx, db.CreateUserParams{
		Name:  "Owner",
		Email: email,
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_user WHERE id = $1`, owner.ID)
	})
	if err := testHandler.Queries.SetUserSubjectID(ctx, db.SetUserSubjectIDParams{
		ID:        owner.ID,
		SubjectID: pgtype.Text{String: ownerSubject, Valid: true},
	}); err != nil {
		t.Fatalf("bind owner subject_id: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	_, _, err = testHandler.findOrCreateCasdoorUser(req, &casdoorUserInfo{
		Sub:   intruderSubject,
		Name:  "Intruder",
		Email: email,
	})
	if err == nil {
		t.Fatal("expected error when email is owned by a different subject_id, got nil")
	}
}
