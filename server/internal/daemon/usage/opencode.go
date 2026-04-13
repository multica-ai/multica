package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// scanOpenCode reads OpenCode message JSON files from
// ~/.local/share/opencode/storage/message/ses_*/*.json
// and extracts token usage from assistant messages.
func (s *Scanner) scanOpenCode() []Record {
	root := openCodeStorageRoot()
	if root == "" {
		return nil
	}

	// Glob for message files: storage/message/ses_*/*.json
	pattern := filepath.Join(root, "ses_*", "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		s.logger.Debug("opencode glob error", "error", err)
		return nil
	}

	var allRecords []Record
	for _, f := range files {
		record := s.parseOpenCodeFile(f)
		if record != nil {
			allRecords = append(allRecords, *record)
		}
	}

	return mergeRecords(allRecords)
}

// openCodeStorageRoot returns the OpenCode message storage directory.
func openCodeStorageRoot() string {
	// Check XDG_DATA_HOME first, then fall back to ~/.local/share
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dataHome = filepath.Join(home, ".local", "share")
	}

	dir := filepath.Join(dataHome, "opencode", "storage", "message")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// openCodeMessage represents the subset of an OpenCode message JSON file we need.
type openCodeMessage struct {
	Role       string `json:"role"`
	ModelID    string `json:"modelID"`
	ProviderID string `json:"providerID"`
	Time       *struct {
		Created int64 `json:"created"` // unix milliseconds
	} `json:"time"`
	Tokens *struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     *struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

// parseOpenCodeFile reads a single OpenCode message JSON file and returns a Record
// if it contains assistant token usage. Returns nil otherwise.
func (s *Scanner) parseOpenCodeFile(path string) *Record {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var msg openCodeMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil
	}

	// Only count assistant messages with token usage.
	if msg.Role != "assistant" || msg.Tokens == nil || msg.Time == nil {
		return nil
	}

	// Skip messages with no meaningful token usage.
	if msg.Tokens.Input == 0 && msg.Tokens.Output == 0 {
		return nil
	}

	ts := time.UnixMilli(msg.Time.Created)
	date := ts.Local().Format("2006-01-02")

	model := msg.ModelID
	if model == "" {
		model = "unknown"
	}

	var cacheRead, cacheWrite int64
	if msg.Tokens.Cache != nil {
		cacheRead = msg.Tokens.Cache.Read
		cacheWrite = msg.Tokens.Cache.Write
	}

	return &Record{
		Date:             date,
		Provider:         "opencode",
		Model:            model,
		InputTokens:      msg.Tokens.Input,
		OutputTokens:     msg.Tokens.Output + msg.Tokens.Reasoning,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
	}
}
