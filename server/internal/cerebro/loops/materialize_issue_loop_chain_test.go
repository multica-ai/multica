package loops

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestIssueLoopBridgeRejectsRetiredSpecBeforeWriting(t *testing.T) {
	bridge := &IssueLoopBridge{}
	err := bridge.SyncIssueLoop(context.Background(), pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, "member", []byte(`{
		"version": 1,
		"verification": [],
		"caps": {"max_iterations": 3, "max_revisions": 2, "no_progress_stalls": 1}
	}`))
	if err == nil || !strings.Contains(err.Error(), "version 1 is retired") {
		t.Fatalf("legacy spec was not rejected safely: %v", err)
	}
}
