package testutil

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// FakeSteerCodex blocks its only turn until turn/steer supplies marker.
// The append-only trace proves that start, steer and final answer share a PID.
func FakeSteerCodex(t *testing.T, marker string) (binary, trace string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Python executable fixture is POSIX-only")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("python3 is required for the deterministic Codex fixture")
	}
	dir := t.TempDir()
	binary, trace = filepath.Join(dir, "codex"), filepath.Join(dir, "trace.jsonl")
	markerJSON, _ := json.Marshal(marker)
	traceJSON, _ := json.Marshal(trace)
	script := "#!" + python + "\n" + "marker = " + string(markerJSON) + "\ntrace = " + string(traceJSON) + "\n" + `import json, os, sys

def record(event):
    with open(trace, 'a') as f:
        f.write(json.dumps({'event':event,'pid':os.getpid()})+'\n')

def emit(value):
    print(json.dumps(value), flush=True)

if '--version' in sys.argv:
    print('codex-cli 0.120.0')
    sys.exit(0)
record('process')
started = False
for line in sys.stdin:
    req = json.loads(line)
    method = req.get('method')
    params = req.get('params', {})
    rid = req.get('id')
    if method == 'initialized':
        continue
    result = {}
    if method == 'thread/start':
        result = {'thread':{'id':'thread-current'}}
    elif method == 'turn/start':
        assert not started, 'second turn'
        assert params['threadId'] == 'thread-current'
        started = True
        record('start')
    elif method == 'turn/steer':
        assert started
        assert params['threadId'] == 'thread-current'
        assert params['expectedTurnId'] == 'turn-current'
        assert marker in params['input'][0]['text']
        record('steer')
    elif method == 'turn/interrupt':
        raise AssertionError('unexpected interruption')
    if rid is not None:
        emit({'jsonrpc':'2.0','id':rid,'result':result})
    if method == 'turn/start':
        emit({'method':'turn/started','params':{'threadId':'thread-current','turn':{'id':'turn-current'}}})
        emit({'method':'item/completed','params':{'threadId':'thread-current','turnId':'turn-current','item':{'type':'agentMessage','id':'ready','phase':'commentary','text':'fixture ready'}}})
    elif method == 'turn/steer':
        emit({'method':'item/completed','params':{'threadId':'thread-current','turnId':'turn-current','item':{'type':'agentMessage','id':'answer','phase':'final_answer','text':marker}}})
        record('final')
        emit({'method':'turn/completed','params':{'threadId':'thread-current','turn':{'id':'turn-current','status':'completed'}}})
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return binary, trace
}
