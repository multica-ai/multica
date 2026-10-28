package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeKiroSteerACPScript holds session/prompt open until a `_session/steer`
// request arrives, mirroring kiro-cli 2.33.0: the steer is acknowledged with
// {"queued":true}, then the same prompt answers it and ends the turn.
// KIRO_STEER_RESULT overrides the acknowledgement payload; KIRO_STEER_ERROR
// makes the extension fail with a JSON-RPC error instead.
func fakeKiroSteerACPScript() string {
	return `#!/bin/sh
prompt_id=""
while IFS= read -r line; do
  if [ -n "$KIRO_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$KIRO_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_new"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      prompt_id="$id"
      if [ -z "$KIRO_WAIT_FOR_STEER" ]; then
        printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_new","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"plain"}}}}\n'
        printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$prompt_id"
        exit 0
      fi
      ;;
    *'"method":"_session/steer"'*)
      if [ -n "$KIRO_STEER_ERROR" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":"no active turn"}}\n' "$id"
        continue
      fi
      steer_result='{"queued":true}'
      if [ -n "$KIRO_STEER_RESULT" ]; then
        steer_result="$KIRO_STEER_RESULT"
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":%s}\n' "$id" "$steer_result"
      case "$steer_result" in
        *'"queued":true'*) ;;
        *) continue ;;
      esac
      printf '{"jsonrpc":"2.0","method":"_kiro.dev/session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"AgentExecutionSteeringInjected","messageId":"steer-1"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_new","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"PINEAPPLE"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$prompt_id"
      exit 0
      ;;
  esac
done
`
}

func startKiroSteerSession(t *testing.T, env map[string]string, negotiated bool) (*Session, context.Context) {
	t.Helper()
	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroSteerACPScript()))
	backend, err := New("kiro", Config{ExecutablePath: fakePath, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	session, err := backend.Execute(ctx, "do the task", ExecOptions{Timeout: 20 * time.Second, EnableTaskSupplement: negotiated})
	if err != nil {
		t.Fatal(err)
	}
	return session, ctx
}

func waitKiroSupplementReady(t *testing.T, session *Session) {
	t.Helper()
	// Generous: the fake shell is slow to start when the package runs in parallel.
	deadline := time.Now().Add(15 * time.Second)
	for !session.SupplementReady() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !session.SupplementReady() {
		t.Fatal("supplement never became ready during the active prompt")
	}
}

func drainKiroMessages(session *Session) <-chan []Message {
	out := make(chan []Message, 1)
	go func() {
		var messages []Message
		for msg := range session.Messages {
			messages = append(messages, msg)
		}
		out <- messages
	}()
	return out
}

func TestKiroSupplementSteersActivePrompt(t *testing.T) {
	t.Parallel()
	requestsPath := filepath.Join(t.TempDir(), "requests.jsonl")
	session, ctx := startKiroSteerSession(t, map[string]string{
		"KIRO_WAIT_FOR_STEER": "1",
		"KIRO_REQUESTS_FILE":  requestsPath,
	}, true)
	if session.Supplement == nil || session.SupplementReady == nil {
		t.Fatal("negotiated Kiro session did not expose supplement callbacks")
	}
	waitKiroSupplementReady(t, session)
	messages := drainKiroMessages(session)

	if err := session.Supplement(ctx, "The word is PINEAPPLE."); err != nil {
		t.Fatalf("steer: %v", err)
	}
	result := <-session.Result
	<-messages
	if result.Status != "completed" {
		t.Fatalf("turn status=%q error=%q", result.Status, result.Error)
	}
	if result.Output != "PINEAPPLE" {
		t.Fatalf("turn output=%q, want the steered answer in the same turn", result.Output)
	}
	if session.SupplementReady() {
		t.Fatal("supplement stayed ready after session/prompt completed")
	}
	if err := session.Supplement(ctx, "too late"); err == nil {
		t.Fatal("supplement after the turn ended must fail, not report delivery")
	}
	requests, err := os.ReadFile(requestsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"method":"_session/steer"`, `"sessionId":"ses_new"`, `"message":"The word is PINEAPPLE."`} {
		if !strings.Contains(string(requests), want) {
			t.Errorf("ACP requests missing %s:\n%s", want, requests)
		}
	}
}

func TestKiroSupplementRequiresQueuedAcknowledgement(t *testing.T) {
	t.Parallel()
	for name, env := range map[string]map[string]string{
		"not queued": {"KIRO_STEER_RESULT": `{"queued":false}`},
		"rpc error":  {"KIRO_STEER_ERROR": "1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env["KIRO_WAIT_FOR_STEER"] = "1"
			session, ctx := startKiroSteerSession(t, env, true)
			waitKiroSupplementReady(t, session)
			if err := session.Supplement(ctx, "hello"); err == nil {
				t.Fatal("unacknowledged steer must not be reported as delivered")
			}
			// The turn is still live; t.Cleanup cancels ctx and reaps the fake.
		})
	}
}

func TestKiroSupplementNotExposedWithoutNegotiation(t *testing.T) {
	t.Parallel()
	session, _ := startKiroSteerSession(t, nil, false)
	if session.Supplement != nil || session.SupplementReady != nil {
		t.Fatal("un-negotiated Kiro session must not expose supplement callbacks")
	}
	messages := drainKiroMessages(session)
	result := <-session.Result
	<-messages
	if result.Status != "completed" || result.Output != "plain" {
		t.Fatalf("result=%+v, want a normal completed turn", result)
	}
}
