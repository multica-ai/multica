package internalbrowserqa

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
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

type classifiedFailingCommander struct {
	kind commandFailureKind
}

type concurrentProbeCommander struct {
	active    atomic.Int32
	maxActive atomic.Int32
}

func (failingCommander) Run(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return nil, errors.New("must not escape")
}

func (blockingCommander) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c classifiedFailingCommander) Run(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return nil, commandFailure{kind: c.kind}
}

func (c *recordingCommander) Run(_ context.Context, stdin string, args ...string) ([]byte, error) {
	c.calls = append(c.calls, commandCall{args: append([]string(nil), args...), stdin: stdin})
	switch {
	case len(args) > 0 && args[len(args)-1] == "url":
		return []byte("http://firtal-data-registry-private.internal:3000/\n"), nil
	case len(args) > 0 && args[len(args)-1] == "snapshot":
		return []byte("Dashboard\nData Sources\nYour roles:\nIssues\nAgents\nSettings\nDesk\nAnalytics\nLogout\n"), nil
	case len(args) > 0 && args[len(args)-1] == "errors":
		return []byte("[]\n"), nil
	default:
		return nil, nil
	}
}

func (c *recordingCommander) CaptureScreenshot(_ context.Context, args ...string) ([]byte, error) {
	args = append(append([]string(nil), args...), "capture-screenshot")
	c.calls = append(c.calls, commandCall{args: args})
	return []byte("\x89PNG\r\n\x1a\nverified-registry-dashboard"), nil
}

func (failingCommander) CaptureScreenshot(_ context.Context, _ ...string) ([]byte, error) {
	return nil, errors.New("must not escape")
}

func (blockingCommander) CaptureScreenshot(ctx context.Context, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (classifiedFailingCommander) CaptureScreenshot(_ context.Context, _ ...string) ([]byte, error) {
	return nil, errors.New("unused")
}

func (c *concurrentProbeCommander) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	active := c.active.Add(1)
	for max := c.maxActive.Load(); active > max && !c.maxActive.CompareAndSwap(max, active); max = c.maxActive.Load() {
	}
	defer c.active.Add(-1)
	time.Sleep(5 * time.Millisecond)

	switch {
	case len(args) > 0 && args[len(args)-1] == "snapshot":
		return []byte("Desk\nAnalytics\n"), nil
	case len(args) > 0 && args[len(args)-1] == "url":
		return []byte("http://customer-service.internal:3456/desk\n"), nil
	case len(args) > 0 && args[len(args)-1] == "errors":
		return []byte("[]\n"), nil
	default:
		return nil, nil
	}
}

