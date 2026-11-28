package agent

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The browser host tests never start docker or node. Their sidecar is this
// test binary re-executed with -test.run matching
// TestZcodeBrowserFakeSidecarHelper and the scenario in the environment, the
// same pattern as the other helper processes in this package. The fake speaks
// the sidecar's NDJSON protocol on stdio and records what it received in the
// directory named by zcodeTestBrowserFakeDirEnv.

const (
	zcodeTestBrowserFakeEnv           = "MULTICA_TEST_ZCODE_BROWSER_FAKE"
	zcodeTestBrowserFakeDirEnv        = "MULTICA_TEST_ZCODE_BROWSER_FAKE_DIR"
	zcodeTestBrowserFakeGenerationEnv = "MULTICA_TEST_ZCODE_BROWSER_FAKE_GENERATION"
	zcodeTestBrowserFakeSlowReadyEnv  = "MULTICA_TEST_ZCODE_BROWSER_FAKE_SLOW_READY_MS"

	zcodeTestBrowserFakeNormal          = "normal"
	zcodeTestBrowserFakeSlowReady       = "slow-ready"
	zcodeTestBrowserFakeExitBeforeReady = "exit-before-ready"
	zcodeTestBrowserFakeExitOnExec      = "exit-on-exec"
	zcodeTestBrowserFakeGarbageLine     = "garbage-line"
	zcodeTestBrowserFakeBigScreenshot   = "big-screenshot"
	zcodeTestBrowserFakeHold            = "hold"
	zcodeTestBrowserFakeHoldSnapshot    = "hold-snapshot"

	zcodeTestBrowserFakePIDFile      = "pid"
	zcodeTestBrowserFakeRequestsFile = "requests.log"
)

func TestZcodeBrowserFakeSidecarHelper(t *testing.T) {
	scenario := os.Getenv(zcodeTestBrowserFakeEnv)
	if scenario == "" {
		return
	}
	os.Exit(runZcodeBrowserFakeSidecar(scenario))
}

// zcodeFakeSidecarFrame is the part of a host frame the fake acts on.
type zcodeFakeSidecarFrame struct {
	ID                int64           `json:"id"`
	Method            string          `json:"method"`
	SessionID         string          `json:"sessionId"`
	RequestID         string          `json:"requestId"`
	TurnID            string          `json:"turnId"`
	BrowserGeneration *int            `json:"browserGeneration"`
	Command           json.RawMessage `json:"command"`
}

// zcodeFakeSidecarRequest is one line of the fake's request log.
type zcodeFakeSidecarRequest struct {
	ID         int64  `json:"id"`
	Method     string `json:"method"`
	RequestID  string `json:"requestId"`
	SessionID  string `json:"sessionId"`
	TurnID     string `json:"turnId"`
	Command    string `json:"command"`
	URL        string `json:"url"`
	Generation int    `json:"generation"`
}

// zcodeFakeSidecarResult is what the fake answers for an ordinary command.
// Besides the envelope keys it carries keys the host must drop.
type zcodeFakeSidecarResult struct {
	OK        bool            `json:"ok"`
	State     json.RawMessage `json:"state"`
	Meta      json.RawMessage `json:"meta"`
	Debug     string          `json:"debug"`
	Cookies   []string        `json:"cookies"`
	SessionID string          `json:"sessionId"`
	ElapsedMS int             `json:"elapsedMs"`
}

