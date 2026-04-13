package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// scanHermes reads Hermes JSONL session files from
// ~/.hermes/sessions/*.jsonl
// and extracts token usage from assistant message and usage_update entries.
//
// Hermes communicates via the ACP (Agent Communication Protocol) and logs
// session events as JSONL. Token usage appears in:
//   - "assistant" messages with a "usage" field
//   - "usage_update" notification entries with cumulative token snapshots
func (s *Scanner) scanHermes() []Record {
	root := hermesSessionRoot()
	if root == "" {
		return nil
	}

	// Glob for session files: sessions/*.jsonl
	pattern := filepath.Join(root, "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		s.logger.Debug("hermes glob error", "error", err)
		return nil
	}

	var allRecords []Record
	for _, f := range files {
		record := s.parseHermesFile(f)
		if record != nil {
			allRecords = append(allRecords, *record)
		}
	}

	return mergeRecords(allRecords)
}

// hermesSessionRoot returns the Hermes sessions directory.
func hermesSessionRoot() string {
	if hermesHome := os.Getenv("HERMES_HOME"); hermesHome != "" {
		dir := filepath.Join(hermesHome, "sessions")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// Check common locations.
	candidates := []string{
		filepath.Join(home, ".hermes", "sessions"),
		filepath.Join(home, ".local", "share", "hermes", "sessions"),
		filepath.Join(home, ".config", "hermes", "sessions"),
	}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}

// hermesLine represents a line in a Hermes session JSONL file.
// Hermes session logs contain both message events and notification events.
type hermesLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"` // RFC3339
	Model     string `json:"model"`
	Usage     *struct {
		InputTokens      int64 `json:"inputTokens"`
		OutputTokens     int64 `json:"outputTokens"`
		CachedReadTokens int64 `json:"cachedReadTokens"`
		ThoughtTokens    int64 `json:"thoughtTokens"`
	} `json:"usage"`
}

// parseHermesFile extracts the final cumulative token usage from a Hermes session file.
// Hermes usage_update events are cumulative snapshots — the last one in the file
// represents the total usage for the session. Returns nil if no usage data found.
func (s *Scanner) parseHermesFile(path string) *Record {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var lastUsage *struct {
		InputTokens      int64 `json:"inputTokens"`
		OutputTokens     int64 `json:"outputTokens"`
		CachedReadTokens int64 `json:"cachedReadTokens"`
		ThoughtTokens    int64 `json:"thoughtTokens"`
	}
	var lastModel string
	var lastTimestamp string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Fast pre-filter.
		if !bytesContains(line, `"usage"`) && !bytesContains(line, `"inputTokens"`) {
			continue
		}

		var entry hermesLine
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		if entry.Usage == nil {
			continue
		}

		// Take the latest usage snapshot (cumulative).
		lastUsage = entry.Usage
		if entry.Model != "" {
			lastModel = entry.Model
		}
		if entry.Timestamp != "" {
			lastTimestamp = entry.Timestamp
		}
	}

	if lastUsage == nil {
		return nil
	}
	if lastUsage.InputTokens == 0 && lastUsage.OutputTokens == 0 {
		return nil
	}

	// Parse timestamp for date.
	var date string
	if lastTimestamp != "" {
		if ts, err := time.Parse(time.RFC3339Nano, lastTimestamp); err == nil {
			date = ts.Local().Format("2006-01-02")
		} else if ts, err := time.Parse(time.RFC3339, lastTimestamp); err == nil {
			date = ts.Local().Format("2006-01-02")
		}
	}
	if date == "" {
		// Fall back to file modification time.
		if info, err := os.Stat(path); err == nil {
			date = info.ModTime().Local().Format("2006-01-02")
		} else {
			return nil
		}
	}

	model := lastModel
	if model == "" {
		model = "unknown"
	}

	return &Record{
		Date:            date,
		Provider:        "hermes",
		Model:           model,
		InputTokens:     lastUsage.InputTokens,
		OutputTokens:    lastUsage.OutputTokens + lastUsage.ThoughtTokens,
		CacheReadTokens: lastUsage.CachedReadTokens,
	}
}
