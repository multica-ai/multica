package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestCodexGuardianRateLimitCircuitCountsDistinctParentReviews(t *testing.T) {
	t.Parallel()
	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	c.threadID = "parent"
	gate := &codexTurnNotificationGate{}
	gate.arm()
	c.acceptNotification = gate.accept
	c.guardianRateLimitCircuit = &codexGuardianRateLimitCircuit{}
	trips := 0
	c.onGuardianRateLimit = func() { trips++ }
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"parent","turn":{"id":"turn-1"}}}`)

	notify := func(threadID, reviewID, rationale string) {
		c.handleLine(fmt.Sprintf(`{"jsonrpc":"2.0","method":"item/guardianApprovalReviewCompleted","params":{"threadId":%q,"turnId":"turn-1","reviewId":%q,"review":{"status":"denied","rationale":%q}}}`,
			threadID, reviewID, rationale))
	}
	limit := "Automatic approval review failed: exceeded retry limit, last status: 429 Too Many Requests"
	notify("parent", "review-1", limit)
	notify("parent", "review-1", limit) // replay of one review is one failure
	notify("child", "review-2", limit)  // another thread cannot affect the parent
	notify("parent", "review-3", "too risky")
	notify("parent", "review-4", limit)
	if trips != 0 {
		t.Fatalf("a policy denial must break the consecutive 429 sequence, got %d trips", trips)
	}
	notify("parent", "review-5", limit)
	notify("parent", "review-5", limit)
	if trips != 1 {
		t.Fatalf("distinct parent review failures should trip exactly once, got %d", trips)
	}
}

func TestCodexGuardianRateLimitCircuitInterruptsRepeatedApprovalFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	interruptMarker := filepath.Join(t.TempDir(), "interrupt")
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-guardian"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-guardian","turn":{"id":"turn-guardian"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/guardianApprovalReviewCompleted","params":{"threadId":"thr-guardian","turnId":"turn-guardian","reviewId":"review-1","review":{"status":"denied","rationale":"Automatic approval review failed: exceeded retry limit, last status: 429 Too Many Requests"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-guardian","turnId":"turn-guardian","item":{"type":"commandExecution","id":"cmd-1","status":"declined","aggregatedOutput":null}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/guardianApprovalReviewCompleted","params":{"threadId":"thr-guardian","turnId":"turn-guardian","reviewId":"review-2","review":{"status":"denied","rationale":"Automatic approval review failed: exceeded retry limit, last status: 429 Too Many Requests"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-guardian","turnId":"turn-guardian","item":{"type":"commandExecution","id":"cmd-2","status":"declined","aggregatedOutput":null}}}'`+"\n"+
		`read line`+"\n"+
		`echo interrupted > `+interruptMarker+"\n"+
		`echo '{"jsonrpc":"2.0","id":4,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-guardian","turn":{"id":"turn-guardian","status":"cancelled"}}}'`+"\n")

	result, _ := executeFakeCodexCollectingMessages(t, fakePath, ExecOptions{
		Timeout:              5 * time.Second,
		TurnInterruptTimeout: 200 * time.Millisecond,
	}, 3*time.Second)
	if result.Status != "failed" || !strings.Contains(result.Error, "429") || !strings.Contains(result.Error, "Guardian") {
		t.Fatalf("repeated approval failures must stop as a visible rate-limit failure, got %+v", result)
	}
	if reason := taskfailure.Classify(result.Error); reason != taskfailure.ReasonAgentProviderCapacityOrRateLimit {
		t.Fatalf("circuit result classified as %q, want provider rate limit", reason)
	}
	if _, err := os.Stat(interruptMarker); err != nil {
		t.Fatalf("Codex turn was not interrupted after the second approval failure: %v", err)
	}
}

func TestCodexGuardianRateLimitCircuitKeepsSingleFailureVisible(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-single"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-single","turn":{"id":"turn-single"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/guardianApprovalReviewCompleted","params":{"threadId":"thr-single","turnId":"turn-single","reviewId":"review-1","review":{"status":"denied","rationale":"Automatic approval review failed: exceeded retry limit, last status: 429 Too Many Requests"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/guardianApprovalReviewCompleted","params":{"threadId":"thr-single","turnId":"turn-single","reviewId":"review-2","review":{"status":"approved","rationale":"safe"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/guardianApprovalReviewCompleted","params":{"threadId":"thr-single","turnId":"turn-single","reviewId":"review-3","review":{"status":"denied","rationale":"Automatic approval review failed: exceeded retry limit, last status: 429 Too Many Requests"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-single","turnId":"turn-single","item":{"type":"agentMessage","id":"msg-1","text":"Done","phase":"final_answer"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-single","turn":{"id":"turn-single","status":"completed"}}}'`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{Timeout: 5 * time.Second})
	if result.Status != "completed" || result.Output != "Done" {
		t.Fatalf("nonconsecutive Guardian failures must not stop a completed turn, got %+v", result)
	}
}
