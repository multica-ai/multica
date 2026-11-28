package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The sidecar script is embedded so the docker launcher never depends on a file
// that exists only in the source tree.
//
//go:embed zcode_browser_sidecar.mjs
var zcodeBrowserSidecarScript []byte

const (
	zcodeBrowserModeAuto   = "auto"
	zcodeBrowserModeDocker = "docker"
	zcodeBrowserModeOff    = "off"

	zcodeBrowserEnvMode      = "MULTICA_ZCODE_BROWSER"
	zcodeBrowserEnvImage     = "MULTICA_ZCODE_BROWSER_IMAGE"
	zcodeBrowserEnvDockerBin = "MULTICA_ZCODE_BROWSER_DOCKER_BIN"
	zcodeBrowserEnvModules   = "MULTICA_ZCODE_BROWSER_MODULES"

	zcodeBrowserDefaultImage    = "mcr.microsoft.com/playwright:v1.62.1-noble"
	zcodeBrowserSidecarFile     = "zcode_browser_sidecar.mjs"
	zcodeBrowserContainerLabel  = "multica.zcode-browser"
	zcodeBrowserContainerPrefix = "multica-zcode-browser-"
	zcodeBrowserName            = "Multica Headless Chromium"

	zcodeBrowserInvalidParams = -32602

	zcodeBrowserDefaultStartTimeout     = 20 * time.Second
	zcodeBrowserDefaultDeadline         = 45 * time.Second
	zcodeBrowserDefaultCloseGrace       = 1500 * time.Millisecond
	zcodeBrowserDefaultOrphanSweepDelay = 3 * time.Second
	// zcodeBrowserDockerCommandTimeout plus the wait delay bounds every
	// short-lived docker call (image probe and container removal).
	zcodeBrowserDockerCommandTimeout = 4 * time.Second
	zcodeBrowserDockerWaitDelay      = time.Second
	zcodeBrowserKillWait             = time.Second
	zcodeBrowserStartJoinTimeout     = 5 * time.Second
	zcodeBrowserSidecarQueueSize     = 64
	zcodeBrowserStderrTailBytes      = 4 * 1024
	zcodeBrowserTraceTextLimit       = 4000
)

// Reasons browser use can be unavailable. They appear in the operator-facing
// log line, so treat them as a stable vocabulary.
const (
	zcodeBrowserReasonOff                = "off"
	zcodeBrowserReasonDockerBinMissing   = "docker_bin_missing"
	zcodeBrowserReasonDockerUnreachable  = "docker_unreachable"
	zcodeBrowserReasonImageMissing       = "image_missing"
	zcodeBrowserReasonModulesMissing     = "modules_missing"
	zcodeBrowserReasonSidecarStartFailed = "sidecar_start_failed"
	zcodeBrowserReasonBrowserStartFailed = "browser_start_failed"
	zcodeBrowserReasonNoSession          = "no_session"
	// zcodeBrowserReasonClosed is only used for tracing and replies: a closed
	// host belongs to a run that is ending, so it is never logged as a warning.
	zcodeBrowserReasonClosed = "closed"
)

const (
	zcodeBrowserCodeBackendUnavailable    = "backend_unavailable"
	zcodeBrowserCodeCapabilityUnsupported = "capability_unsupported"
	zcodeBrowserCodeCancelled             = "cancelled"
	zcodeBrowserCodeTimeout               = "timeout"
	zcodeBrowserCodeExecutionError        = "execution_error"
	zcodeBrowserCodeDuplicateRequestID    = "duplicate_request_id"

	zcodeBrowserMethodList    = "interaction/browserList"
	zcodeBrowserMethodExecute = "interaction/browserExecute"
)

var (
	errZcodeBrowserClosed      = errors.New("zcode browser host is closed")
	errZcodeBrowserUnavailable = errors.New("zcode browser use is unavailable")
	errZcodeDockerUnreachable  = errors.New("docker daemon is unreachable")
)

// zcodeSafeGo runs fn in a goroutine. Browser-use work runs many small
// goroutines next to the daemon's request loop; a panic in one of them must be
// logged, never take the daemon down.
func zcodeSafeGo(logger *slog.Logger, fn func()) {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil && logger != nil {
				logger.Error("panic in zcode browser-use goroutine", "panic", fmt.Sprint(recovered))
			}
		}()
		fn()
	}()
}

// zcodeBrowserOptions configures the browser-use sidecar. The zero value is
// OFF: no environment is read, no docker binary is looked up and no child
// process is ever started.
type zcodeBrowserOptions struct {
	// Mode is "" (auto), "auto", "docker" or "off".
	Mode       string
	DockerBin  string // optional explicit path; else PATH, /usr/local/bin, /opt/homebrew/bin
	Image      string // default mcr.microsoft.com/playwright:v1.62.1-noble
	ModulesDir string // directory containing node_modules/playwright-core; operator-provisioned, no default

	StartTimeout     time.Duration // sidecar must report ready within this window (default 20s)
	Deadline         time.Duration // per non-lifecycle request (default 45s)
	CloseGrace       time.Duration // graceful exit window before SIGKILL (default 1.5s)
	OrphanSweepDelay time.Duration // second container removal for a sidecar killed before ready (default 3s)

	// Test seams. A nil Command, Remove or Probe means the real docker path.
	Command func(ctx context.Context, workDir, name, browserID string, generation int) (*exec.Cmd, error)
	Remove  func(name string) error
	Probe   func(dockerBin, image string) error
}

func (o zcodeBrowserOptions) enabled() bool {
	return o.Mode == zcodeBrowserModeAuto || o.Mode == zcodeBrowserModeDocker
}

func (o zcodeBrowserOptions) mode() string {
	if o.Mode == "" {
		return zcodeBrowserModeAuto
	}
	return o.Mode
}

func (o zcodeBrowserOptions) image() string {
	if o.Image != "" {
		return o.Image
	}
	return zcodeBrowserDefaultImage
}

func zcodeBrowserDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func (o zcodeBrowserOptions) startTimeout() time.Duration {
	return zcodeBrowserDuration(o.StartTimeout, zcodeBrowserDefaultStartTimeout)
}

func (o zcodeBrowserOptions) deadline() time.Duration {
	return zcodeBrowserDuration(o.Deadline, zcodeBrowserDefaultDeadline)
}

func (o zcodeBrowserOptions) closeGrace() time.Duration {
	return zcodeBrowserDuration(o.CloseGrace, zcodeBrowserDefaultCloseGrace)
}

func (o zcodeBrowserOptions) orphanSweepDelay() time.Duration {
	return zcodeBrowserDuration(o.OrphanSweepDelay, zcodeBrowserDefaultOrphanSweepDelay)
}

// zcodeBrowserOptionsFromEnv reads the browser-use settings from the daemon's
// own environment. It is called once per backend by newZcodeBackend.
func zcodeBrowserOptionsFromEnv(lookup func(string) (string, bool), logger *slog.Logger) zcodeBrowserOptions {
	mode := strings.ToLower(zcodeBrowserEnv(lookup, zcodeBrowserEnvMode))
	switch mode {
	case "":
		mode = zcodeBrowserModeAuto
	case zcodeBrowserModeAuto, zcodeBrowserModeDocker, zcodeBrowserModeOff:
	default:
		// An unrecognized value must never resolve to "on" by accident.
		if logger != nil {
			logger.Warn("zcode browser use disabled: unrecognized " + zcodeBrowserEnvMode + " value")
		}
		mode = zcodeBrowserModeOff
	}
	return zcodeBrowserOptions{
		Mode:       mode,
		DockerBin:  zcodeBrowserEnv(lookup, zcodeBrowserEnvDockerBin),
		Image:      zcodeBrowserEnv(lookup, zcodeBrowserEnvImage),
		ModulesDir: zcodeBrowserEnv(lookup, zcodeBrowserEnvModules),
	}
}

func zcodeBrowserEnv(lookup func(string) (string, bool), name string) string {
	if lookup == nil {
		return ""
	}
	value, _ := lookup(name)
	return strings.TrimSpace(value)
}

