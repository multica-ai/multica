//go:build !windows

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Regressions for the second review round on #6275. All three are cases where an
// earlier revision of this branch was *worse* than the launch.go helpers it
// replaced, which is the bar any replacement has to clear.

// writeWrapperExitingBeforeChild writes a CLI whose direct child exits 0
// immediately while a backgrounded descendant, holding the inherited stdout,
// prints the answer `delay` later.
//
// This is not a contrived shape. An npm-installed CLI on Windows is reached
// through a shim, a PowerShell launcher spawns a native child, and either can
// return before the process that owes us the answer has written it.
func writeWrapperExitingBeforeChild(t *testing.T, delay, answer string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	body := "#!/bin/sh\n" +
		"( sleep " + delay + "; printf '%s\\n' '" + answer + "' ) &\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return bin
}

// TestDetectCLIVersionWaitsForAWrapperDescendant pins the baseline comparison the
// review asked for: whatever mechanism detectCLIVersion uses must not do worse
// than launch.go's outputOwned on the same stub.
//
// The failure it guards against was measured on an earlier revision: treating the
// direct child's exit as "the answer is in" returned an empty version with a *nil
// error* in 0.41s, while outputOwned returned the version in 0.68s. Empty-and-nil
// is the worst possible shape here — DetectVersion routes all 23 providers through
// this function, and a caller cannot tell that answer from a CLI that legitimately
// prints nothing.
func TestDetectCLIVersionWaitsForAWrapperDescendant(t *testing.T) {
	bin := writeWrapperExitingBeforeChild(t, "0.5", "fake-cli 1.2.3")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got, err := detectCLIVersion(ctx, Command{Path: bin, logger: slog.Default()})
	if err != nil {
		t.Fatalf("detectCLIVersion: %v", err)
	}
	if !strings.Contains(got, "1.2.3") {
		t.Fatalf("version = %q, want the descendant's output — leader exit is not "+
			"the end of output, only pipe EOF is", got)
	}

	// The baseline, on the identical stub, so this test fails if the mechanism
	// ever regresses below it rather than merely changing.
	cmd := Command{Path: bin}.exec(ctx)
	hideAgentWindow(cmd)
	out, oerr := outputOwned(cmd, slog.Default())
	if oerr != nil {
		t.Fatalf("outputOwned baseline: %v", oerr)
	}
	baseline, _ := extractVersionLine(string(out))
	if !strings.Contains(baseline, "1.2.3") {
		t.Fatalf("baseline outputOwned = %q, want the version — the stub is wrong, "+
			"not the code under test", out)
	}
}

// TestDetectCLIVersionDoesNotSalvageABannerAsTheVersion pins the third-round
// review finding: the ErrWaitDelay salvage was gated on `version != ""`, and
// extractVersionLine's trimmed-raw fallback makes any non-empty text satisfy
// that — including a line the wrapper printed before the real version existed.
//
// Measured on the reviewed head with this stub: detectCLIVersion returned
// version="initializing plugins" with a nil error in 2.31s, and logged "CLI
// answered but left its output pipes open" — the opposite of what happened. The
// banner would then be persisted as the runtime's version for every one of the 23
// providers, since DetectVersion routes them all through here.
//
// The contract: when the bound expires and no *recognised* version arrived, the
// original error stands. There is no answer to salvage.
func TestDetectCLIVersionDoesNotSalvageABannerAsTheVersion(t *testing.T) {
	// Banner on stdout, leader exits 0, and the real version arrives from a
	// descendant holding the pipe well past the 2s WaitDelay this probe sets.
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	body := "#!/bin/sh\n" +
		"printf 'initializing plugins\\n'\n" +
		"( sleep 5; printf 'fake-cli 1.2.3\\n' ) &\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got, err := detectCLIVersion(ctx, Command{Path: bin, logger: slog.Default()})
	if err == nil {
		t.Fatalf("detectCLIVersion = %q with a nil error; a banner is not the "+
			"answer, so the ErrWaitDelay failure must stand", got)
	}
	if got != "" {
		t.Errorf("version = %q, want empty — a failed probe must not report a "+
			"version the CLI never gave", got)
	}
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Errorf("err = %v, want it to wrap exec.ErrWaitDelay (the bound that "+
			"actually expired)", err)
	}
}

