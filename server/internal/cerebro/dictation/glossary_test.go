package dictation

import (
	"context"
	"testing"
)

func TestMergeGlossaryDedupesAndOrders(t *testing.T) {
	got := mergeGlossary("Helsebixen, made4men", "Mia, made4men, Multica Features")
	want := "Helsebixen, made4men, Mia, Multica Features"
	if got != want {
		t.Errorf("mergeGlossary = %q, want %q", got, want)
	}
}

func TestMergeGlossaryHandlesEmptySides(t *testing.T) {
	if got := mergeGlossary("", "Mia, Fætta"); got != "Mia, Fætta" {
		t.Errorf("auto-only = %q", got)
	}
	if got := mergeGlossary("Helsebixen", ""); got != "Helsebixen" {
		t.Errorf("manual-only = %q", got)
	}
	if got := mergeGlossary("  ,  , ", ""); got != "" {
		t.Errorf("blank in = %q, want empty", got)
	}
}

func TestWorkspaceGlossaryNoQueriesIsEmpty(t *testing.T) {
	h := &Handler{}
	if got := h.workspaceGlossary(context.Background(), "any-ws"); got != "" {
		t.Errorf("workspaceGlossary without queries = %q, want empty", got)
	}
}
