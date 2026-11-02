//go:build !windows

package agent

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A Hermes launch that reinstalls dependencies before serving ACP gets a Warn
// in the daemon log carrying the task's HERMES_HOME and the measured delay,
// and the task itself still completes normally (GH #9390).
func TestHermesExecuteLogsDependencyReinstall(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(`#!/bin/sh
printf '%s\n' 'hermes: completing source-update dependencies...' >&2
sleep 0.3
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_dep"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_dep","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`))

	var logs syncBuffer
	backend, err := New("hermes", Config{
		ExecutablePath: fakePath,
		Env:            map[string]string{"HERMES_HOME": "/work/task/hermes-home"},
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("reinstall notice must not change the outcome; status=%q error=%q", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	recs := decodeDepSyncLogs(t, bytes.NewBufferString(logs.String()))
	if len(recs) != 2 {
		t.Fatalf("want start+finish reinstall logs, got %d: %s", len(recs), bytes.NewBufferString(logs.String()).String())
	}
	if recs[0].Level != "WARN" || recs[0].HermesHome != "/work/task/hermes-home" ||
		!strings.HasPrefix(recs[0].HermesOutput, "hermes: completing source-update dependencies") {
		t.Errorf("unexpected start record: %+v", recs[0])
	}
	if recs[1].HandshakeOK == nil || !*recs[1].HandshakeOK || recs[1].StartupDelayMs < 200 {
		t.Errorf("finish record should report a successful handshake after the simulated sync: %+v", recs[1])
	}
}

// Hold the stderr forwarder until session/new proves initialize has returned.
// This forces an allowed ordering between the independent pipe readers.
type depSyncDelayedStderrHandler struct {
	slog.Handler
	ready chan struct{}
	once  sync.Once
}

func (h *depSyncDelayedStderrHandler) Handle(ctx context.Context, r slog.Record) error {
	if strings.HasPrefix(r.Message, "[hermes:stderr] ") {
		select {
		case <-h.ready:
		case <-time.After(10 * time.Second):
		}
	}
	if r.Message == "hermes session created" {
		h.once.Do(func() { close(h.ready) })
	}
	return h.Handler.Handle(ctx, r)
}

// Regression for the PR #9434 review: Hermes writes the reinstall marker to
// stderr before serving ACP, but the stderr copier may process it only after
// initialize has returned. The finish log must still be emitted.
func TestHermesExecuteLogsDependencyReinstallWhenStderrLags(t *testing.T) {
	t.Parallel()
	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(`#!/bin/sh
printf '%s\n' 'hermes: completing source-update dependencies...' >&2
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_delayed"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_delayed","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`))
	var logs syncBuffer
	handler := &depSyncDelayedStderrHandler{
		Handler: slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}),
		ready:   make(chan struct{}),
	}
	backend, err := New("hermes", Config{ExecutablePath: fakePath, Logger: slog.New(handler)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "done" {
		t.Fatalf("unexpected result: %+v", result)
	}
	recs := decodeDepSyncLogs(t, bytes.NewBufferString(logs.String()))
	if len(recs) != 2 {
		t.Fatalf("want start+finish reinstall logs even when stderr lags the handshake, got %d; logs:\n%s", len(recs), logs.String())
	}
	if recs[1].HandshakeOK == nil || !*recs[1].HandshakeOK {
		t.Errorf("finish record should report the successful handshake: %+v", recs[1])
	}
}