// zcodeDockerBinPath resolves the docker CLI: an explicit path wins, then PATH,
// then two conventional install locations that shells often omit.
func zcodeDockerBinPath(explicit string, lookPath func(string) (string, error)) (string, error) {
	for _, candidate := range []string{explicit, "docker", "/usr/local/bin/docker", "/opt/homebrew/bin/docker"} {
		if candidate == "" {
			continue
		}
		if resolved, err := lookPath(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", errors.New("docker CLI not found")
}

// resolve reports why browser use is unavailable, or "" when it is available.
// The order guarantees that no child process is spawned unless the modules
// directory is configured: mode, then a plain file check, then the docker
// binary, and only then the image probe.
func (o zcodeBrowserOptions) resolve(lookPath func(string) (string, error), probe func(dockerBin, image string) error) string {
	if !o.enabled() {
		return zcodeBrowserReasonOff
	}
	if o.ModulesDir == "" || !zcodeBrowserFileExists(filepath.Join(o.ModulesDir, "node_modules", "playwright-core", "package.json")) {
		return zcodeBrowserReasonModulesMissing
	}
	if o.Command != nil {
		// The sidecar process is provided (test seam): no docker CLI or image
		// is consulted.
		return ""
	}
	bin, err := zcodeDockerBinPath(o.DockerBin, lookPath)
	if err != nil {
		return zcodeBrowserReasonDockerBinMissing
	}
	if probe == nil {
		probe = zcodeDockerProbeImage
	}
	switch err := probe(bin, o.image()); {
	case err == nil:
		return ""
	case errors.Is(err, errZcodeDockerUnreachable):
		return zcodeBrowserReasonDockerUnreachable
	default:
		return zcodeBrowserReasonImageMissing
	}
}

// hint is the operator-facing advice for one unavailability reason.
func (o zcodeBrowserOptions) hint(reason string) string {
	switch reason {
	case zcodeBrowserReasonOff:
		return "set " + zcodeBrowserEnvMode + " to auto or docker to enable browser use"
	case zcodeBrowserReasonModulesMissing:
		return "provision playwright-core and point " + zcodeBrowserEnvModules + " at the directory that contains node_modules"
	case zcodeBrowserReasonDockerBinMissing:
		return "install the Docker CLI or set " + zcodeBrowserEnvDockerBin
	case zcodeBrowserReasonDockerUnreachable:
		return "start the Docker daemon"
	case zcodeBrowserReasonImageMissing:
		return "make sure the image is present locally (nothing is pulled automatically) or set " + zcodeBrowserEnvImage
	case zcodeBrowserReasonSidecarStartFailed:
		return "check the daemon log for the sidecar start error"
	case zcodeBrowserReasonBrowserStartFailed:
		return "check the daemon log for the browser start error"
	case zcodeBrowserReasonNoSession:
		return "the browser request arrived before the session handshake completed"
	}
	return "retry the run"
}

func zcodeBrowserFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

var zcodeDockerEnvNames = []string{"PATH", "HOME", "TMPDIR", "USER", "LOGNAME", "XDG_CONFIG_HOME", "XDG_RUNTIME_DIR"}

// zcodeDockerChildEnv keeps only what the docker CLI needs. The daemon's
// environment carries agent credentials that a container client has no business seeing.
func zcodeDockerChildEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if zcodeDockerEnvAllowed(name) {
			out = append(out, entry)
		}
	}
	return out
}

func zcodeDockerEnvAllowed(name string) bool {
	if strings.HasPrefix(name, "DOCKER_") {
		return true
	}
	// Windows may spell PATH as "Path".
	return slices.ContainsFunc(zcodeDockerEnvNames, func(candidate string) bool {
		return strings.EqualFold(candidate, name)
	})
}

// zcodeDockerRun runs one short-lived docker CLI call with a hard time bound.
func zcodeDockerRun(dockerBin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), zcodeBrowserDockerCommandTimeout)
	defer cancel()
	cmd := newRuntimeCmd(exec.CommandContext(ctx, dockerBin, args...))
	cmd.Env = zcodeDockerChildEnv(os.Environ())
	cmd.WaitDelay = zcodeBrowserDockerWaitDelay
	return outputOwned(cmd, nil)
}

func zcodeDockerProbeImage(dockerBin, image string) error {
	_, err := zcodeDockerRun(dockerBin, "image", "inspect", image)
	return zcodeDockerProbeError(err)
}

// zcodeDockerProbeError separates "docker itself did not answer" from "docker
// answered that the image is missing": only the first is transient.
func zcodeDockerProbeError(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return fmt.Errorf("%w: %v", errZcodeDockerUnreachable, err)
	}
	stderr := zcodeOneLine(exitErr.Stderr)
	if exitErr.ExitCode() < 0 || zcodeDockerDaemonUnreachable(stderr) {
		return fmt.Errorf("%w: %s", errZcodeDockerUnreachable, stderr)
	}
	return fmt.Errorf("docker image inspect failed: %s", stderr)
}

