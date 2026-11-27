package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// zcodeBrowserHarness wires a zcodeClient to a browser host whose sidecar is
// the fake sidecar in this package. Nothing here resolves or runs docker or
// node: the only child process is this test binary.
type zcodeBrowserHarness struct {
	t      *testing.T
	dir    string
	self   string
	opts   zcodeBrowserOptions
	host   *zcodeBrowserHost
	client *zcodeClient
	stdin  *bytes.Buffer

	// childEnv holds extra variables for the fake sidecar, such as its
	// ready delay. Set it before the first request.
	childEnv []string

	mu      sync.Mutex
	starts  []int
	names   []string
	scripts []string
	removed []string
	probes  int

	extra []string
}

var zcodeBrowserTestName = regexp.MustCompile(`^multica-zcode-browser-[0-9a-f]{12}$`)

func zcodeBrowserTestModules(t *testing.T, dir string) string {
	t.Helper()
	modules := filepath.Join(dir, "modules")
	if err := os.MkdirAll(filepath.Join(modules, "node_modules", "playwright-core"), 0o755); err != nil {
		t.Fatalf("create modules dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modules, "node_modules", "playwright-core", "package.json"), []byte(`{"name":"playwright-core"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	return modules
}

func newZcodeBrowserHarness(t *testing.T, scenario string, tune func(*zcodeBrowserOptions)) *zcodeBrowserHarness {
	t.Helper()
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	h := &zcodeBrowserHarness{t: t, dir: dir, self: self, stdin: &bytes.Buffer{}}
	opts := zcodeBrowserOptions{
		Mode:             zcodeBrowserModeDocker,
		ModulesDir:       zcodeBrowserTestModules(t, dir),
		StartTimeout:     10 * time.Second,
		Deadline:         10 * time.Second,
		CloseGrace:       3 * time.Second,
		OrphanSweepDelay: 3 * time.Second,
		Command: func(ctx context.Context, workDir, name, _ string, generation int) (*exec.Cmd, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.starts = append(h.starts, generation)
			h.names = append(h.names, name)
			h.scripts = append(h.scripts, workDir)
			cmd := exec.CommandContext(ctx, self, "-test.run=^TestZcodeBrowserFakeSidecarHelper$")
			cmd.Env = append([]string{
				zcodeTestBrowserFakeEnv + "=" + scenario,
				zcodeTestBrowserFakeDirEnv + "=" + dir,
				zcodeTestBrowserFakeGenerationEnv + "=" + strconv.Itoa(generation),
			}, h.childEnv...)
			return cmd, nil
		},
		Remove: func(name string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.removed = append(h.removed, name)
			return nil
		},
		Probe: func(string, string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.probes++
			return nil
		},
	}
	if tune != nil {
		tune(&opts)
	}
	logger := zcodeTestLogger()
	h.opts = opts
	h.host = newZcodeBrowserHost(opts, logger)
	h.client = &zcodeClient{
		cfg:         Config{Logger: logger},
		stdin:       h.stdin,
		pending:     make(map[int]*pendingRPC),
		processDone: make(chan struct{}),
		browserOpts: opts,
		browser:     h.host,
	}
	t.Cleanup(h.host.close)
	return h
}

func (h *zcodeBrowserHarness) startGenerations() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int(nil), h.starts...)
}

func (h *zcodeBrowserHarness) startNames() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.names...)
}

func (h *zcodeBrowserHarness) scriptDirs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.scripts...)
}

func (h *zcodeBrowserHarness) removedNames() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.removed...)
}

func (h *zcodeBrowserHarness) probeCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.probes
}

func zcodeBrowserWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// frames drains everything the client wrote so far, earliest first.
func (h *zcodeBrowserHarness) frames() []string {
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

// waitFor returns the frame with the given id; frames that arrive earlier
// are kept for later.
func (h *zcodeBrowserHarness) waitFor(id string) string {
	h.t.Helper()
	want := `"` + id + `"`
	var found string
	zcodeBrowserWaitFor(h.t, "reply "+id, func() bool {
		for _, line := range h.frames() {
			if found == "" && zcodeBrowserTestFrameID(h.t, line) == want {
				found = line
				continue
			}
			h.extra = append(h.extra, line)
		}
		return found != ""
	})
	return found
}

// waitFrames returns once at least n frames were written.
func (h *zcodeBrowserHarness) waitFrames(n int) []string {
	h.t.Helper()
	var lines []string
	zcodeBrowserWaitFor(h.t, fmt.Sprintf("%d frames", n), func() bool {
		lines = append(lines, h.frames()...)
		return len(lines) >= n
	})
	return lines
}

// expectQuiet asserts that nothing more is written to app-server.
func (h *zcodeBrowserHarness) expectQuiet(settle time.Duration) {
	h.t.Helper()
	time.Sleep(settle)
	if lines := h.frames(); len(lines) != 0 {
		h.t.Fatalf("expected no further frames, got %q", lines)
	}
}

func zcodeBrowserTestFrameID(t *testing.T, line string) string {
	t.Helper()
	var msg struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		t.Fatalf("client wrote a malformed frame %q: %v", line, err)
	}
	return string(msg.ID)
}

type zcodeBrowserTestReply struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code       int    `json:"code"`
		Message    string `json:"message"`
		SideEffect string `json:"sideEffect"`
	} `json:"error"`
}

func zcodeBrowserTestParse(t *testing.T, line string) zcodeBrowserTestReply {
	t.Helper()
	var reply zcodeBrowserTestReply
	if err := json.Unmarshal([]byte(line), &reply); err != nil {
		t.Fatalf("malformed frame %q: %v", line, err)
	}
	return reply
}

func zcodeBrowserTestResult(t *testing.T, reply zcodeBrowserTestReply) zcodeBrowserEnvelope {
	t.Helper()
	var env zcodeBrowserEnvelope
	if reply.Error != nil {
		t.Fatalf("expected a result, got a protocol error %+v", reply.Error)
	}
	if err := json.Unmarshal(reply.Result, &env); err != nil {
		t.Fatalf("malformed result %s: %v", reply.Result, err)
	}
	return env
}

func zcodeBrowserTestList(t *testing.T, line string) zcodeBrowserListResult {
	t.Helper()
	reply := zcodeBrowserTestParse(t, line)
	if reply.Error != nil {
		t.Fatalf("expected a result, got a protocol error %+v", reply.Error)
	}
	var list zcodeBrowserListResult
	if err := json.Unmarshal(reply.Result, &list); err != nil {
		t.Fatalf("malformed list %s: %v", reply.Result, err)
	}
	return list
}

func zcodeBrowserTestListFrame(id, requestID, sessionID string) string {
	return fmt.Sprintf(`{"id":%q,"method":"interaction/browserList","params":{"requestId":%q,"sessionId":%q}}`, id, requestID, sessionID)
}

func zcodeBrowserTestExecuteFrame(id, requestID, sessionID, turnID, command string) string {
	return fmt.Sprintf(`{"id":%q,"method":"interaction/browserExecute","params":{"requestId":%q,"sessionId":%q,"turnId":%q,"command":%s}}`, id, requestID, sessionID, turnID, command)
}

