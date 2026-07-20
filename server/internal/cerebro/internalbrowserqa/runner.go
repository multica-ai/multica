package internalbrowserqa

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Credential struct {
	Username     string
	Password     string
	SessionToken string
}

type Target struct {
	Name             string
	URL              string
	Vault            string
	UsernameSelector string
	PasswordSelector string
	SubmitSelector   string
	NavigateLinkName string
	ExpectedText     []string
	SessionCookie    bool
}

func (t Target) Host() string {
	parsed, err := url.Parse(t.URL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

var targets = map[string]Target{
	"multica": {
		Name: "multica", URL: "http://multica.internal:3000/login",
		NavigateLinkName: "Issues", ExpectedText: []string{"Issues", "Agents", "Settings"}, SessionCookie: true,
	},
	"cerebro": {
		Name: "cerebro", URL: "http://multica-staging-web.internal:3000/login",
		NavigateLinkName: "Issues", ExpectedText: []string{"Issues", "Agents", "Settings"}, SessionCookie: true,
	},
	"registry": {
		Name: "registry", URL: "http://firtal-data-registry-private.internal:3000/auth/login?manual=true",
		Vault: "Shared/browser-login/registry", UsernameSelector: "#email", PasswordSelector: "#password",
		SubmitSelector: "button[type=submit]", ExpectedText: []string{"Dashboard", "Data Sources"},
	},
	"finance": {
		Name: "finance", URL: "http://firtal-internal-private.internal:3000/login?manual=true",
		Vault: "Shared/browser-login/finance", UsernameSelector: "#email", PasswordSelector: "#password",
		SubmitSelector: "button[type=submit]", ExpectedText: []string{"Your roles:"},
	},
	"pricing": {
		Name: "pricing", URL: "http://ecommerce-pricing-engine-private.internal:3000/login?manual=true",
		Vault: "Shared/browser-login/pricing", UsernameSelector: "#email", PasswordSelector: "#password",
		SubmitSelector: "button[type=submit]", ExpectedText: []string{"Dashboard"},
	},
	"customer-service": {
		Name: "customer-service", URL: "http://customer-service.internal:3456/desk",
		ExpectedText: []string{"Desk", "Analytics"},
	},
}

func TargetFor(name string) (Target, error) {
	target, ok := targets[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Target{}, fmt.Errorf("internal browser target is not allowed")
	}
	parsed, err := url.Parse(target.URL)
	if err != nil || parsed.Scheme != "http" || !strings.Contains(parsed.Hostname(), ".internal") {
		return Target{}, fmt.Errorf("internal browser target is misconfigured")
	}
	return target, nil
}

type Commander interface {
	Run(ctx context.Context, stdin string, args ...string) ([]byte, error)
	CaptureScreenshot(ctx context.Context, args ...string) ([]byte, error)
}

type ExecCommander struct{}

// agent-browser's CLI gives its daemon 30 seconds to answer. Keep the action
// timeout below that transport ceiling so slow navigation returns a classified
// browser error instead of stranding the CLI until its IPC read times out.
const agentBrowserDefaultTimeout = 25 * time.Second

func agentBrowserCommandEnv(environ []string) []string {
	const timeoutKey = "AGENT_BROWSER_DEFAULT_TIMEOUT="
	env := make([]string, 0, len(environ)+1)
	for _, entry := range environ {
		if !strings.HasPrefix(entry, timeoutKey) {
			env = append(env, entry)
		}
	}
	return append(env, fmt.Sprintf("%s%d", timeoutKey, agentBrowserDefaultTimeout.Milliseconds()))
}

type commandFailureKind string

const (
	commandFailureUnknown       commandFailureKind = "unknown"
	commandFailureTimeout       commandFailureKind = "timeout"
	commandFailureDNS           commandFailureKind = "dns"
	commandFailureDNSTimeout    commandFailureKind = "dns-timeout"
	commandFailureConnection    commandFailureKind = "connection"
	commandFailureBrowserLaunch commandFailureKind = "browser-launch"
	commandFailureNotFound      commandFailureKind = "not-found"
)

type commandFailure struct {
	kind commandFailureKind
}

func (failure commandFailure) Error() string {
	return "agent-browser command failed"
}

// stageError names both the stage that failed and why it failed. Every caller-
// visible failure flows through it so a reader gets a concrete cause instead of
// a single opaque line, and so SafeError can allowlist the whole closed set.
type stageError struct {
	stage string
	kind  commandFailureKind
}

func (err stageError) Error() string {
	// The kind is dropped when it says nothing the stage has not already said:
	// unknown carries no cause, and "dns dns" is just noise.
	if err.kind == "" || err.kind == commandFailureUnknown || string(err.kind) == err.stage {
		return fmt.Sprintf("internal browser stage %s failed", err.stage)
	}
	return fmt.Sprintf("internal browser stage %s %s failed", err.stage, err.kind)
}

// hasPageToCapture reports whether a failed stage can still yield a meaningful
// screenshot. A name that does not resolve and a browser that never launched
// have no page behind them, so attempting a capture only burns a stage timeout.
func (err stageError) hasPageToCapture() bool {
	switch err.kind {
	case commandFailureDNS, commandFailureDNSTimeout, commandFailureBrowserLaunch:
		return false
	}
	return err.stage != dnsStage
}

func classifyCommandFailure(ctx context.Context, err error) commandFailureKind {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return commandFailureTimeout
	}

	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		return commandFailureUnknown
	}
	stderr := strings.ToLower(string(exitError.Stderr))
	switch {
	case strings.Contains(stderr, "err_name_not_resolved"),
		strings.Contains(stderr, "name does not resolve"),
		strings.Contains(stderr, "no such host"),
		strings.Contains(stderr, "getaddrinfo"):
		return commandFailureDNS
	// Element lookups are checked before the generic timeout patterns: a missing
	// control reports "timed out waiting for selector", which is a not-found, not
	// a slow page. Misreading it as a timeout is what made every failure look alike.
	case strings.Contains(stderr, "element not found"),
		strings.Contains(stderr, "no element matching"),
		strings.Contains(stderr, "no matching element"),
		strings.Contains(stderr, "waiting for selector"),
		strings.Contains(stderr, "selector resolved to no"):
		return commandFailureNotFound
	case strings.Contains(stderr, "timed out"), strings.Contains(stderr, "timeout"):
		return commandFailureTimeout
	case strings.Contains(stderr, "err_connection_refused"),
		strings.Contains(stderr, "connection refused"),
		strings.Contains(stderr, "err_connection_reset"),
		strings.Contains(stderr, "err_connection_closed"):
		return commandFailureConnection
	case strings.Contains(stderr, "failed to launch chrome"),
		strings.Contains(stderr, "chrome exited early"),
		strings.Contains(stderr, "browser process exited"):
		return commandFailureBrowserLaunch
	default:
		return commandFailureUnknown
	}
}