func zcodeDockerDaemonUnreachable(stderr string) bool {
	lower := strings.ToLower(stderr)
	for _, marker := range []string{
		"cannot connect to the docker daemon",
		"is the docker daemon running",
		"error during connect",
		"docker.sock: permission denied",
		"docker.sock: connect:",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func zcodeOneLine(text []byte) string {
	return strings.TrimSpace(strings.Join(strings.Fields(string(text)), " "))
}

// dockerRunCommand builds the `docker run` invocation for one sidecar.
func (o zcodeBrowserOptions) dockerRunCommand(ctx context.Context, dockerBin, workDir, name, browserID string, generation int) (*exec.Cmd, error) {
	modules, err := filepath.Abs(o.ModulesDir)
	if err != nil {
		return nil, err
	}
	args := []string{
		"run", "--rm", "-i", "--pull", "never",
		"--name", name,
		"--label", zcodeBrowserContainerLabel + "=1",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", "1024",
		"--memory", "1500m",
		"--shm-size", "256m",
		"-v", workDir + ":/sidecar:ro",
		// The mounts must stay siblings: a mount point cannot be created
		// inside the read-only /sidecar mount.
		"-v", filepath.Join(modules, "node_modules") + ":/node_modules:ro",
		"-e", "PLAYWRIGHT_BROWSERS_PATH=/ms-playwright",
		"--entrypoint", "node",
		o.image(),
		"/sidecar/" + zcodeBrowserSidecarFile,
		"--browser-id", browserID,
		"--generation", strconv.Itoa(generation),
	}
	cmd := exec.CommandContext(ctx, dockerBin, args...)
	cmd.Env = zcodeDockerChildEnv(os.Environ())
	return cmd, nil
}

// --- Wire types ----------------------------------------------------------------

// zcodeBrowserError is the error object of a result envelope.
type zcodeBrowserError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	SideEffect string `json:"sideEffect,omitempty"`
}

// zcodeBrowserEnvelope carries only the keys the strict result schema allows.
// A key without a value is omitted, never sent as null (only dialog may be null).
type zcodeBrowserEnvelope struct {
	OK        bool               `json:"ok"`
	Scope     json.RawMessage    `json:"scope,omitempty"`
	State     json.RawMessage    `json:"state,omitempty"`
	Snapshot  json.RawMessage    `json:"snapshot,omitempty"`
	Image     json.RawMessage    `json:"image,omitempty"`
	Tabs      json.RawMessage    `json:"tabs,omitempty"`
	UserTabs  json.RawMessage    `json:"userTabs,omitempty"`
	Tab       json.RawMessage    `json:"tab,omitempty"`
	Value     json.RawMessage    `json:"value,omitempty"`
	Element   json.RawMessage    `json:"element,omitempty"`
	Dialog    json.RawMessage    `json:"dialog,omitempty"`
	Recording json.RawMessage    `json:"recording,omitempty"`
	Error     *zcodeBrowserError `json:"error,omitempty"`
	Meta      json.RawMessage    `json:"meta,omitempty"`
	ElapsedMS int64              `json:"elapsedMs"`
}

func zcodeBrowserFailure(code, message, sideEffect string, started time.Time) zcodeBrowserEnvelope {
	return zcodeBrowserEnvelope{
		OK:        false,
		Error:     &zcodeBrowserError{Code: code, Message: message, SideEffect: sideEffect},
		ElapsedMS: time.Since(started).Milliseconds(),
	}
}

// zcodeBrowserWhitelist passes a sidecar result through the envelope schema.
func zcodeBrowserWhitelist(raw json.RawMessage) (zcodeBrowserEnvelope, error) {
	var in struct {
		OK        *bool              `json:"ok"`
		Scope     json.RawMessage    `json:"scope"`
		State     json.RawMessage    `json:"state"`
		Snapshot  json.RawMessage    `json:"snapshot"`
		Image     json.RawMessage    `json:"image"`
		Tabs      json.RawMessage    `json:"tabs"`
		UserTabs  json.RawMessage    `json:"userTabs"`
		Tab       json.RawMessage    `json:"tab"`
		Value     json.RawMessage    `json:"value"`
		Element   json.RawMessage    `json:"element"`
		Dialog    json.RawMessage    `json:"dialog"`
		Recording json.RawMessage    `json:"recording"`
		Error     *zcodeBrowserError `json:"error"`
		Meta      json.RawMessage    `json:"meta"`
		ElapsedMS *json.Number       `json:"elapsedMs"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return zcodeBrowserEnvelope{}, err
	}
	if in.OK == nil {
		return zcodeBrowserEnvelope{}, errors.New("sidecar result has no ok flag")
	}
	out := zcodeBrowserEnvelope{OK: *in.OK, Error: in.Error}
	out.Scope = zcodeBrowserPresent(in.Scope)
	out.State = zcodeBrowserPresent(in.State)
	out.Snapshot = zcodeBrowserPresent(in.Snapshot)
	out.Image = zcodeBrowserPresent(in.Image)
	out.Tabs = zcodeBrowserPresent(in.Tabs)
	out.UserTabs = zcodeBrowserPresent(in.UserTabs)
	out.Tab = zcodeBrowserPresent(in.Tab)
	out.Value = zcodeBrowserPresent(in.Value)
	out.Element = zcodeBrowserPresent(in.Element)
	out.Dialog = zcodeBrowserPresent(in.Dialog)
	out.Recording = zcodeBrowserPresent(in.Recording)
	out.Meta = zcodeBrowserPresent(in.Meta)
	if in.ElapsedMS != nil {
		if millis, err := in.ElapsedMS.Int64(); err == nil && millis >= 0 {
			out.ElapsedMS = millis
		}
	}
	return out, nil
}

func zcodeBrowserPresent(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	return trimmed
}

type zcodeBrowserDescriptor struct {
	ID                  string            `json:"id"`
	Generation          int               `json:"generation"`
	Type                string            `json:"type"`
	Name                string            `json:"name"`
	Capabilities        zcodeBrowserCaps  `json:"capabilities"`
	APISupportOverrides map[string]bool   `json:"apiSupportOverrides"`
	Metadata            map[string]string `json:"metadata"`
}

type zcodeBrowserCaps struct {
	Browser []any `json:"browser"`
	Tab     []any `json:"tab"`
}

// The nine overrides the managed runtime of the official CLI advertises.
var zcodeBrowserSupportOverrides = map[string]bool{
	"BrowserUser.openTabs":            false,
	"BrowserUser.history":             false,
	"PlaywrightAPI.waitForEvent":      false,
	"PlaywrightDownload.path":         false,
	"PlaywrightFileChooser.setFiles":  false,
	"Playwrightlocator.downloadMedia": false,
	"Playwrightlocator.evaluate":      false,
	"CUAAPI.downloadMedia":            false,
	"DomCUAAPI.downloadMedia":         false,
}

type zcodeBrowserListResult struct {
	Browsers []zcodeBrowserDescriptor `json:"browsers"`
}

// zcodeBrowserTraceEvent is an evidence hook for integration tests. It is nil
// in production; nothing browser-related is logged through slog except the
// single availability warning.
type zcodeBrowserTraceEvent struct {
	Kind     string // list | execute | unavailable
	Method   string
	Command  string
	Code     string
	OK       bool
	Browsers int
	Image    []byte
	URLHost  string
	DOM      string
}

var zcodeBrowserTrace func(ev zcodeBrowserTraceEvent)

func zcodeBrowserEmit(ev zcodeBrowserTraceEvent) {
	if hook := zcodeBrowserTrace; hook != nil {
		hook(ev)
	}
}

// zcodeBrowserParams are the fields of the interaction/browser* params that
// the host acts on.
type zcodeBrowserParams struct {
	RequestID         string          `json:"requestId"`
	SessionID         string          `json:"sessionId"`
	TurnID            string          `json:"turnId"`
	BrowserID         string          `json:"browserId"`
	BrowserGeneration *int            `json:"browserGeneration"`
	Command           json.RawMessage `json:"command"`
}

type zcodeBrowserCommand struct {
	Method    string `json:"method"`
	RequestID string `json:"requestId"`
	TurnID    string `json:"turnId"`
	SessionID string `json:"sessionId"`
	URL       string `json:"url"`
	Action    *struct {
		Name      string `json:"name"`
		Operation string `json:"operation"`
	} `json:"action"`
}

func zcodeBrowserIsLifecycle(method string) bool {
	return method == "cancelRequest" || method == "turnEnded" || method == "closeSession"
}

// zcodeBrowserGoUnsupported reports commands the official runtime rejects in
// its page-command fallback. They are answered here so no sidecar is started
// just to say no.
func zcodeBrowserGoUnsupported(method string) bool {
	return method == "openScope" || method == "releaseScope" || strings.HasPrefix(method, "recording")
}

var zcodeBrowserSideEffectMethods = []string{
	"navigate", "back", "forward", "reload", "click", "fill", "type", "press", "cuaKeypress",
	"scroll", "cuaScroll", "domCuaScroll", "hover", "select", "check", "drag", "cuaDrag",
	"handleDialog", "close", "newTab", "evaluate",
}

var zcodeBrowserSideEffectLocators = []string{"click", "dblclick", "fill", "press", "selectOption", "setChecked"}

// zcodeBrowserHasSideEffects mirrors the official runtime: only a cancelled or
// timed-out command that could have changed page state reports "uncertain".
func zcodeBrowserHasSideEffects(cmd *zcodeBrowserCommand) bool {
	if cmd.Method == "playwright" && cmd.Action != nil {
		if cmd.Action.Name == "locator" {
			return slices.Contains(zcodeBrowserSideEffectLocators, cmd.Action.Operation)
		}
		return cmd.Action.Name == "evaluate"
	}
	return slices.Contains(zcodeBrowserSideEffectMethods, cmd.Method)
}

// --- Sidecar process ------------------------------------------------------------

type zcodeSidecarFrame struct {
	ID                int64           `json:"id"`
	Method            string          `json:"method"`
	SessionID         string          `json:"sessionId,omitempty"`
	RequestID         string          `json:"requestId,omitempty"`
	TurnID            string          `json:"turnId,omitempty"`
	BrowserGeneration *int            `json:"browserGeneration,omitempty"`
	Command           json.RawMessage `json:"command,omitempty"`
}

type zcodeSidecarReply struct {
	Result     json.RawMessage
	Error      *zcodeBrowserError
	Generation int
}

// zcodeSidecarProc is one started sidecar process (normally a `docker run` client).
type zcodeSidecarProc struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  *tailBuffer
	name    string
	workDir string

	ready      chan struct{} // closed once the sidecar reported ready
	readyOnce  sync.Once
	readerDone chan struct{} // closed when stdout reached EOF
	exited     chan struct{} // closed after cmd.Wait returned

	wmu    sync.Mutex
	queue  chan []byte
	writer bool // true once the writer was told to stop; guarded by wmu

	wasReady bool // guarded by the host mutex
	torn     bool // guarded by the host mutex

	// cancel ends the process context once the sidecar is gone. The context
	// exists only to carry newRuntimeCmd's kill hook; nothing cancels it while
	// the host still owns the process.
	cancel context.CancelFunc
}

func (p *zcodeSidecarProc) markReady() {
	p.readyOnce.Do(func() { close(p.ready) })
}

// enqueue hands a frame to the single writer. It never blocks and returns
// false when the sidecar is already shutting down.
func (p *zcodeSidecarProc) enqueue(frame []byte) bool {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	if p.writer {
		return false
	}
	select {
	case p.queue <- frame:
		return true
	default:
		return false
	}
}

func (p *zcodeSidecarProc) stopWriter() {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	if !p.writer {
		p.writer = true
		close(p.queue)
	}
}

func (p *zcodeSidecarProc) writeLoop() {
	for frame := range p.queue {
		if _, err := p.stdin.Write(frame); err != nil {
			p.stopWriter()
			for range p.queue { // drain so enqueue stays non-blocking
			}
			return
		}
	}
}

func zcodeWaitSignal(ch <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return true
	case <-timer.C:
		return false
	}
}

// --- Host ------------------------------------------------------------------------

type zcodeBrowserRequest struct {
	requestID  string
	sessionID  string
	turnID     string
	sideEffect bool
	started    time.Time

	sidecarID int64 // guarded by the host mutex
	proc      *zcodeSidecarProc
	reply     chan zcodeSidecarReply // buffered 1
	done      chan struct{}          // closed when the request is cancelled
	cancelled bool                   // guarded by the host mutex
}

type zcodeBrowserHost struct {
	opts   zcodeBrowserOptions
	logger *slog.Logger

	mu          sync.Mutex
	closed      bool
	starting    bool
	startDone   chan struct{}
	startErr    error
	proc        *zcodeSidecarProc
	inflight    map[string]*zcodeBrowserRequest
	pending     map[int64]*zcodeBrowserRequest
	nextID      int64
	browserID   string
	generation  int
	browserUp   bool
	unavailable string
	warned      bool
}

func newZcodeBrowserHost(opts zcodeBrowserOptions, logger *slog.Logger) *zcodeBrowserHost {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &zcodeBrowserHost{
		opts:       opts,
		logger:     logger,
		inflight:   make(map[string]*zcodeBrowserRequest),
		pending:    make(map[int64]*zcodeBrowserRequest),
		browserID:  "cdp:" + zcodeRandomUUID(),
		generation: 1,
	}
}

func (h *zcodeBrowserHost) safeGo(fn func()) { zcodeSafeGo(h.logger, fn) }

func (h *zcodeBrowserHost) logError(message string, args ...any) {
	if h.logger != nil {
		h.logger.Error(message, args...)
	}
}

// zcodeRandomUUID returns a version 4 UUID using the standard library only.
func zcodeRandomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand never fails on the supported platforms; keep the id
		// well-formed anyway.
		for i := range b {
			b[i] = byte(i)
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	u := hex.EncodeToString(b[:])
	return u[0:8] + "-" + u[8:12] + "-" + u[12:16] + "-" + u[16:20] + "-" + u[20:32]
}

func zcodeRandomHex(count int) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:count])
}

// start returns the current sidecar, starting one if needed. Concurrent callers
// share a single start and observe the same outcome.
func (h *zcodeBrowserHost) start(ctx context.Context) (*zcodeSidecarProc, error) {
	h.mu.Lock()
	switch {
	case h.closed:
		h.mu.Unlock()
		return nil, errZcodeBrowserClosed
	case h.proc != nil:
		proc := h.proc
		h.mu.Unlock()
		return proc, nil
	case h.unavailable != "":
		h.mu.Unlock()
		return nil, errZcodeBrowserUnavailable
	case h.starting:
		done := h.startDone
		h.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		h.mu.Lock()
		proc, err := h.proc, h.startErr
		h.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if proc == nil {
			// The sidecar that was just started has been lost again.
			return nil, errZcodeBrowserUnavailable
		}
		return proc, nil
	}
	h.starting = true
	h.startErr = nil
	h.startDone = make(chan struct{})
	name := zcodeBrowserContainerPrefix + zcodeRandomHex(6)
	generation := h.generation
	h.mu.Unlock()

	proc, err := h.launchSlow(name, generation)
	if err != nil {
		return nil, err
	}
	return h.awaitReady(ctx, proc)
}

// launchSlow resolves availability and starts one sidecar process. It always
// ends the starting window, so a failed or panicking start can never leave
// later callers waiting on a channel that nobody closes.
func (h *zcodeBrowserHost) launchSlow(name string, generation int) (proc *zcodeSidecarProc, err error) {
	complete := false
	defer func() {
		if complete {
			return
		}
		// Only reachable through a panic in this function.
		h.mu.Lock()
		if h.starting {
			h.failLocked(zcodeBrowserReasonSidecarStartFailed)
			h.finishStartLocked(errZcodeBrowserUnavailable)
		}
		ours := proc != nil && h.proc == proc
		if ours {
			h.proc = nil
		}
		h.mu.Unlock()
		if ours {
			h.teardown(proc)
		}
	}()

	reason := h.opts.resolve(exec.LookPath, h.opts.Probe)
	if reason != "" {
		h.fail(reason)
		complete = true
		h.mu.Lock()
		h.finishStartLocked(errZcodeBrowserUnavailable)
		h.mu.Unlock()
		return nil, errZcodeBrowserUnavailable
	}
	proc, err = h.launch(name, generation)
	if err != nil {
		proc = nil
		h.logError("zcode browser sidecar could not be started", "error", zcodeOneLine([]byte(err.Error())))
		h.fail(zcodeBrowserReasonSidecarStartFailed)
		complete = true
		h.mu.Lock()
		h.finishStartLocked(errZcodeBrowserUnavailable)
		h.mu.Unlock()
		return nil, errZcodeBrowserUnavailable
	}

	// Back under the lock as soon as Start returned. If the run ended in the
	// meantime this goroutine owns the teardown of the process it just started,
	// and it keeps the starting window open until that teardown is done, so a
	// close() waiting on it does not return while the container still exists.
	h.mu.Lock()
	closed := h.closed
	if closed {
		h.mu.Unlock()
		complete = true
		h.teardown(proc)
		h.mu.Lock()
		h.finishStartLocked(errZcodeBrowserClosed)
		h.mu.Unlock()
		return nil, errZcodeBrowserClosed
	}
	h.proc = proc
	h.finishStartLocked(nil)
	h.mu.Unlock()
	complete = true
	h.safeGo(func() { h.startWatchdog(proc) })
	return proc, nil
}

// finishStartLocked ends the starting window and publishes its outcome. The
// host mutex must be held.
func (h *zcodeBrowserHost) finishStartLocked(err error) {
	h.starting = false
	h.startErr = err
	close(h.startDone)
}

// startWatchdog bounds the start handshake independently of the request that
// triggered it: a sidecar that never reports ready is taken away and torn
// down, so a cancelled or expired request cannot leave it behind.
func (h *zcodeBrowserHost) startWatchdog(proc *zcodeSidecarProc) {
	timer := time.NewTimer(h.opts.startTimeout())
	defer timer.Stop()
	select {
	case <-proc.ready:
	case <-proc.exited:
	case <-timer.C:
		h.abandon(proc, zcodeBrowserReasonSidecarStartFailed)
	}
}

// awaitReady waits for the sidecar's ready event or exit. The start timeout
// itself is enforced by startWatchdog.
func (h *zcodeBrowserHost) awaitReady(ctx context.Context, proc *zcodeSidecarProc) (*zcodeSidecarProc, error) {
	select {
	case <-proc.ready:
		h.mu.Lock()
		current := h.proc == proc && !h.closed
		h.mu.Unlock()
		if !current {
			return nil, errZcodeBrowserUnavailable
		}
		return proc, nil
	case <-proc.exited:
		return nil, errZcodeBrowserUnavailable
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// abandon takes a sidecar away from the host when its start failed or it
// outlived its deadline. Whoever takes it tears it down; the caller that finds
// it already gone reports the failure without touching it.
func (h *zcodeBrowserHost) abandon(proc *zcodeSidecarProc, reason string) {
	if !h.take(proc) {
		return
	}
	if reason != "" {
		h.fail(reason)
	}
	h.teardown(proc)
}

// fail caches an unavailability reason for the rest of the run.
func (h *zcodeBrowserHost) fail(reason string) {
	h.mu.Lock()
	h.failLocked(reason)
	h.mu.Unlock()
}

// failLocked caches an unavailability reason for the rest of the run. The host
// mutex must be held.
func (h *zcodeBrowserHost) failLocked(reason string) {
	if !h.closed && h.unavailable == "" {
		h.unavailable = reason
	}
}

func (h *zcodeBrowserHost) launch(name string, generation int) (*zcodeSidecarProc, error) {
	workDir, err := os.MkdirTemp("", "multica-zcode-browser-")
	if err != nil {
		return nil, err
	}
	proc, err := h.startProcess(workDir, name, generation)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}
	return proc, nil
}

func (h *zcodeBrowserHost) startProcess(workDir, name string, generation int) (*zcodeSidecarProc, error) {
	if err := os.WriteFile(filepath.Join(workDir, zcodeBrowserSidecarFile), zcodeBrowserSidecarScript, 0o644); err != nil {
		return nil, err
	}
	// The host stops the sidecar itself (teardown), so the process context is
	// never cancelled while it is running: it only carries the kill hook that
	// newRuntimeCmd installs, and is cancelled once the process is gone.
	ctx, cancel := context.WithCancel(context.Background())
	var (
		cmd *exec.Cmd
		err error
	)
	if h.opts.Command != nil {
		h.mu.Lock()
		browserID := h.browserID
		h.mu.Unlock()
		cmd, err = h.opts.Command(ctx, workDir, name, browserID, generation)
	} else {
		var bin string
		bin, err = zcodeDockerBinPath(h.opts.DockerBin, exec.LookPath)
		if err == nil {
			h.mu.Lock()
			browserID := h.browserID
			h.mu.Unlock()
			cmd, err = h.opts.dockerRunCommand(ctx, bin, workDir, name, browserID, generation)
		}
	}
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr := &tailBuffer{max: zcodeBrowserStderrTailBytes}
	if cmd.Stderr == nil {
		cmd.Stderr = stderr
	}
	if err := startOwnedProcessTree(newRuntimeCmd(cmd), h.logger); err != nil {
		cancel()
		return nil, err
	}
	proc := &zcodeSidecarProc{
		cmd:        cmd,
		cancel:     cancel,
		stdin:      stdin,
		stdout:     stdout,
		stderr:     stderr,
		name:       name,
		workDir:    workDir,
		ready:      make(chan struct{}),
		readerDone: make(chan struct{}),
		exited:     make(chan struct{}),
		queue:      make(chan []byte, zcodeBrowserSidecarQueueSize),
	}
	// reader -> reaper -> exit watcher is a strict chain, so teardown can wait
	// on exited without deadlocking against a reader that is still draining.
	h.safeGo(func() {
		defer close(proc.readerDone)
		h.readLoop(proc)
	})
	h.safeGo(func() {
		<-proc.readerDone
		_ = proc.cmd.Wait()
		close(proc.exited)
	})
	h.safeGo(func() {
		<-proc.exited
		h.sidecarExited(proc)
	})
	h.safeGo(proc.writeLoop)
	h.logger.Info("zcode browser sidecar started", "container", zcodeContainerShortName(name))
	return proc, nil
}

func zcodeContainerShortName(name string) string {
	if len(name) > len(zcodeBrowserContainerPrefix)+4 {
		return name[:len(zcodeBrowserContainerPrefix)+4] + "..."
	}
	return name
}

func (h *zcodeBrowserHost) readLoop(proc *zcodeSidecarProc) {
	scanner := newAgentStreamScanner(proc.stdout)
	for scanner.Scan() {
		if line := bytes.TrimSpace(scanner.Bytes()); len(line) > 0 {
			h.handleSidecarLine(proc, line)
		}
	}
}

func (h *zcodeBrowserHost) handleSidecarLine(proc *zcodeSidecarProc, line []byte) {
	var msg struct {
		Event      string             `json:"event"`
		Node       string             `json:"node"`
		ID         *int64             `json:"id"`
		Result     json.RawMessage    `json:"result"`
		Error      *zcodeBrowserError `json:"error"`
		Generation int                `json:"generation"`
		Unexpected bool               `json:"unexpected"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		h.logger.Debug("zcode browser sidecar sent an unreadable line")
		return
	}
	var reply zcodeSidecarReply
	h.mu.Lock()
	switch {
	case msg.Event == "ready":
		proc.wasReady = true
		proc.markReady()
	case msg.Event == "browserClosed":
		if h.proc == proc {
			if msg.Generation > 0 {
				h.generation = msg.Generation
			}
			h.browserUp = false
		}
	case msg.ID != nil:
		if r := h.pending[*msg.ID]; r != nil {
			delete(h.pending, *msg.ID)
			reply = zcodeSidecarReply{Result: msg.Result, Error: msg.Error}
			if msg.Result != nil && len(bytes.TrimSpace(msg.Result)) > 0 {
				var result struct {
					Generation int `json:"generation"`
				}
				if json.Unmarshal(msg.Result, &result) == nil {
					reply.Generation = result.Generation
				}
			}
			// A reply that beat a concurrent cancel still wins.
			if !r.cancelled {
				select {
				case r.reply <- reply:
				default:
				}
			}
		}
	}
	h.mu.Unlock()
}

// sidecarExited runs once per started process, after cmd.Wait returned. When
// the host still owns the process the exit was not expected: a lost browser
// advances the generation, requests in flight are answered by their own
// goroutines, and the process is torn down here. The next request relaunches.
func (h *zcodeBrowserHost) sidecarExited(proc *zcodeSidecarProc) {
	h.mu.Lock()
	ours := h.proc == proc
	ready := proc.wasReady
	if ours {
		h.proc = nil
		if h.browserUp {
			h.generation++
			h.browserUp = false
		}
		if !ready && !h.closed && h.unavailable == "" {
			h.unavailable = zcodeBrowserReasonSidecarStartFailed
		}
	}
	h.mu.Unlock()
	if !ours {
		return
	}
	h.logger.Warn("zcode browser sidecar exited unexpectedly",
		"container", zcodeContainerShortName(proc.name),
		"ready", ready,
		"stderr", zcodeTruncate(zcodeOneLine(proc.stderr.Bytes()), 300))
	h.teardown(proc)
}

// take removes proc from the host when it is still the current sidecar. The
// caller that succeeds owns its teardown.
func (h *zcodeBrowserHost) take(proc *zcodeSidecarProc) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.proc != proc {
		return false
	}
	h.proc = nil
	if h.browserUp {
		// The browser it hosted is lost; the next start uses the next generation.
		h.generation++
		h.browserUp = false
	}
	return true
}