func (h *zcodeBrowserHarness) fakeRequests() []zcodeFakeSidecarRequest {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, zcodeTestBrowserFakeRequestsFile))
	if err != nil {
		return nil
	}
	var out []zcodeFakeSidecarRequest
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var request zcodeFakeSidecarRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			h.t.Fatalf("malformed fake request line %q: %v", line, err)
		}
		out = append(out, request)
	}
	return out
}

func (h *zcodeBrowserHarness) waitForFakeRequest(what string, match func(zcodeFakeSidecarRequest) bool) zcodeFakeSidecarRequest {
	h.t.Helper()
	var found zcodeFakeSidecarRequest
	zcodeBrowserWaitFor(h.t, what, func() bool {
		for _, request := range h.fakeRequests() {
			if match(request) {
				found = request
				return true
			}
		}
		return false
	})
	return found
}

func (h *zcodeBrowserHarness) waitForFakePID() int {
	h.t.Helper()
	var pid int
	zcodeBrowserWaitFor(h.t, "fake sidecar pid", func() bool {
		data, err := os.ReadFile(filepath.Join(h.dir, zcodeTestBrowserFakePIDFile))
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
		return err == nil && pid > 0
	})
	return pid
}

// ready runs one list so the sidecar is up with a browser, then returns the
// first descriptor.
func (h *zcodeBrowserHarness) ready() (zcodeBrowserDescriptor, string) {
	h.t.Helper()
	h.client.setSessionID("s")
	h.client.handleLine(zcodeBrowserTestListFrame("boot", "boot", "s"))
	line := h.waitFor("boot")
	list := zcodeBrowserTestList(h.t, line)
	if len(list.Browsers) != 1 {
		h.t.Fatalf("expected one browser after startup, got %s", line)
	}
	return list.Browsers[0], line
}

func TestZcodeBrowserListOffReturnsEmpty(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{zcodeBrowserModeOff, "sometimes", ""} {
		h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, func(o *zcodeBrowserOptions) { o.Mode = mode })
		h.client.setSessionID("s")
		h.client.handleLine(zcodeBrowserTestListFrame("l1", "r1", "s"))
		if got, want := h.waitFor("l1"), `{"id":"l1","result":{"browsers":[]}}`; got != want {
			t.Fatalf("mode %q: got %s, want %s", mode, got, want)
		}
		if got := h.startGenerations(); len(got) != 0 {
			t.Fatalf("mode %q: expected no sidecar start, got %v", mode, got)
		}
	}
}

func TestZcodeBrowserListBeforeHandshake(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	h.client.handleLine(zcodeBrowserTestListFrame("early", "r1", "s"))
	if got, want := h.waitFor("early"), `{"id":"early","result":{"browsers":[]}}`; got != want {
		t.Fatalf("before the handshake got %s, want %s", got, want)
	}
	if got := h.startGenerations(); len(got) != 0 {
		t.Fatalf("a list before the handshake must not start a sidecar, got %v", got)
	}
	// The same run after the handshake owns a session answers with a browser.
	desc, _ := h.ready()
	if desc.Type != "cdp" {
		t.Fatalf("expected a cdp descriptor after the handshake, got %+v", desc)
	}
}