// TestRunCollectQuietWaitsForAWrapperDescendant is the same contract on the
// collector, which is the one mechanism in this package that can return before
// pipe EOF.
//
// It may do so only when the caller's completeness rule says the answer is in.
// With a rule that is not yet satisfied at leader exit, it has to keep waiting —
// bounded by collectDrainGrace — or it kills the process that owes the answer.
func TestRunCollectQuietWaitsForAWrapperDescendant(t *testing.T) {
	bin := writeWrapperExitingBeforeChild(t, "0.5", `{"ok":true}`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, _, _, err := RunCollectQuiet(ctx, nil, 0, JSONOutputComplete, bin)
	if err != nil {
		t.Fatalf("RunCollectQuiet: %v", err)
	}
	if strings.TrimSpace(string(out)) != `{"ok":true}` {
		t.Fatalf("stdout = %q, want the descendant's document", out)
	}
}

// TestRunCollectQuietDoesNotWaitWhenTheAnswerIsIn is the other half: the drain
// wait must not become a tax on every call.
//
// A CLI that prints a complete answer and exits, leaving a helper on the pipe —
// which is what OpenClaw does on every invocation — has nothing left to wait for,
// so the rule short-circuits the drain and the call returns promptly rather than
// paying collectDrainGrace.
func TestRunCollectQuietDoesNotWaitWhenTheAnswerIsIn(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	body := "#!/bin/sh\n" +
		"printf '{\"ok\":true}\\n'\n" +
		"sleep 300 &\n" + // inherits stdout, so EOF never arrives
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	out, _, _, err := RunCollectQuiet(ctx, nil, 0, JSONOutputComplete, bin)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunCollectQuiet: %v", err)
	}
	if strings.TrimSpace(string(out)) != `{"ok":true}` {
		t.Fatalf("stdout = %q", out)
	}
	if elapsed >= collectDrainGrace {
		t.Errorf("took %v, i.e. at least the full drain grace (%v) — a satisfied "+
			"completeness rule must short-circuit the wait for EOF", elapsed, collectDrainGrace)
	}
}

// TestCollectedStderrKeepsOnlyItsTail pins the memory bound the review found
// missing. An earlier revision retained 13,107,400 bytes of stderr from a CLI
// writing continuously, where launch.go's outputOwned keeps the last
// probeStderrSampleBytes; a broken local CLI in a log loop could exhaust daemon
// memory inside the probe window.
//
// The tail rather than the head, matching outputOwned, because a CLI's actual
// failure line is at the end.
func TestCollectedStderrKeepsOnlyItsTail(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	// ~64 KiB per line, 200 lines: ~12.8 MB, i.e. 400x the bound.
	body := "#!/bin/sh\n" +
		"line=$(printf 'x%.0s' $(seq 1 65536))\n" +
		"i=0\n" +
		"while [ $i -lt 200 ]; do printf '%s\\n' \"$line\" >&2; i=$((i+1)); done\n" +
		"printf 'LAST-STDERR-LINE\\n' >&2\n" +
		"printf '{\"ok\":true}\\n'\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, stderr, _, err := RunCollectQuiet(ctx, nil, 0, JSONOutputComplete, bin)
	if err != nil {
		t.Fatalf("RunCollectQuiet: %v", err)
	}
	if len(stderr) > collectStderrTail {
		t.Errorf("retained %d bytes of stderr, want at most %d — the bound "+
			"outputOwned already applied", len(stderr), collectStderrTail)
	}
	if !strings.Contains(stderr, "LAST-STDERR-LINE") {
		t.Error("the tail was dropped instead of the head; a failed probe's " +
			"diagnosis is its last line")
	}
}

