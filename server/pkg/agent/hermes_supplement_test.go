package agent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeHermesSteerACPScript mirrors Hermes Agent 0.22.5's gateway ACP. The
// primary session/prompt reports a tool call and stays open; a concurrent
// `/steer` session/prompt is handled per HERMES_STEER_MODE:
//   - "" (live turn): acknowledge with the "Steer queued" chunk, answer the
//     command, then finish the primary turn with the steered answer.
//   - "noack": answer the command without an acknowledgement.
//   - "late": the primary turn ends first; the command waits like a stray
//     prompt until session/cancel answers it as cancelled.
//
// HERMES_ADVERTISE_STEER publishes the command after session/new. Without it,
// the primary turn ends on its own after a short delay.
func fakeHermesSteerACPScript() string {
	return `#!/bin/sh
prompt_id=""
steer_id=""
while IFS= read -r line; do
  if [ -n "$HERMES_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$HERMES_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_new"}}\n' "$id"
      if [ -n "$HERMES_ADVERTISE_STEER" ]; then
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"queue"},{"name":"steer"}]}}}\n'
      fi
      ;;
    *'"method":"session/cancel"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"cancelled"}}\n' "$steer_id"
      ;;
    *'/steer '*)
      steer_id="$id"
      case "$HERMES_STEER_MODE" in
        noack)
          printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$steer_id"
          ;;
        late)
          printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}\n'
          printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$prompt_id"
          ;;
        *)
          printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"⏩ Steer queued for the active turn: The word is PINEAPPLE.\\n"}}}}\n'
          printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$steer_id"
          printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"PINEAPPLE"}}}}\n'
          printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":7,"outputTokens":3}}}\n' "$prompt_id"
          ;;
      esac
      ;;
    *'"method":"session/prompt"'*)
      prompt_id="$id"
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"tool_call","toolCallId":"call_1","title":"terminal: sleep 5","kind":"execute","status":"pending"}}}\n'
      if [ -z "$HERMES_ADVERTISE_STEER" ]; then
        sleep 0.5
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"plain"}}}}\n'
        printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$prompt_id"
      fi
      ;;
  esac
done
`
}

func startHermesSteerSession(t *testing.T, env map[string]string, negotiated bool) (*Session, context.Context) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeHermesSteerACPScript()))
	backend, err := New("hermes", Config{ExecutablePath: fakePath, Env: env, Logger: slog.Default()})
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

