package internalbrowserqa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os/exec"
	"slices"
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

// stageFailingCommander behaves normally until the named stage runs, then fails
// it with a fixed cause. It identifies a stage by the agent-browser verb the
// runner uses for it, so a stage cannot be faked into passing.
type stageFailingCommander struct {
	stage       string
	kind        commandFailureKind
	finalURL    string
	screenshots atomic.Int32
}

func stageVerb(args []string) string {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "find role link click"),
		strings.Contains(joined, "find text ") && strings.Contains(joined, " click --exact"):
		return "navigation"
	case strings.HasSuffix(joined, "batch"):
		return "auth"
	case strings.HasSuffix(joined, "reload"):
		return "reload"
	case strings.HasSuffix(joined, "snapshot"):
		return "snapshot"
	case strings.HasSuffix(joined, "get url"):
		return "url"
	case strings.HasSuffix(joined, "errors"):
		return "errors"
	case strings.Contains(joined, "open http"):
		return "open"
	}
	return ""
}

func (c *stageFailingCommander) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if stageVerb(args) == c.stage {
		return nil, commandFailure{kind: c.kind}
	}
	switch {
	case len(args) > 0 && args[len(args)-1] == "url":
		if c.finalURL != "" {
			return []byte(c.finalURL + "\n"), nil
		}
		return []byte("http://multica.internal:3000/firtal/issues\n"), nil
	case len(args) > 0 && args[len(args)-1] == "snapshot":
		return []byte("Dashboard\nData Sources\nYour roles:\nIssues\nAgents\nSettings\nDesk\nAnalytics\n"), nil
	case len(args) > 0 && args[len(args)-1] == "errors":
		return []byte("[]\n"), nil
	}
	return nil, nil
}

func (c *stageFailingCommander) CaptureScreenshot(_ context.Context, _ ...string) ([]byte, error) {
	c.screenshots.Add(1)
	return append(append([]byte(nil), pngSignature...), "failure-evidence"...), nil
}

// markerlessCommander loads a page successfully but without the expected text.
type markerlessCommander struct{}

func (markerlessCommander) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[len(args)-1] == "snapshot" {
		return []byte("Login\nPassword\n"), nil
	}
	return nil, nil
}

