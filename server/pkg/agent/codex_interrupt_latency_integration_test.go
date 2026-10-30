//go:build agentintegration

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	codexInterruptSamplesEnv     = "MULTICA_CODEX_INTERRUPT_SAMPLES"
	codexInterruptWarmupsEnv     = "MULTICA_CODEX_INTERRUPT_WARMUPS"
	codexInterruptSettleDelayEnv = "MULTICA_CODEX_INTERRUPT_SETTLE_DELAY"
	codexInterruptModelEnv       = "MULTICA_CODEX_INTERRUPT_MODEL"
)

type codexInterruptScenario struct {
	name          string
	prompt        string
	waitFor       func(Message) bool
	waitForLog    string
	settleDelay   time.Duration
	sampleTimeout time.Duration
}

type codexInterruptSample struct {
	interruptLatency time.Duration
	resultLatency    time.Duration
	status           string
	inputTokens      int64
	outputTokens     int64
}

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// TestCodexRealTurnInterruptLatency measures the real app-server boundary that
// TurnInterruptTimeout protects. It is intentionally excluded from ordinary
// test runs: besides requiring an installed, authenticated Codex CLI, every
// sample makes a provider request and may consume quota.
//
// Run it explicitly with:
//
//	MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags=agentintegration ./pkg/agent \
//	  -run TestCodexRealTurnInterruptLatency -count=1 -v
//
// MULTICA_CODEX_INTERRUPT_SAMPLES and MULTICA_CODEX_INTERRUPT_WARMUPS control
// the measured and discarded sample counts. MULTICA_CODEX_INTERRUPT_MODEL can
// pin a model; empty preserves the authenticated CLI's configured default.
// MULTICA_CODEX_INTERRUPT_SETTLE_DELAY controls how long generation continues
// after its first observed agent-message delta before cancellation.
func TestCodexRealTurnInterruptLatency(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary interrupt latency test in -short mode")
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex not on PATH; skipping real-binary interrupt latency test")
	}
	version := "unknown"
	if output, versionErr := exec.Command(path, "--version").CombinedOutput(); versionErr == nil {
		version = strings.TrimSpace(string(output))
	}

	samples := positiveEnvInt(t, codexInterruptSamplesEnv, 10, true)
	warmups := positiveEnvInt(t, codexInterruptWarmupsEnv, 1, false)
	settleDelay := durationEnv(t, codexInterruptSettleDelayEnv, 100*time.Millisecond)
	model := strings.TrimSpace(os.Getenv(codexInterruptModelEnv))

	t.Logf("environment: os=%s arch=%s codex=%q model=%q samples=%d warmups=%d settle_delay=%s",
		runtime.GOOS, runtime.GOARCH, version, model, samples, warmups, settleDelay)

	scenarios := []codexInterruptScenario{
		{
			name: "generation",
			prompt: "Write a detailed 2,000-word technical explanation of how cancellation propagates " +
				"through a concurrent server. Do not use tools. Start answering immediately and do not summarize.",
			waitForLog:    `"activity":"item/agentMessage/delta:`,
			settleDelay:   settleDelay,
			sampleTimeout: 90 * time.Second,
		},
		{
			name:   "active_tool",
			prompt: "Use the shell tool now to run exactly `sleep 30`. After it exits, reply with exactly: done",
			waitFor: func(message Message) bool {
				return message.Type == MessageToolUse && message.Tool == "exec_command"
			},
			sampleTimeout: 90 * time.Second,
		},
	}

	for _, scenario := range scenarios {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			measured := make([]codexInterruptSample, 0, samples)
			for i := 0; i < warmups+samples; i++ {
				sample := runCodexInterruptSample(t, path, model, scenario)
				if i < warmups {
					t.Logf("warmup %d: interrupt=%s result=%s status=%s input_tokens=%d output_tokens=%d",
						i+1, sample.interruptLatency, sample.resultLatency, sample.status, sample.inputTokens, sample.outputTokens)
					continue
				}
				measured = append(measured, sample)
				t.Logf("sample %d/%d: interrupt=%s result=%s status=%s input_tokens=%d output_tokens=%d",
					i-warmups+1, samples, sample.interruptLatency, sample.resultLatency, sample.status, sample.inputTokens, sample.outputTokens)
			}

			interrupts := make([]time.Duration, 0, len(measured))
			results := make([]time.Duration, 0, len(measured))
			for _, sample := range measured {
				interrupts = append(interrupts, sample.interruptLatency)
				results = append(results, sample.resultLatency)
			}
			logCodexInterruptDistribution(t, "turn/interrupt_to_turn/completed", interrupts)
			logCodexInterruptDistribution(t, "cancel_to_session_result", results)
		})
	}
}

