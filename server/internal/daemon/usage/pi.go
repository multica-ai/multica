package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// scanPi reads Pi session JSONL logs produced by the multica daemon and
// extracts token usage from assistant `message` events.
//
// The agent backend writes every run's session file into
// ~/.multica/pi-sessions/ (see agent.PiSessionDir). Pi appends events to
// the file as it runs; each assistant `message` event carries cumulative
// usage for that turn in the shape:
//
//	{"type":"message","timestamp":"...",
//	 "message":{"role":"assistant","model":"...",
//	            "usage":{"input":N,"output":N,"cacheRead":N,"cacheWrite":N,...}}}
func (s *Scanner) scanPi() []Record {
	root, err := agent.PiSessionDir()
	if err != nil || root == "" {
		return nil
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil
	}

	files, err := filepath.Glob(filepath.Join(root, "*.jsonl"))
	if err != nil {
		s.logger.Debug("pi glob error", "error", err)
		return nil
	}

	var records []Record
	for _, f := range files {
		records = append(records, s.parsePiFile(f)...)
	}
	return mergeRecords(records)
}

type piSessionLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   *struct {
		Role  string `json:"role"`
		Model string `json:"model"`
		Usage *struct {
			Input      int64 `json:"input"`
			Output     int64 `json:"output"`
			CacheRead  int64 `json:"cacheRead"`
			CacheWrite int64 `json:"cacheWrite"`
		} `json:"usage"`
	} `json:"message"`
}

// parsePiFile walks a single session file and emits one Record per
// assistant message with non-zero usage. Each assistant message carries
// the cost for that specific turn (not cumulative), so they can be
// summed by mergeRecords downstream.
func (s *Scanner) parsePiFile(path string) []Record {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)

	var records []Record
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytesContains(line, `"usage"`) {
			continue
		}
		var entry piSessionLine
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "message" || entry.Message == nil || entry.Message.Usage == nil {
			continue
		}
		if entry.Message.Role != "assistant" {
			continue
		}
		u := entry.Message.Usage
		if u.Input == 0 && u.Output == 0 && u.CacheRead == 0 && u.CacheWrite == 0 {
			continue
		}

		date := ""
		if entry.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
				date = ts.Local().Format("2006-01-02")
			}
		}
		if date == "" {
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			date = info.ModTime().Local().Format("2006-01-02")
		}

		model := entry.Message.Model
		if model == "" {
			model = "unknown"
		}

		records = append(records, Record{
			Date:             date,
			Provider:         "pi",
			Model:            model,
			InputTokens:      u.Input,
			OutputTokens:     u.Output,
			CacheReadTokens:  u.CacheRead,
			CacheWriteTokens: u.CacheWrite,
		})
	}
	return records
}