// teardown is the only cleanup path for a started sidecar and is idempotent:
// stop the writer, close stdin, give the sidecar a graceful window, then kill
// the process group, remove the container by name and delete the temp dir.
func (h *zcodeBrowserHost) teardown(proc *zcodeSidecarProc) {
	h.mu.Lock()
	if proc.torn {
		h.mu.Unlock()
		return
	}
	proc.torn = true
	h.mu.Unlock()

	proc.stopWriter()
	_ = proc.stdin.Close()
	killed := !zcodeWaitSignal(proc.exited, h.opts.closeGrace())
	if killed {
		signalProcessGroup(proc.cmd, syscall.SIGKILL)
		waitProcessGroupGone(proc.cmd, zcodeBrowserKillWait)
		zcodeWaitSignal(proc.exited, 2*zcodeBrowserKillWait)
	}
	h.remove(proc.name)
	_ = os.RemoveAll(proc.workDir)
	releaseProcessGroup(proc.cmd)
	proc.cancel()

	h.mu.Lock()
	wasReady := proc.wasReady
	h.mu.Unlock()
	unconfirmed := killed && !wasReady
	if unconfirmed {
		// A container client killed before its sidecar reported ready may have
		// been interrupted while the container was still being created. Remove
		// the name once more after the daemon had time to finish that.
		name := proc.name
		h.safeGo(func() {
			time.Sleep(h.opts.orphanSweepDelay())
			h.remove(name)
		})
	}
}