func runCodexInterruptSample(t *testing.T, path, model string, scenario codexInterruptScenario) codexInterruptSample {
	t.Helper()

	var logs synchronizedBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	env := make(map[string]string)
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		// The daemon passes its task-local CODEX_HOME explicitly. Mirror that
		// shape so the post-exit rollout fallback is exercised as well as the
		// live app-server notification path.
		env["CODEX_HOME"] = codexHome
	}
	backend := &codexBackend{cfg: Config{
		ExecutablePath: path,
		Env:            env,
		Logger:         logger,
		TaskID:         "interrupt-latency-" + scenario.name,
	}}

	parentCtx, stop := context.WithTimeout(context.Background(), scenario.sampleTimeout)
	defer stop()
	runCtx, cancelRun := context.WithCancel(parentCtx)
	session, err := backend.executeOnce(runCtx, scenario.prompt, ExecOptions{
		Cwd:                  t.TempDir(),
		Model:                model,
		Timeout:              scenario.sampleTimeout,
		TurnInterruptTimeout: 10 * time.Second,
		ThinkingLevel:        "low",
	}, 1)
	if err != nil {
		t.Fatalf("start Codex sample: %v", err)
	}

	cancelled := make(chan time.Time, 1)
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			triggered := false
			select {
			case message, ok := <-session.Messages:
				if !ok {
					return
				}
				triggered = scenario.waitFor != nil && scenario.waitFor(message)
			case <-ticker.C:
				triggered = scenario.waitForLog != "" && strings.Contains(logs.String(), scenario.waitForLog)
			case <-parentCtx.Done():
				return
			}
			if !triggered {
				continue
			}
			if scenario.settleDelay > 0 {
				timer := time.NewTimer(scenario.settleDelay)
				select {
				case <-timer.C:
				case <-parentCtx.Done():
					timer.Stop()
					return
				}
			}
			cancelAt := time.Now()
			cancelRun()
			cancelled <- cancelAt
			return
		}
	}()

	var cancelAt time.Time
	select {
	case cancelAt = <-cancelled:
	case result := <-session.Result:
		t.Fatalf("scenario %s ended before its cancellation trigger: status=%q error=%q\nlogs:\n%s",
			scenario.name, result.Status, result.Error, logs.String())
	case <-parentCtx.Done():
		t.Fatalf("scenario %s did not reach its cancellation trigger: %v\nlogs:\n%s", scenario.name, parentCtx.Err(), logs.String())
	}

	var result Result
	select {
	case result = <-session.Result:
	case <-parentCtx.Done():
		t.Fatalf("scenario %s did not return a result after cancellation: %v\nlogs:\n%s", scenario.name, parentCtx.Err(), logs.String())
	}
	resultLatency := time.Since(cancelAt)
	if result.Status != "aborted" {
		t.Fatalf("scenario %s status=%q error=%q, want aborted\nlogs:\n%s", scenario.name, result.Status, result.Error, logs.String())
	}
	var inputTokens, outputTokens int64
	for _, usage := range result.Usage {
		inputTokens += usage.InputTokens
		outputTokens += usage.OutputTokens
	}

	interruptLatency, err := codexInterruptLatencyFromLogs(logs.String())
	if err != nil {
		t.Fatalf("scenario %s: %v\nlogs:\n%s", scenario.name, err, logs.String())
	}
	return codexInterruptSample{
		interruptLatency: interruptLatency,
		resultLatency:    resultLatency,
		status:           result.Status,
		inputTokens:      inputTokens,
		outputTokens:     outputTokens,
	}
}

func codexInterruptLatencyFromLogs(logs string) (time.Duration, error) {
	for _, line := range strings.Split(logs, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record["msg"] != "codex turn interrupted" {
			continue
		}
		latency, ok := record["latency"].(string)
		if !ok {
			return 0, fmt.Errorf("interrupt log has no string latency field")
		}
		parsed, err := time.ParseDuration(latency)
		if err != nil {
			return 0, fmt.Errorf("parse interrupt latency %q: %w", latency, err)
		}
		return parsed, nil
	}
	return 0, fmt.Errorf("no successful codex turn interrupt record found")
}

func logCodexInterruptDistribution(t *testing.T, label string, values []time.Duration) {
	t.Helper()
	if len(values) == 0 {
		t.Fatalf("no samples for %s", label)
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	t.Logf("SUMMARY %s n=%d p50=%s p95=%s p99=%s max=%s",
		label,
		len(sorted),
		nearestRankDuration(sorted, 0.50),
		nearestRankDuration(sorted, 0.95),
		nearestRankDuration(sorted, 0.99),
		sorted[len(sorted)-1],
	)
}

func nearestRankDuration(sorted []time.Duration, quantile float64) time.Duration {
	index := int(float64(len(sorted))*quantile+0.999999999) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func positiveEnvInt(t *testing.T, name string, fallback int, requirePositive bool) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || (requirePositive && value == 0) {
		requirement := "a non-negative"
		if requirePositive {
			requirement = "a positive"
		}
		t.Fatalf("%s=%q must be %s integer", name, raw, requirement)
	}
	return value
}

func durationEnv(t *testing.T, name string, fallback time.Duration) time.Duration {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		t.Fatalf("%s=%q must be a non-negative Go duration", name, raw)
	}
	return value
}
