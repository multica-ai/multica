package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// scanOpenClaw reads OpenClaw JSONL session files from
// ~/.openclaw/agents/*/sessions/*.jsonl
// and extracts token usage from assistant message entries.
func (s *Scanner) scanOpenClaw() []Record {
	root := openClawSessionRoot()
	if root == "" {
		return nil
	}

	// Glob for session files: agents/*/sessions/*.jsonl
	pattern := filepath.Join(root, "*", "sessions", "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		s.logger.Debug("openclaw glob error", "error", err)
		return nil
	}

	var allRecords []Record
	for _, f := range files {
		records := s.parseOpenClawFile(f)
		allRecords = append(allRecords, records...)
	}

	return mergeRecords(allRecords)
}

// openClawSessionRoot returns the OpenClaw agents directory.
func openClawSessionRoot() string {
	if openclawHome := os.Getenv("OPENCLAW_HOME"); openclawHome != "" {
		dir := filepath.Join(openclawHome, "agents")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	dir := filepath.Join(home, ".openclaw", "agents")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// openClawLine represents a line in an OpenClaw JSONL session file.
type openClawLine struct {
	Type      string    `json:"type"`
	Timestamp string    `json:"timestamp"` // RFC3339
	Message   *struct {
		Role     string `json:"role"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Usage    *struct {
			Input      int64 `json:"input"`
			Output     int64 `json:"output"`
			CacheRead  int64 `json:"cacheRead"`
			CacheWrite int64 `json:"cacheWrite"`
		} `json:"usage"`
	} `json:"message"`
}

// parseOpenClawFile extracts token usage records from an OpenClaw session JSONL file.
func (s *Scanner) parseOpenClawFile(path string) []Record {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var records []Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Fast pre-filter: skip lines that don't contain relevant data.
		if !bytesContains(line, `"usage"`) {
			continue
		}
		if !bytesContains(line, `"assistant"`) {
			continue
		}

		var entry openClawLine
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "message" || entry.Message == nil || entry.Message.Role != "assistant" || entry.Message.Usage == nil {
			continue
		}

		u := entry.Message.Usage
		if u.Input == 0 && u.Output == 0 {
			continue
		}

		// Parse timestamp to get date.
		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				continue
			}
		}

		model := entry.Message.Model
		if model == "" {
			model = "unknown"
		}

		// Construct provider string: if the session has a provider, use "openclaw/<provider>"
		// for attribution, but the Record.Provider field should be "openclaw".
		provider := "openclaw"
		_ = entry.Message.Provider // available but not used in provider field

		records = append(records, Record{
			Date:             ts.Local().Format("2006-01-02"),
			Provider:         provider,
			Model:            normalizeOpenClawModel(entry.Message.Provider, model),
			InputTokens:      u.Input,
			OutputTokens:     u.Output,
			CacheReadTokens:  u.CacheRead,
			CacheWriteTokens: u.CacheWrite,
		})
	}

	return records
}

// normalizeOpenClawModel returns a model identifier. If the provider is known,
// it prefixes the model name for clarity (e.g. "deepseek/deepseek-chat").
func normalizeOpenClawModel(provider, model string) string {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider != "" && !strings.Contains(model, "/") {
		return provider + "/" + model
	}
	return model
}
