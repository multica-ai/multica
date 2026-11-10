package localenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

const (
	// KindServer is reached at its URL; KindApp is a desktop app, whose window
	// only the machine running it can bring forward.
	KindServer = "server"
	KindApp    = "app"
)

// ErrAppGone means the app an environment names is no longer running.
var ErrAppGone = errors.New("the app is no longer running")

// ErrFocusUnsupported is returned where the daemon cannot bring a window
// forward.
var ErrFocusUnsupported = errors.New("bringing an app forward is not supported on this platform yet")

// Process is what detection learns about a listening process.
type Process struct {
	Args string
	// App names the desktop app the process is and Bundle is where that app
	// lives; both are empty for a process without windows of its own.
	App    string
	Bundle string
}

var remoteDebuggingPort = regexp.MustCompile(`--remote-debugging-port[= ]+(\d+)`)

// debugs reports whether the process was told to serve the Chrome DevTools
// Protocol on port: a debugger endpoint, not something to open.
func (p Process) debugs(port int) bool {
	for _, m := range remoteDebuggingPort.FindAllStringSubmatch(p.Args, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n == port {
			return true
		}
	}
	return false
}

// probeCDP reports whether port answers as a DevTools endpoint, for an app
// that turned debugging on from its own code rather than its arguments.
func probeCDP(ctx context.Context, port int) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", port), nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var version struct {
		Protocol  string `json:"Protocol-Version"`
		WebSocket string `json:"webSocketDebuggerUrl"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&version) != nil {
		return false
	}
	return version.Protocol != "" && version.WebSocket != ""
}

// Focus brings the desktop app the environment names to the front.
func (e Environment) Focus(ctx context.Context) error {
	if e.Kind != KindApp || e.pid <= 0 {
		return ErrAppGone
	}
	return focusApp(ctx, e.pid, e.bundle)
}
