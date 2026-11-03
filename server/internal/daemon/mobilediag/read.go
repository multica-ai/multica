package mobilediag

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Each read stays under MaxReadBytes, itself under the relay's 5 MB transfer limit.
// That limit is enforced on another route, so this total is what keeps a read within it.
const (
	MaxReadBytes     = 4 << 20
	headerKind       = "multica-mobile-diagnostics"
	summaryKind      = "summary"
	statusAvailable  = "available"
	statusEmpty      = "empty"
	statusOtherOwner = "unavailable"
)

var sourceBudgets = []struct {
	source string
	bytes  int
}{{SourceApp, 1400 << 10}, {SourceLink, 1400 << 10}, {SourceMarks, 900 << 10}}

// SourceSummary is what one read found of one source.
type SourceSummary struct {
	Status    string `json:"status"`
	Records   int    `json:"records"`
	Truncated bool   `json:"truncated"`
}

// Summary closes a read; its absence means the read did not finish.
type Summary struct {
	Kind    string                   `json:"kind"`
	Sources map[string]SourceSummary `json:"sources"`
}

type span struct {
	path   string
	offset int
}

// recent walks source's files newest first and returns where to start reading each
// so the newest records since then fit the budget. Each file is at most one log file
// in memory at a time.
func (s *Store) recent(source string, since time.Time, budget int) ([]span, SourceSummary) {
	files := s.files(source)
	sinceDay := since.UTC().Format(dayFormat)
	spans := []span{}
	summary := SourceSummary{Status: statusEmpty}
	used := 0
	for i := len(files) - 1; i >= 0 && !summary.Truncated; i-- {
		if files[i].day < sinceDay {
			break
		}
		data, err := os.ReadFile(files[i].path)
		if err != nil {
			continue
		}
		start := len(data)
		for end := len(data); end > 0; {
			lineStart := bytes.LastIndexByte(data[:end-1], '\n') + 1
			line := data[lineStart:end]
			if inRange(line, since) {
				if used+len(line) > budget {
					summary.Truncated = true
					break
				}
				used += len(line)
				summary.Records++
				start = lineStart
			}
			end = lineStart
		}
		if start < len(data) {
			spans = append(spans, span{files[i].path, start})
		}
	}
	if summary.Records > 0 {
		summary.Status = statusAvailable
	}
	return spans, summary
}

// inRange reports whether line is a complete record at or after since. A line still
// being written fails to parse.
func inRange(line []byte, since time.Time) bool {
	var fields struct {
		At string `json:"at"`
	}
	if json.Unmarshal(line, &fields) != nil {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, fields.At)
	return err == nil && !at.Before(since)
}

// WriteRead writes the logs of the last sinceMinutes to w: a header line, each
// source's records oldest first, then the summary. A reader keeps what arrived
// when this stops early, because every line stands alone.
func (s *Store) WriteRead(w io.Writer, userID string, sinceMinutes int) (Summary, error) {
	now := s.now()
	since := now.Add(-time.Duration(sinceMinutes) * time.Minute)
	var runtime json.RawMessage
	if data, err := os.ReadFile(filepath.Join(s.dir, runtimeFile)); err == nil && json.Valid(data) {
		runtime = data
	}
	header := map[string]any{
		"kind": headerKind, "schemaVersion": 1, "exportedAt": now.UTC().Format(time.RFC3339Nano), "runtime": runtime,
		"filter": map[string]string{"since": since.UTC().Format(time.RFC3339Nano), "until": now.UTC().Format(time.RFC3339Nano)},
	}
	if err := writeJSONLine(w, header); err != nil {
		return Summary{}, err
	}
	account, _ := os.ReadFile(filepath.Join(s.dir, accountFile))
	summary := Summary{Kind: summaryKind, Sources: map[string]SourceSummary{}}
	for _, budget := range sourceBudgets {
		if string(account) != userID {
			summary.Sources[budget.source] = SourceSummary{Status: statusOtherOwner}
			continue
		}
		spans, found := s.recent(budget.source, since, budget.bytes)
		summary.Sources[budget.source] = found
		for i := len(spans) - 1; i >= 0; i-- {
			if err := copySpan(w, spans[i], since); err != nil {
				return Summary{}, err
			}
		}
	}
	if err := writeJSONLine(w, summary); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func copySpan(w io.Writer, from span, since time.Time) error {
	data, err := os.ReadFile(from.path)
	if err != nil || from.offset > len(data) {
		return nil
	}
	for data = data[from.offset:]; len(data) > 0; {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			return nil
		}
		line := data[:end+1]
		data = data[end+1:]
		if !inRange(line, since) {
			continue
		}
		if _, err := w.Write(line); err != nil {
			return err
		}
	}
	return nil
}

func writeJSONLine(w io.Writer, value any) error {
	line, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = w.Write(append(line, '\n'))
	return err
}

// ClearExports removes the temporary files of shared exports.
func (s *Store) ClearExports() { _ = os.RemoveAll(filepath.Join(s.dir, exportsDir)) }

// ExportFile writes a read to a private temporary file for the app to share. The
// caller deletes it; anything left behind goes when the store next opens or clears.
func (s *Store) ExportFile(sinceMinutes int) (string, Summary, error) {
	dir := filepath.Join(s.dir, exportsDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", Summary{}, err
	}
	path := filepath.Join(dir, "multica-mobile-logs-"+s.now().UTC().Format("2006-01-02T15-04-05")+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", Summary{}, err
	}
	defer file.Close()
	// The person holding the phone may read whatever it has, signed in or not.
	account, _ := os.ReadFile(filepath.Join(s.dir, accountFile))
	summary, err := s.WriteRead(file, string(account), sinceMinutes)
	if err != nil {
		_ = os.Remove(path)
		return "", Summary{}, err
	}
	return path, summary, nil
}
