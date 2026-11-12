//go:build agentintegration

package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBBRealTaskEnvironment(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to run a real BB agent")
	}
	executable := os.Getenv("MULTICA_BB_PATH")
	if executable == "" {
		t.Skip("set MULTICA_BB_PATH to the BB CLI with the companion plugin installed")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	nonce := hex.EncodeToString(random[:])
	expected := sha256.Sum256([]byte(nonce))
	backend, err := New("bb", Config{ExecutablePath: executable, Env: map[string]string{
		"MULTICA_BB_SMOKE_NONCE": nonce,
	}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(),
		"Use the terminal tool to compute SHA-256 of the environment variable MULTICA_BB_SMOKE_NONCE with Python hashlib, and read BB_THREAD_ID. Return only the digest and thread ID. Do not print the environment variable itself. Do not modify files.",
		ExecOptions{Cwd: t.TempDir(), ThreadName: "Multica native BB environment smoke", Timeout: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("BB execution: %s: %s", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, hex.EncodeToString(expected[:])) || !strings.Contains(result.Output, result.SessionID) {
		t.Fatalf("BB did not return the independently computed digest and session ID: %s", result.Output)
	}
	t.Logf("BB session=%s; independent environment digest matched; output=%s", result.SessionID, result.Output)
}
