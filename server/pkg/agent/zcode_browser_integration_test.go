//go:build agentintegration

package agent

// Integration tests for browser use. Both start the real sidecar (the
// playwright-core runtime inside the docker image) through the Go host; the
// default test build never compiles this file.
//
// TestZcodeBrowserExecutorIntegration drives the host from a scripted
// app-server and needs docker plus the image and the modules directory
// (MULTICA_ZCODE_BROWSER_MODULES). TestZcodeRealBrowserUseSmoke additionally
// lets the real ZCode CLI use the browser for one turn and is gated by
// MULTICA_RUN_REAL_AGENT_SMOKE=1.
//
// Neither test pulls an image or installs anything.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	zcodeBrowserIntegrationEnv        = "MULTICA_ZCODE_BROWSER_INTEGRATION"
	zcodeBrowserIntegrationTargetEnv  = "MULTICA_ZCODE_BROWSER_TARGET_URL"
	zcodeBrowserScreenshotPathEnv     = "MULTICA_ZCODE_BROWSER_SCREENSHOT_PATH"
	zcodeBrowserIntegrationDockerHost = "host.docker.internal"

	zcodeBrowserSmokePathEnv  = "MULTICA_ZCODE_PATH"
	zcodeBrowserSmokeModelEnv = "MULTICA_ZCODE_SMOKE_MODEL"

	zcodeBrowserIntegrationReplyBudget = 90 * time.Second
	zcodeBrowserIntegrationLeftoverTTL = 15 * time.Second
	zcodeBrowserIntegrationSentinel    = "multica-browser-sentinel-7f3a"
	zcodeBrowserIntegrationSession     = "s"
	zcodeBrowserIntegrationTurn        = "t1"

	// The reply the host gives to a server-to-client method it does not know.
	zcodeBrowserIntegrationUnsupported = "method not supported by multica: interaction/browser"
)

var zcodeBrowserPNGSignature = []byte("\x89PNG\r\n\x1a\n")

// zcodeBrowserIntegration wires a zcodeClient to a browser host without any
// test seam: its sidecar is the real one, started through docker.
type zcodeBrowserIntegration struct {
	t      *testing.T
	opts   zcodeBrowserOptions
	docker string
	host   *zcodeBrowserHost
	client *zcodeClient
	stdin  *bytes.Buffer

	// extra holds frames read before the one a test waits for.
	extra []string
}