func waitHermesSupplementReady(t *testing.T, session *Session) {
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

func drainHermesMessages(session *Session) <-chan []Message {
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

func TestHermesSupplementSteersActiveTurn(t *testing.T) {
	t.Parallel()
	requestsPath := filepath.Join(t.TempDir(), "requests.jsonl")
	session, ctx := startHermesSteerSession(t, map[string]string{
		"HERMES_ADVERTISE_STEER": "1",
		"HERMES_REQUESTS_FILE":   requestsPath,
	}, true)
	if session.Supplement == nil || session.SupplementReady == nil {
		t.Fatal("negotiated Hermes session did not expose supplement callbacks")
	}
	waitHermesSupplementReady(t, session)
	messages := drainHermesMessages(session)

	if err := session.Supplement(ctx, "The word is PINEAPPLE.\nKeep going."); err != nil {
		t.Fatalf("steer: %v", err)
	}
	result := <-session.Result
	transcript := <-messages
	if result.Status != "completed" || result.Output != "PINEAPPLE" {
		t.Fatalf("result status=%q output=%q error=%q, want the steered answer in the same turn", result.Status, result.Output, result.Error)
	}
	// The command's own response must not stand in for the primary turn's.
	if usage := result.Usage["unknown"]; usage.InputTokens != 7 || usage.OutputTokens != 3 {
		t.Fatalf("usage=%+v, want the primary prompt's", result.Usage)
	}
	for _, msg := range transcript {
		if strings.Contains(msg.Content, "Steer queued") {
			t.Fatalf("acknowledgement leaked into the transcript: %+v", msg)
		}
	}
	if session.SupplementReady() {
		t.Fatal("supplement stayed ready after session/prompt completed")
	}
	if err := session.Supplement(ctx, "too late"); !errors.Is(err, ErrSupplementTurnEnded) {
		t.Fatalf("supplement after the turn ended: err=%v, want ErrSupplementTurnEnded", err)
	}
	requests, err := os.ReadFile(requestsPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"text":"/steer The word is PINEAPPLE.\nKeep going."`; !strings.Contains(string(requests), want) {
		t.Errorf("ACP requests missing %s:\n%s", want, requests)
	}
}

func TestHermesSupplementRequiresAcknowledgement(t *testing.T) {
	t.Parallel()
	session, ctx := startHermesSteerSession(t, map[string]string{
		"HERMES_ADVERTISE_STEER": "1",
		"HERMES_STEER_MODE":      "noack",
	}, true)
	waitHermesSupplementReady(t, session)
	err := session.Supplement(ctx, "hello")
	if err == nil {
		t.Fatal("unacknowledged steer must not be reported as delivered")
	}
	if errors.Is(err, ErrSupplementTurnEnded) {
		t.Fatalf("err=%v: the turn is still running, so this is a rejection", err)
	}
	// The turn is still live; t.Cleanup cancels ctx and reaps the fake.
}

func TestHermesSupplementCancelsSteerThatMissedTheTurn(t *testing.T) {
	t.Parallel()
	requestsPath := filepath.Join(t.TempDir(), "requests.jsonl")
	session, ctx := startHermesSteerSession(t, map[string]string{
		"HERMES_ADVERTISE_STEER": "1",
		"HERMES_STEER_MODE":      "late",
		"HERMES_REQUESTS_FILE":   requestsPath,
	}, true)
	waitHermesSupplementReady(t, session)
	messages := drainHermesMessages(session)

	if err := session.Supplement(ctx, "hello"); !errors.Is(err, ErrSupplementTurnEnded) {
		t.Fatalf("err=%v, want ErrSupplementTurnEnded for a steer that missed the turn", err)
	}
	result := <-session.Result
	<-messages
	if result.Status != "completed" || result.Output != "done" {
		t.Fatalf("result=%+v, want the primary turn's own completion", result)
	}
	requests, err := os.ReadFile(requestsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(requests), `"method":"session/cancel"`) {
		t.Errorf("a steer that missed the turn must be cancelled so Hermes cannot run it as a new prompt:\n%s", requests)
	}
}

func TestHermesSupplementWaitsForAdvertisedCommand(t *testing.T) {
	t.Parallel()
	session, _ := startHermesSteerSession(t, nil, true)
	messages := drainHermesMessages(session)
	done := make(chan Result, 1)
	go func() { done <- <-session.Result }()
	for {
		select {
		case result := <-done:
			<-messages
			if result.Status != "completed" || result.Output != "plain" {
				t.Fatalf("result=%+v, want a normal completed turn", result)
			}
			return
		default:
			if session.SupplementReady() {
				t.Fatal("supplement became ready for a session that never advertised /steer")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestHermesSupplementNotExposedWithoutNegotiation(t *testing.T) {
	t.Parallel()
	session, _ := startHermesSteerSession(t, nil, false)
	if session.Supplement != nil || session.SupplementReady != nil {
		t.Fatal("un-negotiated Hermes session must not expose supplement callbacks")
	}
	messages := drainHermesMessages(session)
	result := <-session.Result
	<-messages
	if result.Status != "completed" || result.Output != "plain" {
		t.Fatalf("result=%+v, want a normal completed turn", result)
	}
}

// Hermes publishes available_commands_update and usage_update after attach,
// independently of the turn, so only agent output or a tool call opens steering.
func TestHermesSupplementStartsOnlyOnTurnActivity(t *testing.T) {
	t.Parallel()
	s := newHermesSupplementSession()
	s.steerAdvertised.Store(true)
	s.open("ses_new")
	for _, kind := range []string{"usage_update", "available_commands_update", "turn_end", ""} {
		s.acceptUpdate(kind, []byte(`{}`))
		if s.ready() {
			t.Fatalf("%q opened steering before the turn produced output", kind)
		}
	}
	s.acceptUpdate("agent_thought_chunk", []byte(`{"content":{"type":"text","text":"thinking"}}`))
	if !s.ready() {
		t.Fatal("agent output for the primary prompt must open steering")
	}
}
