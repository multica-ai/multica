package internalbrowserqa

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type commandCall struct {
	args  []string
	stdin string
}

type recordingCommander struct {
	calls []commandCall
}

type failingCommander struct{}

type blockingCommander struct{}

func (failingCommander) Run(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return nil, errors.New("must not escape")
}

func (blockingCommander) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *recordingCommander) Run(_ context.Context, stdin string, args ...string) ([]byte, error) {
	c.calls = append(c.calls, commandCall{args: append([]string(nil), args...), stdin: stdin})
	switch {
	case len(args) > 0 && args[len(args)-1] == "url":
		return []byte("http://firtal-data-registry-private.internal:3000/\n"), nil
	case len(args) > 0 && args[len(args)-1] == "snapshot":
		return []byte("Dashboard\nData Sources\nYour roles:\nIssues\nAgents\nDesk\nAnalytics\nLogout\n"), nil
	case len(args) > 0 && args[len(args)-1] == "errors":
		return []byte("[]\n"), nil
	default:
		return nil, nil
	}
}

func TestTargetForUsesOnlyInternalAllowlist(t *testing.T) {
	target, err := TargetFor("registry")
	if err != nil {
		t.Fatalf("TargetFor(registry): %v", err)
	}
	if target.URL != "http://firtal-data-registry-private.internal:3000/auth/login?manual=true" {
		t.Fatalf("registry URL = %q", target.URL)
	}
	if !strings.HasSuffix(target.Host(), ".internal:3000") {
		t.Fatalf("registry host = %q, want internal host", target.Host())
	}
	if _, err := TargetFor("https://registry.firtal.com"); err == nil {
		t.Fatal("arbitrary public URL was accepted as a target")
	}
}

func TestFinanceTargetUsesAuthenticatedDashboardMarker(t *testing.T) {
	target, err := TargetFor("finance")
	if err != nil {
		t.Fatalf("TargetFor(finance): %v", err)
	}
	if len(target.ExpectedText) != 1 || target.ExpectedText[0] != "Your roles:" {
		t.Fatalf("finance markers = %v, want Your roles:", target.ExpectedText)
	}
}

func TestRunnerSendsCredentialsOnlyThroughBatchStdin(t *testing.T) {
	const username = "registry-test@example.com"
	const password = "password-must-never-leak"
	commander := &recordingCommander{}
	runner := NewRunner(commander)

	result, err := runner.Verify(context.Background(), "registry", Credential{Username: username, Password: password})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), username) || strings.Contains(string(encoded), password) {
		t.Fatalf("result leaked a credential: %s", encoded)
	}

	var secretCallCount int
	for _, call := range commander.calls {
		argv := strings.Join(call.args, " ")
		if strings.Contains(argv, username) || strings.Contains(argv, password) {
			t.Fatalf("credential leaked into argv: %q", argv)
		}
		if strings.Contains(call.stdin, username) || strings.Contains(call.stdin, password) {
			secretCallCount++
			if len(call.args) == 0 || call.args[len(call.args)-1] != "batch" {
				t.Fatalf("credential was sent outside batch stdin: args=%v", call.args)
			}
		}
	}
	if secretCallCount != 1 {
		t.Fatalf("secret-bearing stdin calls = %d, want 1", secretCallCount)
	}
}

func TestRunnerRejectsCredentialForNoLoginTarget(t *testing.T) {
	runner := NewRunner(&recordingCommander{})
	_, err := runner.Verify(context.Background(), "customer-service", Credential{Username: "unexpected", Password: "unexpected"})
	if err == nil {
		t.Fatal("credential was accepted for a no-login target")
	}
}

func TestRunnerSendsSessionCookieOnlyThroughBatchStdin(t *testing.T) {
	const sessionToken = "signed-session-must-never-leak"
	commander := &recordingCommander{}
	runner := NewRunner(commander)

	result, err := runner.Verify(context.Background(), "multica", Credential{SessionToken: sessionToken})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), sessionToken) {
		t.Fatalf("result leaked session token: %s", encoded)
	}
	var stdinCount int
	var sessionBatch string
	for _, call := range commander.calls {
		if strings.Contains(strings.Join(call.args, " "), sessionToken) {
			t.Fatalf("session token leaked into argv: %v", call.args)
		}
		if strings.Contains(call.stdin, sessionToken) {
			stdinCount++
			sessionBatch = call.stdin
			if call.args[len(call.args)-1] != "batch" {
				t.Fatalf("session token sent outside batch stdin: %v", call.args)
			}
		}
	}
	if stdinCount != 1 {
		t.Fatalf("secret-bearing stdin calls = %d, want 1", stdinCount)
	}
	if !strings.Contains(sessionBatch, `"multica_auth"`) || !strings.Contains(sessionBatch, `"multica_logged_in"`) {
		t.Fatalf("session batch does not set both required cookies: %s", sessionBatch)
	}
}

func TestRunnerWaitsForClientRenderAfterReload(t *testing.T) {
	commander := &recordingCommander{}
	if _, err := NewRunner(commander).Verify(context.Background(), "customer-service", Credential{}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	var reloadIndex, waitIndex, snapshotIndex = -1, -1, -1
	for i, call := range commander.calls {
		last := call.args[len(call.args)-1]
		switch last {
		case "reload":
			reloadIndex = i
		case "2500":
			waitIndex = i
		case "snapshot":
			snapshotIndex = i
		}
	}
	if !(reloadIndex >= 0 && reloadIndex < waitIndex && waitIndex < snapshotIndex) {
		t.Fatalf("reload/wait/snapshot order = %d/%d/%d", reloadIndex, waitIndex, snapshotIndex)
	}
}

func TestRunnerReturnsOnlySafeFailureStage(t *testing.T) {
	_, err := NewRunner(failingCommander{}).Verify(context.Background(), "customer-service", Credential{})
	if err == nil || err.Error() != "internal browser stage open failed" {
		t.Fatalf("error = %q", err)
	}
}

func TestRunnerBoundsStagesAndCleanup(t *testing.T) {
	runner := &Runner{
		commander:      blockingCommander{},
		openTimeout:    20 * time.Millisecond,
		stageTimeout:   5 * time.Millisecond,
		cleanupTimeout: 5 * time.Millisecond,
	}
	started := time.Now()
	_, err := runner.Verify(context.Background(), "customer-service", Credential{})
	if err == nil || err.Error() != "internal browser stage open failed" {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond || elapsed > 100*time.Millisecond {
		t.Fatalf("bounded runner took %s", elapsed)
	}
}

func TestSafeErrorRejectsUnexpectedDetails(t *testing.T) {
	if got := SafeError(errors.New("password=must-not-escape")); got != "internal browser verification failed" {
		t.Fatalf("safe error = %q", got)
	}
}
