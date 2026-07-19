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
	commandFailureConnection    commandFailureKind = "connection"
	commandFailureBrowserLaunch commandFailureKind = "browser-launch"
)

type commandFailure struct {
	kind commandFailureKind
}

func (failure commandFailure) Error() string {
	return "agent-browser command failed"
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
}

type Runner struct {
	commander      Commander
	openTimeout    time.Duration
	stageTimeout   time.Duration
	cleanupTimeout time.Duration
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
		if kind := safeCommandFailureKind(err); stage == "open" && kind != commandFailureUnknown {
			return nil, fmt.Errorf("internal browser stage %s %s failed", stage, kind)
		}
		return nil, fmt.Errorf("internal browser stage %s failed", stage)
	}
	return output, nil
}

func SafeError(err error) string {
	message := err.Error()
	switch message {
	case "internal browser stage open failed",
		"internal browser stage open dns failed",
		"internal browser stage open connection failed",
		"internal browser stage open browser-launch failed",
		"internal browser stage open timeout failed",
		"internal browser stage auth failed",
		"internal browser stage reload failed",
		"internal browser stage render failed",
		"internal browser stage navigation failed",
		"internal browser stage snapshot failed",
		"internal browser stage markers failed",
		"internal browser stage url failed",
		"internal browser stage errors failed",
		"internal browser stage screenshot failed":
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
		return Result{}, err
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
			return Result{}, err
		}
	}
	if _, err := r.runStage(ctx, target, "reload", "", append(baseArgs, "reload")...); err != nil {
		return Result{}, err
	}
	if _, err := r.runStage(ctx, target, "render", "", append(baseArgs, "wait", "2500")...); err != nil {
		return Result{}, err
	}
	if target.NavigateLinkName != "" {
		if _, err := r.runStage(ctx, target, "navigation", "", append(baseArgs, "find", "role", "link", "click", "--name", target.NavigateLinkName)...); err != nil {
			return Result{}, err
		}
		if _, err := r.runStage(ctx, target, "render", "", append(baseArgs, "wait", "2500")...); err != nil {
			return Result{}, err
		}
	}
	snapshot, err := r.runStage(ctx, target, "snapshot", "", append(baseArgs, "snapshot")...)
	if err != nil {
		return Result{}, err
	}
	for _, marker := range target.ExpectedText {
		if !strings.Contains(string(snapshot), marker) {
			return Result{}, fmt.Errorf("internal browser stage markers failed")
		}
	}
	finalURL, err := r.runStage(ctx, target, "url", "", append(baseArgs, "get", "url")...)
	if err != nil {
		return Result{}, err
	}
	rawErrors, err := r.runStage(ctx, target, "errors", "", append(baseArgs, "errors")...)
	if err != nil {
		return Result{}, err
	}
	screenshotCtx, cancel := context.WithTimeout(ctx, r.stageTimeout)
	defer cancel()
	screenshot, err := r.commander.CaptureScreenshot(screenshotCtx, baseArgs...)
	if err != nil {
		return Result{}, fmt.Errorf("internal browser stage screenshot failed")
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
