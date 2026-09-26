package providerusage

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// kiroUsageArgs asks the signed-in CLI for its own /usage card. The daemon
// does not open Kiro's sqlite token store.
var kiroUsageArgs = []string{"chat", "--no-interactive", "/usage"}

var (
	kiroANSI      = regexp.MustCompile("\x1b\\[[0-9;:?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")
	kiroPercent   = regexp.MustCompile(`█+\s*(\d+(?:\.\d+)?)%`)
	kiroCredits   = regexp.MustCompile(`\((\d+(?:\.\d+)?)\s+of\s+(\d+(?:\.\d+)?)\s+covered`)
	kiroPlan      = regexp.MustCompile(`(?i)Plan:[ \t]*([^|\r\n]+)`)
	kiroReset     = regexp.MustCompile(`(?i)resets on (\d{4}-\d{2}-\d{2}|\d{1,2}/\d{1,2})`)
	kiroBonus     = regexp.MustCompile(`(?i)Bonus credits:\s*(\d+(?:\.\d+)?)/(\d+(?:\.\d+)?)`)
	kiroLoggedOut = []string{"not logged in", "login required", "failed to initialize auth portal", "kiro-cli login", "oauth error"}
)

// KiroCollector reads plan credits from `kiro-cli /usage` stdout.
type KiroCollector struct {
	Locate func() (string, error)
	Run    ClaudeCommand
	Now    func() time.Time
}

func (c KiroCollector) Collect(ctx context.Context) Result {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	locate := c.Locate
	if locate == nil {
		locate = locateKiro
	}
	path, err := locate()
	if err != nil || path == "" {
		return emptyResult(ProviderKiro, ReasonCLIUnavailable, now)
	}
	run := c.Run
	if run == nil {
		run = runKiro
	}
	stdout, stderr, _, runErr := run(ctx, path, kiroUsageArgs, "")
	text := stdout
	if runErr != nil && strings.TrimSpace(text) == "" {
		return Result{Upload: false}
	}
	if kiroLooksLoggedOut(text) || kiroLooksLoggedOut(stderr) {
		return emptyResult(ProviderKiro, ReasonNotLoggedIn, now)
	}
	windows, plan, ok := ParseKiroUsage(text, now)
	if !ok {
		return Result{Upload: false}
	}
	return Result{
		Upload: true,
		Snapshot: Snapshot{
			Provider:    ProviderKiro,
			PlanName:    plan,
			CollectedAt: now,
			Windows:     windows,
		},
	}
}

func locateKiro() (string, error) {
	if path, err := exec.LookPath("kiro-cli"); err == nil && filepath.IsAbs(path) && isExecutable(path) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, path := range []string{
		filepath.Join(home, ".local", "bin", "kiro-cli"),
		"/opt/homebrew/bin/kiro-cli",
		"/usr/local/bin/kiro-cli",
		"/usr/bin/kiro-cli",
	} {
		if isExecutable(path) {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

func runKiro(ctx context.Context, path string, args []string, dir string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "TERM=dumb")
	cmd.Stdin = bytes.NewReader(nil)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdout, n: 256 * 1024}
	cmd.Stderr = &limitedWriter{w: &stderr, n: 4 * 1024}
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if ctx.Err() != nil {
			return stdout.String(), stderr.String(), -1, ctx.Err()
		}
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
			err = nil
		}
	}
	return stdout.String(), stderr.String(), exitCode, err
}

// ParseKiroUsage extracts the credit bar from kiro-cli /usage text.
func ParseKiroUsage(text string, now time.Time) (windows []Window, plan string, ok bool) {
	text = kiroANSI.ReplaceAllString(text, "")
	if kiroLooksLoggedOut(text) {
		return nil, "", false
	}
	if match := kiroPlan.FindStringSubmatch(text); len(match) == 2 {
		plan = trimPlan(kiroDisplayPlan(match[1]))
	}
	var percent float64
	hasPercent := false
	if match := kiroPercent.FindStringSubmatch(text); len(match) == 2 {
		if value, err := strconv.ParseFloat(match[1], 64); err == nil {
			if value, usable := usablePercent(value); usable {
				percent = value
				hasPercent = true
			}
		}
	}
	if !hasPercent {
		if match := kiroCredits.FindStringSubmatch(text); len(match) == 3 {
			used, usedErr := strconv.ParseFloat(match[1], 64)
			total, totalErr := strconv.ParseFloat(match[2], 64)
			if usedErr == nil && totalErr == nil && total > 0 {
				if value, usable := usablePercent(used / total * 100); usable {
					percent = value
					hasPercent = true
				}
			}
		}
	}
	if hasPercent {
		windows = append(windows, Window{
			ID:          "credits",
			PercentUsed: percent,
			ResetsAt:    kiroResetTime(text, now),
		})
	}
	if match := kiroBonus.FindStringSubmatch(text); len(match) == 3 {
		used, usedErr := strconv.ParseFloat(match[1], 64)
		total, totalErr := strconv.ParseFloat(match[2], 64)
		if usedErr == nil && totalErr == nil && total > 0 {
			if value, usable := usablePercent(used / total * 100); usable && len(windows) < 8 {
				windows = append(windows, Window{ID: "bonus", PercentUsed: value})
			}
		}
	}
	return windows, plan, len(windows) > 0
}

func kiroLooksLoggedOut(text string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range kiroLoggedOut {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func kiroDisplayPlan(raw string) string {
	raw = strings.Join(strings.Fields(raw), " ")
	if !strings.Contains(strings.ToLower(raw), "kiro") {
		return raw
	}
	parts := strings.Fields(raw)
	for i, part := range parts {
		if strings.EqualFold(part, "kiro") {
			parts[i] = "Kiro"
			continue
		}
		lower := strings.ToLower(part)
		parts[i] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	return strings.Join(parts, " ")
}

func kiroResetTime(text string, now time.Time) *time.Time {
	match := kiroReset.FindStringSubmatch(text)
	if len(match) != 2 {
		return nil
	}
	stamp := match[1]
	if strings.Contains(stamp, "-") {
		parsed, err := time.ParseInLocation("2006-01-02", stamp, time.Local)
		if err != nil {
			return nil
		}
		utc := parsed.UTC()
		return &utc
	}
	parts := strings.Split(stamp, "/")
	if len(parts) != 2 {
		return nil
	}
	month, monthErr := strconv.Atoi(parts[0])
	day, dayErr := strconv.Atoi(parts[1])
	if monthErr != nil || dayErr != nil || month < 1 || month > 12 || day < 1 || day > 31 {
		return nil
	}
	year := now.Year()
	parsed := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
	if parsed.Before(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)) {
		parsed = parsed.AddDate(1, 0, 0)
	}
	utc := parsed.UTC()
	return &utc
}