func (markerlessCommander) CaptureScreenshot(_ context.Context, _ ...string) ([]byte, error) {
	return append(append([]byte(nil), pngSignature...), "marker-failure"...), nil
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
		// The shared fake supports both the Registry detail-route assertion and
		// Multica's final-path assertion. App-specific failure tests override it.
		return []byte("https://registry.firtal.com/authentication/api-keys/key-1/issues\n"), nil
	case len(args) > 0 && args[len(args)-1] == "snapshot":
		return []byte("Dashboard\nAuthentication\nAPI Keys\nGenerate New Key\nYour Registry API URL\nActors\nSystems\nYour roles:\nIssues\nAgents\nSettings\nDesk\nAnalytics\nLogout\n"), nil
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

// testRunner is NewRunner with the DNS preflight stubbed to succeed. Production
// resolves the real internal host; a unit test must not depend on private DNS.
func testRunner(commander Commander) *Runner {
	runner := NewRunner(commander)
	runner.resolveHost = func(context.Context, string) error { return nil }
	runner.fetchVersion = func(context.Context, string) (string, error) {
		return "0123456789abcdef0123456789abcdef01234567", nil
	}
	return runner
}

func TestNewRunnerPreflightsDNSWithinFiveSeconds(t *testing.T) {
	runner := NewRunner(&recordingCommander{})
	if runner.resolveHost == nil {
		t.Fatal("production runner has no DNS preflight")
	}
	if runner.dnsTimeout != 5*time.Second {
		t.Fatalf("dns timeout = %s, want 5s", runner.dnsTimeout)
	}
	if runner.dnsTimeout >= runner.openTimeout {
		t.Fatalf("dns timeout %s must be well below open timeout %s", runner.dnsTimeout, runner.openTimeout)
	}
}

// An unresolvable host is the whole point of the preflight: it must come back as
// an address problem, fast, instead of being reported as a slow page.
func TestVerifyReportsUnresolvableHostAsDNSWithoutOpeningBrowser(t *testing.T) {
	commander := &recordingCommander{}
	runner := NewRunner(commander)
	runner.resolveHost = func(context.Context, string) error { return errors.New("no such host") }

	result, err := runner.Verify(context.Background(), "customer-service", Credential{})
	if err == nil || err.Error() != "internal browser stage dns failed" {
		t.Fatalf("error = %v, want internal browser stage dns failed", err)
	}
	if result.FailureStage != "dns" || result.FailureCause != "dns" {
		t.Fatalf("failure = %s/%s, want dns/dns", result.FailureStage, result.FailureCause)
	}
	if result.InternalHost != "customer-service.internal:3456" {
		t.Fatalf("internal host = %q, want the attempted host", result.InternalHost)
	}
	if result.FailureDetail != "http://customer-service.internal:3456/desk" {
		t.Fatalf("failure detail = %q, want the attempted URL", result.FailureDetail)
	}
	for _, call := range commander.calls {
		if len(call.args) > 0 && call.args[len(call.args)-1] != "close" {
			t.Fatalf("browser was driven despite an unresolvable host: %v", call.args)
		}
	}
}

func TestVerifyDistinguishesDNSTimeoutFromMissingName(t *testing.T) {
	runner := NewRunner(&recordingCommander{})
	runner.dnsTimeout = time.Millisecond
	runner.resolveHost = func(ctx context.Context, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	}

	result, err := runner.Verify(context.Background(), "customer-service", Credential{})
	if err == nil || err.Error() != "internal browser stage dns dns-timeout failed" {
		t.Fatalf("error = %v, want a dns-timeout stage error", err)
	}
	if result.FailureCause != "dns-timeout" {
		t.Fatalf("failure cause = %q, want dns-timeout", result.FailureCause)
	}
}

// Every stage — not just open — must name its cause, otherwise a failure late in
// the run is indistinguishable from any other failure late in the run.
func TestVerifyClassifiesFailureCauseOnEveryStage(t *testing.T) {
	for _, test := range []struct {
		stage string
		kind  commandFailureKind
		want  string
	}{
		{stage: "open", kind: commandFailureConnection, want: "internal browser stage open connection failed"},
		{stage: "reload", kind: commandFailureTimeout, want: "internal browser stage reload timeout failed"},
		{stage: "navigation", kind: commandFailureNotFound, want: "internal browser stage navigation not-found failed"},
	} {
		t.Run(test.stage, func(t *testing.T) {
			commander := &stageFailingCommander{stage: test.stage, kind: test.kind}
			if test.stage == "reload" {
				commander.finalURL = "about:blank"
			}
			runner := testRunner(commander)
			_, err := runner.Verify(context.Background(), "multica", Credential{SessionToken: "signed-session"})
			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if got := SafeError(err); got != test.want {
				t.Fatalf("SafeError = %q, want %q", got, test.want)
			}
		})
	}
}

// The screenshot is the evidence a human reads. A stage that failed on a page
// that exists must still hand one back.
func TestVerifyCapturesScreenshotOnRecoverableStageFailure(t *testing.T) {
	commander := &stageFailingCommander{stage: "navigation", kind: commandFailureNotFound}
	result, err := testRunner(commander).Verify(context.Background(), "multica", Credential{SessionToken: "signed-session"})
	if err == nil {
		t.Fatal("Verify succeeded, want failure")
	}
	if !bytes.HasPrefix(result.ScreenshotPNG, pngSignature) {
		t.Fatalf("failure screenshot = %q, want PNG evidence", result.ScreenshotPNG)
	}
	if result.FailureDetail != "exact text: Issues" {
		t.Fatalf("failure detail = %q, want the exact label that was looked for", result.FailureDetail)
	}
}

// A name that never resolved and a browser that never launched have no page, so
// the runner must not burn a stage timeout trying to photograph one.
func TestVerifySkipsScreenshotWhenNoPageCanExist(t *testing.T) {
	commander := &stageFailingCommander{stage: "open", kind: commandFailureBrowserLaunch}
	result, err := testRunner(commander).Verify(context.Background(), "customer-service", Credential{})
	if err == nil {
		t.Fatal("Verify succeeded, want failure")
	}
	if result.ScreenshotPNG != nil {
		t.Fatalf("screenshot = %q, want none for a browser that never launched", result.ScreenshotPNG)
	}
	if commander.screenshots.Load() != 0 {
		t.Fatalf("screenshot attempts = %d, want 0", commander.screenshots.Load())
	}
}

func TestVerifyReportsMissingMarkerAsNotFoundWithDetail(t *testing.T) {
	commander := &markerlessCommander{}
	result, err := testRunner(commander).Verify(context.Background(), "customer-service", Credential{})
	if err == nil || err.Error() != "internal browser stage markers not-found failed" {
		t.Fatalf("error = %v", err)
	}
	if result.FailureDetail != "missing marker: Desk" {
		t.Fatalf("failure detail = %q, want the missing marker", result.FailureDetail)
	}
	if !bytes.HasPrefix(result.ScreenshotPNG, pngSignature) {
		t.Fatal("marker failure returned no screenshot")
	}
}

// The failure result travels to an already-authorized caller, but it must still
// never carry a credential.
func TestFailureResultCarriesNoCredential(t *testing.T) {
	const password = "password-must-never-leak"
	commander := &stageFailingCommander{stage: "reload", kind: commandFailureTimeout}
	result, err := testRunner(commander).Verify(context.Background(), "registry", Credential{
		Username: "registry-test@example.com", Password: password,
	})
	if err == nil {
		t.Fatal("Verify succeeded, want failure")
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), password) || strings.Contains(string(encoded), "registry-test@example.com") {
		t.Fatalf("failure result leaked a credential: %s", encoded)
	}
}

func TestTargetForUsesOnlyInternalAllowlist(t *testing.T) {
	target, err := TargetFor("registry")
	if err != nil {
		t.Fatalf("TargetFor(registry): %v", err)
	}
	if target.URL != "https://registry.firtal.com/auth/login?manual=true" {
		t.Fatalf("registry URL = %q", target.URL)
	}
	if target.Host() != "registry.firtal.com" || !target.AccessHeaders {
		t.Fatalf("registry target = %q access=%v, want Access-gated public edge", target.Host(), target.AccessHeaders)
	}
	if target.NavigatePath != "/authentication/api-keys" {
		t.Fatalf("registry navigation = %q, want the direct api-keys route", target.NavigatePath)
	}
	if target.NavigateLinkName != "" || target.NavigateSelector != "" || target.NavigateTabName != "" {
		t.Fatalf("registry must not click through the scrollable sidebar: %q/%q/%q", target.NavigateLinkName, target.NavigateSelector, target.NavigateTabName)
	}
	if target.ExpectedURLPart != "/authentication/api-keys" {
		t.Fatalf("registry expected URL = %q", target.ExpectedURLPart)
	}
	wantMarkers := []string{"API Keys", "Generate New Key", "Your Registry API URL", "Actors", "Systems"}
	if strings.Join(target.ExpectedText, "|") != strings.Join(wantMarkers, "|") {
		t.Fatalf("registry markers = %v, want %v", target.ExpectedText, wantMarkers)
	}
	if _, err := TargetFor("https://registry.firtal.com"); err == nil {
		t.Fatal("arbitrary public URL was accepted as a target")
	}
}