// TestCollectedStdoutOverflowIsReportedNotTruncated is the stdout half of the
// same bound, and it is deliberately not symmetric with stderr.
//
// stderr is a sample, so dropping its front costs nothing. stdout is the answer,
// and handing a caller a head-truncated answer is how a partial catalog becomes a
// confident empty one — so overflow is an error instead.
func TestCollectedStdoutOverflowIsReportedNotTruncated(t *testing.T) {
	prev := collectStdoutLimit
	collectStdoutLimit = 64 << 10
	t.Cleanup(func() { collectStdoutLimit = prev })

	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	body := "#!/bin/sh\n" +
		"line=$(printf 'y%.0s' $(seq 1 65536))\n" +
		"i=0\n" +
		"while [ $i -lt 8 ]; do printf '%s\\n' \"$line\"; i=$((i+1)); done\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, _, _, err := RunCollectQuiet(ctx, nil, 0, nil, bin)
	if err == nil {
		t.Fatalf("an over-limit answer must be reported, not silently shortened "+
			"(got %d bytes and a nil error)", len(out))
	}
	if !errors.Is(err, errCollectStdoutTooLarge) {
		t.Errorf("err = %v, want errCollectStdoutTooLarge", err)
	}
}

// TestCollectedStdoutBoundaryIsExact pins where that cap falls, because it is a
// compatibility boundary and not only a memory bound: a legal answer one byte
// too large stops a task from starting. Off-by-one here would move the boundary
// without anyone noticing.
//
// The limit is shrunk rather than fed 8 MiB, for the same reason
// detectVersionTimeout is a var: the property under test is the comparison, not
// the constant.
func TestCollectedStdoutBoundaryIsExact(t *testing.T) {
	prev := collectStdoutLimit
	collectStdoutLimit = 8 << 10
	t.Cleanup(func() { collectStdoutLimit = prev })

	catBin, lookErr := exec.LookPath("cat")
	if lookErr != nil {
		t.Skipf("no cat binary to emit an exact byte count: %v", lookErr)
	}

	cases := []struct {
		name       string
		size       int
		wantErrIs  error
		wantOutLen int
	}{
		{name: "exactly at the limit", size: collectStdoutLimit, wantOutLen: collectStdoutLimit},
		{name: "one byte past the limit", size: collectStdoutLimit + 1, wantErrIs: errCollectStdoutTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// A file rather than a shell loop, so the byte count is exact — no
			// trailing newline a printf would add.
			payload := filepath.Join(dir, "payload")
			if err := os.WriteFile(payload, bytes.Repeat([]byte("z"), tc.size), 0o600); err != nil {
				t.Fatalf("write payload: %v", err)
			}
			bin := filepath.Join(dir, "fake-cli")
			if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+catBin+" "+payload+"\n"), 0o755); err != nil {
				t.Fatalf("write stub: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			out, _, _, err := RunCollectQuiet(ctx, nil, 0, nil, bin)
			if tc.wantErrIs != nil {
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("%d bytes against a %d-byte limit: err = %v, want %v",
						tc.size, collectStdoutLimit, err, tc.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("%d bytes against a %d-byte limit must be accepted: %v",
					tc.size, collectStdoutLimit, err)
			}
			if len(out) != tc.wantOutLen {
				t.Errorf("kept %d of %d bytes, want all of them", len(out), tc.wantOutLen)
			}
		})
	}
}