// newZcodeBrowserIntegration resolves the options from the environment and
// skips when docker, the image or the modules directory is not available. It
// only ever runs behind an explicit opt-in, so nothing here runs by default.
func newZcodeBrowserIntegration(t *testing.T) *zcodeBrowserIntegration {
	t.Helper()
	opts := zcodeBrowserOptionsFromEnv(os.LookupEnv, nil)
	if !opts.enabled() {
		t.Skipf("%s is off; browser integration needs it set to auto or docker", zcodeBrowserEnvMode)
	}
	if !zcodeBrowserFileExists(filepath.Join(opts.ModulesDir, "node_modules", "playwright-core", "package.json")) {
		t.Skipf("set %s to a directory whose node_modules holds playwright-core", zcodeBrowserEnvModules)
	}
	docker, err := zcodeDockerBinPath(opts.DockerBin, exec.LookPath)
	if err != nil {
		t.Skipf("docker CLI not found: %v", err)
	}
	if err := zcodeDockerProbeImage(docker, opts.image()); err != nil {
		t.Skipf("docker image %s is not present locally and this test never pulls: %v", opts.image(), err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := &zcodeBrowserIntegration{t: t, opts: opts, docker: docker, stdin: &bytes.Buffer{}}
	h.host = newZcodeBrowserHost(opts, logger)
	h.client = &zcodeClient{
		cfg:         Config{Logger: logger},
		stdin:       h.stdin,
		pending:     make(map[int]*pendingRPC),
		processDone: make(chan struct{}),
		browserOpts: opts,
		browser:     h.host,
	}
	h.client.setSessionID(zcodeBrowserIntegrationSession)
	t.Cleanup(h.host.close)
	return h
}

// drain returns every complete frame the client wrote so far, in order.
func (h *zcodeBrowserIntegration) drain() []string {
	h.client.writeMu.Lock()
	buffered := h.stdin.String()
	h.stdin.Reset()
	h.client.writeMu.Unlock()
	chunks := append(h.extra, buffered)
	h.extra = nil
	var lines []string
	for _, chunk := range chunks {
		for _, line := range strings.Split(chunk, "\n") {
			if line != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

// waitReply returns the frame with the given id. A sidecar start and a page
// load both happen inside the first request, so the budget is generous.
func (h *zcodeBrowserIntegration) waitReply(id string) string {
	h.t.Helper()
	want := `"` + id + `"`
	deadline := time.Now().Add(zcodeBrowserIntegrationReplyBudget)
	for {
		// Frames after the wanted one are kept: a cancel settles its target
		// before it answers itself, so the two replies can arrive together.
		var found string
		for _, line := range h.drain() {
			if found == "" && zcodeBrowserTestFrameID(h.t, line) == want {
				found = line
				continue
			}
			h.extra = append(h.extra, line)
		}
		if found != "" {
			return found
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for the reply to %s; earlier frames: %q", id, zcodeBrowserShortFrames(h.extra))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func zcodeBrowserShortFrames(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, zcodeTruncate(line, 200))
	}
	return out
}

func (h *zcodeBrowserIntegration) execute(id, command string) zcodeBrowserEnvelope {
	h.t.Helper()
	h.client.handleLine(zcodeBrowserTestExecuteFrame(id, id, zcodeBrowserIntegrationSession, zcodeBrowserIntegrationTurn, command))
	return zcodeBrowserTestResult(h.t, zcodeBrowserTestParse(h.t, h.waitReply(id)))
}

// assertNoLeftovers fails when a browser container or a container named like
// one is still known to docker.
func (h *zcodeBrowserIntegration) assertNoLeftovers() {
	h.t.Helper()
	zcodeBrowserAssertNoLeftovers(h.t, h.docker)
}

func zcodeBrowserAssertNoLeftovers(t *testing.T, docker string) {
	t.Helper()
	filters := []string{
		"label=" + zcodeBrowserContainerLabel + "=1",
		"name=" + zcodeBrowserContainerPrefix,
	}
	deadline := time.Now().Add(zcodeBrowserIntegrationLeftoverTTL)
	for {
		leftover := ""
		for _, filter := range filters {
			out, err := zcodeDockerRun(docker, "ps", "-aq", "--filter", filter)
			if err != nil {
				t.Fatalf("docker ps --filter %s: %v", filter, err)
			}
			if text := strings.TrimSpace(string(out)); text != "" {
				leftover += fmt.Sprintf(" %s:%s", filter, zcodeContainerShortName(text))
			}
		}
		if leftover == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("browser containers were left behind:%s", leftover)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// zcodeBrowserContainerURL rewrites a local test server URL to the name the
// container uses for the host.
func zcodeBrowserContainerURL(serverURL, path string) string {
	u, err := url.Parse(serverURL)
	if err != nil {
		panic("httptest returned an unparsable URL: " + err.Error())
	}
	u.Host = net.JoinHostPort(zcodeBrowserIntegrationDockerHost, u.Port())
	u.Path = path
	return u.String()
}

// newZcodeBrowserIntegrationServer serves one page with a sentinel and one
// slow endpoint that answers only once its client went away.
func newZcodeBrowserIntegrationServer(t *testing.T) (page string, slowStarted <-chan struct{}) {
	t.Helper()
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow":
			select {
			case started <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
			return
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html><head><title>%[1]s</title></head><body><h1>%[1]s</h1><input aria-label="query" name="q"><button>Go</button></body></html>`, zcodeBrowserIntegrationSentinel)
		}
	}))
	t.Cleanup(server.Close)
	return zcodeBrowserContainerURL(server.URL, "/"), started
}

// TestZcodeBrowserExecutorIntegration runs the real sidecar: one container,
// a real page load, the blocked and unsupported answers, cancellation, and a
// clean shutdown that leaves nothing in docker.
func TestZcodeBrowserExecutorIntegration(t *testing.T) {
	if os.Getenv(zcodeBrowserIntegrationEnv) != "docker" {
		t.Skipf("set %s=docker to run the real browser sidecar in docker", zcodeBrowserIntegrationEnv)
	}
	h := newZcodeBrowserIntegration(t)
	page, slowStarted := newZcodeBrowserIntegrationServer(t)

	// Listing starts the sidecar and answers with this run's descriptor.
	h.client.handleLine(zcodeBrowserTestListFrame("l1", "r1", zcodeBrowserIntegrationSession))
	list := zcodeBrowserTestList(t, h.waitReply("l1"))
	if len(list.Browsers) != 1 {
		t.Fatalf("expected exactly one browser, got %d: %+v", len(list.Browsers), list.Browsers)
	}
	descriptor := list.Browsers[0]
	if descriptor.Type != "cdp" || !strings.HasPrefix(descriptor.ID, "cdp:") || descriptor.Generation != 1 {
		t.Fatalf("unexpected descriptor: %+v", descriptor)
	}
	if descriptor.Metadata["provider"] != "multica" || descriptor.Metadata["launchMode"] != "docker" || descriptor.Metadata["headless"] != "true" {
		t.Fatalf("unexpected descriptor metadata: %+v", descriptor.Metadata)
	}
	if len(descriptor.Capabilities.Browser) != 0 || len(descriptor.Capabilities.Tab) != 0 || len(descriptor.APISupportOverrides) != len(zcodeBrowserSupportOverrides) {
		t.Fatalf("unexpected descriptor capabilities: %+v", descriptor)
	}
	t.Logf("descriptor: id prefix %q generation %d type %q", descriptor.ID[:len("cdp:")], descriptor.Generation, descriptor.Type)

	// A real page load, read back through state, snapshot and screenshot.
	env := h.execute("n1", fmt.Sprintf(`{"method":"navigate","url":%q}`, page))
	if !env.OK || env.Error != nil {
		t.Fatalf("navigate failed: %+v", env)
	}
	var state struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(env.State, &state); err != nil || state.URL != page {
		t.Fatalf("navigate returned state %s, want the page url", env.State)
	}

	env = h.execute("s1", `{"method":"snapshot"}`)
	if !env.OK || !strings.Contains(string(env.Snapshot), zcodeBrowserIntegrationSentinel) {
		t.Fatalf("snapshot must contain the sentinel: ok=%v error=%+v snapshot=%s", env.OK, env.Error, zcodeTruncate(string(env.Snapshot), 600))
	}
	t.Logf("snapshot: %d bytes, sentinel present", len(env.Snapshot))

	env = h.execute("p1", `{"method":"screenshot"}`)
	png := zcodeBrowserDecodeImage(env.Image)
	if !env.OK || !bytes.HasPrefix(png, zcodeBrowserPNGSignature) {
		t.Fatalf("screenshot must be a PNG: ok=%v error=%+v bytes=%d", env.OK, env.Error, len(png))
	}
	t.Logf("screenshot: %d bytes, PNG signature ok", len(png))

	// Disallowed and unknown commands are refused with their own codes.
	env = h.execute("f1", `{"method":"navigate","url":"file:///not-allowed.html"}`)
	if env.OK || env.Error == nil || env.Error.Code != "navigation_blocked" || env.Error.SideEffect != "" {
		t.Fatalf("a file URL must be refused without side effects: %+v", env)
	}
	env = h.execute("c0", `{"method":"capabilities"}`)
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeCapabilityUnsupported {
		t.Fatalf("capabilities must be unsupported: %+v", env)
	}

	if target := os.Getenv(zcodeBrowserIntegrationTargetEnv); target != "" {
		h.visit(t, "x1", target)
	}

	// Cancelling a request aborts the load it is waiting for.
	h.client.handleLine(zcodeBrowserTestExecuteFrame("n2", "n2", zcodeBrowserIntegrationSession, zcodeBrowserIntegrationTurn,
		fmt.Sprintf(`{"method":"navigate","url":%q}`, zcodeBrowserContainerURL(page, "/slow"))))
	select {
	case <-slowStarted:
	case <-time.After(zcodeBrowserIntegrationReplyBudget):
		t.Fatal("the browser never requested the slow page")
	}
	h.client.handleLine(zcodeBrowserTestExecuteFrame("k1", "k1", zcodeBrowserIntegrationSession, zcodeBrowserIntegrationTurn, `{"method":"cancelRequest","requestId":"n2"}`))
	if lifecycle := h.waitReply("k1"); !strings.Contains(lifecycle, `"ok":true`) {
		t.Fatalf("cancelRequest must answer ok, got %s", lifecycle)
	}
	env = zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitReply("n2")))
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeCancelled || env.Error.SideEffect != "uncertain" {
		t.Fatalf("a cancelled navigate must report cancelled with an uncertain side effect: %+v", env)
	}

	h.client.handleLine(zcodeBrowserTestExecuteFrame("e1", "e1", zcodeBrowserIntegrationSession, zcodeBrowserIntegrationTurn,
		fmt.Sprintf(`{"method":"closeSession","sessionId":%q}`, zcodeBrowserIntegrationSession)))
	closed := zcodeBrowserTestParse(t, h.waitReply("e1"))
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(closed.Result, &keys); err != nil || len(keys) != 2 || string(keys["ok"]) != "true" {
		t.Fatalf("closeSession must answer ok with no other result key, got %s", zcodeTruncate(string(closed.Result), 200))
	}

	// The host removes the container before close returns.
	h.host.close()
	h.assertNoLeftovers()
}

// visit loads an external page and reads it back the same way.
func (h *zcodeBrowserIntegration) visit(t *testing.T, id, target string) {
	t.Helper()
	env := h.execute(id+"n", fmt.Sprintf(`{"method":"navigate","url":%q}`, target))
	if !env.OK || env.Error != nil {
		t.Fatalf("navigate to the target failed: %+v", env)
	}
	env = h.execute(id+"s", `{"method":"snapshot"}`)
	if !env.OK || len(env.Snapshot) == 0 {
		t.Fatalf("snapshot of the target failed: %+v", env)
	}
	snapshotBytes := len(env.Snapshot)
	env = h.execute(id+"p", `{"method":"screenshot"}`)
	if !env.OK || !bytes.HasPrefix(zcodeBrowserDecodeImage(env.Image), zcodeBrowserPNGSignature) {
		t.Fatalf("screenshot of the target failed: %+v", env)
	}
	t.Logf("target: navigated, snapshot %d bytes, screenshot ok", snapshotBytes)
}

// zcodeBrowserSmokePath returns the ZCode CLI to smoke test, or skips.
func zcodeBrowserSmokePath(t *testing.T) string {
	t.Helper()
	path := os.Getenv(zcodeBrowserSmokePathEnv)
	if path == "" {
		discovered, err := exec.LookPath("zcode")
		if err != nil {
			t.Skip("zcode not on PATH and MULTICA_ZCODE_PATH unset; skipping the browser use smoke test")
		}
		path = discovered
	}
	return path
}

// TestZcodeRealBrowserUseSmoke lets the real ZCode CLI use the managed browser
// for one turn. It is gated by MULTICA_RUN_REAL_AGENT_SMOKE=1 before anything
// is looked up, and it reads the browser traffic through the trace hook.
func TestZcodeRealBrowserUseSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping the real-CLI smoke test in -short mode")
	}
	target := os.Getenv(zcodeBrowserIntegrationTargetEnv)
	if target == "" {
		t.Skipf("set %s to the page the smoke test should open", zcodeBrowserIntegrationTargetEnv)
	}
	targetURL, err := url.Parse(target)
	if err != nil || targetURL.Host == "" {
		t.Fatalf("%s must be an absolute URL: %v", zcodeBrowserIntegrationTargetEnv, err)
	}
	path := zcodeBrowserSmokePath(t)
	// The real backend resolves the same settings from the environment.
	options := zcodeBrowserOptionsFromEnv(os.LookupEnv, nil)
	if !options.enabled() || options.ModulesDir == "" {
		t.Skipf("the smoke test needs %s and %s", zcodeBrowserEnvMode, zcodeBrowserEnvModules)
	}
	docker, err := zcodeDockerBinPath(options.DockerBin, exec.LookPath)
	if err != nil {
		t.Skipf("docker CLI not found: %v", err)
	}
	if err := zcodeDockerProbeImage(docker, options.image()); err != nil {
		t.Skipf("docker image %s is not present locally and this test never pulls: %v", options.image(), err)
	}

	var (
		traceMu sync.Mutex
		events  []zcodeBrowserTraceEvent
	)
	zcodeBrowserTrace = func(ev zcodeBrowserTraceEvent) {
		traceMu.Lock()
		events = append(events, ev)
		traceMu.Unlock()
	}
	t.Cleanup(func() { zcodeBrowserTrace = nil })

	backend, err := New("zcode", Config{ExecutablePath: path, Logger: slog.Default(), TaskID: "task-1"})
	if err != nil {
		t.Fatalf("new zcode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// A headless app-server session has no default model, so pick one the
	// way the protocol smoke test does.
	model, thinkingLevel := zcodeBrowserSmokeModel(t, ctx, path)

	cwd := t.TempDir()
	session, err := backend.Execute(ctx, fmt.Sprintf(
		"Use the browser-use skill to open %s, read the page DOM, take a screenshot, then close the browser. Reply with the page title.",
		target), ExecOptions{
		Cwd:           cwd,
		Model:         model,
		ThinkingLevel: thinkingLevel,
		Timeout:       170 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	messages, result := collectZcodeSession(t, session)
	if result.Status != "completed" {
		t.Fatalf("the real zcode run did not complete: status=%q error=%q", result.Status, result.Error)
	}

	traceMu.Lock()
	copied := append([]zcodeBrowserTraceEvent(nil), events...)
	traceMu.Unlock()

	// Every browser method must have been answered by the host: a -32601
	// would only ever show up as text the agent repeated.
	for _, message := range messages {
		if strings.Contains(message.Content, zcodeBrowserIntegrationUnsupported) || strings.Contains(message.Output, zcodeBrowserIntegrationUnsupported) {
			t.Fatalf("a browser method reached the unsupported-method reply: %q", zcodeTruncate(message.Content+message.Output, 300))
		}
	}
	for _, event := range copied {
		if event.Code == "-32601" {
			t.Fatalf("a browser method was answered with -32601: %+v", event)
		}
	}

	lists, navigations, screenshots := 0, 0, 0
	for _, event := range copied {
		switch {
		case event.Kind == "list" && event.Browsers == 1:
			lists++
		case event.Kind == "execute" && event.Command == "navigate" && event.OK && event.URLHost == targetURL.Hostname():
			navigations++
		case event.Kind == "execute" && event.Command == "screenshot" && event.OK && len(event.Image) > 0:
			screenshots++
		}
		if event.DOM != "" {
			// Only the size is logged: a page can show personal data or
			// prefilled credentials, and this output is meant to be pasted
			// into an issue.
			t.Logf("page text captured: %d chars", len(event.DOM))
		}
		if screenshot := os.Getenv(zcodeBrowserScreenshotPathEnv); screenshot != "" && len(event.Image) > 0 {
			if err := os.WriteFile(screenshot, event.Image, 0o600); err != nil {
				t.Errorf("write screenshot: %v", err)
			}
		}
	}
	t.Logf("browser trace: %d events, %d lists with one browser, %d navigations to the target, %d screenshots", len(copied), lists, navigations, screenshots)
	if lists == 0 || navigations == 0 || screenshots == 0 {
		t.Fatalf("expected a list, a navigation to the target and a screenshot, got %d/%d/%d from %+v", lists, navigations, screenshots, zcodeBrowserShortEvents(copied))
	}

	zcodeBrowserAssertNoLeftovers(t, docker)
}

// zcodeBrowserShortEvents drops the bulky fields of a trace for a failure
// message.
func zcodeBrowserShortEvents(events []zcodeBrowserTraceEvent) []zcodeBrowserTraceEvent {
	out := make([]zcodeBrowserTraceEvent, 0, len(events))
	for _, event := range events {
		event.Image = nil
		event.DOM = ""
		out = append(out, event)
	}
	return out
}

// zcodeBrowserSmokeModel picks the model for the real turn: the one named by
// MULTICA_ZCODE_SMOKE_MODEL, or a reasoning-capable one from discovery.
func zcodeBrowserSmokeModel(t *testing.T, ctx context.Context, path string) (model, thinkingLevel string) {
	t.Helper()
	if model = os.Getenv(zcodeBrowserSmokeModelEnv); model != "" {
		// An explicit pick still needs a reasoning level the registry accepts
		// for that model; "low" is not universal.
		return model, zcodeResolveThinkingLevel(ctx, NewCommand(path, nil), model)
	}
	catalog, err := ListModels(ctx, "zcode", NewCommand(path, nil))
	if err != nil {
		t.Fatalf("discover zcode models: %v", err)
	}
	if len(catalog.Models) == 0 {
		t.Fatal("zcode model discovery returned no models; is the CLI logged in?")
	}
	for _, candidate := range catalog.Models {
		// zcode rejects a reasoning model without one of its own levels.
		if candidate.Thinking == nil || len(candidate.Thinking.SupportedLevels) == 0 {
			continue
		}
		model = candidate.ID
		thinkingLevel = candidate.Thinking.DefaultLevel
		if thinkingLevel == "" {
			thinkingLevel = candidate.Thinking.SupportedLevels[0].Value
		}
		break
	}
	if model == "" {
		model = catalog.Models[0].ID
	}
	t.Logf("smoke model from discovery: %s (thinking level %q)", model, thinkingLevel)
	return model, thinkingLevel
}