// remove deletes the container by name. It runs regardless of whether the
// sidecar ever became ready or how it exited.
func (h *zcodeBrowserHost) remove(name string) {
	if remove := h.opts.Remove; remove != nil {
		if err := remove(name); err != nil {
			h.logger.Warn("zcode browser container removal failed", "container", zcodeContainerShortName(name), "error", zcodeTruncate(err.Error(), 200))
		}
		return
	}
	bin, err := zcodeDockerBinPath(h.opts.DockerBin, exec.LookPath)
	if err != nil {
		return
	}
	if _, err := zcodeDockerRun(bin, "rm", "-f", name); err != nil {
		h.logger.Warn("zcode browser container removal failed", "container", zcodeContainerShortName(name), "error", zcodeTruncate(err.Error(), 200))
	}
}

func zcodeTruncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// close ends browser use for the run. It is idempotent, never writes a reply
// itself, and returns once the sidecar is torn down, or once a start that is
// still in flight has been given the chance to hand its process over.
func (h *zcodeBrowserHost) close() {
	h.mu.Lock()
	h.closed = true
	for _, r := range h.inflight {
		h.settleLocked(r)
	}
	starting, startDone := h.starting, h.startDone
	h.mu.Unlock()

	if starting {
		// The launcher registers its process under the lock right after Start
		// returned; waiting here lets this goroutine tear it down synchronously.
		timer := time.NewTimer(zcodeBrowserStartJoinTimeout)
		select {
		case <-startDone:
		case <-timer.C:
		}
		timer.Stop()
	}

	h.mu.Lock()
	proc := h.proc
	h.proc = nil
	h.browserUp = false
	h.mu.Unlock()
	if proc != nil {
		h.teardown(proc)
	}
}

