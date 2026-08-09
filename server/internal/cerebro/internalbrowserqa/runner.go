package internalbrowserqa

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
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
	// LoginCode is the staging master code (MULTICA_DEV_MASTER_CODE) that
	// replaces the emailed one-time code on the staging deployment.
	LoginCode string
	// AccessClientID/AccessClientSecret are the Cloudflare Access service-token
	// headers that let the verifier through the public edge for a target that
	// has no reachable internal address.
	AccessClientID     string
	AccessClientSecret string
}

type Target struct {
	Name             string
	URL              string
	Vault            string
	UsernameSelector string
	PasswordSelector string
	SubmitSelector   string
	NavigateLinkName string
	// NavigatePath opens a fixed same-origin route after authentication. Use it
	// when the app's navigation has no stable accessible label, such as an
	// icon-only sidebar.
	NavigatePath     string
	NavigateSelector string
	NavigateTabName  string
	ExpectedURLPart  string
	// ExpectedPathSuffix proves that navigation reached the intended in-app
	// route instead of accepting global sidebar markers from another page.
	ExpectedPathSuffix string
	// VersionPath is a same-origin, public endpoint whose response contains the
	// deployed commit. It is configured only for Multica so a successful UI
	// verification also proves exactly which production build served it.
	VersionPath string
	// NavigateExactText clicks the exact visible label without assuming the
	// control's ARIA role. Multica's current sidebar can render the active
	// workspace row without exposing role=link, while the label remains the
	// stable user-facing contract.
	NavigateExactText bool
	ExpectedText      []string
	SessionCookie     bool
	// SubmitButtonName clicks a button by its accessible name instead of a CSS
	// selector, for a form whose submit control carries no stable selector.
	SubmitButtonName string
	// CodeSelector turns the login into the two-step code flow: submit the
	// email, then type LoginCode into this field.
	CodeSelector string
	// AccessHeaders sends the Cloudflare Access service-token headers with the
	// first navigation. Only a target on the public edge needs this, and it is
	// the one case where the URL is allowed to be public HTTPS.
	AccessHeaders         bool
	AccessClientIDKey     string
	AccessClientSecretKey string
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
		NavigateLinkName: "Issues", NavigateExactText: true,
		ExpectedPathSuffix: "/issues", VersionPath: "/version",
		ExpectedText: []string{"Issues", "Agents", "Settings"}, SessionCookie: true,
	},
	// FIR-4480: production QA of Settings → Permissions. The "multica" target
	// stops at the issue list, so a production permission change could only be
	// approved on staging evidence. Same internal host and server-minted session
	// as "multica" — the internal route already reaches the production container,
	// so this needs no Cloudflare Access token and no browser-login vault.
	"multica-permissions": {
		Name: "multica-permissions", URL: "http://multica.internal:3000/login",
		SessionCookie: true, VersionPath: "/version",
		// The workspace slug is part of the settings route, and production has
		// exactly one: firtal-tech. Same hardcoding as the staging permission
		// target's /firtal.
		NavigatePath: "/firtal-tech/settings?tab=permissions", ExpectedPathSuffix: "/firtal-tech/settings",
		ExpectedText: []string{"Permissions", "Access rules", "Permission profiles", "Security controls", "How access is decided"},
	},
	// Cerebro staging runs on its own Sliplane server, so the verifier cannot
	// resolve its internal address. It is reached over the public edge with a
	// Cloudflare Access service token instead, and signs in with the staging
	// master code because a production session token means nothing there.
	"cerebro": {
		Name: "cerebro", URL: "https://cerebro.firtal.com/login",
		Vault: "Shared/browser-login/cerebro", AccessHeaders: true,
		AccessClientIDKey: "CF_ACCESS_CLIENT_ID", AccessClientSecretKey: "CF_ACCESS_CLIENT_SECRET",
		UsernameSelector: "#login-email", SubmitButtonName: "Continue",
		CodeSelector: "input[data-input-otp]",
		// Login can preserve /workspaces/new from an earlier session. Opening
		// the root is the browser-equivalent of that page's Back action and
		// lets the workspace router select the user's existing workspace.
		NavigatePath: "/", NavigateLinkName: "Issues", NavigateExactText: true,
		ExpectedText: []string{"Issues", "Agents", "Settings"},
	},
	"cerebro-permission-profiles": {
		Name: "cerebro-permission-profiles", URL: "https://cerebro.firtal.com/login",
		Vault: "Shared/browser-login/cerebro", AccessHeaders: true,
		AccessClientIDKey: "CF_ACCESS_CLIENT_ID", AccessClientSecretKey: "CF_ACCESS_CLIENT_SECRET",
		UsernameSelector: "#login-email", SubmitButtonName: "Continue",
		CodeSelector: "input[data-input-otp]",
		NavigatePath: "/firtal/settings?tab=permissions", NavigateTabName: "Permission profiles",
		ExpectedPathSuffix: "/firtal/settings",
		ExpectedText: []string{"Permission profiles", "When should I use a Permission profile?", "One agent", "Several agents or members"},
	},
	"registry": {
		// Registry and the verifier run on different Sliplane servers. Internal
		// DNS is server-scoped, so use the Cloudflare Access-gated public edge
		// with the app-specific service token instead of an unreachable
		// .internal hostname.
		Name: "registry", URL: "https://registry.firtal.com/auth/login?manual=true",
		Vault: "Shared/browser-login/registry", AccessHeaders: true,
		AccessClientIDKey: "CF_ACCESS_CLIENT_ID", AccessClientSecretKey: "CF_ACCESS_CLIENT_SECRET",
		UsernameSelector: "#email", PasswordSelector: "#password",
		SubmitSelector: "button[type=submit]", NavigatePath: "/authentication/api-keys",
		NavigateSelector: "tbody tr", NavigateTabName: "Permissions",
		ExpectedURLPart: "/authentication/api-keys/", VersionPath: "/api/health",
		ExpectedText: []string{"Data Sources", "API Endpoints", "AI Models", "Apps", "API Access", "Save"},
	},
	// Finance is firtal-agents-private, not firtal-internal-private — those are
	// two different apps that share a login, so pointing at the wrong one logged
	// in cleanly and "passed" against the employee portal instead. The AI CFO
	// screen is what this target exists to prove, so the run navigates there and
	// matches its starter prompts rather than stopping at the landing page.
	"finance": {
		Name: "finance", URL: "http://firtal-agents-private.internal:3000/auth/login?manual=true",
		Vault: "Shared/browser-login/finance", UsernameSelector: "#email", PasswordSelector: "#password",
		SubmitSelector: "button[type=submit]", NavigatePath: "/cfo", ExpectedPathSuffix: "/cfo",
		ExpectedText: []string{"Monthly overview", "Controllership review", "Versus budget"},
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
	// Firtal Shift is served under the /shift base path, so every in-app route —
	// including its login page — carries that prefix on the private host too.
	// Production planning is the board this target exists to prove, so the run
	// navigates there instead of stopping at the post-login landing page.
	"warehouse": {
		Name: "warehouse", URL: "http://firtal-shift-private.internal:3000/shift/auth/login",
		Vault: "Shared/browser-login/warehouse", UsernameSelector: "#email", PasswordSelector: "#password",
		SubmitSelector: "button[type=submit]", NavigateLinkName: "Production planning",
		ExpectedText: []string{"Production planning", "Job assignment"},
	},
	// Atlas role checks use two dedicated Cloudflare Access service tokens on the
	// public edge. The application validates the resulting Access assertion and
	// maps each token to exactly one built-in role. The unknown check deliberately
	// uses the private host without credentials to prove the default-deny path.
	"data-catalog": {
		Name: "data-catalog", URL: "https://atlas.firtal.com/",
		Vault: "Shared/browser-login/data-catalog", AccessHeaders: true,
		AccessClientIDKey: "ADMIN_CF_ACCESS_CLIENT_ID", AccessClientSecretKey: "ADMIN_CF_ACCESS_CLIENT_SECRET",
		// The Identities section sits below the fold of the scrollable sidebar,
		// and agent-browser's find-by-role only matches on-screen elements, so
		// the run opens /permissions directly — the same route the reader token
		// must be denied on, which keeps the admin/reader contrast exact.
		NavigatePath: "/permissions", ExpectedPathSuffix: "/permissions",
		ExpectedText: []string{"Permissions", "People & agents", "Role model"},
	},
	"data-catalog-reader": {
		Name: "data-catalog-reader", URL: "https://atlas.firtal.com/",
		Vault: "Shared/browser-login/data-catalog", AccessHeaders: true,
		AccessClientIDKey: "READER_CF_ACCESS_CLIENT_ID", AccessClientSecretKey: "READER_CF_ACCESS_CLIENT_SECRET",
		ExpectedText: []string{"Atlas Graph Health"},
	},
	"data-catalog-reader-permissions": {
		Name: "data-catalog-reader-permissions", URL: "https://atlas.firtal.com/permissions",
		Vault: "Shared/browser-login/data-catalog", AccessHeaders: true,
		AccessClientIDKey: "READER_CF_ACCESS_CLIENT_ID", AccessClientSecretKey: "READER_CF_ACCESS_CLIENT_SECRET",
		ExpectedText: []string{"No access"},
	},
	"data-catalog-unknown": {
		Name: "data-catalog-unknown", URL: "http://data-catalog.internal:3000/",
		ExpectedText: []string{"No access", "All services healthy"},
	},
}