func (c *concurrentProbeCommander) CaptureScreenshot(_ context.Context, _ ...string) ([]byte, error) {
	return append([]byte(nil), pngSignature...), nil
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

func TestMulticaTargetUsesFullProductionNavigationMarkers(t *testing.T) {
	target, err := TargetFor("multica")
	if err != nil {
		t.Fatalf("TargetFor(multica): %v", err)
	}
	want := []string{"Issues", "Agents", "Settings"}
	if strings.Join(target.ExpectedText, "|") != strings.Join(want, "|") {
		t.Fatalf("multica markers = %v, want %v", target.ExpectedText, want)
	}
}

func TestRunnerNavigatesMulticaToIssuesBeforeSnapshot(t *testing.T) {
	commander := &recordingCommander{}
	if _, err := NewRunner(commander).Verify(context.Background(), "multica", Credential{SessionToken: "signed-session"}); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	var navigateIndex, snapshotIndex = -1, -1
	for i, call := range commander.calls {
		joined := strings.Join(call.args, " ")
		if strings.Contains(joined, "find role link click --name Issues") {
			navigateIndex = i
		}
		if len(call.args) > 0 && call.args[len(call.args)-1] == "snapshot" {
			snapshotIndex = i
		}
	}
	if navigateIndex < 0 || snapshotIndex <= navigateIndex {
		t.Fatalf("navigate/snapshot order = %d/%d, want Issues navigation before snapshot", navigateIndex, snapshotIndex)
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
	if !strings.HasPrefix(string(result.ScreenshotPNG), "\x89PNG\r\n\x1a\n") {
		t.Fatalf("result screenshot is not PNG data: %q", result.ScreenshotPNG)
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

func TestRunnerCapturesScreenshotAfterAuthenticatedMarkers(t *testing.T) {
	commander := &recordingCommander{}
	result, err := NewRunner(commander).Verify(context.Background(), "registry", Credential{
		Username: "registry-test@example.com",
		Password: "password-must-never-leak",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !strings.HasPrefix(string(result.ScreenshotPNG), "\x89PNG\r\n\x1a\n") {
		t.Fatalf("screenshot = %q, want PNG", result.ScreenshotPNG)
	}

	var snapshotIndex, screenshotIndex = -1, -1
	for i, call := range commander.calls {
		if len(call.args) == 0 {
			continue
		}
		switch call.args[len(call.args)-1] {
		case "snapshot":
			snapshotIndex = i
		case "capture-screenshot":
			screenshotIndex = i
		}
	}
	if snapshotIndex < 0 || screenshotIndex <= snapshotIndex {
		t.Fatalf("snapshot/screenshot order = %d/%d, want screenshot after verified markers", snapshotIndex, screenshotIndex)
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

func TestRunnerReturnsSafeOpenFailureClass(t *testing.T) {
	_, err := NewRunner(classifiedFailingCommander{kind: commandFailureDNS}).Verify(context.Background(), "customer-service", Credential{})
	if err == nil || err.Error() != "internal browser stage open dns failed" {
		t.Fatalf("error = %q", err)
	}
}

func TestRunnerSerializesConcurrentVerifications(t *testing.T) {
	commander := &concurrentProbeCommander{}
	runner := NewRunner(commander)
	start := make(chan struct{})
	errors := make(chan error, 4)
	var group sync.WaitGroup

	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := runner.Verify(context.Background(), "customer-service", Credential{})
			errors <- err
		}()
	}
	close(start)
	group.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}
	if got := commander.maxActive.Load(); got != 1 {
		t.Fatalf("concurrent browser commands = %d, want 1", got)
	}
}

func TestClassifyCommandFailure(t *testing.T) {
	tests := []struct {
		name   string
		ctx    context.Context
		stderr string
		want   commandFailureKind
	}{
		{name: "dns", ctx: context.Background(), stderr: "Navigation failed: net::ERR_NAME_NOT_RESOLVED", want: commandFailureDNS},
		{name: "connection", ctx: context.Background(), stderr: "Navigation failed: net::ERR_CONNECTION_REFUSED", want: commandFailureConnection},
		{name: "browser launch", ctx: context.Background(), stderr: "Failed to launch Chrome: Chrome exited early", want: commandFailureBrowserLaunch},
		{name: "unknown", ctx: context.Background(), stderr: "password=must-not-escape", want: commandFailureUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := &exec.ExitError{Stderr: []byte(test.stderr)}
			if got := classifyCommandFailure(test.ctx, err); got != test.want {
				t.Fatalf("classifyCommandFailure() = %q, want %q", got, test.want)
			}
		})
	}

	timedOut, cancel := context.WithCancel(context.Background())
	cancel()
	if got := classifyCommandFailure(timedOut, context.Canceled); got != commandFailureTimeout {
		t.Fatalf("cancelled classification = %q, want %q", got, commandFailureTimeout)
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
	if err == nil || err.Error() != "internal browser stage open timeout failed" {
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

func TestSafeErrorAllowsScreenshotStageWithoutDetails(t *testing.T) {
	err := errors.New("internal browser stage screenshot failed")
	if got := SafeError(err); got != "internal browser stage screenshot failed" {
		t.Fatalf("safe error = %q", got)
	}
}

func TestSafeErrorAllowsOnlyKnownOpenFailureClasses(t *testing.T) {
	for _, message := range []string{
		"internal browser stage open dns failed",
		"internal browser stage open connection failed",
		"internal browser stage open browser-launch failed",
		"internal browser stage open timeout failed",
	} {
		if got := SafeError(errors.New(message)); got != message {
			t.Fatalf("safe error = %q, want %q", got, message)
		}
	}
	if got := SafeError(errors.New("internal browser stage open password=must-not-escape failed")); got != "internal browser verification failed" {
		t.Fatalf("unexpected open detail escaped: %q", got)
	}
}