func safeCommandFailureKind(err error) commandFailureKind {
	var failure commandFailure
	if errors.As(err, &failure) {
		return failure.kind
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return commandFailureTimeout
	}
	return commandFailureUnknown
}

func (ExecCommander) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "agent-browser", args...)
	command.Env = agentBrowserCommandEnv(os.Environ())
	if stdin != "" {
		command.Stdin = strings.NewReader(stdin)
	}
	output, err := command.Output()
	if err != nil {
		return nil, commandFailure{kind: classifyCommandFailure(ctx, err)}
	}
	return output, nil
}

const maxScreenshotBytes = 10 << 20

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

func (commander ExecCommander) CaptureScreenshot(ctx context.Context, args ...string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "multica-internal-browser-")
	if err != nil {
		return nil, fmt.Errorf("create screenshot directory")
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "screenshot.png")
	if _, err := commander.Run(ctx, "", append(args, "screenshot", path)...); err != nil {
		return nil, err
	}
	screenshot, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read screenshot")
	}
	if len(screenshot) > maxScreenshotBytes || !bytes.HasPrefix(screenshot, pngSignature) {
		return nil, fmt.Errorf("invalid screenshot")
	}
	return screenshot, nil
}

type Result struct {
	App           string   `json:"app"`
	InternalHost  string   `json:"internal_host"`
	FinalURL      string   `json:"final_url"`
	Markers       []string `json:"markers"`
	Errors        []string `json:"errors"`
	ScreenshotPNG []byte   `json:"screenshot_png"`

	// Failure diagnostics. Empty on success. FailureDetail is sourced only from
	// this package's static target allowlist — never from browser output — so it
	// can name the attempted address or the missing marker without leaking page
	// content or a credential.
	FailureStage  string `json:"failure_stage,omitempty"`
	FailureCause  string `json:"failure_cause,omitempty"`
	FailureDetail string `json:"failure_detail,omitempty"`
}

