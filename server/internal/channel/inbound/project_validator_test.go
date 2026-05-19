package inbound

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestDBProjectWorkspaceValidator_NilPoolFailsClosed(t *testing.T) {
	validator := NewDBProjectWorkspaceValidator(nil)
	id := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ValidateProjectInWorkspace panicked with nil pool: %v", r)
		}
	}()

	err := validator.ValidateProjectInWorkspace(context.Background(), id, id)
	if err == nil {
		t.Fatal("ValidateProjectInWorkspace should return an error")
	}
	if !strings.Contains(err.Error(), "project validator is not configured") {
		t.Fatalf("error = %q, want missing project validator", err.Error())
	}
}