// firtal-internal-private (the employee portal) and firtal-agents-private (the
// finance app) share a login, so aiming this target at the employee portal
// logged in and reported PASS while never once loading the finance app. Pin the
// host, and prove the AI CFO screen itself rather than a post-login landing page.
func TestFinanceTargetReachesTheAiCfoScreenOnTheFinanceApp(t *testing.T) {
	target, err := TargetFor("finance")
	if err != nil {
		t.Fatalf("TargetFor(finance): %v", err)
	}
	if target.Host() != "firtal-agents-private.internal:3000" {
		t.Fatalf("host = %q, want firtal-agents-private.internal:3000", target.Host())
	}
	if target.NavigatePath != "/cfo" {
		t.Fatalf("navigate path = %q, want /cfo", target.NavigatePath)
	}
	if target.NavigateLinkName != "" {
		t.Fatalf("navigate link = %q, want no dependency on the icon-only sidebar", target.NavigateLinkName)
	}
	if target.ExpectedPathSuffix != "/cfo" {
		t.Fatalf("expected path suffix = %q, want /cfo", target.ExpectedPathSuffix)
	}
	want := []string{"Monthly overview", "Controllership review", "Versus budget"}
	if strings.Join(target.ExpectedText, "|") != strings.Join(want, "|") {
		t.Fatalf("finance markers = %v, want %v", target.ExpectedText, want)
	}
	// "Your roles:" is the employee portal's marker. If it ever comes back here,
	// the target has drifted onto the wrong app again.
	for _, marker := range target.ExpectedText {
		if marker == "Your roles:" {
			t.Fatal("finance is matching the employee portal marker again")
		}
	}
}

type financeCommander struct {
	recordingCommander
}

func (c *financeCommander) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[len(args)-1] == "snapshot" {
		c.calls = append(c.calls, commandCall{args: append([]string(nil), args...), stdin: stdin})
		return []byte("Monthly overview\nControllership review\nVersus budget\n"), nil
	}
	if len(args) > 0 && args[len(args)-1] == "url" {
		c.calls = append(c.calls, commandCall{args: append([]string(nil), args...), stdin: stdin})
		return []byte("http://firtal-agents-private.internal:3000/cfo\n"), nil
	}
	return c.recordingCommander.Run(ctx, stdin, args...)
}

