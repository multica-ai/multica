package service

import (
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/textpatch"
)

// Unit tests for document service logic that doesn't require a database.
// Integration tests (with real Postgres) live in handler tests or a
// separate integration_test.go.

func TestProvenanceHelpers(t *testing.T) {
	t.Run("nil author returns invalid UUID", func(t *testing.T) {
		u := provenanceAuthorToNullableUUID(nil)
		if u.Valid {
			t.Error("expected invalid UUID for nil author")
		}
	})

	t.Run("nil task returns invalid UUID", func(t *testing.T) {
		u := provenanceTaskToNullableUUID(nil)
		if u.Valid {
			t.Error("expected invalid UUID for nil task")
		}
	})
}

func TestErrDocumentConflict(t *testing.T) {
	if !errors.Is(ErrDocumentConflict, ErrDocumentConflict) {
		t.Error("ErrDocumentConflict should match itself")
	}
}

func TestFuzzyPatchErrors(t *testing.T) {
	// Verify that textpatch errors propagate correctly.
	_, err := textpatch.FuzzyReplace("hello world", "nonexistent", "replacement")
	if !errors.Is(err, textpatch.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	_, err = textpatch.FuzzyReplace("foo bar foo bar", "foo", "baz")
	if !errors.Is(err, textpatch.ErrAmbiguous) {
		t.Errorf("expected ErrAmbiguous, got %v", err)
	}
}