func runZcodeBrowserFakeSidecar(scenario string) int {
	dir := os.Getenv(zcodeTestBrowserFakeDirEnv)
	generation, _ := strconv.Atoi(os.Getenv(zcodeTestBrowserFakeGenerationEnv))
	if dir != "" {
		_ = os.WriteFile(filepath.Join(dir, zcodeTestBrowserFakePIDFile), []byte(strconv.Itoa(os.Getpid())), 0o600)
	}
	emit := func(payload any) {
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		_, _ = os.Stdout.Write(append(data, '\n'))
	}

	switch scenario {
	case zcodeTestBrowserFakeExitBeforeReady:
		return 7
	case zcodeTestBrowserFakeGarbageLine:
		// A line the host must ignore without losing the protocol.
		_, _ = os.Stdout.WriteString("this line is not json\n")
	case zcodeTestBrowserFakeSlowReady:
		delay, _ := strconv.Atoi(os.Getenv(zcodeTestBrowserFakeSlowReadyEnv))
		// Asleep, not reading stdin: only a signal ends this phase.
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
	emit(map[string]any{"event": "ready", "node": "fake"})

	held := map[int64]bool{}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var frame zcodeFakeSidecarFrame
		if json.Unmarshal([]byte(line), &frame) != nil {
			continue
		}
		var command struct {
			Method string `json:"method"`
			URL    string `json:"url"`
		}
		_ = json.Unmarshal(frame.Command, &command)
		logZcodeFakeSidecarRequest(dir, zcodeFakeSidecarRequest{
			ID:         frame.ID,
			Method:     frame.Method,
			RequestID:  frame.RequestID,
			SessionID:  frame.SessionID,
			TurnID:     frame.TurnID,
			Command:    command.Method,
			URL:        command.URL,
			Generation: generation,
		})

		switch frame.Method {
		case "ensureBrowser":
			emit(map[string]any{"id": frame.ID, "result": map[string]any{"generation": generation}})
		case "cancel":
			if held[frame.ID] {
				delete(held, frame.ID)
				// Too late to matter: the host already settled the request, and
				// must drop this reply instead of answering app-server twice.
				emit(map[string]any{"id": frame.ID, "result": zcodeFakeSidecarResult{OK: true, ElapsedMS: 1}})
			}
		case "closeSession":
			emit(map[string]any{"id": frame.ID, "result": map[string]any{"ok": true}})
		case "exec":
			switch {
			case scenario == zcodeTestBrowserFakeExitOnExec:
				return 3
			case scenario == zcodeTestBrowserFakeHold && command.Method == "navigate",
				scenario == zcodeTestBrowserFakeHoldSnapshot && command.Method == "snapshot":
				held[frame.ID] = true
			default:
				emit(map[string]any{"id": frame.ID, "result": zcodeFakeSidecarResultFor(scenario, command.Method)})
			}
		}
	}
	return 0
}

func logZcodeFakeSidecarRequest(dir string, request zcodeFakeSidecarRequest) {
	if dir == "" {
		return
	}
	data, err := json.Marshal(request)
	if err != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(dir, zcodeTestBrowserFakeRequestsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.Write(append(data, '\n'))
	_ = file.Close()
}

func zcodeFakeSidecarResultFor(scenario, method string) any {
	switch method {
	case "screenshot":
		image := map[string]any{"type": "png", "width": 1, "height": 1, "base64": "iVBORw0KGgo="}
		if scenario == zcodeTestBrowserFakeBigScreenshot {
			// Larger than the default bufio limit: the host must use the agent stream scanner.
			image["base64"] = base64.StdEncoding.EncodeToString(make([]byte, 1<<20))
		}
		return map[string]any{"ok": true, "image": image, "elapsedMs": 3}
	case "snapshot":
		return map[string]any{"ok": true, "snapshot": map[string]any{"elements": []map[string]string{
			{"ref": "e1", "tag": "input", "role": "textbox", "name": "q", "value": "not-a-real-value"},
			{"ref": "e2", "tag": "button", "role": "button", "name": "Go"},
		}}}
	case "evaluate":
		return map[string]any{"ok": true, "value": 42}
	}
	return zcodeFakeSidecarResult{
		OK:        true,
		State:     json.RawMessage(`{"url":"about:blank","title":"Fake"}`),
		Meta:      json.RawMessage(`{"browserUse":true,"backendType":"cdp"}`),
		Debug:     "must be dropped",
		Cookies:   []string{"must be dropped"},
		SessionID: "must be dropped",
		ElapsedMS: 7,
	}
}
