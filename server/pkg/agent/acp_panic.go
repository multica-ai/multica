package agent

import (
	"regexp"
	"strings"
	"sync"
)

// acpRuntimePanicDetector watches an ACP runtime's stderr for a panic report
// and signals it the moment the header line arrives.
//
// Why it has to be live rather than a post-mortem scan: a Rust ACP runtime
// (Kiro) can panic inside its agent loop while the ACP server task keeps
// running. The process stays alive, stdout stays open, and the in-flight
// session/prompt never gets a response or an error. Every existing stderr
// check (promoteACPResultOnProviderError) only runs after the process exits,
// so it never sees this panic, and the task sits in "waiting for reply" until
// the 2h idle watchdog or a manual cancel (MUL-8188). Observed verbatim:
//
//	The application panicked (crashed).
//	Message:  first agent loop request should never fail: AgentLoopResponse(Custom("invalid conversation history received"))
//	Location: crates/agent/src/agent/mod.rs:3744
//
// Only line-anchored panic headers count, so an ordinary log line that merely
// mentions the word "panicked" does not end a healthy run.
type acpRuntimePanicDetector struct {
	mu       sync.Mutex
	remains  []byte
	fired    bool
	lines    []string // header + up to acpPanicDetailLines following lines
	detected chan struct{}
}

// acpPanicDetailLines bounds how many lines after the header are kept for the
// error text: enough for color-eyre's Message/Location pair or Rust's default
// "panicked at <loc>:\n<msg>" shape, without swallowing a whole backtrace.
const acpPanicDetailLines = 3

// acpPanicDetailMaxLen caps the surfaced panic text so a pathological message
// cannot bloat the task error column.
const acpPanicDetailMaxLen = 600

var (
	// color-eyre / human-panic header (Kiro installs color-eyre) and the Rust
	// default hook header ("thread 'main' panicked at src/x.rs:1:2:").
	acpPanicHeaderRe = regexp.MustCompile(`^(The application panicked\b|thread '[^']*' panicked at\b)`)
	ansiEscapeRe     = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
)

func newACPRuntimePanicDetector() *acpRuntimePanicDetector {
	return &acpRuntimePanicDetector{detected: make(chan struct{})}
}

// Write implements io.Writer so the detector can sit behind the stderr
// io.MultiWriter next to the log forwarder and the provider-error sniffer.
func (d *acpRuntimePanicDetector) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data := append(d.remains, p...)
	nl := strings.LastIndexByte(string(data), '\n')
	if nl < 0 {
		d.remains = append(d.remains[:0], data...)
		return len(p), nil
	}
	complete := string(data[:nl])
	d.remains = append(d.remains[:0], data[nl+1:]...)

	for _, raw := range strings.Split(complete, "\n") {
		line := strings.TrimSpace(ansiEscapeRe.ReplaceAllString(strings.TrimSuffix(raw, "\r"), ""))
		if line == "" {
			continue
		}
		if !d.fired {
			if acpPanicHeaderRe.MatchString(line) {
				d.fired = true
				d.lines = append(d.lines, line)
				close(d.detected)
			}
			continue
		}
		if len(d.lines) <= acpPanicDetailLines {
			d.lines = append(d.lines, line)
		}
	}
	return len(p), nil
}

// Detected is closed when the first panic header is seen.
func (d *acpRuntimePanicDetector) Detected() <-chan struct{} {
	return d.detected
}

// Fired reports whether a panic header has been seen.
func (d *acpRuntimePanicDetector) Fired() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.fired
}

// Detail returns the captured panic header and the lines that followed it,
// joined into a single line. Read it after the stderr copier has drained so
// the Message/Location lines that trail the header are included.
func (d *acpRuntimePanicDetector) Detail() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	detail := strings.Join(d.lines, " | ")
	if len(detail) > acpPanicDetailMaxLen {
		detail = strings.ToValidUTF8(detail[:acpPanicDetailMaxLen], "") + "…"
	}
	return detail
}