// A wrong internal hostname makes the resolver hang far longer than it takes to
// learn the answer, and the browser then reports the wait as a plain timeout.
// Resolving the host ourselves first turns that into a fast, honest "the address
// does not exist" and keeps the browser's own timeouts for pages that do resolve.
const dnsPreflightTimeout = 5 * time.Second

const dnsStage = "dns"

func resolveInternalHost(ctx context.Context, host string) error {
	hostname := host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		hostname = parsedHost
	}
	addresses, err := net.DefaultResolver.LookupHost(ctx, hostname)
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		return fmt.Errorf("no addresses")
	}
	return nil
}

type Runner struct {
	commander      Commander
	openTimeout    time.Duration
	stageTimeout   time.Duration
	cleanupTimeout time.Duration
	dnsTimeout     time.Duration
	resolveHost    func(context.Context, string) error
	verificationMu sync.Mutex
	observeStage   func(stageObservation)
}

type stageObservation struct {
	App        string
	Stage      string
	Duration   time.Duration
	ExitClass  commandFailureKind
	TargetHost string
}

func NewRunner(commander Commander) *Runner {
	return &Runner{
		commander: commander, openTimeout: 75 * time.Second,
		stageTimeout: 30 * time.Second, cleanupTimeout: 30 * time.Second,
		dnsTimeout: dnsPreflightTimeout, resolveHost: resolveInternalHost,
		observeStage: func(observation stageObservation) {
			log.Printf("internal browser diagnostic app=%s stage=%s duration_ms=%d exit_class=%s target_host=%s",
				observation.App, observation.Stage, observation.Duration.Milliseconds(), observation.ExitClass, observation.TargetHost)
		},
	}
}

func (r *Runner) runStage(ctx context.Context, target Target, stage, stdin string, args ...string) ([]byte, error) {
	stageTimeout := r.stageTimeout
	if stage == "open" {
		stageTimeout = r.openTimeout
	}
	if stageTimeout <= 0 {
		if stage == "open" {
			stageTimeout = 60 * time.Second
		} else {
			stageTimeout = 30 * time.Second
		}
	}
	stageCtx, cancel := context.WithTimeout(ctx, stageTimeout)
	defer cancel()
	started := time.Now()
	output, err := r.commander.Run(stageCtx, stdin, args...)
	exitClass := commandFailureKind("success")
	if err != nil {
		exitClass = safeCommandFailureKind(err)
	}
	if r.observeStage != nil {
		r.observeStage(stageObservation{
			App: target.Name, Stage: stage, Duration: time.Since(started), ExitClass: exitClass, TargetHost: target.Host(),
		})
	}
	if err != nil {
		return nil, stageError{stage: stage, kind: safeCommandFailureKind(err)}
	}
	return output, nil
}

// preflightDNS resolves the target host before the browser ever launches, so an
// unreachable name costs dnsTimeout instead of the full open timeout and comes
// back labelled as an address problem rather than a generic slow page.
func (r *Runner) preflightDNS(ctx context.Context, target Target) error {
	if r.resolveHost == nil {
		return nil
	}
	timeout := r.dnsTimeout
	if timeout <= 0 {
		timeout = dnsPreflightTimeout
	}
	dnsCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()
	err := r.resolveHost(dnsCtx, target.Host())
	kind := commandFailureKind("success")
	if err != nil {
		kind = commandFailureDNS
		if dnsCtx.Err() != nil {
			kind = commandFailureDNSTimeout
		}
	}
	if r.observeStage != nil {
		r.observeStage(stageObservation{
			App: target.Name, Stage: dnsStage, Duration: time.Since(started), ExitClass: kind, TargetHost: target.Host(),
		})
	}
	if err != nil {
		return stageError{stage: dnsStage, kind: kind}
	}
	return nil
}