func TestRunnerNavigatesFinanceDirectlyToCfoBeforeSnapshot(t *testing.T) {
	commander := &financeCommander{}
	if _, err := testRunner(commander).Verify(context.Background(), "finance", Credential{
		Username: "finance@example.com",
		Password: "secret",
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	var navigateIndex, snapshotIndex = -1, -1
	for i, call := range commander.calls {
		joined := strings.Join(call.args, " ")
		if strings.Contains(joined, "open http://firtal-agents-private.internal:3000/cfo") {
			navigateIndex = i
		}
		if len(call.args) > 0 && call.args[len(call.args)-1] == "snapshot" {
			snapshotIndex = i
		}
	}
	if navigateIndex < 0 || snapshotIndex <= navigateIndex {
		t.Fatalf("navigate/snapshot order = %d/%d, want direct /cfo navigation before snapshot", navigateIndex, snapshotIndex)
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
	if !target.NavigateExactText {
		t.Fatal("multica navigation must not assume the current Issues row exposes role=link")
	}
	if target.VersionPath != "/version" {
		t.Fatalf("multica version path = %q, want /version", target.VersionPath)
	}
	if target.ExpectedPathSuffix != "/issues" {
		t.Fatalf("multica expected path suffix = %q, want /issues", target.ExpectedPathSuffix)
	}
}

func TestRunnerReturnsSafeMulticaVersionCommit(t *testing.T) {
	runner := testRunner(&recordingCommander{})
	var requestedURL string
	runner.fetchVersion = func(_ context.Context, versionURL string) (string, error) {
		requestedURL = versionURL
		return "abcdef0123456789abcdef0123456789abcdef01", nil
	}
	result, err := runner.Verify(context.Background(), "multica", Credential{SessionToken: "signed-session"})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if requestedURL != "http://multica.internal:3000/version" {
		t.Fatalf("version URL = %q, want same-origin /version", requestedURL)
	}
	if result.VersionCommit != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("version commit = %q", result.VersionCommit)
	}
}

func TestRunnerFailsClosedWhenMulticaVersionIsUnavailable(t *testing.T) {
	runner := testRunner(&stageFailingCommander{stage: "no-such-stage"})
	runner.fetchVersion = func(context.Context, string) (string, error) {
		return "", errors.New("must not escape")
	}
	result, err := runner.Verify(context.Background(), "multica", Credential{SessionToken: "signed-session"})
	if err == nil || err.Error() != "internal browser stage version failed" {
		t.Fatalf("error = %v, want version stage failure", err)
	}
	if result.FailureDetail != "version endpoint: /version" {
		t.Fatalf("failure detail = %q", result.FailureDetail)
	}
	if !bytes.HasPrefix(result.ScreenshotPNG, pngSignature) {
		t.Fatalf("version failure screenshot = %q, want UI evidence", result.ScreenshotPNG)
	}
}

func TestRunnerFailsClosedWhenMulticaVersionIsUnknown(t *testing.T) {
	runner := testRunner(&recordingCommander{})
	runner.fetchVersion = func(context.Context, string) (string, error) {
		return "unknown", nil
	}
	result, err := runner.Verify(context.Background(), "multica", Credential{SessionToken: "signed-session"})
	if err == nil || err.Error() != "internal browser stage version failed" {
		t.Fatalf("error = %v, want version stage failure", err)
	}
	if result.FailureDetail != "version endpoint: /version" {
		t.Fatalf("failure detail = %q", result.FailureDetail)
	}
}

func TestSafeVersionCommitRejectsUntrustedVersionText(t *testing.T) {
	for _, test := range []struct {
		commit string
		want   bool
	}{
		{commit: "unknown", want: false},
		{commit: "abcdef0", want: true},
		{commit: "0123456789abcdef0123456789abcdef01234567", want: true},
		{commit: "", want: false},
		{commit: "release/latest", want: false},
		{commit: "abcdef\\nsecret", want: false},
	} {
		if got := safeVersionCommit(test.commit); got != test.want {
			t.Fatalf("safeVersionCommit(%q) = %v, want %v", test.commit, got, test.want)
		}
	}
}

type wrongPathCommander struct {
	recordingCommander
}

func (c *wrongPathCommander) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[len(args)-1] == "url" {
		c.calls = append(c.calls, commandCall{args: append([]string(nil), args...), stdin: stdin})
		return []byte("http://multica.internal:3000/firtal/settings\n"), nil
	}
	return c.recordingCommander.Run(ctx, stdin, args...)
}

func TestRunnerFailsClosedWhenMulticaNavigationMissesIssues(t *testing.T) {
	commander := &wrongPathCommander{}
	result, err := testRunner(commander).Verify(context.Background(), "multica", Credential{SessionToken: "signed-session"})
	if err == nil || err.Error() != "internal browser stage url not-found failed" {
		t.Fatalf("error = %v, want url not-found stage failure", err)
	}
	if result.FailureDetail != "unexpected final path" {
		t.Fatalf("failure detail = %q", result.FailureDetail)
	}
	if !bytes.HasPrefix(result.ScreenshotPNG, pngSignature) {
		t.Fatalf("path failure screenshot = %q, want UI evidence", result.ScreenshotPNG)
	}
}

func TestRunnerNavigatesMulticaToIssuesBeforeSnapshot(t *testing.T) {
	commander := &recordingCommander{}
	if _, err := testRunner(commander).Verify(context.Background(), "multica", Credential{SessionToken: "signed-session"}); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	var navigateIndex, snapshotIndex = -1, -1
	for i, call := range commander.calls {
		joined := strings.Join(call.args, " ")
		if strings.Contains(joined, "find text Issues click --exact") {
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
	const accessID = "access-id-must-never-leak"
	const accessSecret = "access-secret-must-never-leak"
	commander := &recordingCommander{}
	runner := testRunner(commander)

	result, err := runner.Verify(context.Background(), "registry", Credential{
		Username: username, Password: password,
		AccessClientID: accessID, AccessClientSecret: accessSecret,
	})
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
		if strings.Contains(argv, username) || strings.Contains(argv, password) ||
			strings.Contains(argv, accessID) || strings.Contains(argv, accessSecret) {
			t.Fatalf("credential leaked into argv: %q", argv)
		}
		if strings.Contains(call.stdin, username) || strings.Contains(call.stdin, password) ||
			strings.Contains(call.stdin, accessID) || strings.Contains(call.stdin, accessSecret) {
			secretCallCount++
			if len(call.args) == 0 || call.args[len(call.args)-1] != "batch" {
				t.Fatalf("credential was sent outside batch stdin: args=%v", call.args)
			}
		}
	}
	if secretCallCount != 2 {
		t.Fatalf("secret-bearing stdin calls = %d, want 2", secretCallCount)
	}
}

func TestRunnerCapturesScreenshotAfterAuthenticatedMarkers(t *testing.T) {
	commander := &recordingCommander{}
	result, err := testRunner(commander).Verify(context.Background(), "registry", Credential{
		Username:           "registry-test@example.com",
		Password:           "password-must-never-leak",
		AccessClientID:     "access-id-must-never-leak",
		AccessClientSecret: "access-secret-must-never-leak",
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
	runner := testRunner(&recordingCommander{})
	_, err := runner.Verify(context.Background(), "customer-service", Credential{Username: "unexpected", Password: "unexpected"})
	if err == nil {
		t.Fatal("credential was accepted for a no-login target")
	}
}

func TestRunnerSendsSessionCookieOnlyThroughBatchStdin(t *testing.T) {
	const sessionToken = "signed-session-must-never-leak"
	commander := &recordingCommander{}
	runner := testRunner(commander)

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
	if strings.Contains(sessionBatch, `"open"`) {
		t.Fatalf("session batch drives navigation, want a separately recoverable open stage: %s", sessionBatch)
	}
	var openedRoot bool
	for _, call := range commander.calls {
		if strings.HasSuffix(strings.Join(call.args, " "), "open http://multica.internal:3000/") {
			openedRoot = true
		}
	}
	if !openedRoot {
		t.Fatal("Multica root was not opened after the session cookies were set")
	}
}

func TestRunnerWaitsForClientRenderAfterReload(t *testing.T) {
	commander := &recordingCommander{}
	if _, err := testRunner(commander).Verify(context.Background(), "customer-service", Credential{}); err != nil {
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
	_, err := testRunner(failingCommander{}).Verify(context.Background(), "customer-service", Credential{})
	if err == nil || err.Error() != "internal browser stage open failed" {
		t.Fatalf("error = %q", err)
	}
}

func TestRunnerReturnsSafeOpenFailureClass(t *testing.T) {
	_, err := testRunner(classifiedFailingCommander{kind: commandFailureDNS}).Verify(context.Background(), "customer-service", Credential{})
	if err == nil || err.Error() != "internal browser stage open dns failed" {
		t.Fatalf("error = %q", err)
	}
}

func TestRunnerObservesStageWithOnlySafeDiagnosticFields(t *testing.T) {
	runner := testRunner(classifiedFailingCommander{kind: commandFailureDNS})
	var got stageObservation
	runner.observeStage = func(observation stageObservation) {
		got = observation
	}

	_, err := runner.Verify(context.Background(), "customer-service", Credential{})
	if err == nil {
		t.Fatal("Verify succeeded, want failure")
	}
	if got.App != "customer-service" || got.Stage != "open" || got.TargetHost != "customer-service.internal:3456" || got.ExitClass != commandFailureDNS {
		t.Fatalf("observation = %#v", got)
	}
	if got.Duration <= 0 {
		t.Fatalf("duration = %s, want positive", got.Duration)
	}
}

func TestRunnerSerializesConcurrentVerifications(t *testing.T) {
	commander := &concurrentProbeCommander{}
	runner := testRunner(commander)
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

func TestAgentBrowserCommandEnvStaysBelowCLITransportTimeout(t *testing.T) {
	env := agentBrowserCommandEnv([]string{
		"PATH=/usr/bin",
		"AGENT_BROWSER_DEFAULT_TIMEOUT=60000",
	})

	var timeoutValues []string
	for _, entry := range env {
		if strings.HasPrefix(entry, "AGENT_BROWSER_DEFAULT_TIMEOUT=") {
			timeoutValues = append(timeoutValues, entry)
		}
	}
	want := "AGENT_BROWSER_DEFAULT_TIMEOUT=25000"
	if len(timeoutValues) != 1 || timeoutValues[0] != want {
		t.Fatalf("timeout env = %v, want [%s]", timeoutValues, want)
	}
	if runner := testRunner(&recordingCommander{}); runner.openTimeout <= agentBrowserDefaultTimeout {
		t.Fatalf("open timeout = %s, must exceed action timeout %s", runner.openTimeout, agentBrowserDefaultTimeout)
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

// coldStartCommander times out the first open, then behaves like a healthy app.
// This is the Finance shape: an idle container that wakes on the second try.
type coldStartCommander struct {
	stageFailingCommander
	opens atomic.Int32
}

func (c *coldStartCommander) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	if stageVerb(args) == openStage && c.opens.Add(1) == 1 {
		return nil, commandFailure{kind: commandFailureTimeout}
	}
	return c.stageFailingCommander.Run(ctx, stdin, args...)
}

func TestVerifyRetriesOpenOnceWhenTheAppIsSlowToWake(t *testing.T) {
	commander := &coldStartCommander{stageFailingCommander: stageFailingCommander{stage: "no-such-stage"}}
	result, err := testRunner(commander).Verify(context.Background(), "customer-service", Credential{})
	if err != nil {
		t.Fatalf("Verify failed after a cold start: %v", err)
	}
	if commander.opens.Load() != 2 {
		t.Fatalf("open attempts = %d, want 2 (one cold-start retry)", commander.opens.Load())
	}
	if len(result.Markers) == 0 {
		t.Fatal("markers empty, want the verified page markers")
	}
}

// A navigation can time out after the browser has already reached the app when
// the page keeps a request open. The expected host plus the later marker checks
// are enough to continue without spending a second 30-second open attempt.
type reachedTargetTimeoutCommander struct {
	stageFailingCommander
	stage     string
	targetURL string
	attempts  atomic.Int32
}

func (c *reachedTargetTimeoutCommander) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	if stageVerb(args) == c.stage {
		c.attempts.Add(1)
		return nil, commandFailure{kind: commandFailureTimeout}
	}
	if len(args) > 0 && args[len(args)-1] == "url" {
		return []byte(c.targetURL + "\n"), nil
	}
	return c.stageFailingCommander.Run(ctx, stdin, args...)
}

func TestVerifyContinuesWhenOpenTimesOutAfterReachingExpectedHost(t *testing.T) {
	commander := &reachedTargetTimeoutCommander{
		stageFailingCommander: stageFailingCommander{stage: "no-such-stage"},
		stage:                 openStage, targetURL: "http://customer-service.internal:3456/desk",
	}
	result, err := testRunner(commander).Verify(context.Background(), "customer-service", Credential{})
	if err != nil {
		t.Fatalf("Verify failed after reaching the expected host: %v", err)
	}
	if commander.attempts.Load() != 1 {
		t.Fatalf("open attempts = %d, want 1 after the URL probe proved the page arrived", commander.attempts.Load())
	}
	if len(result.Markers) == 0 {
		t.Fatal("markers empty, want the verified page markers")
	}
}

func TestVerifyContinuesWhenReloadTimesOutAfterReachingExpectedHost(t *testing.T) {
	commander := &reachedTargetTimeoutCommander{
		stageFailingCommander: stageFailingCommander{stage: "no-such-stage"},
		stage:                 "reload", targetURL: "http://customer-service.internal:3456/desk",
	}
	if _, err := testRunner(commander).Verify(context.Background(), "customer-service", Credential{}); err != nil {
		t.Fatalf("Verify failed after reload reached the expected host: %v", err)
	}
	if commander.attempts.Load() != 1 {
		t.Fatalf("reload attempts = %d, want 1", commander.attempts.Load())
	}
}

type redirectedTimeoutCommander struct {
	stageFailingCommander
	opens atomic.Int32
}

func (c *redirectedTimeoutCommander) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	if stageVerb(args) == openStage {
		c.opens.Add(1)
		return nil, commandFailure{kind: commandFailureTimeout}
	}
	if len(args) > 0 && args[len(args)-1] == "url" {
		return []byte("https://firtal.cloudflareaccess.com/cdn-cgi/access/login\n"), nil
	}
	return c.stageFailingCommander.Run(ctx, stdin, args...)
}

func TestVerifyDoesNotRecoverNavigationTimeoutOnAnUnexpectedHost(t *testing.T) {
	commander := &redirectedTimeoutCommander{
		stageFailingCommander: stageFailingCommander{stage: "no-such-stage"},
	}
	if _, err := testRunner(commander).Verify(context.Background(), "customer-service", Credential{}); err == nil {
		t.Fatal("Verify succeeded after a redirect to an unexpected host")
	}
	if commander.opens.Load() != 2 {
		t.Fatalf("open attempts = %d, want the bounded cold-start retry", commander.opens.Load())
	}
}

// Only a timeout earns the retry. A refused connection is a real failure and
// must be reported on the first attempt instead of doubling every run's cost.
func TestVerifyDoesNotRetryOpenOnNonTimeoutFailure(t *testing.T) {
	commander := &countingOpenFailureCommander{
		stageFailingCommander: stageFailingCommander{stage: "no-such-stage"}, kind: commandFailureConnection,
	}
	if _, err := testRunner(commander).Verify(context.Background(), "customer-service", Credential{}); err == nil {
		t.Fatal("Verify succeeded, want failure")
	}
	if commander.opens.Load() != 1 {
		t.Fatalf("open attempts = %d, want 1 (no retry for a refused connection)", commander.opens.Load())
	}
}

type countingOpenFailureCommander struct {
	stageFailingCommander
	kind  commandFailureKind
	opens atomic.Int32
}

func (c *countingOpenFailureCommander) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	if stageVerb(args) == openStage {
		c.opens.Add(1)
		return nil, commandFailure{kind: c.kind}
	}
	return c.stageFailingCommander.Run(ctx, stdin, args...)
}

// Firtal Shift lives under a /shift base path, so a login URL without that
// prefix 404s on the private host and the whole run fails at the auth stage.
func TestTargetForResolvesWarehouseUnderItsBasePath(t *testing.T) {
	target, err := TargetFor("warehouse")
	if err != nil {
		t.Fatalf("TargetFor(warehouse) failed: %v", err)
	}
	if target.Host() != "firtal-shift-private.internal:3000" {
		t.Fatalf("host = %q, want firtal-shift-private.internal:3000", target.Host())
	}
	if !strings.Contains(target.URL, "/shift/auth/login") {
		t.Fatalf("url = %q, want the /shift base path on the login route", target.URL)
	}
	if target.Vault != "Shared/browser-login/warehouse" {
		t.Fatalf("vault = %q, want Shared/browser-login/warehouse", target.Vault)
	}
	if target.NavigateLinkName != "Production planning" {
		t.Fatalf("navigate link = %q, want Production planning", target.NavigateLinkName)
	}
}

func TestTargetForResolvesRoleAwareDataCatalogChecks(t *testing.T) {
	tests := []struct {
		name         string
		host         string
		path         string
		access       bool
		idKey        string
		secretKey    string
		navigate     string
		navigatePath string
		wantMarkers  []string
	}{
		{
			name: "data-catalog", host: "atlas.firtal.com", path: "/",
			access: true, idKey: "ADMIN_CF_ACCESS_CLIENT_ID", secretKey: "ADMIN_CF_ACCESS_CLIENT_SECRET",
			navigatePath: "/permissions", wantMarkers: []string{"Permissions", "People & agents", "Role model"},
		},
		{
			name: "data-catalog-reader", host: "atlas.firtal.com", path: "/",
			access: true, idKey: "READER_CF_ACCESS_CLIENT_ID", secretKey: "READER_CF_ACCESS_CLIENT_SECRET",
			wantMarkers: []string{"Atlas Graph Health"},
		},
		{
			name: "data-catalog-reader-permissions", host: "atlas.firtal.com", path: "/permissions",
			access: true, idKey: "READER_CF_ACCESS_CLIENT_ID", secretKey: "READER_CF_ACCESS_CLIENT_SECRET",
			wantMarkers: []string{"No access"},
		},
		{
			name: "data-catalog-unknown", host: "data-catalog.internal:3000", path: "/",
			wantMarkers: []string{"No access", "All services healthy"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, err := TargetFor(test.name)
			if err != nil {
				t.Fatalf("TargetFor(%s) failed: %v", test.name, err)
			}
			parsed, _ := url.Parse(target.URL)
			if target.Host() != test.host || parsed.Path != test.path {
				t.Fatalf("target = %q%s, want %q%s", target.Host(), parsed.Path, test.host, test.path)
			}
			if target.AccessHeaders != test.access {
				t.Fatalf("AccessHeaders = %t, want %t", target.AccessHeaders, test.access)
			}
			if target.AccessClientIDKey != test.idKey || target.AccessClientSecretKey != test.secretKey {
				t.Fatalf("access keys = %q/%q, want %q/%q", target.AccessClientIDKey, target.AccessClientSecretKey, test.idKey, test.secretKey)
			}
			if target.NavigateLinkName != test.navigate {
				t.Fatalf("navigate = %q, want %q", target.NavigateLinkName, test.navigate)
			}
			if target.NavigatePath != test.navigatePath {
				t.Fatalf("navigate path = %q, want %q", target.NavigatePath, test.navigatePath)
			}
			if strings.Join(target.ExpectedText, "|") != strings.Join(test.wantMarkers, "|") {
				t.Fatalf("markers = %v, want %v", target.ExpectedText, test.wantMarkers)
			}
			if target.UsernameSelector != "" || target.PasswordSelector != "" {
				t.Fatal("data-catalog service-token targets must not request a form login")
			}
		})
	}
}

type dataCatalogAccessCommander struct {
	calls []commandCall
}

func (c *dataCatalogAccessCommander) Run(_ context.Context, stdin string, args ...string) ([]byte, error) {
	c.calls = append(c.calls, commandCall{args: append([]string(nil), args...), stdin: stdin})
	switch {
	case len(args) > 0 && args[len(args)-1] == "snapshot":
		return []byte("Atlas Graph Health\n"), nil
	case len(args) > 0 && args[len(args)-1] == "url":
		return []byte("https://atlas.firtal.com/\n"), nil
	case len(args) > 0 && args[len(args)-1] == "errors":
		return []byte("[]\n"), nil
	}
	return nil, nil
}

func (c *dataCatalogAccessCommander) CaptureScreenshot(_ context.Context, args ...string) ([]byte, error) {
	c.calls = append(c.calls, commandCall{args: append([]string(nil), args...)})
	return append([]byte(nil), pngSignature...), nil
}

func TestVerifyUsesAccessOnlyDataCatalogCredentialWithoutFormLogin(t *testing.T) {
	commander := &dataCatalogAccessCommander{}
	credential := Credential{AccessClientID: "reader.access", AccessClientSecret: "reader-secret"}

	if _, err := testRunner(commander).Verify(context.Background(), "data-catalog-reader", credential); err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	batchCalls := 0
	for _, call := range commander.calls {
		joined := strings.Join(call.args, " ")
		if strings.HasSuffix(joined, "batch") {
			batchCalls++
			if !strings.Contains(call.stdin, "reader.access") || !strings.Contains(call.stdin, "reader-secret") {
				t.Fatalf("access batch did not carry both service-token headers: %s", call.stdin)
			}
			// agent-browser only applies headers to the navigation via the
			// session-level `set headers` command; an `open ... --headers`
			// batch entry is silently ignored and the edge rejects the run
			// (FIR-3796). Pin the working shape.
			var commands [][]string
			if err := json.Unmarshal([]byte(call.stdin), &commands); err != nil {
				t.Fatalf("access batch stdin is not a command array: %v", err)
			}
			if len(commands) < 2 || commands[0][0] != "set" || commands[0][1] != "headers" {
				t.Fatalf("access batch must set session headers before navigating: %s", call.stdin)
			}
			for _, command := range commands {
				if command[0] == "open" && slices.Contains(command, "--headers") {
					t.Fatalf("open must not carry --headers inside batch (silently ignored): %s", call.stdin)
				}
			}
		}
		if strings.Contains(joined, "reader.access") || strings.Contains(joined, "reader-secret") {
			t.Fatalf("access credential leaked into argv: %s", joined)
		}
		if strings.Contains(call.stdin, "\"fill\"") {
			t.Fatalf("access-only target attempted a form login: %s", call.stdin)
		}
	}
	if batchCalls != 1 {
		t.Fatalf("batch calls = %d, want one access-header navigation", batchCalls)
	}
}

// dialogCommander reproduces the first-login survey: the navigation link is not
// clickable until the modal's Skip button has been pressed.
type dialogCommander struct {
	stageFailingCommander
	skipped  atomic.Bool
	navTries atomic.Int32
}

func (c *dialogCommander) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "role button click --name Skip") {
		c.skipped.Store(true)
		return nil, nil
	}
	if stageVerb(args) == "navigation" {
		c.navTries.Add(1)
		if !c.skipped.Load() {
			return nil, commandFailure{kind: commandFailureNotFound}
		}
		return nil, nil
	}
	return c.stageFailingCommander.Run(ctx, stdin, args...)
}

func TestVerifyDismissesTheFirstLoginDialogBeforeNavigating(t *testing.T) {
	commander := &dialogCommander{stageFailingCommander: stageFailingCommander{stage: "no-such-stage"}}
	if _, err := testRunner(commander).Verify(context.Background(), "multica", Credential{SessionToken: "signed-session"}); err != nil {
		t.Fatalf("Verify failed with the survey dialog up: %v", err)
	}
	if !commander.skipped.Load() {
		t.Fatal("the blocking dialog was never dismissed")
	}
	if commander.navTries.Load() != 1 {
		t.Fatalf("navigation attempts = %d, want 1 after the dialog was cleared", commander.navTries.Load())
	}
}

// No dialog is the normal case: a missing Skip button must not fail the run.
func TestVerifySucceedsWhenThereIsNoDialogToDismiss(t *testing.T) {
	commander := &stageFailingCommander{stage: "no-such-stage"}
	if _, err := testRunner(commander).Verify(context.Background(), "multica", Credential{SessionToken: "signed-session"}); err != nil {
		t.Fatalf("Verify failed without a dialog: %v", err)
	}
}

func TestTargetForAllowsTheCerebroPublicEdgeWithAccessHeaders(t *testing.T) {
	target, err := TargetFor("cerebro")
	if err != nil {
		t.Fatalf("TargetFor(cerebro) failed: %v", err)
	}
	if !target.AccessHeaders {
		t.Fatal("cerebro must carry Cloudflare Access headers: its internal address is on another server")
	}
	if target.Host() != "cerebro.firtal.com" {
		t.Fatalf("host = %q, want cerebro.firtal.com", target.Host())
	}
	if target.SessionCookie {
		t.Fatal("a production session token is worthless on staging; cerebro must use the code login")
	}
	if target.CodeSelector == "" || target.SubmitButtonName == "" {
		t.Fatal("cerebro needs both the code field and the submit button for the two-step login")
	}
	if !target.NavigateExactText {
		t.Fatal("cerebro navigation must not assume the current Issues row exposes role=link")
	}
}

func TestTargetForCerebroPermissionProfilesUsesTheFirtalSettingsRoute(t *testing.T) {
	target, err := TargetFor("cerebro-permission-profiles")
	if err != nil {
		t.Fatalf("TargetFor(cerebro-permission-profiles) failed: %v", err)
	}
	if target.NavigatePath != "/firtal/settings?tab=permissions" {
		t.Fatalf("navigate path = %q, want /firtal/settings?tab=permissions", target.NavigatePath)
	}
	if target.NavigateTabName != "Permission profiles" {
		t.Fatalf("navigate tab = %q, want Permission profiles", target.NavigateTabName)
	}
	if target.ExpectedPathSuffix != "/firtal/settings" {
		t.Fatalf("expected path suffix = %q, want /firtal/settings", target.ExpectedPathSuffix)
	}
	wantMarkers := []string{"Permission profiles", "When should I use a Permission profile?", "One agent", "Several agents or members"}
	if strings.Join(target.ExpectedText, "|") != strings.Join(wantMarkers, "|") {
		t.Fatalf("markers = %v, want %v", target.ExpectedText, wantMarkers)
	}
}

// Only a target that carries Access headers may leave the internal network. A
// public URL without them would put an unguarded host on the allowlist.
func TestTargetForRejectsAPublicURLWithoutAccessHeaders(t *testing.T) {
	original := targets["cerebro"]
	t.Cleanup(func() { targets["cerebro"] = original })
	stripped := original
	stripped.AccessHeaders = false
	targets["cerebro"] = stripped
	if _, err := TargetFor("cerebro"); err == nil {
		t.Fatal("TargetFor accepted a public URL with no Access headers")
	}
}

// codeLoginCommander records the batch payload so the test can prove the code
// login was driven, without the payload ever reaching an argument vector.
type codeLoginCommander struct {
	stageFailingCommander
	authStdin      string
	navigationURLs []string
}

func (c *codeLoginCommander) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	if strings.HasSuffix(strings.Join(args, " "), "batch") && strings.Contains(stdin, "fill") {
		c.authStdin = stdin
	}
	if len(args) >= 2 && args[len(args)-2] == "open" {
		c.navigationURLs = append(c.navigationURLs, args[len(args)-1])
	}
	return c.stageFailingCommander.Run(ctx, stdin, args...)
}

func TestVerifyDrivesTheCodeLoginAndKeepsSecretsOffTheCommandLine(t *testing.T) {
	commander := &codeLoginCommander{stageFailingCommander: stageFailingCommander{stage: "no-such-stage"}}
	credential := Credential{
		Username: "agent-testing@firtal.com", LoginCode: "the-staging-code",
		AccessClientID: "id.access", AccessClientSecret: "the-token-secret",
	}
	if _, err := testRunner(commander).Verify(context.Background(), "cerebro", credential); err != nil {
		t.Fatalf("Verify failed for the code login: %v", err)
	}
	for _, want := range []string{"agent-testing@firtal.com", "the-staging-code", "input[data-input-otp]", "Continue"} {
		if !strings.Contains(commander.authStdin, want) {
			t.Fatalf("auth payload missing %q: %s", want, commander.authStdin)
		}
	}
	if strings.Contains(commander.authStdin, "PASSWORD") {
		t.Fatal("a code-login target must never send a password")
	}
	if !slices.Contains(commander.navigationURLs, "https://cerebro.firtal.com/") {
		t.Fatal("cerebro verification did not return through the workspace root after login")
	}
}