// settleLocked marks a request as cancelled and wakes its goroutine, which
// writes its own reply. The host mutex must be held.
func (h *zcodeBrowserHost) settleLocked(r *zcodeBrowserRequest) {
	if r.cancelled {
		return
	}
	r.cancelled = true
	close(r.done)
}

func newZcodeBrowserRequest(requestID, sessionID, turnID string, sideEffect bool) *zcodeBrowserRequest {
	return &zcodeBrowserRequest{
		requestID:  requestID,
		sessionID:  sessionID,
		turnID:     turnID,
		sideEffect: sideEffect,
		started:    time.Now(),
		reply:      make(chan zcodeSidecarReply, 1),
		done:       make(chan struct{}),
	}
}

// uncertain reports the side-effect marker for a command that was cut short.
func (r *zcodeBrowserRequest) uncertain() string {
	if r.sideEffect {
		return "uncertain"
	}
	return ""
}

// lifecycle handles cancelRequest, turnEnded and closeSession on the local
// in-flight table and answers immediately with ok:true. These frames are never
// stale, never unavailable and never wait for the sidecar.
func (h *zcodeBrowserHost) lifecycle(p *zcodeBrowserParams, cmd *zcodeBrowserCommand) zcodeBrowserEnvelope {
	started := time.Now()
	sessionID := cmd.SessionID
	if sessionID == "" {
		sessionID = p.SessionID
	}
	turnID := cmd.TurnID
	if turnID == "" {
		turnID = p.TurnID
	}
	switch cmd.Method {
	case "cancelRequest":
		h.cancelMatching(func(r *zcodeBrowserRequest) bool { return r.requestID == cmd.RequestID })
	case "turnEnded":
		// Session and turn must both match; absent on both sides counts as a match.
		h.cancelMatching(func(r *zcodeBrowserRequest) bool { return r.sessionID == sessionID && r.turnID == turnID })
	case "closeSession":
		h.cancelMatching(func(r *zcodeBrowserRequest) bool { return r.sessionID == sessionID })
		h.notify("closeSession", sessionID)
	}
	return zcodeBrowserEnvelope{OK: true, ElapsedMS: time.Since(started).Milliseconds()}
}

// cancelMatching settles every in-flight request that matches and asks the
// sidecar, best effort and without blocking, to stop the matching work.
func (h *zcodeBrowserHost) cancelMatching(match func(*zcodeBrowserRequest) bool) {
	type abortTarget struct {
		proc      *zcodeSidecarProc
		requestID string
	}
	h.mu.Lock()
	targets := make([]abortTarget, 0, 1)
	for _, r := range h.inflight {
		if r.cancelled || !match(r) {
			continue
		}
		h.settleLocked(r)
		targets = append(targets, abortTarget{proc: r.proc, requestID: r.requestID})
	}
	h.mu.Unlock()
	for _, target := range targets {
		if target.proc != nil {
			h.sendTo(target.proc, zcodeSidecarFrame{Method: "cancel", RequestID: target.requestID})
		}
	}
}

// notify sends a frame to the current sidecar without waiting for a reply.
func (h *zcodeBrowserHost) notify(method, sessionID string) {
	h.mu.Lock()
	proc := h.proc
	h.mu.Unlock()
	if proc != nil {
		h.sendTo(proc, zcodeSidecarFrame{Method: method, SessionID: sessionID})
	}
}

// sendTo assigns a protocol id and queues one frame. It never blocks.
func (h *zcodeBrowserHost) sendTo(proc *zcodeSidecarProc, frame zcodeSidecarFrame) bool {
	h.mu.Lock()
	h.nextID++
	frame.ID = h.nextID
	h.mu.Unlock()
	data, err := json.Marshal(frame)
	if err != nil {
		return false
	}
	return proc.enqueue(append(data, '\n'))
}

type zcodeBrowserOutcome int

const (
	zcodeBrowserOutcomeReply zcodeBrowserOutcome = iota
	zcodeBrowserOutcomeCancelled
	zcodeBrowserOutcomeExited
	zcodeBrowserOutcomeDeadline
)

// roundTrip sends one frame and waits for its reply, the request's
// cancellation, the sidecar's exit or the caller's deadline.
func (h *zcodeBrowserHost) roundTrip(ctx context.Context, r *zcodeBrowserRequest, proc *zcodeSidecarProc, frame zcodeSidecarFrame) (zcodeSidecarReply, zcodeBrowserOutcome) {
	h.mu.Lock()
	if r.cancelled {
		h.mu.Unlock()
		return zcodeSidecarReply{}, zcodeBrowserOutcomeCancelled
	}
	h.nextID++
	frame.ID = h.nextID
	r.sidecarID = frame.ID
	r.proc = proc
	h.pending[frame.ID] = r
	h.mu.Unlock()

	outcome := zcodeBrowserOutcomeExited
	var reply zcodeSidecarReply
	if data, err := json.Marshal(frame); err == nil && proc.enqueue(append(data, '\n')) {
		reply, outcome = h.waitReply(ctx, r, proc)
	}

	h.mu.Lock()
	if h.pending[r.sidecarID] == r {
		delete(h.pending, r.sidecarID)
	}
	h.mu.Unlock()
	return reply, outcome
}

func (h *zcodeBrowserHost) waitReply(ctx context.Context, r *zcodeBrowserRequest, proc *zcodeSidecarProc) (zcodeSidecarReply, zcodeBrowserOutcome) {
	// A reply that raced with a cancel still wins when the sidecar already finished.
	select {
	case reply := <-r.reply:
		return reply, zcodeBrowserOutcomeReply
	default:
	}
	select {
	case reply := <-r.reply:
		return reply, zcodeBrowserOutcomeReply
	case <-r.done:
		select {
		case reply := <-r.reply:
			return reply, zcodeBrowserOutcomeReply
		default:
		}
		return zcodeSidecarReply{}, zcodeBrowserOutcomeCancelled
	case <-proc.exited:
		return zcodeSidecarReply{}, zcodeBrowserOutcomeExited
	case <-ctx.Done():
		return zcodeSidecarReply{}, zcodeBrowserOutcomeDeadline
	}
}

// interrupted converts a non-reply outcome into the envelope the client gets.
func (h *zcodeBrowserHost) interrupted(r *zcodeBrowserRequest, outcome zcodeBrowserOutcome) zcodeBrowserEnvelope {
	switch outcome {
	case zcodeBrowserOutcomeCancelled:
		return zcodeBrowserFailure(zcodeBrowserCodeCancelled, "Browser command cancelled", r.uncertain(), r.started)
	case zcodeBrowserOutcomeExited:
		return zcodeBrowserFailure(zcodeBrowserCodeBackendUnavailable, "zcode browser sidecar exited", "", r.started)
	default:
		return zcodeBrowserFailure(zcodeBrowserCodeTimeout, "zcode browser request timed out", r.uncertain(), r.started)
	}
}

