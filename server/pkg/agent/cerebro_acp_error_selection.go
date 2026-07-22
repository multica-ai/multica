package agent

// Cerebro-only companion to the acpProviderErrorSniffer in hermes.go
// (FIR-3651). It owns two decisions the upstream sniffer got wrong:
// which stderr lines belong to a provider-error block at all, and which
// of the captured lines describes the failure that actually ended the
// run.
//
// Both are load-bearing beyond cosmetics. The failure text is what
// taskfailure.Classify() reads, and the resulting failure_reason is what
// failrouter turns into retry / pause / surface. A run killed by an
// upstream 429 that gets labelled with an unrelated line lands in
// agent_error.unknown — the one bucket with no retry at all — so a
// transient rate limit becomes a permanent red failure on the issue.

import (
	"regexp"
	"strings"
)

// acpSelfEvidentProviderErrorRe matches a detail line that carries provider
// evidence on its own — an HTTP status or a named provider error kind — and so
// is safe to capture even when no header line opened the block.
//
// This is the safety valve on acpCaptureErrorLine's header gate: an adapter
// that emits a bare "Error: HTTP 500: ..." with no ⚠️/❌ banner must still be
// captured, otherwise the run reports empty output instead of a real failure.
// An ordinary failing shell command carries neither marker, so it stays out.
var acpSelfEvidentProviderErrorRe = regexp.MustCompile(`HTTP [0-9]{3}|BadRequestError|AuthenticationError|RateLimitError|Non-retryable|API call failed`)

// acpCaptureErrorLine reports whether a stderr line should be captured
// as part of a provider-error block.
//
// A detail line ("Error: ...", "detail: ...") only counts once a header
// line has introduced an error block, which is what acpErrorDetailRe's
// own docstring describes ("the subsequent lines of the error block")
// but did not enforce. Without the gate the pattern matches any line
// containing "Error:" — including hermes' stderr echo of an ordinary
// terminal tool that exited non-zero, e.g.
//
//	terminal result - **output:** Error: write file: open x/y: no such file or directory", "exit_code": 1
//
// That is normal run output: the agent sees the failure and corrects
// itself. Capturing it as a provider error is how production run
// 17fe5ffb reported a recovered file-write mistake as the cause of a
// run that in fact died on an upstream HTTP 429 a half minute later.
func acpCaptureErrorLine(line string, sawHeader bool) bool {
	if acpErrorHeaderRe.MatchString(line) {
		return true
	}
	if !acpErrorDetailRe.MatchString(line) {
		return false
	}
	return sawHeader || acpSelfEvidentProviderErrorRe.MatchString(line)
}

// selectACPErrorDetail returns the newest captured line that describes
// the provider failure: the most recent "Error:" / "detail:" fragment,
// otherwise the most recent header line. Returns "" when lines holds
// nothing useful.
//
// Newest rather than oldest, because a run's error lines are appended
// in wall-clock order and the line that ended the run is the last one.
// The upstream implementation returned the FIRST match, so an early
// transient warning outranked the terminal failure that followed it.
func selectACPErrorDetail(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if m := acpErrorDetailRe.FindStringSubmatch(lines[i]); m != nil {
			if detail := strings.TrimSpace(m[1]); detail != "" {
				return detail
			}
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if acpErrorHeaderRe.MatchString(lines[i]) {
			return lines[i]
		}
	}
	return ""
}
