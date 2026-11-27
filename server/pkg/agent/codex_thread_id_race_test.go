package agent

import (
	"runtime"
	"testing"
	"time"
)

// GH #8422: the app-server may emit a valid notification immediately after the
// thread/start response, while executeOnce is still publishing the returned
// thread ID. The stdout goroutine then reads c.threadID in the thread filter
// concurrently with that write. The filter runs ahead of the current-turn gate,
// so the gate's own state cannot serialize this access.
func TestCodexThreadIDPublishedBeforeNotificationFilter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := writeFakeCodexAppServer(t, `read line
echo '{"jsonrpc":"2.0","id":1,"result":{}}'
read line
read line
echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-race"}}}'
echo '{"jsonrpc":"2.0","method":"thread/status/changed","params":{"threadId":"thr-race","status":{"type":"idle"}}}'
read line
echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-race","turn":{"id":"turn-race"}}}'
echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-race","item":{"type":"agentMessage","id":"msg-race","text":"Done"}}}'
echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-race","turn":{"id":"turn-race","status":"completed"}}}'
`)
	result := executeFakeCodex(t, fakePath, ExecOptions{Timeout: 5 * time.Second})
	if result.Status != "completed" || result.Output != "Done" {
		t.Fatalf("unexpected result: status=%q output=%q error=%q", result.Status, result.Output, result.Error)
	}
}

// The thread filter is the first thing every notification passes through, so it
// must tolerate absent, null and non-string threadId rather than take the
// process down with it (GH #8422).
func TestCodexThreadFilterToleratesMalformedParams(t *testing.T) {
	c, _, _ := newTestCodexClient(t)
	c.setThreadID("thr-main")
	for _, params := range []map[string]any{
		nil, {}, {"threadId": nil}, {"threadId": 42},
		{"threadId": []any{}}, {"threadId": map[string]any{}},
	} {
		if c.isNotificationFromOtherThread(params) {
			t.Fatalf("unexpected other-thread match for %#v", params)
		}
	}
	for _, line := range []string{
		`{"method":"thread/status/changed"}`,
		`{"method":"thread/status/changed","params":null}`,
		`{"method":"thread/status/changed","params":[]}`,
		`{"method":"thread/status/changed","params":42}`,
		`{"method":"thread/status/changed","params":{"threadId":null}}`,
	} {
		c.handleLine(line)
	}
}

// A notification genuinely belonging to another thread must still be filtered
// out once the current thread ID is known.
func TestCodexThreadFilterStillRejectsOtherThread(t *testing.T) {
	c, _, _ := newTestCodexClient(t)
	if c.isNotificationFromOtherThread(map[string]any{"threadId": "thr-other"}) {
		t.Fatal("no current thread ID yet: nothing to filter against")
	}
	c.setThreadID("thr-main")
	if !c.isNotificationFromOtherThread(map[string]any{"threadId": "thr-other"}) {
		t.Fatal("expected other-thread notification to be filtered")
	}
	if c.isNotificationFromOtherThread(map[string]any{"threadId": "thr-main"}) {
		t.Fatal("expected own-thread notification to pass")
	}
}