// TestCollectStdoutLimitHasHeadroomOverTheLargestAnswer is the derivation behind
// collectStdoutLimit, made executable — the review found the value asserted with
// no upstream limit, measured high-water mark, or capacity argument behind it.
//
// The largest answer any call site asks this collector for is the fully resolved
// OpenClaw config (`config get --json`), whose size scales with the user's agents
// and MCP servers. Upstream publishes no ceiling on either, so the derivation is
// a constructed high-water mark: a host far past anything a real deployment has,
// with the fields OpenClaw actually carries, and a stated multiple on top.
//
// It runs the payload through the collector rather than only comparing lengths,
// so the assertion covers the whole path a large answer takes — chunked reads,
// the head-capped buffer, and the completeness rule.
func TestCollectStdoutLimitHasHeadroomOverTheLargestAnswer(t *testing.T) {
	// The multiple on top of the constructed worst case. 8x, so a host an order
	// of magnitude past the construction is still inside the bound.
	const wantHeadroom = 8

	payload := openclawResolvedConfigHighWaterMark(t)
	t.Logf("constructed high-water mark: %d bytes (%d agents, %d mcp servers); "+
		"collectStdoutLimit = %d (%.0fx)",
		len(payload), highWaterMarkAgents, highWaterMarkMcpServers,
		collectStdoutLimit, float64(collectStdoutLimit)/float64(len(payload)))

	if collectStdoutLimit < wantHeadroom*len(payload) {
		t.Errorf("collectStdoutLimit = %d leaves less than %dx over the largest "+
			"answer we ask for (%d bytes); a legal config past the cap cannot start "+
			"a task at all, so this bound has to be derived rather than picked",
			collectStdoutLimit, wantHeadroom, len(payload))
	}

	catBin, lookErr := exec.LookPath("cat")
	if lookErr != nil {
		t.Skipf("no cat binary to emit the payload verbatim: %v", lookErr)
	}
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "resolved-config.json")
	if err := os.WriteFile(payloadPath, payload, 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	bin := filepath.Join(dir, "fake-cli")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+catBin+" "+payloadPath+"\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, _, _, err := RunCollectQuiet(ctx, nil, 0, JSONOutputComplete, bin)
	if err != nil {
		t.Fatalf("the largest answer we ask for must come back cleanly: %v", err)
	}
	if !bytes.Equal(out, payload) {
		t.Errorf("answer came back changed: %d bytes of %d", len(out), len(payload))
	}
}

const (
	// Deliberately past any real deployment: the largest OpenClaw configs seen in
	// the field carry single-digit agent counts.
	highWaterMarkAgents     = 1000
	highWaterMarkMcpServers = 250
)

// openclawResolvedConfigHighWaterMark builds a `config get --json` payload for a
// host at the scale above, with the fields OpenClaw's resolved config actually
// carries, so its size is a defensible worst case rather than a round number.
func openclawResolvedConfigHighWaterMark(t *testing.T) []byte {
	t.Helper()

	agents := make([]map[string]any, 0, highWaterMarkAgents)
	for i := range highWaterMarkAgents {
		agents = append(agents, map[string]any{
			"id":        fmt.Sprintf("agent-with-a-descriptive-name-%04d", i),
			"workspace": fmt.Sprintf("/Users/somebody/work/very/deeply/nested/checkout-%04d/service", i),
			"model": map[string]any{
				"primary":  "anthropic/claude-sonnet-4-6",
				"fallback": "openai/gpt-5.5-codex-preview",
			},
			"instructions": "Follow the repository guidelines, prefer small changes, and " +
				"always run the package's tests before handing work back for review.",
			"skills":      []string{"code-review", "release-notes", "incident-triage", "migrations"},
			"permissions": map[string]any{"edit": true, "shell": true, "network": false},
		})
	}

	servers := make(map[string]any, highWaterMarkMcpServers)
	for i := range highWaterMarkMcpServers {
		servers[fmt.Sprintf("mcp-server-%04d", i)] = map[string]any{
			"command": "/opt/homebrew/bin/node",
			"args":    []string{fmt.Sprintf("/Users/somebody/.openclaw/servers/server-%04d/dist/index.js", i), "--stdio"},
			"env": map[string]string{
				"SERVER_TOKEN": "__OPENCLAW_REDACTED__",
				"SERVER_HOME":  fmt.Sprintf("/Users/somebody/.openclaw/servers/server-%04d", i),
			},
		}
	}

	payload, err := json.Marshal(map[string]any{
		"agents": map[string]any{"list": agents},
		"mcp":    map[string]any{"servers": servers},
	})
	if err != nil {
		t.Fatalf("build high-water-mark payload: %v", err)
	}
	return payload
}