// failure builds the diagnostic result that accompanies a failed verification:
// the address that was tried, the stage and cause, and — when a page could exist
// — a screenshot of what the browser was actually looking at.
func (r *Runner) failure(target Target, baseArgs []string, detail string, err error) (Result, error) {
	result := Result{
		App: target.Name, InternalHost: target.Host(),
		Markers: []string{}, Errors: []string{}, FailureDetail: detail,
	}
	var stage stageError
	if !errors.As(err, &stage) {
		return result, err
	}
	result.FailureStage = stage.stage
	result.FailureCause = string(stage.kind)
	if baseArgs == nil || !stage.hasPageToCapture() {
		return result, err
	}
	timeout := r.stageTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	// Detached from the caller's context: the failure that brought us here is
	// often that context expiring, and the screenshot is the point of this path.
	shotCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if screenshot, shotErr := r.commander.CaptureScreenshot(shotCtx, baseArgs...); shotErr == nil {
		result.ScreenshotPNG = screenshot
	}
	return result, err
}

// stages and failure kinds are enumerated so SafeError stays a closed set that
// cannot echo an arbitrary error string back to the caller, while still covering
// every stage/cause pair automatically as new ones are added.
var safeStageErrors = buildSafeStageErrors()

func buildSafeStageErrors() map[string]struct{} {
	stages := []string{dnsStage, "open", "auth", "reload", "render", "navigation", "snapshot", "markers", "url", "errors", "screenshot"}
	kinds := []commandFailureKind{
		"", commandFailureUnknown, commandFailureTimeout, commandFailureDNS, commandFailureDNSTimeout,
		commandFailureConnection, commandFailureBrowserLaunch, commandFailureNotFound,
	}
	allowed := make(map[string]struct{}, len(stages)*len(kinds))
	for _, stage := range stages {
		for _, kind := range kinds {
			allowed[stageError{stage: stage, kind: kind}.Error()] = struct{}{}
		}
	}
	return allowed
}

func SafeError(err error) string {
	message := err.Error()
	if _, ok := safeStageErrors[message]; ok {
		return message
	}
	return "internal browser verification failed"
}