func TargetFor(name string) (Target, error) {
	target, ok := targets[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Target{}, fmt.Errorf("internal browser target is not allowed")
	}
	parsed, err := url.Parse(target.URL)
	if err != nil {
		return Target{}, fmt.Errorf("internal browser target is misconfigured")
	}
	// A target is either a private internal address over http, or — when it has
	// no internal address the verifier can resolve — the public edge over HTTPS
	// behind a Cloudflare Access service token. Nothing else is allowed.
	internal := parsed.Scheme == "http" && strings.Contains(parsed.Hostname(), ".internal")
	publicEdge := target.AccessHeaders && parsed.Scheme == "https" && strings.HasSuffix(parsed.Hostname(), ".firtal.com")
	if !internal && !publicEdge {
		return Target{}, fmt.Errorf("internal browser target is misconfigured")
	}
	if target.AccessHeaders && (target.Vault == "" || target.AccessClientIDKey == "" || target.AccessClientSecretKey == "") {
		return Target{}, fmt.Errorf("internal browser target is misconfigured")
	}
	if target.VersionPath != "" && (!strings.HasPrefix(target.VersionPath, "/") || strings.Contains(target.VersionPath, "://")) {
		return Target{}, fmt.Errorf("internal browser target is misconfigured")
	}
	if target.NavigatePath != "" && (!strings.HasPrefix(target.NavigatePath, "/") || strings.Contains(target.NavigatePath, "://")) {
		return Target{}, fmt.Errorf("internal browser target is misconfigured")
	}
	if target.ExpectedPathSuffix != "" && (!strings.HasPrefix(target.ExpectedPathSuffix, "/") || strings.Contains(target.ExpectedPathSuffix, "://")) {
		return Target{}, fmt.Errorf("internal browser target is misconfigured")
	}
	if target.ExpectedURLPart != "" && (!strings.HasPrefix(target.ExpectedURLPart, "/") || strings.Contains(target.ExpectedURLPart, "://")) {
		return Target{}, fmt.Errorf("internal browser target is misconfigured")
	}
	return target, nil
}