// ensureBrowser makes sure the sidecar's Chromium is running. It is skipped
// once this sidecar has reported a live browser. On a deadline the sidecar is
// recycled and returned so the caller can reply first and tear down after.
func (h *zcodeBrowserHost) ensureBrowser(ctx context.Context, proc *zcodeSidecarProc, r *zcodeBrowserRequest) (env zcodeBrowserEnvelope, failed bool, recycle *zcodeSidecarProc) {
	h.mu.Lock()
	up := h.browserUp && h.proc == proc
	h.mu.Unlock()
	if up {
		return zcodeBrowserEnvelope{}, false, nil
	}
	reply, outcome := h.roundTrip(ctx, r, proc, zcodeSidecarFrame{Method: "ensureBrowser"})
	if outcome == zcodeBrowserOutcomeReply && reply.Error == nil {
		h.mu.Lock()
		if h.proc == proc {
			h.browserUp = true
			if reply.Generation > 0 {
				h.generation = reply.Generation
			}
		}
		h.mu.Unlock()
		return zcodeBrowserEnvelope{}, false, nil
	}
	if outcome != zcodeBrowserOutcomeReply {
		var recycle *zcodeSidecarProc
		if outcome == zcodeBrowserOutcomeDeadline && h.take(proc) {
			recycle = proc
		}
		return h.interrupted(r, outcome), true, recycle
	}
	h.fail(zcodeBrowserReasonBrowserStartFailed)
	code := reply.Error.Code
	if code == "" {
		code = zcodeBrowserCodeBackendUnavailable
	}
	return zcodeBrowserFailure(code, reply.Error.Message, "", r.started), true, nil
}

// exec sends one command frame and passes the result through the whitelist.
func (h *zcodeBrowserHost) exec(ctx context.Context, proc *zcodeSidecarProc, r *zcodeBrowserRequest, frame zcodeSidecarFrame) (env zcodeBrowserEnvelope, recycle *zcodeSidecarProc) {
	reply, outcome := h.roundTrip(ctx, r, proc, frame)
	if outcome != zcodeBrowserOutcomeReply {
		if outcome == zcodeBrowserOutcomeDeadline && h.take(proc) {
			return h.interrupted(r, outcome), proc
		}
		return h.interrupted(r, outcome), nil
	}
	if reply.Error != nil {
		code := reply.Error.Code
		if code == "" {
			code = zcodeBrowserCodeExecutionError
		}
		return zcodeBrowserFailure(code, reply.Error.Message, "", r.started), nil
	}
	env, err := zcodeBrowserWhitelist(reply.Result)
	if err != nil {
		return zcodeBrowserFailure(zcodeBrowserCodeExecutionError, "zcode browser sidecar returned an unreadable result", "", r.started), nil
	}
	if env.ElapsedMS == 0 {
		env.ElapsedMS = time.Since(r.started).Milliseconds()
	}
	return env, nil
}

// execute runs one browserExecute to completion: it registers the request,
// makes sure a sidecar with a live browser exists and dispatches the command.
// A non-empty reason means the failure is worth the per-run warning.
func (h *zcodeBrowserHost) execute(ctx context.Context, r *zcodeBrowserRequest, frame zcodeSidecarFrame) (env zcodeBrowserEnvelope, reason string, recycle *zcodeSidecarProc) {
	ctx, cancel := context.WithTimeout(ctx, h.opts.deadline())
	defer cancel()

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return zcodeBrowserUnavailableEnvelope(zcodeBrowserReasonClosed, r.started), zcodeBrowserReasonClosed, nil
	}
	if cached := h.unavailable; cached != "" {
		h.mu.Unlock()
		return zcodeBrowserUnavailableEnvelope(cached, r.started), cached, nil
	}
	if _, duplicate := h.inflight[r.requestID]; duplicate {
		h.mu.Unlock()
		return zcodeBrowserFailure(zcodeBrowserCodeDuplicateRequestID, "Browser request '"+r.requestID+"' is already in flight", "", r.started), "", nil
	}
	h.inflight[r.requestID] = r
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.inflight, r.requestID)
		h.mu.Unlock()
	}()

	proc, err := h.start(ctx)
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			// The request ran out of time while the sidecar was still starting.
			// The start handshake itself is bounded by the watchdog.
			return zcodeBrowserFailure(zcodeBrowserCodeTimeout, "zcode browser request timed out", r.uncertain(), r.started), "", nil
		}
		reason = h.startFailureReason(err)
		return zcodeBrowserUnavailableEnvelope(reason, r.started), reason, nil
	}
	if env, failed, taken := h.ensureBrowser(ctx, proc, r); failed {
		switch {
		case env.Error != nil && env.Error.Code == zcodeBrowserCodeCancelled:
			return env, zcodeBrowserReasonClosed, taken
		case taken != nil:
			// The deadline expired: a transient failure, the sidecar is recycled.
			return env, "", taken
		default:
			return env, zcodeBrowserReasonBrowserStartFailed, taken
		}
	}
	env, recycle = h.exec(ctx, proc, r, frame)
	return env, "", recycle
}

// list returns the descriptor, or the reason browser use is unavailable.
func (h *zcodeBrowserHost) list(ctx context.Context) (zcodeBrowserDescriptor, string) {
	ctx, cancel := context.WithTimeout(ctx, h.opts.deadline())
	defer cancel()
	proc, err := h.start(ctx)
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return zcodeBrowserDescriptor{}, zcodeBrowserReasonSidecarStartFailed
		}
		return zcodeBrowserDescriptor{}, h.startFailureReason(err)
	}
	if env, failed, recycle := h.ensureBrowser(ctx, proc, newZcodeBrowserRequest("", "", "", false)); failed {
		if recycle != nil {
			h.teardown(recycle)
		}
		if env.Error != nil && env.Error.Code == zcodeBrowserCodeCancelled {
			return zcodeBrowserDescriptor{}, zcodeBrowserReasonClosed
		}
		return zcodeBrowserDescriptor{}, zcodeBrowserReasonBrowserStartFailed
	}
	return h.descriptor(), ""
}

// descriptor is only ever produced after Chromium really started.
func (h *zcodeBrowserHost) descriptor() zcodeBrowserDescriptor {
	h.mu.Lock()
	defer h.mu.Unlock()
	return zcodeBrowserDescriptor{
		ID:                  h.browserID,
		Generation:          h.generation,
		Type:                "cdp",
		Name:                zcodeBrowserName,
		Capabilities:        zcodeBrowserCaps{Browser: []any{}, Tab: []any{}},
		APISupportOverrides: zcodeBrowserSupportOverrides,
		Metadata:            map[string]string{"provider": "multica", "launchMode": "docker", "headless": "true"},
	}
}

// startFailureReason maps a start error to the reason that was cached for it.
func (h *zcodeBrowserHost) startFailureReason(err error) string {
	if errors.Is(err, errZcodeBrowserClosed) {
		return zcodeBrowserReasonClosed
	}
	h.mu.Lock()
	reason := h.unavailable
	h.mu.Unlock()
	if reason == "" {
		reason = zcodeBrowserReasonSidecarStartFailed
	}
	return reason
}

// quickUnavailable reports a cached unavailability reason without probing or
// starting anything.
func (h *zcodeBrowserHost) quickUnavailable(noSession bool) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.closed:
		return zcodeBrowserReasonClosed
	case h.unavailable != "":
		return h.unavailable
	case noSession:
		return zcodeBrowserReasonNoSession
	}
	return ""
}

// staleReason mirrors the official runtime's fast staleness check on the
// backend id and generation the client last saw.
func (h *zcodeBrowserHost) staleReason(browserID string, generation *int) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if (browserID == "" || browserID == h.browserID) && (generation == nil || *generation == h.generation) {
		return ""
	}
	shownID, shownGeneration := browserID, h.generation
	if shownID == "" {
		shownID = h.browserID
	}
	if generation != nil {
		shownGeneration = *generation
	}
	return fmt.Sprintf("Browser backend '%s' generation %d is stale", shownID, shownGeneration)
}

// reportUnavailable logs the single per-run warning for an unavailable browser.
func (h *zcodeBrowserHost) reportUnavailable(reason string) {
	if reason == "" || reason == zcodeBrowserReasonClosed {
		return
	}
	h.mu.Lock()
	first := !h.warned
	h.warned = true
	h.mu.Unlock()
	if first {
		h.logger.Warn("zcode browser use unavailable", "mode", h.opts.mode(), "reason", reason, "hint", h.opts.hint(reason))
	}
}

