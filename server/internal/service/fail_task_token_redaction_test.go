package service

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// A failure diagnostic can quote a credential (an HTTP 401 echoing the token,
// a CLI error printing its own flags). The stored error, the failure comment
// and the backend log must not keep it, and the failure reason must still be
// classified from the original text.
func TestFailTaskRedactsMulticaTokenInDiagnostic(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	f.Cleanup(t, "DELETE FROM comment WHERE issue_id=$1", issue)
	taskID := parseTestUUID(t, f.Task(t, agent, testutil.Cols{
		"issue_id": issue, "status": "running", "started_at": testutil.Raw("now()"), "context": "{}",
		"runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')"),
	}))
	token := "mul_" + strings.Repeat("a1", 20)
	diagnostic := "HTTP 401 unauthorized credential " + token

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	row, err := s.Tasks.FailTask(ctx, taskID, diagnostic, "", "", "", "", false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row.Error.String, token) || !strings.Contains(row.Error.String, "[REDACTED_MULTICA_TOKEN]") {
		t.Fatalf("stored error = %q", row.Error.String)
	}
	if row.FailureReason.String != string(taskfailure.Classify(diagnostic)) {
		t.Fatalf("failure reason = %q, want %q", row.FailureReason.String, taskfailure.Classify(diagnostic))
	}
	var comment string
	f.QueryRow(t, "SELECT content FROM comment WHERE issue_id=$1 AND source_task_id=$2", issue, taskID).Scan(&comment)
	if strings.Contains(comment, token) || !strings.Contains(comment, "[REDACTED_MULTICA_TOKEN]") {
		t.Fatalf("failure comment = %q", comment)
	}
	if strings.Contains(logs.String(), token) || !strings.Contains(logs.String(), "[REDACTED_MULTICA_TOKEN]") {
		t.Fatal("backend failure log kept the token")
	}
}
