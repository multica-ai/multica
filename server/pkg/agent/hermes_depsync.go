package agent

import (
	"log/slog"
	"strings"
	"sync"
	"time"
)

// hermesDepSyncMarkers are the stderr lines Hermes prints right before it
// rebuilds its managed Python dependency environment at launch
// (hermes_cli/venv_sync.py prepare_launch). Matched as prefixes of the trimmed
// line so a trailing detail (the data-root variant names the root) still
// matches.
//
// Why this is surfaced: Hermes keys the dependency stamp on the ACTIVE home's
// plugin selection. Multica's per-task Hermes home links installs/ back to the
// user's home but blanks memory.provider (no cross-task memory sharing), so a
// user whose memory provider plugin declares Python dependencies gets a
// different stamp per home. Alternating between the Hermes CLI and Multica
// tasks then reinstalls the shared environment on every switch (~45-60s and a
// new generation on disk each time; GH multica#9390, upstream
// NousResearch/hermes-agent#137726). The stderr forwarder logs at Debug, so
// without this the cost is invisible in a default daemon log.
var hermesDepSyncMarkers = []string{
	"hermes: completing source-update dependencies",
	"hermes: preparing dependencies for this update",
	"hermes: preparing dependencies for this data root",
}

const hermesDepSyncHint = "Hermes resolves its dependency set from the active home's plugins; " +
	"the per-task home disables memory.provider, so a memory plugin with Python dependencies " +
	"in your own Hermes home makes each switch between the Hermes CLI and Multica tasks reinstall " +
	"the shared environment (multica#9390, NousResearch/hermes-agent#137726)"

// hermesDepSyncWatcher sniffs Hermes stderr for a launch-time dependency
// reinstall and logs one Warn when it is seen and one once the ACP handshake
// has returned, with the measured startup time. It never alters the task
// outcome. Safe for concurrent Write (stderr copier) and Finish (run
// goroutine).
//
// stdout and stderr are read independently, so the stderr copier may process
// the marker only AFTER the initialize response was handled, even though
// Hermes wrote the marker first. The watcher therefore records both events
// and emits the finish log when the second of them arrives, whichever it is.
// The delay is measured from launch (watcher creation, right after the
// process starts) to the handshake, so it does not depend on when the stderr
// copier got to the marker line.
type hermesDepSyncWatcher struct {
	logger     *slog.Logger
	hermesHome string
	now        func() time.Time
	launched   time.Time

	mu          sync.Mutex
	remains     []byte
	marker      string
	detected    bool
	handshake   bool // Finish has been called
	handshakeOK bool
	handshakeAt time.Time
	finished    bool // finish log emitted
}

func newHermesDepSyncWatcher(logger *slog.Logger, hermesHome string) *hermesDepSyncWatcher {
	return newHermesDepSyncWatcherAt(logger, hermesHome, time.Now)
}

func newHermesDepSyncWatcherAt(logger *slog.Logger, hermesHome string, now func() time.Time) *hermesDepSyncWatcher {
	return &hermesDepSyncWatcher{logger: logger, hermesHome: hermesHome, now: now, launched: now()}
}

// hermesDepSyncRemainsCap bounds the partial-line buffer. Marker lines are
// short; a long unterminated line (tool echo) is irrelevant and is dropped
// rather than buffered without limit.
const hermesDepSyncRemainsCap = 4096

func (w *hermesDepSyncWatcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.detected {
		// One reinstall per launch is all we report; skip scanning.
		return len(p), nil
	}
	data := append(w.remains, p...)
	nl := strings.LastIndexByte(string(data), '\n')
	if nl < 0 {
		if len(data) > hermesDepSyncRemainsCap {
			data = data[:0]
		}
		w.remains = append(w.remains[:0], data...)
		return len(p), nil
	}
	complete := string(data[:nl])
	rest := data[nl+1:]
	if len(rest) > hermesDepSyncRemainsCap {
		rest = rest[:0]
	}
	w.remains = append(w.remains[:0], rest...)

	for _, line := range strings.Split(complete, "\n") {
		line = strings.TrimSpace(line)
		for _, m := range hermesDepSyncMarkers {
			if strings.HasPrefix(line, m) {
				w.detected = true
				w.marker = line
				w.remains = w.remains[:0]
				w.logger.Warn("hermes is reinstalling its dependency environment before this task starts; startup will be slower",
					"hermes_output", line,
					"hermes_home", w.hermesHome,
					"hint", hermesDepSyncHint,
				)
				w.maybeLogFinishLocked()
				return len(p), nil
			}
		}
	}
	return len(p), nil
}

// Finish records that the ACP initialize handshake has returned (Hermes'
// dependency sync runs before its ACP server starts, so this bounds the
// reinstall's added startup time). ok=false means the handshake failed. Only
// the first call counts. The finish log is emitted here if the marker was
// already seen, or later by Write if the marker is processed afterwards.
func (w *hermesDepSyncWatcher) Finish(ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.handshake {
		return
	}
	w.handshake = true
	w.handshakeOK = ok
	w.handshakeAt = w.now()
	w.maybeLogFinishLocked()
}

func (w *hermesDepSyncWatcher) maybeLogFinishLocked() {
	if !w.detected || !w.handshake || w.finished {
		return
	}
	w.finished = true
	w.logger.Warn("hermes dependency reinstall finished",
		"startup_delay_ms", w.handshakeAt.Sub(w.launched).Milliseconds(),
		"handshake_ok", w.handshakeOK,
		"hermes_output", w.marker,
		"hermes_home", w.hermesHome,
	)
}

// Detected reports whether a dependency reinstall marker has been seen.
func (w *hermesDepSyncWatcher) Detected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.detected
}