func zcodeBrowserUnavailableEnvelope(reason string, started time.Time) zcodeBrowserEnvelope {
	message := "zcode browser use unavailable: " + reason
	if reason == zcodeBrowserReasonClosed {
		message = "zcode browser backend is closed for this run"
	}
	return zcodeBrowserFailure(zcodeBrowserCodeBackendUnavailable, message, "", started)
}

// --- Client glue -----------------------------------------------------------------

// newZcodeBackend builds the ZCode backend. The browser-use settings are
// resolved here, once, from the daemon's own environment.
func newZcodeBackend(cfg Config) *zcodeBackend {
	return &zcodeBackend{cfg: cfg, browser: zcodeBrowserOptionsFromEnv(os.LookupEnv, cfg.Logger)}
}

func (c *zcodeClient) setSessionID(sessionID string) {
	c.mu.Lock()
	c.sessionID = sessionID
	c.mu.Unlock()
}

// browserReady reports whether the session handshake completed: browser
// requests are only honoured for a session this run owns.
func (c *zcodeClient) browserReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID != ""
}

func (c *zcodeClient) closeBrowser() {
	if c.browser != nil {
		c.browser.close()
	}
}

// dispatchBrowser answers one interaction/browser* request from app-server.
// It runs on its own goroutine so the reader loop is never blocked.
func (c *zcodeClient) dispatchBrowser(id json.RawMessage, method string, params json.RawMessage) {
	var p zcodeBrowserParams
	if err := json.Unmarshal(params, &p); err != nil {
		c.respondError(id, zcodeBrowserInvalidParams, fmt.Sprintf("invalid params: %s requires a params object", method))
		return
	}
	// Malformed frames are rejected before anything is probed or started.
	if p.RequestID == "" || p.SessionID == "" {
		c.respondError(id, zcodeBrowserInvalidParams, fmt.Sprintf("invalid params: %s requires requestId and sessionId", method))
		return
	}
	if method == zcodeBrowserMethodList {
		c.browserList(id)
		return
	}
	var cmd zcodeBrowserCommand
	if len(p.Command) == 0 || json.Unmarshal(p.Command, &cmd) != nil || cmd.Method == "" {
		c.respondError(id, zcodeBrowserInvalidParams, "invalid params: "+zcodeBrowserMethodExecute+" requires command.method")
		return
	}
	c.browserExecute(id, &p, &cmd)
}

func (c *zcodeClient) browserList(id json.RawMessage) {
	h := c.browser
	desc, reason := h.listFor(c.browserReady())
	if reason != "" {
		h.reportUnavailable(reason)
		zcodeBrowserEmit(zcodeBrowserTraceEvent{Kind: "unavailable", Method: zcodeBrowserMethodList, Code: reason})
		c.respond(id, zcodeBrowserListResult{Browsers: []zcodeBrowserDescriptor{}})
		return
	}
	zcodeBrowserEmit(zcodeBrowserTraceEvent{Kind: "list", Method: zcodeBrowserMethodList, Browsers: 1})
	c.respond(id, zcodeBrowserListResult{Browsers: []zcodeBrowserDescriptor{desc}})
}

// listFor answers a browserList: an empty list before the handshake, when
// browser use is off or unavailable, or when the sidecar cannot be started.
func (h *zcodeBrowserHost) listFor(hasSession bool) (zcodeBrowserDescriptor, string) {
	if reason := h.quickUnavailable(!hasSession); reason != "" {
		return zcodeBrowserDescriptor{}, reason
	}
	return h.list(context.Background())
}

func (c *zcodeClient) browserExecute(id json.RawMessage, p *zcodeBrowserParams, cmd *zcodeBrowserCommand) {
	h := c.browser
	if zcodeBrowserIsLifecycle(cmd.Method) {
		c.respond(id, h.lifecycle(p, cmd))
		return
	}
	started := time.Now()
	if reason := h.quickUnavailable(!c.browserReady()); reason != "" {
		h.reportUnavailable(reason)
		zcodeBrowserEmit(zcodeBrowserTraceEvent{Kind: "unavailable", Method: zcodeBrowserMethodExecute, Command: cmd.Method, Code: reason})
		c.respond(id, zcodeBrowserUnavailableEnvelope(reason, started))
		return
	}
	if stale := h.staleReason(p.BrowserID, p.BrowserGeneration); stale != "" {
		c.respond(id, zcodeBrowserFailure(zcodeBrowserCodeBackendUnavailable, stale, "", started))
		return
	}
	if zcodeBrowserGoUnsupported(cmd.Method) {
		c.respond(id, zcodeBrowserFailure(zcodeBrowserCodeCapabilityUnsupported,
			fmt.Sprintf("Browser command '%s' is unavailable in the managed CDP page runtime", cmd.Method), "", started))
		return
	}
	r := newZcodeBrowserRequest(p.RequestID, p.SessionID, p.TurnID, zcodeBrowserHasSideEffects(cmd))
	frame := zcodeSidecarFrame{
		Method:            "exec",
		SessionID:         p.SessionID,
		RequestID:         p.RequestID,
		TurnID:            p.TurnID,
		BrowserGeneration: p.BrowserGeneration,
		Command:           p.Command,
	}
	env, reason, recycle := h.execute(context.Background(), r, frame)
	if reason != "" {
		h.reportUnavailable(reason)
		zcodeBrowserEmit(zcodeBrowserTraceEvent{Kind: "unavailable", Method: zcodeBrowserMethodExecute, Command: cmd.Method, Code: reason})
	}
	zcodeBrowserTraceExecute(cmd, env)
	c.respond(id, env)
	if recycle != nil {
		// The sidecar outlived its deadline. The reply is out; now recycle it so
		// the next request relaunches with the next generation.
		h.teardown(recycle)
	}
}

func zcodeBrowserTraceExecute(cmd *zcodeBrowserCommand, env zcodeBrowserEnvelope) {
	ev := zcodeBrowserTraceEvent{Kind: "execute", Method: zcodeBrowserMethodExecute, Command: cmd.Method, OK: env.OK}
	if env.Error != nil {
		ev.Code = env.Error.Code
	}
	playwrightAction := ""
	if cmd.Method == "playwright" && cmd.Action != nil {
		playwrightAction = cmd.Action.Name
	}
	switch {
	case cmd.Method == "screenshot" || playwrightAction == "elementScreenshot":
		if env.OK {
			ev.Image = zcodeBrowserDecodeImage(env.Image)
		}
	case cmd.Method == "navigate":
		ev.URLHost = zcodeBrowserURLHost(cmd.URL)
	case cmd.Method == "snapshot":
		if env.OK {
			ev.DOM = zcodeBrowserRenderSnapshot(env.Snapshot)
		}
	case playwrightAction == "domSnapshot":
		if env.OK {
			ev.DOM = zcodeBrowserStringValue(env.Value)
		}
	}
	zcodeBrowserEmit(ev)
}

func zcodeBrowserDecodeImage(raw json.RawMessage) []byte {
	var image struct {
		Base64 string `json:"base64"`
	}
	if json.Unmarshal(raw, &image) != nil || image.Base64 == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(image.Base64)
	if err != nil {
		return nil
	}
	return decoded
}

// zcodeBrowserURLHost keeps only the host name: a URL's path and query never
// reach a trace.
func zcodeBrowserURLHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// zcodeBrowserRenderSnapshot turns a snapshot into short reference lines so a
// test can check what the page exposed without keeping the whole payload.
func zcodeBrowserRenderSnapshot(raw json.RawMessage) string {
	var snapshot struct {
		Elements []struct {
			Ref   string `json:"ref"`
			Tag   string `json:"tag"`
			Role  string `json:"role"`
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"elements"`
	}
	if json.Unmarshal(raw, &snapshot) != nil {
		return ""
	}
	var out strings.Builder
	for _, element := range snapshot.Elements {
		fmt.Fprintf(&out, "%s <%s> role=%s name=%q", element.Ref, element.Tag, element.Role, zcodeTruncate(element.Name, 80))
		// Only the length is recorded: an input's value may be sensitive.
		if element.Value != "" {
			fmt.Fprintf(&out, " value=%d chars", len(element.Value))
		}
		out.WriteByte('\n')
	}
	return zcodeTruncate(out.String(), zcodeBrowserTraceTextLimit)
}

func zcodeBrowserStringValue(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return zcodeTruncate(value, zcodeBrowserTraceTextLimit)
}