func TestZcodeBrowserListReturnsRealDescriptor(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	desc, boot := h.ready()

	idPattern := regexp.MustCompile(`^cdp:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if !idPattern.MatchString(desc.ID) {
		t.Fatalf("unexpected backend id %q", desc.ID)
	}
	if desc.Generation != 1 || desc.Type != "cdp" || desc.Name != zcodeBrowserName {
		t.Fatalf("unexpected descriptor %+v", desc)
	}
	if desc.Capabilities.Browser == nil || len(desc.Capabilities.Browser) != 0 || desc.Capabilities.Tab == nil || len(desc.Capabilities.Tab) != 0 {
		t.Fatalf("capabilities must be empty arrays, got %+v", desc.Capabilities)
	}
	if len(desc.APISupportOverrides) != 9 {
		t.Fatalf("expected 9 apiSupportOverrides, got %d", len(desc.APISupportOverrides))
	}
	for name, supported := range desc.APISupportOverrides {
		if supported {
			t.Fatalf("override %s must be false", name)
		}
	}
	wantMeta := map[string]string{"provider": "multica", "launchMode": "docker", "headless": "true"}
	for key, want := range wantMeta {
		if desc.Metadata[key] != want {
			t.Fatalf("metadata[%s]=%q, want %q", key, desc.Metadata[key], want)
		}
	}
	if raw := zcodeBrowserTestParse(t, boot).Result; !strings.Contains(string(raw), `"capabilities":{"browser":[],"tab":[]}`) {
		t.Fatalf("capabilities must be encoded as arrays, got %s", raw)
	}
	if got := h.startGenerations(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected exactly one start at generation 1, got %v", got)
	}
	if names := h.startNames(); len(names) != 1 || !zcodeBrowserTestName.MatchString(names[0]) {
		t.Fatalf("unexpected container names %v", names)
	}
	// A second list reuses the sidecar and reports the same backend.
	h.client.handleLine(zcodeBrowserTestListFrame("l2", "r2", "s"))
	again := zcodeBrowserTestList(t, h.waitFor("l2"))
	if len(again.Browsers) != 1 || again.Browsers[0].ID != desc.ID {
		t.Fatalf("expected the same backend on the second list, got %+v", again.Browsers)
	}
	if got := h.startGenerations(); len(got) != 1 {
		t.Fatalf("the second list must reuse the sidecar, got %v", got)
	}
}

func TestZcodeBrowserListStartFailureCaches(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeExitBeforeReady, nil)
	h.client.setSessionID("s")
	for _, id := range []string{"l1", "l2"} {
		h.client.handleLine(zcodeBrowserTestListFrame(id, "r"+id, "s"))
		if got, want := h.waitFor(id), `{"id":"`+id+`","result":{"browsers":[]}}`; got != want {
			t.Fatalf("list %s: got %s, want %s", id, got, want)
		}
	}
	if got := h.startGenerations(); len(got) != 1 {
		t.Fatalf("a failed start must be cached for the run, got starts %v", got)
	}
	zcodeBrowserWaitFor(t, "container removal", func() bool { return len(h.removedNames()) >= 1 })
}

func TestZcodeBrowserResolveAuto(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	modules := zcodeBrowserTestModules(t, dir)
	lookups, probes := 0, 0
	lookPath := func(string) (string, error) {
		lookups++
		return "", errors.New("not found")
	}
	notFound := func(name string) (string, error) {
		lookups++
		if name == "docker" {
			return "/tools/docker", nil
		}
		return "", errors.New("not found")
	}
	probe := func(string, string) error {
		probes++
		return nil
	}
	unreachable := func(dockerBin, image string) error {
		probes++
		return fmt.Errorf("%w: no daemon", errZcodeDockerUnreachable)
	}
	missing := func(dockerBin, image string) error {
		probes++
		return errors.New("exit status 1")
	}

	cases := []struct {
		name        string
		opts        zcodeBrowserOptions
		lookPath    func(string) (string, error)
		want        string
		wantLookups bool
		wantProbes  bool
	}{
		{"zero value is off", zcodeBrowserOptions{}, lookPath, zcodeBrowserReasonOff, false, false},
		{"off wins over a complete configuration", zcodeBrowserOptions{Mode: zcodeBrowserModeOff, ModulesDir: modules, DockerBin: "docker"}, lookPath, zcodeBrowserReasonOff, false, false},
		{"auto without a modules dir never looks for docker", zcodeBrowserOptions{Mode: zcodeBrowserModeAuto}, lookPath, zcodeBrowserReasonModulesMissing, false, false},
		{"modules dir without playwright-core", zcodeBrowserOptions{Mode: zcodeBrowserModeAuto, ModulesDir: dir}, lookPath, zcodeBrowserReasonModulesMissing, false, false},
		{"modules present, docker missing", zcodeBrowserOptions{Mode: zcodeBrowserModeAuto, ModulesDir: modules}, lookPath, zcodeBrowserReasonDockerBinMissing, true, false},
		{"modules and docker present, image present", zcodeBrowserOptions{Mode: zcodeBrowserModeDocker, ModulesDir: modules, Probe: probe}, notFound, "", true, true},
		{"modules and docker present, daemon unreachable", zcodeBrowserOptions{Mode: zcodeBrowserModeDocker, ModulesDir: modules, Probe: unreachable}, notFound, zcodeBrowserReasonDockerUnreachable, true, true},
		{"modules and docker present, image missing", zcodeBrowserOptions{Mode: zcodeBrowserModeDocker, ModulesDir: modules, Probe: missing}, notFound, zcodeBrowserReasonImageMissing, true, true},
	}
	for _, tc := range cases {
		lookups, probes = 0, 0
		got := tc.opts.resolve(tc.lookPath, tc.opts.Probe)
		if got != tc.want {
			t.Errorf("%s: resolve = %q, want %q", tc.name, got, tc.want)
		}
		if (lookups > 0) != tc.wantLookups {
			t.Errorf("%s: docker lookups = %d, want any = %v", tc.name, lookups, tc.wantLookups)
		}
		if (probes > 0) != tc.wantProbes {
			t.Errorf("%s: image probes = %d, want any = %v", tc.name, probes, tc.wantProbes)
		}
	}

	// The real probe against a fake docker CLI (a test-created script).
	fakeDocker := func(body string) string {
		path := filepath.Join(t.TempDir(), "docker")
		writeTestExecutable(t, path, []byte("#!/bin/sh\n"+body))
		return path
	}
	if err := zcodeDockerProbeImage(fakeDocker("exit 0"), "img"); err != nil {
		t.Fatalf("a reachable daemon with the image must probe clean, got %v", err)
	}
	err := zcodeDockerProbeImage(fakeDocker("printf 'Error: No such image: img\\n' >&2\nexit 1"), "img")
	if err == nil || errors.Is(err, errZcodeDockerUnreachable) {
		t.Fatalf("a missing image must not be reported as unreachable, got %v", err)
	}
	err = zcodeDockerProbeImage(fakeDocker("printf 'Cannot connect to the Docker daemon. Is the docker daemon running?\\n' >&2\nexit 1"), "img")
	if !errors.Is(err, errZcodeDockerUnreachable) {
		t.Fatalf("an unreachable daemon must be reported as such, got %v", err)
	}
}

func TestZcodeBrowserOptionsFromEnv(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		zcodeBrowserEnvMode:      " Docker ",
		zcodeBrowserEnvImage:     "playwright-image:1",
		zcodeBrowserEnvDockerBin: "/tools/docker",
	}
	lookup := func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	opts := zcodeBrowserOptionsFromEnv(lookup, logger)
	if opts.Mode != zcodeBrowserModeDocker || opts.Image != values[zcodeBrowserEnvImage] || opts.DockerBin != values[zcodeBrowserEnvDockerBin] || opts.ModulesDir != "" {
		t.Fatalf("unexpected options %+v", opts)
	}
	if zcodeBrowserOptionsFromEnv(func(string) (string, bool) { return "", false }, nil).Mode != zcodeBrowserModeAuto {
		t.Fatal("an unset mode must default to auto")
	}
	values[zcodeBrowserEnvMode] = "sometimes"
	if opts := zcodeBrowserOptionsFromEnv(lookup, logger); opts.Mode != zcodeBrowserModeOff {
		t.Fatalf("an unrecognized mode must be off, got %q", opts.Mode)
	}
	if !strings.Contains(logs.String(), zcodeBrowserEnvMode) {
		t.Fatalf("an unrecognized mode must be logged, got %q", logs.String())
	}
}

func TestZcodeBrowserDockerRunCommand(t *testing.T) {
	dir := t.TempDir()
	opts := zcodeBrowserOptions{Mode: zcodeBrowserModeDocker, ModulesDir: dir}
	t.Setenv("MULTICA_ZCODE_BROWSER_TEST_ONLY_SECRET", "must-not-leak")
	cmd, err := opts.dockerRunCommand(context.Background(), "docker", "/work", "multica-zcode-browser-abcdef012345", "cdp:id", 2)
	if err != nil {
		t.Fatalf("dockerRunCommand: %v", err)
	}
	want := []string{
		"run", "--rm", "-i", "--pull", "never",
		"--name", "multica-zcode-browser-abcdef012345",
		"--label", "multica.zcode-browser=1",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", "1024",
		"--memory", "1500m",
		"--shm-size", "256m",
		"-v", "/work:/sidecar:ro",
		"-v", filepath.Join(dir, "node_modules") + ":/node_modules:ro",
		"-e", "PLAYWRIGHT_BROWSERS_PATH=/ms-playwright",
		"--entrypoint", "node",
		zcodeBrowserDefaultImage,
		"/sidecar/" + zcodeBrowserSidecarFile,
		"--browser-id", "cdp:id",
		"--generation", "2",
	}
	if got := cmd.Args; !zcodeBrowserTestEqual(got, append([]string{"docker"}, want...)) {
		t.Fatalf("unexpected docker invocation:\n got  %q\n want %q", got, append([]string{"docker"}, want...))
	}
	for _, entry := range cmd.Env {
		if strings.Contains(entry, "must-not-leak") {
			t.Fatalf("the container client must not see the daemon's environment, got %q", entry)
		}
	}
}

func zcodeBrowserTestEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestZcodeBrowserDockerChildEnv(t *testing.T) {
	t.Parallel()
	got := zcodeDockerChildEnv([]string{
		"PATH=/tools:/usr/bin", "Path=/windows", "HOME=/home/u", "TMPDIR=/t", "USER=u", "LOGNAME=u",
		"XDG_CONFIG_HOME=/c", "XDG_RUNTIME_DIR=/r", "DOCKER_HOST=unix:///d", "DOCKER_CONTEXT=x",
		"API_KEY=secret", "AWS_SECRET_ACCESS_KEY=secret", "GH_TOKEN=secret", "MULTICA_TOKEN=secret",
	})
	want := []string{"PATH=/tools:/usr/bin", "Path=/windows", "HOME=/home/u", "TMPDIR=/t", "USER=u", "LOGNAME=u", "XDG_CONFIG_HOME=/c", "XDG_RUNTIME_DIR=/r", "DOCKER_HOST=unix:///d", "DOCKER_CONTEXT=x"}
	if !zcodeBrowserTestEqual(got, want) {
		t.Fatalf("child env = %q, want %q", got, want)
	}
}

func TestZcodeBrowserExecuteEnvelopeWhitelist(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	h.ready()
	h.client.handleLine(zcodeBrowserTestExecuteFrame("x1", "r1", "s", "t1", `{"method":"navigate","url":"about:blank"}`))
	want := `{"id":"x1","result":{"ok":true,"state":{"url":"about:blank","title":"Fake"},"meta":{"browserUse":true,"backendType":"cdp"},"elapsedMs":7}}`
	if got := h.waitFor("x1"); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if request := h.waitForFakeRequest("exec frame", func(r zcodeFakeSidecarRequest) bool { return r.Method == "exec" }); request.Command != "navigate" || request.RequestID != "r1" || request.SessionID != "s" || request.TurnID != "t1" {
		t.Fatalf("unexpected exec frame %+v", request)
	}
}

func TestZcodeBrowserExecuteStaleBackend(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	h.client.setSessionID("s")
	stale := []string{
		`{"id":"s1","method":"interaction/browserExecute","params":{"requestId":"r1","sessionId":"s","browserId":"cdp:someone-else","command":{"method":"snapshot"}}}`,
		`{"id":"s2","method":"interaction/browserExecute","params":{"requestId":"r2","sessionId":"s","browserGeneration":99,"command":{"method":"snapshot"}}}`,
		`{"id":"s3","method":"interaction/browserExecute","params":{"requestId":"r3","sessionId":"s","browserId":"cdp:someone-else","browserGeneration":1,"command":{"method":"snapshot"}}}`,
	}
	for _, frame := range stale {
		h.client.handleLine(frame)
	}
	for i := 1; i <= len(stale); i++ {
		id := fmt.Sprintf("s%d", i)
		raw := h.waitFor(id)
		env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, raw))
		if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeBackendUnavailable || !strings.Contains(env.Error.Message, "is stale") {
			t.Fatalf("%s: expected a stale backend failure, got %s", id, raw)
		}
		if env.Error.SideEffect != "" {
			t.Fatalf("%s: a rejected stale request has no side effect, got %q", id, env.Error.SideEffect)
		}
	}
	// The current generation is not stale.
	h.client.handleLine(`{"id":"s4","method":"interaction/browserExecute","params":{"requestId":"r4","sessionId":"s","browserGeneration":1,"command":{"method":"snapshot"}}}`)
	if env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("s4"))); !env.OK {
		t.Fatalf("the current generation must be accepted, got %+v", env)
	}
	if got := h.startGenerations(); len(got) != 1 {
		t.Fatalf("stale requests must not start a sidecar, got starts %v", got)
	}
}

func TestZcodeBrowserExecuteDuplicateRequestId(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeHold, nil)
	h.ready()
	h.client.handleLine(zcodeBrowserTestExecuteFrame("d1", "dup", "s", "t1", `{"method":"navigate","url":"about:blank"}`))
	h.waitForFakeRequest("first request", func(r zcodeFakeSidecarRequest) bool { return r.RequestID == "dup" })
	h.client.handleLine(zcodeBrowserTestExecuteFrame("d2", "dup", "s", "t1", `{"method":"navigate","url":"about:blank"}`))
	env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("d2")))
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeDuplicateRequestID {
		t.Fatalf("expected duplicate_request_id, got %+v", env)
	}
	// The first request is untouched and still in flight.
	h.client.handleLine(zcodeBrowserTestExecuteFrame("c1", "c", "s", "t1", `{"method":"cancelRequest","requestId":"dup"}`))
	env = zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("c1")))
	if !env.OK {
		t.Fatalf("cancelRequest must answer ok, got %+v", env)
	}
	env = zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("d1")))
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeCancelled {
		t.Fatalf("expected the first request to be cancelled, got %+v", env)
	}
}

func (h *zcodeBrowserHarness) holdNavigate(id, requestID, sessionID, turnID string) {
	h.t.Helper()
	h.client.handleLine(zcodeBrowserTestExecuteFrame(id, requestID, sessionID, turnID, `{"method":"navigate","url":"about:blank"}`))
	h.waitForFakeRequest("held request "+requestID, func(r zcodeFakeSidecarRequest) bool { return r.Method == "exec" && r.RequestID == requestID })
}

func TestZcodeBrowserCancelRequestAbortsTarget(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeHold, nil)
	h.ready()
	h.holdNavigate("n1", "n1", "s", "t1")
	h.client.handleLine(zcodeBrowserTestExecuteFrame("c1", "c1", "s", "t1", `{"method":"cancelRequest","requestId":"n1"}`))
	if env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("c1"))); !env.OK {
		t.Fatalf("cancelRequest must answer ok, got %+v", env)
	}
	env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("n1")))
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeCancelled {
		t.Fatalf("expected the navigate to be cancelled, got %+v", env)
	}
	if env.Error.SideEffect != "uncertain" {
		t.Fatalf("a cancelled navigate has an uncertain side effect, got %q", env.Error.SideEffect)
	}
	abort := h.waitForFakeRequest("abort forwarded to the sidecar", func(r zcodeFakeSidecarRequest) bool { return r.Method == "cancel" && r.RequestID == "n1" })
	if abort.ID == 0 {
		t.Fatalf("unexpected abort frame %+v", abort)
	}
	// The fake answers the aborted request late; the host must drop that reply.
	h.expectQuiet(150 * time.Millisecond)
}

func TestZcodeBrowserCancelReadOnlyNoUncertain(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeHoldSnapshot, nil)
	h.ready()
	h.client.handleLine(zcodeBrowserTestExecuteFrame("q1", "q1", "s", "t1", `{"method":"snapshot"}`))
	// A cancel that outruns its target is a cancel of an unknown request, so
	// wait until the sidecar is holding the snapshot.
	h.waitForFakeRequest("held snapshot", func(r zcodeFakeSidecarRequest) bool { return r.Method == "exec" && r.RequestID == "q1" })
	h.client.handleLine(zcodeBrowserTestExecuteFrame("c1", "c1", "s", "t1", `{"method":"cancelRequest","requestId":"q1"}`))
	env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("q1")))
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeCancelled {
		t.Fatalf("expected the snapshot to be cancelled, got %+v", env)
	}
	if env.Error.SideEffect != "" {
		t.Fatalf("a read-only command has no uncertain side effect, got %q", env.Error.SideEffect)
	}
}

func TestZcodeBrowserCancelUnknownRequest(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	h.client.setSessionID("s")
	h.client.handleLine(zcodeBrowserTestExecuteFrame("c1", "c1", "s", "", `{"method":"cancelRequest","requestId":"never-seen"}`))
	line := h.waitFor("c1")
	reply := zcodeBrowserTestParse(t, line)
	env := zcodeBrowserTestResult(t, reply)
	if !env.OK || reply.Error != nil {
		t.Fatalf("cancelling an unknown request must answer ok, got %s", line)
	}
	if strings.Contains(line, "error") || !strings.HasPrefix(line, `{"id":"c1","result":{"ok":true,"elapsedMs":`) {
		t.Fatalf("unexpected cancel reply %s", line)
	}
	h.expectQuiet(100 * time.Millisecond)
	if got := h.startGenerations(); len(got) != 0 {
		t.Fatalf("a lifecycle command must never start a sidecar, got %v", got)
	}
}

func TestZcodeBrowserTurnEndedScope(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeHold, nil)
	h.ready()
	h.holdNavigate("a", "a", "s1", "t1")
	h.holdNavigate("b", "b", "s1", "t2")
	h.holdNavigate("c", "c", "s2", "t1")
	h.client.handleLine(zcodeBrowserTestExecuteFrame("e1", "e1", "s1", "t1", `{"method":"turnEnded","sessionId":"s1","turnId":"t1"}`))
	if env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("e1"))); !env.OK {
		t.Fatalf("turnEnded must answer ok, got %+v", env)
	}
	env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("a")))
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeCancelled {
		t.Fatalf("the request of the ended turn must be cancelled, got %+v", env)
	}
	// Only that request ended: b and c are still in flight.
	time.Sleep(100 * time.Millisecond)
	if lines := h.frames(); len(lines) != 0 {
		t.Fatalf("turnEnded must only end the requests of its own turn, got %q", lines)
	}
	h.client.handleLine(zcodeBrowserTestExecuteFrame("e2", "e2", "s1", "t2", `{"method":"turnEnded","sessionId":"s1","turnId":"t2"}`))
	h.waitFor("e2")
	env = zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("b")))
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeCancelled {
		t.Fatalf("the second turn must only end its own request, got %+v", env)
	}
	// The request of the other session was never touched by s1's turn endings.
	h.client.handleLine(zcodeBrowserTestExecuteFrame("cx", "cx", "s2", "", `{"method":"cancelRequest","requestId":"c"}`))
	h.waitFor("cx")
	env = zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("c")))
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeCancelled {
		t.Fatalf("expected the other session's request to be cancelled only now, got %+v", env)
	}
}

func TestZcodeBrowserCloseSessionReleases(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeHold, nil)
	h.ready()
	h.holdNavigate("a", "a", "s1", "t1")
	h.holdNavigate("b", "b", "s2", "t1")
	h.client.handleLine(zcodeBrowserTestExecuteFrame("x1", "x1", "s1", "t1", `{"method":"closeSession","sessionId":"s1"}`))
	if env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("x1"))); !env.OK {
		t.Fatalf("closeSession must answer ok, got %+v", env)
	}
	if env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("a"))); env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeCancelled {
		t.Fatalf("the request of the closed session must be cancelled, got %+v", env)
	}
	h.waitForFakeRequest("closeSession forwarded to the sidecar", func(r zcodeFakeSidecarRequest) bool { return r.Method == "closeSession" && r.SessionID == "s1" })
	// The other session's request is still in flight.
	time.Sleep(100 * time.Millisecond)
	if lines := h.frames(); len(lines) != 0 {
		t.Fatalf("closeSession must only release its own session's requests, got %q", lines)
	}
	h.client.handleLine(zcodeBrowserTestExecuteFrame("c2", "c2", "s2", "", `{"method":"cancelRequest","requestId":"b"}`))
	h.waitFor("c2")
	if env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("b"))); env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeCancelled {
		t.Fatalf("the other session's request must still be in flight until cancelled, got %+v", env)
	}
}

func TestZcodeBrowserLifecycleNeverStaleOrUnavailable(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, func(o *zcodeBrowserOptions) { o.Mode = zcodeBrowserModeOff })
	h.client.handleLine(`{"id":"k1","method":"interaction/browserExecute","params":{"requestId":"k1","sessionId":"no-session-yet","browserId":"cdp:gone","browserGeneration":77,"command":{"method":"cancelRequest","requestId":"x"}}}`)
	h.client.handleLine(`{"id":"k2","method":"interaction/browserExecute","params":{"requestId":"k2","sessionId":"no-session-yet","browserGeneration":77,"command":{"method":"turnEnded","sessionId":"no-session-yet","turnId":"t"}}}`)
	h.client.handleLine(`{"id":"k3","method":"interaction/browserExecute","params":{"requestId":"k3","sessionId":"no-session-yet","browserGeneration":77,"command":{"method":"closeSession","sessionId":"no-session-yet"}}}`)
	for _, id := range []string{"k1", "k2", "k3"} {
		line := h.waitFor(id)
		if strings.Contains(line, "error") || !strings.HasPrefix(line, `{"id":"`+id+`","result":{"ok":true,"elapsedMs":`) {
			t.Fatalf("%s: lifecycle commands are answered ok unconditionally, got %s", id, line)
		}
	}
	h.expectQuiet(100 * time.Millisecond)
	if got := h.startGenerations(); len(got) != 0 {
		t.Fatalf("lifecycle commands must never start a sidecar, got %v", got)
	}
}

func TestZcodeBrowserExecuteTimeoutRecycles(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeHold, func(o *zcodeBrowserOptions) { o.Deadline = 200 * time.Millisecond })
	h.ready()
	h.client.handleLine(zcodeBrowserTestExecuteFrame("n1", "n1", "s", "t1", `{"method":"navigate","url":"about:blank"}`))
	env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("n1")))
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeTimeout {
		t.Fatalf("expected a timeout, got %+v", env)
	}
	if env.Error.SideEffect != "uncertain" {
		t.Fatalf("a timed-out navigate has an uncertain side effect, got %q", env.Error.SideEffect)
	}
	// The sidecar is torn down after the reply, and the next request relaunches
	// at the next generation.
	zcodeBrowserWaitFor(t, "container removal", func() bool { return len(h.removedNames()) >= 1 })
	if got := h.startGenerations(); len(got) != 1 {
		t.Fatalf("the relaunch must not have started yet, got %v", got)
	}
	h.client.handleLine(zcodeBrowserTestExecuteFrame("p1", "p1", "s", "t1", `{"method":"snapshot"}`))
	if env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("p1"))); !env.OK {
		t.Fatalf("expected the relaunched sidecar to answer, got %+v", env)
	}
	if got := h.startGenerations(); len(got) != 2 || got[1] != 2 {
		t.Fatalf("expected a relaunch at generation 2, got %v", got)
	}
}

func TestZcodeBrowserSidecarExitFailsInflight(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeExitOnExec, nil)
	desc, _ := h.ready()
	h.client.handleLine(zcodeBrowserTestExecuteFrame("n1", "n1", "s", "t1", `{"method":"navigate","url":"about:blank"}`))
	env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("n1")))
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeBackendUnavailable {
		t.Fatalf("a sidecar that exits mid-request must fail it as backend_unavailable, got %+v", env)
	}
	// The browser was lost: the next request sees the next generation.
	h.client.handleLine(zcodeBrowserTestListFrame("l2", "r2", "s"))
	list := zcodeBrowserTestList(t, h.waitFor("l2"))
	if len(list.Browsers) != 1 || list.Browsers[0].Generation != desc.Generation+1 {
		t.Fatalf("expected generation %d after the loss, got %+v", desc.Generation+1, list.Browsers)
	}
	if got := h.startGenerations(); len(got) != 2 || got[1] != 2 {
		t.Fatalf("expected a relaunch at generation 2, got %v", got)
	}
	zcodeBrowserWaitFor(t, "container removal", func() bool { return len(h.removedNames()) >= 1 })
	if got := h.removedNames(); len(got) != 1 || got[0] != h.startNames()[0] {
		t.Fatalf("the dead sidecar's container must be removed once, got %v", got)
	}
}

func TestZcodeBrowserUnsupportedCommand(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	h.client.setSessionID("s")
	for i, method := range []string{"openScope", "releaseScope", "recordingStart", "recordingStop"} {
		id := fmt.Sprintf("u%d", i)
		h.client.handleLine(zcodeBrowserTestExecuteFrame(id, "r"+id, "s", "t", fmt.Sprintf(`{"method":%q}`, method)))
		env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor(id)))
		if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeCapabilityUnsupported {
			t.Fatalf("%s: expected capability_unsupported, got %+v", method, env)
		}
	}
	if got := h.startGenerations(); len(got) != 0 {
		t.Fatalf("Go-side rejections must not start a sidecar, got %v", got)
	}
}

func TestZcodeBrowserMalformedParams(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	h.client.setSessionID("s")
	bad := []string{
		`{"id":"m1","method":"interaction/browserList","params":{}}`,
		`{"id":"m2","method":"interaction/browserList","params":[]}`,
		`{"id":"m3","method":"interaction/browserList"}`,
		`{"id":"m4","method":"interaction/browserExecute","params":{"requestId":"r","sessionId":"s"}}`,
		`{"id":"m5","method":"interaction/browserExecute","params":{"requestId":"r","sessionId":"s","command":{}}}`,
		`{"id":"m6","method":"interaction/browserExecute","params":{"requestId":"r","sessionId":"s","command":"navigate"}}`,
		`{"id":"m7","method":"interaction/browserExecute","params":{"requestId":"r","command":{"method":"navigate"}}}`,
	}
	for _, frame := range bad {
		h.client.handleLine(frame)
	}
	for _, frame := range bad {
		var probe struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal([]byte(frame), &probe)
		reply := zcodeBrowserTestParse(t, h.waitFor(probe.ID))
		if reply.Error == nil || reply.Error.Code != zcodeBrowserInvalidParams {
			t.Fatalf("%s: expected -32602, got %+v", probe.ID, reply)
		}
	}
	if got := h.startGenerations(); len(got) != 0 {
		t.Fatalf("malformed frames are rejected before anything is started, got %v", got)
	}
}

func TestZcodeBrowserFailClosedUnchanged(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	h.ready()
	h.client.handleLine(`{"id":"server-1","method":"interaction/requestPermission","params":{"toolCallId":"t1"}}`)
	if got, want := h.waitFor("server-1"), `{"id":"server-1","result":{"decision":"deny"}}`; got != want {
		t.Fatalf("deny: got %s, want %s", got, want)
	}
	h.client.handleLine(`{"id":"server-2","method":"session/requestRuntimePreferences","params":{"sessionId":"ses","scope":"runtime-materialization"}}`)
	want := `{"id":"server-2","result":{"askUserQuestionAutoResolutionEnabled":true,"memoryEnabled":false,"modelContextBudgetStrategy":"preflight-v1","nativeSearchEnhancementsEnabled":false}}`
	if got := h.waitFor("server-2"); got != want {
		t.Fatalf("runtime preferences: got %s, want %s", got, want)
	}
	h.client.handleLine(`{"id":"server-3","method":"workspace/generateText","params":{}}`)
	if got := h.waitFor("server-3"); !strings.Contains(got, "-32601") || !strings.Contains(got, "method not supported by multica: workspace/generateText") {
		t.Fatalf("unknown method: got %s", got)
	}
}

func TestZcodeBrowserConcurrentWritesSerialized(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	h.client.setSessionID("s")
	const writers = 32
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 4 {
			case 0:
				h.client.handleLine(fmt.Sprintf(`{"id":"p%d","method":"interaction/requestPermission","params":{}}`, i))
			case 1:
				h.client.handleLine(fmt.Sprintf(`{"id":"u%d","method":"workspace/generateText","params":{}}`, i))
			case 2:
				h.client.handleLine(fmt.Sprintf(`{"id":"c%d","method":"interaction/browserExecute","params":{"requestId":"c%d","sessionId":"s","command":{"method":"cancelRequest","requestId":"none-%d"}}}`, i, i, i))
			case 3:
				h.client.handleLine(zcodeBrowserTestListFrame(fmt.Sprintf("l%d", i), "r", "s"))
			}
		}(i)
	}
	wg.Wait()
	lines := h.waitFrames(writers)
	seen := map[string]bool{}
	for _, line := range lines {
		id := zcodeBrowserTestFrameID(t, line)
		if seen[id] {
			t.Fatalf("duplicate reply id %s", id)
		}
		seen[id] = true
	}
	if len(seen) != writers {
		t.Fatalf("expected %d distinct replies, got %d: %q", writers, len(seen), lines)
	}
	if got := h.startGenerations(); len(got) != 1 {
		t.Fatalf("concurrent lists must share one start, got %v", got)
	}
}

func TestZcodeBrowserSidecarProtocolEdgeCases(t *testing.T) {
	t.Parallel()
	t.Run("garbage line before ready", func(t *testing.T) {
		t.Parallel()
		h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeGarbageLine, nil)
		if desc, _ := h.ready(); desc.Generation != 1 {
			t.Fatalf("expected the descriptor after a garbage line, got %+v", desc)
		}
	})
	t.Run("screenshot beyond the default scanner limit", func(t *testing.T) {
		t.Parallel()
		h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeBigScreenshot, nil)
		h.ready()
		h.client.handleLine(zcodeBrowserTestExecuteFrame("i1", "i1", "s", "t1", `{"method":"screenshot"}`))
		env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("i1")))
		if !env.OK {
			t.Fatalf("expected the screenshot, got %+v", env)
		}
		var image struct {
			Base64 string `json:"base64"`
		}
		if err := json.Unmarshal(env.Image, &image); err != nil {
			t.Fatalf("malformed image %s: %v", env.Image, err)
		}
		decoded, err := base64.StdEncoding.DecodeString(image.Base64)
		if err != nil || len(decoded) != 1<<20 {
			t.Fatalf("expected a %d byte image, got %d bytes (%v)", 1<<20, len(decoded), err)
		}
	})
}

func TestZcodeBrowserCloseBeforeStartRefuses(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	h.host.close()
	h.client.setSessionID("s")
	h.client.handleLine(zcodeBrowserTestListFrame("l1", "r1", "s"))
	if got, want := h.waitFor("l1"), `{"id":"l1","result":{"browsers":[]}}`; got != want {
		t.Fatalf("after close got %s, want %s", got, want)
	}
	h.client.handleLine(zcodeBrowserTestExecuteFrame("x1", "x1", "s", "t1", `{"method":"navigate","url":"about:blank"}`))
	env := zcodeBrowserTestResult(t, zcodeBrowserTestParse(t, h.waitFor("x1")))
	if env.OK || env.Error == nil || env.Error.Code != zcodeBrowserCodeBackendUnavailable {
		t.Fatalf("after close a command must fail as backend_unavailable, got %+v", env)
	}
	if got := h.startGenerations(); len(got) != 0 {
		t.Fatalf("a closed host must never start a sidecar, got %v", got)
	}
}

func TestZcodeBrowserStartFailureUnblocksWaiters(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeExitBeforeReady, nil)
	h.client.setSessionID("s")
	const callers = 6
	for i := 0; i < callers; i++ {
		h.client.handleLine(zcodeBrowserTestListFrame(fmt.Sprintf("w%d", i), fmt.Sprintf("r%d", i), "s"))
	}
	for _, line := range h.waitFrames(callers) {
		if !strings.Contains(line, `"result":{"browsers":[]}`) {
			t.Fatalf("every caller of a failed start gets an empty list, got %s", line)
		}
	}
	if got := h.startGenerations(); len(got) != 1 {
		t.Fatalf("concurrent callers must share the failed start, got %v", got)
	}
}

func TestZcodeBrowserConcurrentListSharesOneStart(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	h.client.setSessionID("s")
	const callers = 8
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h.client.handleLine(zcodeBrowserTestListFrame(fmt.Sprintf("l%d", i), fmt.Sprintf("r%d", i), "s"))
		}(i)
	}
	wg.Wait()
	shared := ""
	for _, line := range h.waitFrames(callers) {
		list := zcodeBrowserTestList(t, line)
		if len(list.Browsers) != 1 {
			t.Fatalf("every caller must get the descriptor, got %s", line)
		}
		if shared == "" {
			shared = list.Browsers[0].ID
		} else if list.Browsers[0].ID != shared {
			t.Fatalf("callers saw different backends: %s and %s", shared, list.Browsers[0].ID)
		}
	}
	if got := h.startGenerations(); len(got) != 1 {
		t.Fatalf("concurrent lists must share one start, got %v", got)
	}
}

func TestZcodeBrowserDockerCleanup(t *testing.T) {
	t.Parallel()
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	h.ready()
	pid := h.waitForFakePID()
	if got := h.removedNames(); len(got) != 0 {
		t.Fatalf("a running sidecar's container must not be removed, got %v", got)
	}
	scriptDir := h.scriptDirs()[0]
	if _, err := os.Stat(filepath.Join(scriptDir, zcodeBrowserSidecarFile)); err != nil {
		t.Fatalf("the sidecar script must be written into the temp dir: %v", err)
	}

	h.host.close()
	names, removed := h.startNames(), h.removedNames()
	if len(removed) != 1 || removed[0] != names[0] {
		t.Fatalf("close must remove exactly the started container, got %v (started %v)", removed, names)
	}
	if _, err := os.Stat(scriptDir); !os.IsNotExist(err) {
		t.Fatalf("the temp dir must be removed, stat err = %v", err)
	}
	assertCursorTestProcessGone(t, pid)

	h.host.close()
	if got := h.removedNames(); len(got) != 1 {
		t.Fatalf("a second close must not remove again, got %v", got)
	}
	h.client.handleLine(zcodeBrowserTestListFrame("after", "after", "s"))
	if got, want := h.waitFor("after"), `{"id":"after","result":{"browsers":[]}}`; got != want {
		t.Fatalf("after close got %s, want %s", got, want)
	}
	if got := h.startGenerations(); len(got) != 1 {
		t.Fatalf("no sidecar may start after close, got %v", got)
	}
}

// fakeZcodeBrowserScript answers the handshake, asks for the browser list
// while serving session/send and records the host's reply before the turn
// goes on with the scripted frames.
func fakeZcodeBrowserScript(afterReply string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"session/create"'*)
      printf '{"id":%s,"result":{"protocol":{"name":"ZCode Protocol","version":1},"session":{"sessionId":"` + fakeZcodeSessionID + `"}}}\n' "$id"
      ;;
    *'"method":"session/subscribe"'*)
      printf '{"id":%s,"result":{"sessionId":"` + fakeZcodeSessionID + `","eventSeq":5,"events":[]}}\n' "$id"
      ;;
    *'"method":"session/send"'*)
      printf '{"id":"b1","method":"interaction/browserList","params":{"requestId":"r1","sessionId":"` + fakeZcodeSessionID + `"}}\n'
      while IFS= read -r reply; do
        case "$reply" in
          *'"browsers"'*) printf '%s\n' "$reply" >> "$ZCODE_BROWSER_CAPTURE"; break ;;
        esac
      done
      printf '{"id":%s,"result":{"sessionId":"` + fakeZcodeSessionID + `","accepted":true,"stateRevision":7}}\n' "$id"
` + afterReply + `      ;;
    *'"method":"session/stop"'*)
      printf '{"id":%s,"result":{}}\n' "$id"
      ;;
  esac
done
`
}

// TestZcodeBrowserRunEndStopsSidecar drives the real backend: a run that
// answers a browser request must stop the sidecar however it ends.
func TestZcodeBrowserRunEndStopsSidecar(t *testing.T) {
	cases := []struct {
		name       string
		afterReply string
		cancel     bool
		timeout    time.Duration
		wantStatus string
	}{
		{
			name:       "completed",
			afterReply: zcodeEventFrame(6, "turn.completed", `{"response":"done","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":1}`),
			wantStatus: "completed",
		},
		{name: "cancelled", cancel: true, wantStatus: "aborted"},
		{name: "timeout", timeout: 4 * time.Second, wantStatus: "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := filepath.Join(t.TempDir(), "browser-reply.txt")
			t.Setenv("ZCODE_BROWSER_CAPTURE", capture)
			bin := writeFakeZcodeScript(t, fakeZcodeBrowserScript(tc.afterReply))
			backend, err := New("zcode", Config{ExecutablePath: bin, Logger: zcodeTestLogger(), TaskID: "task-1"})
			if err != nil {
				t.Fatalf("New(zcode) error: %v", err)
			}
			h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
			backend.(*zcodeBackend).browser = h.opts

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, err := backend.Execute(ctx, "prompt", ExecOptions{Cwd: t.TempDir(), Timeout: tc.timeout})
			if err != nil {
				t.Fatalf("Execute error: %v", err)
			}
			done := make(chan Result, 1)
			go func() {
				for range session.Messages {
				}
				done <- <-session.Result
			}()
			if tc.cancel {
				zcodeBrowserWaitFor(t, "the browser reply", func() bool {
					_, err := os.Stat(capture)
					return err == nil
				})
				cancel()
			}
			var res Result
			select {
			case res = <-done:
			case <-time.After(30 * time.Second):
				t.Fatal("the run did not end")
			}
			if res.Status != tc.wantStatus {
				t.Fatalf("expected status %q, got %q (%s)", tc.wantStatus, res.Status, res.Error)
			}
			reply, err := os.ReadFile(capture)
			if err != nil || !strings.Contains(string(reply), `"type":"cdp"`) {
				t.Fatalf("app-server must have received the browser list, got %q (%v)", reply, err)
			}

			// However the run ended, the sidecar is gone and its container removed.
			zcodeBrowserWaitFor(t, "container removal", func() bool { return len(h.removedNames()) >= 1 })
			if names, removed := h.startNames(), h.removedNames(); len(removed) != 1 || removed[0] != names[0] {
				t.Fatalf("expected the started container to be removed once, got %v (started %v)", removed, names)
			}
			assertCursorTestProcessGone(t, h.waitForFakePID())
		})
	}
}

// TestZcodeBrowserRunEndDuringStartCleansUp covers the window in which a run
// ends while the sidecar is still starting: whoever owns the process at that
// moment removes the container, and a container client killed before its
// sidecar reported ready is removed a second time later.
func TestZcodeBrowserRunEndDuringStartCleansUp(t *testing.T) {
	t.Parallel()
	tune := func(o *zcodeBrowserOptions) {
		o.CloseGrace = 150 * time.Millisecond
		o.OrphanSweepDelay = 40 * time.Millisecond
	}
	slow := func(h *zcodeBrowserHarness) {
		// Asleep long after the test is over; only a signal ends it.
		h.childEnv = []string{zcodeTestBrowserFakeSlowReadyEnv + "=600000"}
		h.client.setSessionID("s")
	}

	t.Run("close takes the registered process", func(t *testing.T) {
		t.Parallel()
		h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeSlowReady, tune)
		slow(h)
		h.client.handleLine(zcodeBrowserTestListFrame("l1", "r1", "s"))
		pid := h.waitForFakePID()
		h.host.close()

		zcodeBrowserWaitFor(t, "container removal", func() bool { return len(h.removedNames()) >= 1 })
		assertZcodeBrowserCleanedUp(t, h, pid)
		if got, want := h.waitFor("l1"), `{"id":"l1","result":{"browsers":[]}}`; got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
		if got := h.startGenerations(); len(got) != 1 {
			t.Fatalf("expected exactly one start, got %v", got)
		}
	})

	t.Run("launcher finds the host closed after Start", func(t *testing.T) {
		t.Parallel()
		h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeSlowReady, tune)
		slow(h)
		base, signalled := h.opts.Command, make(chan struct{})
		var once sync.Once
		h.host.opts.Command = func(ctx context.Context, workDir, name, browserID string, generation int) (*exec.Cmd, error) {
			once.Do(func() { close(signalled) })
			// Hold the launcher until close() has marked the host, so the
			// process is started into a closed host.
			for {
				h.host.mu.Lock()
				closed := h.host.closed
				h.host.mu.Unlock()
				if closed {
					break
				}
				time.Sleep(time.Millisecond)
			}
			return base(ctx, workDir, name, browserID, generation)
		}
		h.client.handleLine(zcodeBrowserTestListFrame("l1", "r1", "s"))
		closed := make(chan struct{})
		go func() {
			<-signalled
			h.host.close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(20 * time.Second):
			t.Fatal("close did not return")
		}
		// close() came back only after the launcher had removed the container.
		names := h.startNames()
		if removed := h.removedNames(); len(names) != 1 || len(removed) < 1 || removed[0] != names[0] {
			t.Fatalf("close must not return before the container is removed, got %v (started %v)", removed, names)
		}
		assertZcodeBrowserCleanedUp(t, h, h.waitForFakePID())
		if got, want := h.waitFor("l1"), `{"id":"l1","result":{"browsers":[]}}`; got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})
}

// assertZcodeBrowserCleanedUp waits for the delayed second removal of a
// container client that was killed before its sidecar reported ready, then
// checks that nothing else was removed and the process is gone.
func assertZcodeBrowserCleanedUp(t *testing.T, h *zcodeBrowserHarness, pid int) {
	t.Helper()
	zcodeBrowserWaitFor(t, "the delayed container removal", func() bool { return len(h.removedNames()) >= 2 })
	names, removed := h.startNames(), h.removedNames()
	if len(names) != 1 || len(removed) != 2 || removed[0] != names[0] || removed[1] != names[0] {
		t.Fatalf("expected the container to be removed twice by name, got %v (started %v)", removed, names)
	}
	assertCursorTestProcessGone(t, pid)
	time.Sleep(10 * time.Millisecond)
	if scripts := h.scriptDirs(); len(scripts) != 1 {
		t.Fatalf("expected one start, got %v", scripts)
	} else if _, err := os.Stat(scripts[0]); !os.IsNotExist(err) {
		t.Fatalf("the temp dir must be removed, stat err = %v", err)
	}
}

// TestZcodeBrowserAppServerArgsUnchanged pins the app-server launch line:
// browser use adds no flag to it.
func TestZcodeBrowserAppServerArgsUnchanged(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv("ZCODE_ARGV_CAPTURE", capture)
	completed := zcodeEventFrame(6, "turn.completed", `{"response":"done","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":1}`)
	script := strings.Replace(fakeZcodeHappyScript(completed), "#!/bin/sh\n",
		"#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ZCODE_ARGV_CAPTURE\"\n", 1)
	bin := writeFakeZcodeScript(t, script)
	backend, err := New("zcode", Config{ExecutablePath: bin, Logger: zcodeTestLogger(), TaskID: "task-1"})
	if err != nil {
		t.Fatalf("New(zcode) error: %v", err)
	}
	h := newZcodeBrowserHarness(t, zcodeTestBrowserFakeNormal, nil)
	backend.(*zcodeBackend).browser = h.opts

	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{
		Cwd:        t.TempDir(),
		CustomArgs: []string{"--keep-me"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	_, res := collectZcodeSession(t, session)
	if res.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", res.Status, res.Error)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("read captured argv: %v", err)
	}
	want := "app-server\n--keep-me"
	if got := strings.TrimSpace(string(data)); got != want {
		t.Fatalf("app-server argv = %q, want %q", got, want)
	}
	if got := h.startGenerations(); len(got) != 0 {
		t.Fatalf("a run without browser requests must not start a sidecar, got %v", got)
	}
}
