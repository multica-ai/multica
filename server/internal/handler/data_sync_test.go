package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

func TestNormalizeRolledBackImportResultZerosCreatedCount(t *testing.T) {
	result := normalizeRolledBackImportResult(&service.WorkspaceImportResult{
		Summary: "partial failure",
		Created: 2,
		Failed:  1,
	})

	if result.Created != 0 {
		t.Fatalf("expected created count to be reset after rollback, got %d", result.Created)
	}
	if result.Failed != 1 {
		t.Fatalf("expected failed count to be preserved, got %d", result.Failed)
	}
	if result.Summary != "apply rolled back: 0 created, 1 failed" {
		t.Fatalf("expected rollback summary, got %q", result.Summary)
	}
}
