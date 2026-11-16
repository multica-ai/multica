//go:build !windows

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestZcodeHandshakeFailureClosesOwnedProcess(t *testing.T) {
	root := t.TempDir()
	pidfile := filepath.Join(root, "pid")
	script := "#!/usr/bin/python3\nimport os,time\nopen(" + fmt.Sprintf("%q", pidfile) + ",'w').write(str(os.getpid()))\ntime.sleep(60)\n"
	bin := filepath.Join(root, "zcode")
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		raw, _ := os.ReadFile(pidfile)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
		if pid > 0 {
			p, _ := os.FindProcess(pid)
			_ = p.Kill()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	session, err := newZcodeTestBackend(t, bin).Execute(ctx, "hello", ExecOptions{Cwd: root, HandshakeTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result:
		t.Logf("terminal status=%s error=%s", result.Status, result.Error)
	case <-time.After(time.Second):
		t.Fatal("no handshake terminal result")
	}
	select {
	case _, open := <-session.Messages:
		if open {
			t.Fatal("unexpected message")
		}
	case <-time.After(400 * time.Millisecond):
		t.Fatal("message channel stays open after terminal handshake failure: owned process was not stopped before Wait")
	}
}

func TestZcodeBlockedPromptWriteHonorsTimeout(t *testing.T) {
	root := t.TempDir()
	pidfile := filepath.Join(root, "pid")
	script := `#!/usr/bin/python3
import os,sys,json,time
open(` + fmt.Sprintf("%q", pidfile) + `,'w').write(str(os.getpid()))
for n in range(2):
 req=json.loads(sys.stdin.readline())
 result={'session':{'sessionId':'fake_blocked'}} if req['method']=='session/create' else {'eventSeq':0}
 print(json.dumps({'id':req['id'],'result':result}),flush=True)
time.sleep(60)
`
	bin := filepath.Join(root, "zcode")
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		raw, _ := os.ReadFile(pidfile)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
		if pid > 0 {
			p, _ := os.FindProcess(pid)
			_ = p.Kill()
		}
	})
	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), strings.Repeat("x", 1<<20), ExecOptions{Cwd: root, Timeout: 2 * time.Second, HandshakeTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result:
		if result.Status != "timeout" {
			t.Fatalf("status=%s error=%s", result.Status, result.Error)
		}
	case <-time.After(3500 * time.Millisecond):
		t.Fatal("hard timeout did not stop session/send blocked on stdin.Write")
	}
}

func TestZcodeResumedReasoningModelRetainsLevel(t *testing.T) {
	script := `#!/usr/bin/python3
import sys,json
for line in sys.stdin:
 req=json.loads(line); method=req.get('method'); params=req.get('params',{})
 if method=='session/resume':
  result={'session':{'sessionId':'saved'},'settings':{'model':{'current':{'providerId':'zai','modelId':'m','options':{'reasoningLevel':'max'}}}}}
 elif method=='session/subscribe':
  result={'eventSeq':0}
 elif method=='session/send':
  if not params.get('modelSelection',{}).get('options',{}).get('reasoningLevel'):
   print(json.dumps({'id':req['id'],'error':{'code':-32603,'message':'Reasoning level is required for zai/m'}}),flush=True)
   continue
  result={}
 else:
  result={}
 print(json.dumps({'id':req['id'],'result':result}),flush=True)
 if method=='session/send':
  print(json.dumps({'method':'session/event','params':{'type':'turn.completed','seq':1,'sessionId':'saved','payload':{'response':'ok','resultType':'success'}}}),flush=True)
`
	bin := writeFakeZcodeScript(t, script)
	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "continue", ExecOptions{Cwd: t.TempDir(), Model: "zai/m", ResumeSessionID: "saved", Timeout: 3 * time.Second, HandshakeTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, result := collectZcodeSession(t, session)
	if result.Status != "completed" {
		t.Fatalf("resume did not preserve existing reasoning level: status=%s error=%s", result.Status, result.Error)
	}
}