func (r *Runner) Verify(ctx context.Context, app string, credential Credential) (Result, error) {
	target, err := TargetFor(app)
	if err != nil {
		return Result{}, err
	}
	hasPassword := credential.Username != "" || credential.Password != ""
	if target.Vault == "" && hasPassword {
		return Result{}, fmt.Errorf("target does not accept a browser credential")
	}
	if target.Vault != "" && (!hasPassword || credential.Username == "" || credential.Password == "") {
		return Result{}, fmt.Errorf("target requires a complete browser credential")
	}
	if target.SessionCookie != (credential.SessionToken != "") {
		return Result{}, fmt.Errorf("target session credential does not match its auth mode")
	}

	// agent-browser launches a process per session, but concurrent first opens
	// inside one verifier container can race and leave one launch waiting forever.
	// Keep the dedicated runner single-flight so every session gets a clean start.
	r.verificationMu.Lock()
	defer r.verificationMu.Unlock()

	if err := r.preflightDNS(ctx, target); err != nil {
		return r.failure(target, nil, target.URL, err)
	}

	session, err := sessionName()
	if err != nil {
		return Result{}, err
	}
	baseArgs := []string{"--session", session}
	defer func() {
		cleanupTimeout := r.cleanupTimeout
		if cleanupTimeout <= 0 {
			cleanupTimeout = 30 * time.Second
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		_, _ = r.commander.Run(cleanupCtx, "", append(baseArgs, "close")...)
	}()

	if _, err := r.runStage(ctx, target, "open", "", append(baseArgs, "open", target.URL)...); err != nil {
		return r.failure(target, baseArgs, target.URL, err)
	}
	if target.Vault != "" || target.SessionCookie {
		commands := make([][]string, 0, 4)
		if target.Vault != "" {
			commands = append(commands,
				[]string{"fill", target.UsernameSelector, credential.Username},
				[]string{"fill", target.PasswordSelector, credential.Password},
				[]string{"click", target.SubmitSelector},
			)
		}
		if target.SessionCookie {
			parsed, _ := url.Parse(target.URL)
			rootURL := parsed.Scheme + "://" + parsed.Host + "/"
			commands = append(commands,
				[]string{"cookies", "set", "multica_auth", credential.SessionToken, "--url", rootURL, "--httpOnly", "--sameSite", "Strict"},
				[]string{"cookies", "set", "multica_logged_in", "1", "--url", rootURL, "--sameSite", "Lax"},
				[]string{"open", rootURL},
			)
		}
		commands = append(commands, []string{"wait", "1500"})
		payload, _ := json.Marshal(commands)
		// This output is deliberately discarded: the stdin payload contains secrets.
		if _, err := r.runStage(ctx, target, "auth", string(payload), append(baseArgs, "batch")...); err != nil {
			return r.failure(target, baseArgs, target.URL, err)
		}
	}
	if _, err := r.runStage(ctx, target, "reload", "", append(baseArgs, "reload")...); err != nil {
		return r.failure(target, baseArgs, target.URL, err)
	}
	if _, err := r.runStage(ctx, target, "render", "", append(baseArgs, "wait", "2500")...); err != nil {
		return r.failure(target, baseArgs, target.URL, err)
	}
	if target.NavigateLinkName != "" {
		if _, err := r.runStage(ctx, target, "navigation", "", append(baseArgs, "find", "role", "link", "click", "--name", target.NavigateLinkName)...); err != nil {
			return r.failure(target, baseArgs, "link: "+target.NavigateLinkName, err)
		}
		if _, err := r.runStage(ctx, target, "render", "", append(baseArgs, "wait", "2500")...); err != nil {
			return r.failure(target, baseArgs, target.URL, err)
		}
	}
	snapshot, err := r.runStage(ctx, target, "snapshot", "", append(baseArgs, "snapshot")...)
	if err != nil {
		return r.failure(target, baseArgs, target.URL, err)
	}
	for _, marker := range target.ExpectedText {
		if !strings.Contains(string(snapshot), marker) {
			return r.failure(target, baseArgs, "missing marker: "+marker,
				stageError{stage: "markers", kind: commandFailureNotFound})
		}
	}
	finalURL, err := r.runStage(ctx, target, "url", "", append(baseArgs, "get", "url")...)
	if err != nil {
		return r.failure(target, baseArgs, target.URL, err)
	}
	rawErrors, err := r.runStage(ctx, target, "errors", "", append(baseArgs, "errors")...)
	if err != nil {
		return r.failure(target, baseArgs, target.URL, err)
	}
	screenshotCtx, cancel := context.WithTimeout(ctx, r.stageTimeout)
	defer cancel()
	screenshot, err := r.commander.CaptureScreenshot(screenshotCtx, baseArgs...)
	if err != nil {
		return r.failure(target, nil, target.URL, stageError{stage: "screenshot", kind: safeCommandFailureKind(err)})
	}
	errors := decodeErrors(rawErrors)
	return Result{
		App: target.Name, InternalHost: target.Host(), FinalURL: strings.TrimSpace(string(finalURL)),
		Markers: append([]string(nil), target.ExpectedText...), Errors: errors, ScreenshotPNG: screenshot,
	}, nil
}

func sessionName() (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("create browser session")
	}
	return "internal-qa-" + hex.EncodeToString(suffix[:]), nil
}

func decodeErrors(raw []byte) []string {
	var errors []string
	if json.Unmarshal(raw, &errors) == nil {
		return errors
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "[]" {
		return []string{}
	}
	return []string{"browser reported errors"}
}