// maxPagePathLength caps a caller-supplied page so an absurd string never
// reaches the browser's argument vector.
const maxPagePathLength = 512

// ValidatePage accepts only an absolute path inside the app TargetFor already
// allowed. Anything that could point the browser at another host — a full URL,
// a scheme-relative "//host/path", or a backslash a URL parser may fold into an
// authority — is rejected rather than followed, so a caller can choose the page
// without widening the host allowlist.
func ValidatePage(page string) (string, error) {
	trimmed := strings.TrimSpace(page)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > maxPagePathLength ||
		!strings.HasPrefix(trimmed, "/") ||
		strings.HasPrefix(trimmed, "//") ||
		strings.Contains(trimmed, `\`) {
		return "", fmt.Errorf("internal browser page must be a path inside the app")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.Opaque != "" {
		return "", fmt.Errorf("internal browser page must be a path inside the app")
	}
	return trimmed, nil
}

// landedOnPage proves the browser is on the requested route. Without it an app
// that bounced the request to its own login screen would still be screenshotted
// as if it were the page the caller asked for.
func landedOnPage(finalURL, page string) bool {
	reached, err := url.Parse(finalURL)
	if err != nil {
		return false
	}
	wanted, err := url.Parse(page)
	if err != nil {
		return false
	}
	return strings.TrimSuffix(reached.Path, "/") == strings.TrimSuffix(wanted.Path, "/")
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
	VersionCommit string   `json:"version_commit,omitempty"`

	// Failure diagnostics. Empty on success. FailureDetail is sourced only from
	// this package's static target allowlist and the caller's own already-
	// validated page path — never from browser output — so it can name the
	// attempted address or the missing marker without leaking page content or a
	// credential.
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

const openStage = "open"

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
	fetchVersion   func(context.Context, string, map[string]string) (string, error)
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

const (
	defaultOpenTimeout  = 75 * time.Second
	defaultStageTimeout = 30 * time.Second
)

// countedStages is how many stageTimeout-bounded steps the longest verification
// can spend after the open stage. A target may chain a direct route open, a
// link click, a row click, and a tab click with their render waits after the
// shared login sequence; Multica additionally verifies its deployed commit
// through /version, and a caller-chosen page adds one more open, render, and
// URL read on top. The count stays at the historical maximum so the ceiling
// never shrinks under a target that composes several navigation steps.
const countedStages = 17

// MaxVerificationDuration is the longest a single Verify can legitimately take:
// the DNS preflight, every open attempt including the cold-start retry, and each
// remaining stage at its own ceiling. A caller MUST allow at least this long.
// A deadline shorter than this makes the cold-start retry unreachable — the
// caller hangs up mid-retry — which is exactly how a healthy but idle app came
// back as a failure.
const MaxVerificationDuration = dnsPreflightTimeout +
	(openStageRetries+1)*defaultOpenTimeout +
	countedStages*defaultStageTimeout

func NewRunner(commander Commander) *Runner {
	return &Runner{
		commander: commander, openTimeout: defaultOpenTimeout,
		stageTimeout: defaultStageTimeout, cleanupTimeout: defaultStageTimeout,
		dnsTimeout: dnsPreflightTimeout, resolveHost: resolveInternalHost,
		fetchVersion: fetchVersionCommit,
		observeStage: func(observation stageObservation) {
			log.Printf("internal browser diagnostic app=%s stage=%s duration_ms=%d exit_class=%s target_host=%s",
				observation.App, observation.Stage, observation.Duration.Milliseconds(), observation.ExitClass, observation.TargetHost)
		},
	}
}

func fetchVersionCommit(ctx context.Context, versionURL string, headers map[string]string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL, nil)
	if err != nil {
		return "", err
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version endpoint returned %d", response.StatusCode)
	}
	var payload struct {
		Commit      string `json:"commit"`
		BuildCommit string `json:"build_commit"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4096))
	if err := decoder.Decode(&payload); err != nil {
		return "", err
	}
	commit := strings.TrimSpace(payload.Commit)
	if commit == "" {
		commit = strings.TrimSpace(payload.BuildCommit)
	}
	if !safeVersionCommit(commit) {
		return "", fmt.Errorf("version endpoint returned an invalid commit")
	}
	return commit, nil
}

