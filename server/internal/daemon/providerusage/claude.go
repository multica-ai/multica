package providerusage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
)

// claudeUsageArgs is the print-mode invocation that reads /usage without
// writing a session transcript or starting the user's MCP servers. It does
// not refresh OAuth.
var claudeUsageArgs = []string{"--print", "--no-session-persistence", "--strict-mcp-config", "/usage"}

var claudeUsageLine = regexp.MustCompile(`(?i)^Current (?:(session)|week \(([^)]+)\)):\s*(\d+(?:\.\d+)?)%\s*used(?:\s*[·|\-]\s*resets\s*(.+?))?\s*$`)

var claudeResetLine = regexp.MustCompile(`(?i)^([A-Za-z]{3})\s+(\d{1,2})\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)(?:\s*\(([^)]+)\))?`)

var claudePlanPhrases = []string{"Max 20x", "Max 5x", "extra usage", "Max", "Pro", "Team"}

// ClaudeCommand runs claude and returns stdout, a short stderr prefix, and
// the exit code. Tests inject this so the real CLI is never executed.
type ClaudeCommand func(ctx context.Context, path string, args []string, dir string) (stdout, stderr string, exitCode int, err error)

// ClaudeCollector reads plan limits from `claude /usage` stdout.
type ClaudeCollector struct {
	// Locate returns the claude binary. Nil uses the production search.
	Locate func() (string, error)
	// Run executes the binary. Nil uses exec.CommandContext.
	Run ClaudeCommand
	Now func() time.Time
}

func (c ClaudeCollector) Collect(ctx context.Context) Result {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	locate := c.Locate
	if locate == nil {
		locate = locateClaude
	}
	path, err := locate()
	if err != nil || path == "" {
		return emptyResult(ProviderClaude, ReasonCLIUnavailable, now)
	}
	run := c.Run
	if run == nil {
		run = runClaude
	}
	dir, _ := claudeScratchDir()
	stdout, stderr, _, runErr := run(ctx, path, claudeUsageArgs, dir)
	if runErr != nil && stdout == "" {
		return Result{Upload: false}
	}
	windows, plan, ok := ParseClaudeUsage(stdout, now)
	if ok {
		return Result{
			Upload: true,
			Snapshot: Snapshot{
				Provider:    ProviderClaude,
				PlanName:    plan,
				CollectedAt: now,
				Windows:     windows,
			},
		}
	}
	if claudeLooksLoggedOut(stdout) || claudeLooksLoggedOut(stderr) {
		return emptyResult(ProviderClaude, ReasonNotLoggedIn, now)
	}
	return Result{Upload: false}
}

func emptyResult(provider, reason string, now time.Time) Result {
	return Result{
		Upload: true,
		Snapshot: Snapshot{
			Provider:    provider,
			PlanName:    "",
			CollectedAt: now,
			ReasonCode:  reason,
		},
	}
}

// ParseClaudeUsage extracts session and weekly windows from /usage stdout.
// ok is false when the text has no limit line. A window whose reset text
// does not parse is kept with a nil reset.
func ParseClaudeUsage(text string, now time.Time) (windows []Window, plan string, ok bool) {
	plan = claudePlan(text)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		m := claudeUsageLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		percent, err := parsePercent(m[3])
		if err != nil {
			continue
		}
		id := "session"
		if m[1] == "" {
			id = weeklyWindowID(m[2])
		}
		var resets *time.Time
		if strings.TrimSpace(m[4]) != "" {
			if t, parsed := parseClaudeReset(m[4], now); parsed {
				resets = &t
			}
		}
		windows = append(windows, Window{ID: id, PercentUsed: percent, ResetsAt: resets})
	}
	return windows, plan, len(windows) > 0
}

func claudePlan(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) > 4 {
		lines = lines[:4]
	}
	head := strings.ToLower(strings.Join(lines, "\n"))
	for _, phrase := range claudePlanPhrases {
		if strings.Contains(head, strings.ToLower(phrase)) {
			return phrase
		}
	}
	return ""
}

func weeklyWindowID(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "all models" {
		return "weekly_all"
	}
	var b strings.Builder
	prevUnderscore := false
	for _, r := range n {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		case r == ' ' || r == '-' || r == '_':
			if !prevUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	id := strings.Trim(b.String(), "_")
	if id == "" {
		return "weekly"
	}
	return "weekly_" + id
}

func parsePercent(s string) (float64, error) {
	var v float64
	_, err := fmt.Sscanf(s, "%f", &v)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func parseClaudeReset(fragment string, now time.Time) (time.Time, bool) {
	m := claudeResetLine.FindStringSubmatch(strings.TrimSpace(fragment))
	if m == nil {
		return time.Time{}, false
	}
	month, ok := claudeMonth(m[1])
	if !ok {
		return time.Time{}, false
	}
	day := atoi(m[2])
	hour := to24(atoi(m[3]), strings.ToLower(m[5]))
	minute := 0
	if m[4] != "" {
		minute = atoi(m[4])
	}
	if hour < 0 || day < 1 || day > 31 || minute < 0 || minute > 59 {
		return time.Time{}, false
	}
	loc := time.UTC
	if zone := strings.TrimSpace(m[6]); zone != "" {
		loaded, err := time.LoadLocation(zone)
		if err != nil {
			return time.Time{}, false
		}
		loc = loaded
	}
	candidate := time.Date(now.Year(), month, day, hour, minute, 0, 0, loc)
	const halfYear = 180 * 24 * time.Hour
	if now.Sub(candidate) > halfYear {
		candidate = candidate.AddDate(1, 0, 0)
	} else if candidate.Sub(now) > halfYear {
		candidate = candidate.AddDate(-1, 0, 0)
	}
	return candidate, true
}

func claudeMonth(s string) (time.Month, bool) {
	switch strings.ToLower(s) {
	case "jan":
		return time.January, true
	case "feb":
		return time.February, true
	case "mar":
		return time.March, true
	case "apr":
		return time.April, true
	case "may":
		return time.May, true
	case "jun":
		return time.June, true
	case "jul":
		return time.July, true
	case "aug":
		return time.August, true
	case "sep":
		return time.September, true
	case "oct":
		return time.October, true
	case "nov":
		return time.November, true
	case "dec":
		return time.December, true
	default:
		return 0, false
	}
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func to24(hour int, ampm string) int {
	if hour < 1 || hour > 12 {
		return -1
	}
	if ampm == "am" {
		if hour == 12 {
			return 0
		}
		return hour
	}
	if ampm == "pm" {
		if hour == 12 {
			return 12
		}
		return hour + 12
	}
	return -1
}

func claudeLooksLoggedOut(text string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range []string{"/login", "not logged", "sign in", "signin", "log in", "authenticate", "unauthorized"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func locateClaude() (string, error) {
	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, rel := range []string{
		".local/bin/claude",
		".claude/local/claude",
		".npm-global/bin/claude",
		".bun/bin/claude",
	} {
		path := filepath.Join(home, rel)
		if isExecutable(path) {
			return path, nil
		}
	}
	for _, path := range []string{"/usr/local/bin/claude", "/usr/bin/claude"} {
		if isExecutable(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("claude cli not found")
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func claudeScratchDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "multica", "claude-usage-scratch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func runClaude(ctx context.Context, path string, args []string, dir string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	if dir != "" {
		cmd.Dir = dir
	}
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

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.w.Write(p)
	l.n -= n
	if err != nil {
		return n, err
	}
	return len(p), nil
}