func safeVersionCommit(commit string) bool {
	if commit == "unknown" {
		return false
	}
	if len(commit) < 7 || len(commit) > 64 {
		return false
	}
	for _, char := range commit {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

func versionURL(target Target) string {
	parsed, _ := url.Parse(target.URL)
	return parsed.Scheme + "://" + parsed.Host + target.VersionPath
}

func (r *Runner) readVersion(ctx context.Context, target Target, credential Credential) (string, error) {
	if target.VersionPath == "" {
		return "", nil
	}
	stageCtx, cancel := context.WithTimeout(ctx, r.stageTimeout)
	defer cancel()
	started := time.Now()
	var headers map[string]string
	if target.AccessHeaders {
		headers = map[string]string{
			"CF-Access-Client-Id":     credential.AccessClientID,
			"CF-Access-Client-Secret": credential.AccessClientSecret,
		}
	}
	commit, err := r.fetchVersion(stageCtx, versionURL(target), headers)
	if err == nil && !safeVersionCommit(commit) {
		err = fmt.Errorf("version endpoint returned an invalid commit")
	}
	exitClass := commandFailureKind("success")
	if err != nil {
		exitClass = commandFailureUnknown
		if stageCtx.Err() != nil {
			exitClass = commandFailureTimeout
		}
	}
	if r.observeStage != nil {
		r.observeStage(stageObservation{
			App: target.Name, Stage: "version", Duration: time.Since(started), ExitClass: exitClass, TargetHost: target.Host(),
		})
	}
	if err != nil {
		return "", stageError{stage: "version", kind: exitClass}
	}
	return commit, nil
}

// openStage is the first navigation of a run, so it absorbs the target's cold
// start. A container that has been idle can spend longer waking than the browser
// action timeout allows, which surfaced as a plain "timeout" for an app that is
// healthy on the very next attempt. Retry that one stage once on a timeout so a
// slow wake-up is not reported as a failing app; every other cause fails on the
// first attempt as before.
const openStageRetries = 1

func (r *Runner) runStage(ctx context.Context, target Target, stage, stdin string, args ...string) ([]byte, error) {
	navigationStage := stage == openStage || stage == "reload"
	if !navigationStage {
		return r.runStageOnce(ctx, target, stage, stdin, args...)
	}
	retries := 0
	if stage == openStage {
		retries = openStageRetries
	}
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		output, err := r.runStageOnce(ctx, target, stage, stdin, args...)
		if err == nil {
			return output, nil
		}
		lastErr = err
		var stage stageError
		if !errors.As(err, &stage) || stage.kind != commandFailureTimeout {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, err
		}
		// Browser navigation waits for the page load event, but modern apps can
		// keep network work alive after the document is already usable. Recover
		// only when a separate URL read proves the browser reached the exact
		// allowlisted origin. The snapshot, markers, final route, and version
		// checks below still fail closed if the app itself is not ready.
		if r.navigationReachedTarget(ctx, target, args) {
			return nil, nil
		}
	}
	return nil, lastErr
}

func (r *Runner) navigationReachedTarget(ctx context.Context, target Target, args []string) bool {
	if len(args) < 2 || args[0] != "--session" || args[1] == "" {
		return false
	}
	timeout := r.stageTimeout
	if timeout <= 0 {
		timeout = defaultStageTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := r.commander.Run(probeCtx, "", args[0], args[1], "get", "url")
	if err != nil {
		return false
	}
	reached, err := url.Parse(strings.TrimSpace(string(output)))
	if err != nil || reached.Scheme == "" || reached.Host == "" {
		return false
	}
	expected, err := url.Parse(target.URL)
	if err != nil {
		return false
	}
	return reached.Scheme == expected.Scheme && reached.Host == expected.Host
}

func (r *Runner) runStageOnce(ctx context.Context, target Target, stage, stdin string, args ...string) ([]byte, error) {
	stageTimeout := r.stageTimeout
	if stage == openStage {
		stageTimeout = r.openTimeout
	}
	if stageTimeout <= 0 {
		if stage == openStage {
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

// dismissBlockingDialog clicks away a modal that a first login can raise before
// the workspace is usable — today the "How did you hear about Multica?" survey,
// which covers the navigation and makes a perfectly healthy app look like it is
// missing its menu. Best effort by design: no dialog is the normal case, so a
// failure here is never the run's verdict and the navigation stage still speaks.
func (r *Runner) dismissBlockingDialog(ctx context.Context, target Target, baseArgs []string) {
	dismissCtx, cancel := context.WithTimeout(ctx, r.stageTimeout)
	defer cancel()
	if _, err := r.commander.Run(dismissCtx, "",
		append(baseArgs, "find", "role", "button", "click", "--name", "Skip")...); err != nil {
		return
	}
	if r.observeStage != nil {
		r.observeStage(stageObservation{
			App: target.Name, Stage: "dialog", ExitClass: "success", TargetHost: target.Host(),
		})
	}
	_, _ = r.commander.Run(dismissCtx, "", append(baseArgs, "wait", "1000")...)
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
	stages := []string{dnsStage, openStage, "auth", "reload", "render", "navigation", "snapshot", "markers", "url", "version", "errors", "screenshot"}
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

// Verify runs the target's fixed login-and-markers check. An optional page is a
// same-origin path opened only after that check has passed, so the caller sees
// any page of the app while every existing assertion stays exactly as it was.
func (r *Runner) Verify(ctx context.Context, app, page string, credential Credential) (Result, error) {
	target, err := TargetFor(app)
	if err != nil {
		return Result{}, err
	}
	page, err = ValidatePage(page)
	if err != nil {
		return Result{}, err
	}
	// A code-login target signs in with the staging master code, so its second
	// secret is the code rather than a password. Cloudflare-only targets also
	// use a vault, but never receive form credentials.
	secondSecret := credential.Password
	if target.CodeSelector != "" {
		secondSecret = credential.LoginCode
	}
	formLogin := target.UsernameSelector != ""
	hasLoginCredential := credential.Username != "" || secondSecret != ""
	if !formLogin && hasLoginCredential {
		return Result{}, fmt.Errorf("target does not accept a browser credential")
	}
	if formLogin && (credential.Username == "" || secondSecret == "") {
		return Result{}, fmt.Errorf("target requires a complete browser credential")
	}
	hasAccessCredential := credential.AccessClientID != "" || credential.AccessClientSecret != ""
	if target.AccessHeaders != hasAccessCredential || (target.AccessHeaders && (credential.AccessClientID == "" || credential.AccessClientSecret == "")) {
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

	if target.AccessHeaders {
		// The service-token headers are secrets, so they go through batch stdin
		// instead of the argument vector. agent-browser honors a `--headers`
		// flag on `open` only when it is a global CLI flag (argv, which would
		// leak the secrets); inside a batch payload the flag is silently
		// dropped and the navigation reaches Cloudflare Access without the
		// token (FIR-3796). The session-level `set headers` command is the one
		// form that both travels over stdin and applies to the navigation.
		headers, headerErr := json.Marshal(map[string]string{
			"CF-Access-Client-Id":     credential.AccessClientID,
			"CF-Access-Client-Secret": credential.AccessClientSecret,
		})
		if headerErr != nil {
			return r.failure(target, baseArgs, target.URL, stageError{stage: openStage, kind: commandFailureUnknown})
		}
		payload, _ := json.Marshal([][]string{{"set", "headers", string(headers)}, {"open", target.URL}})
		if _, err := r.runStage(ctx, target, openStage, string(payload), append(baseArgs, "batch")...); err != nil {
			return r.failure(target, baseArgs, target.URL, err)
		}
	} else if _, err := r.runStage(ctx, target, openStage, "", append(baseArgs, "open", target.URL)...); err != nil {
		return r.failure(target, baseArgs, target.URL, err)
	}
	if formLogin || target.SessionCookie {
		commands := make([][]string, 0, 6)
		var sessionRootURL string
		if formLogin {
			commands = append(commands, []string{"fill", target.UsernameSelector, credential.Username})
			if target.CodeSelector != "" {
				// Two-step code login: submit the email, wait for the code field,
				// then type the staging master code into it.
				commands = append(commands,
					[]string{"find", "role", "button", "click", "--name", target.SubmitButtonName},
					[]string{"wait", target.CodeSelector},
					[]string{"fill", target.CodeSelector, credential.LoginCode},
				)
			} else {
				commands = append(commands,
					[]string{"fill", target.PasswordSelector, credential.Password},
					[]string{"click", target.SubmitSelector},
				)
			}
		}
		if target.SessionCookie {
			parsed, _ := url.Parse(target.URL)
			sessionRootURL = parsed.Scheme + "://" + parsed.Host + "/"
			commands = append(commands,
				[]string{"cookies", "set", "multica_auth", credential.SessionToken, "--url", sessionRootURL, "--httpOnly", "--sameSite", "Strict"},
				[]string{"cookies", "set", "multica_logged_in", "1", "--url", sessionRootURL, "--sameSite", "Lax"},
			)
		}
		if formLogin {
			settle := "1500"
			if target.CodeSelector != "" {
				settle = "4000"
			}
			commands = append(commands, []string{"wait", settle})
		}
		payload, _ := json.Marshal(commands)
		// This output is deliberately discarded: the stdin payload contains secrets.
		if _, err := r.runStage(ctx, target, "auth", string(payload), append(baseArgs, "batch")...); err != nil {
			return r.failure(target, baseArgs, target.URL, err)
		}
		if sessionRootURL != "" {
			if _, err := r.runStage(ctx, target, openStage, "", append(baseArgs, "open", sessionRootURL)...); err != nil {
				return r.failure(target, baseArgs, sessionRootURL, err)
			}
		}
	}
	if _, err := r.runStage(ctx, target, "reload", "", append(baseArgs, "reload")...); err != nil {
		return r.failure(target, baseArgs, target.URL, err)
	}
	if _, err := r.runStage(ctx, target, "render", "", append(baseArgs, "wait", "2500")...); err != nil {
		return r.failure(target, baseArgs, target.URL, err)
	}
	if target.NavigatePath != "" {
		parsed, _ := url.Parse(target.URL)
		navigationURL := parsed.Scheme + "://" + parsed.Host + target.NavigatePath
		if _, err := r.runStage(ctx, target, "navigation", "", append(baseArgs, "open", navigationURL)...); err != nil {
			return r.failure(target, baseArgs, navigationURL, err)
		}
		if _, err := r.runStage(ctx, target, "render", "", append(baseArgs, "wait", "2500")...); err != nil {
			return r.failure(target, baseArgs, navigationURL, err)
		}
	}
	if target.NavigateLinkName != "" {
		r.dismissBlockingDialog(ctx, target, baseArgs)
		navigationArgs := []string{"find", "role", "link", "click", "--name", target.NavigateLinkName}
		navigationDetail := "link: " + target.NavigateLinkName
		if target.NavigateExactText {
			navigationArgs = []string{"find", "text", target.NavigateLinkName, "click", "--exact"}
			navigationDetail = "exact text: " + target.NavigateLinkName
		}
		if _, err := r.runStage(ctx, target, "navigation", "", append(baseArgs, navigationArgs...)...); err != nil {
			return r.failure(target, baseArgs, navigationDetail, err)
		}
		if _, err := r.runStage(ctx, target, "render", "", append(baseArgs, "wait", "2500")...); err != nil {
			return r.failure(target, baseArgs, target.URL, err)
		}
	}
	if target.NavigateSelector != "" {
		if _, err := r.runStage(ctx, target, "navigation", "", append(baseArgs, "click", target.NavigateSelector)...); err != nil {
			return r.failure(target, baseArgs, "selector: "+target.NavigateSelector, err)
		}
		if _, err := r.runStage(ctx, target, "render", "", append(baseArgs, "wait", "2500")...); err != nil {
			return r.failure(target, baseArgs, target.URL, err)
		}
	}
	if target.NavigateTabName != "" {
		if _, err := r.runStage(ctx, target, "navigation", "", append(baseArgs, "find", "role", "tab", "click", "--name", target.NavigateTabName)...); err != nil {
			return r.failure(target, baseArgs, "tab: "+target.NavigateTabName, err)
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
	finalURLText := strings.TrimSpace(string(finalURL))
	if target.ExpectedURLPart != "" && !strings.Contains(finalURLText, target.ExpectedURLPart) {
		return r.failure(target, baseArgs, "missing URL part: "+target.ExpectedURLPart,
			stageError{stage: "url", kind: commandFailureNotFound})
	}
	if target.ExpectedPathSuffix != "" {
		parsedFinalURL, parseErr := url.Parse(finalURLText)
		if parseErr != nil || !strings.HasSuffix(strings.TrimSuffix(parsedFinalURL.Path, "/"), target.ExpectedPathSuffix) {
			return r.failure(target, baseArgs, "unexpected final path",
				stageError{stage: "url", kind: commandFailureNotFound})
		}
	}
	versionCommit, err := r.readVersion(ctx, target, credential)
	if err != nil {
		return r.failure(target, baseArgs, "version endpoint: "+target.VersionPath, err)
	}
	// The caller's page is opened last, on the session the checks above already
	// proved is logged in. Its own address is rebuilt from the allowlisted
	// target's scheme and host, so a path is the only thing the caller controls.
	if page != "" {
		parsedTarget, _ := url.Parse(target.URL)
		pageURL := parsedTarget.Scheme + "://" + parsedTarget.Host + page
		if _, err := r.runStage(ctx, target, "navigation", "", append(baseArgs, "open", pageURL)...); err != nil {
			return r.failure(target, baseArgs, pageURL, err)
		}
		if _, err := r.runStage(ctx, target, "render", "", append(baseArgs, "wait", "2500")...); err != nil {
			return r.failure(target, baseArgs, pageURL, err)
		}
		pageURLOutput, err := r.runStage(ctx, target, "url", "", append(baseArgs, "get", "url")...)
		if err != nil {
			return r.failure(target, baseArgs, pageURL, err)
		}
		finalURLText = strings.TrimSpace(string(pageURLOutput))
		if !landedOnPage(finalURLText, page) {
			return r.failure(target, baseArgs, "unexpected final path",
				stageError{stage: "url", kind: commandFailureNotFound})
		}
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
		App: target.Name, InternalHost: target.Host(), FinalURL: finalURLText,
		Markers: append([]string(nil), target.ExpectedText...), Errors: errors, ScreenshotPNG: screenshot,
		VersionCommit: versionCommit,
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
