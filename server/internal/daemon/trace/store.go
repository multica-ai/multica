package trace

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrStoreClosed is returned when an operation is attempted on a closed store.
var ErrStoreClosed = errors.New("trace store is closed")

// TraceStore is the interface for persisting and querying agent trace lines.
// Implementations must be safe for concurrent use.
type TraceStore interface {
	// Append persists a single trace line. The seq and timestamp fields are
	// assigned by the store; caller-provided values are overwritten.
	// Returns the assigned seq.
	Append(ctx context.Context, line TraceLine) (seq int64, err error)

	// ListSince returns all trace lines for a task/run with seq > afterSeq.
	// If afterSeq is 0, returns all lines from the beginning.
	// Results are ordered by seq ascending.
	ListSince(ctx context.Context, taskID, runID string, afterSeq int64) ([]TraceLine, error)

	// Tail returns the last n trace lines for a task/run, ordered by seq
	// ascending. If n <= 0 or larger than the available lines, all lines
	// are returned.
	Tail(ctx context.Context, taskID, runID string, n int) ([]TraceLine, error)

	// ListRuns returns run IDs recorded for a task, sorted by newest first.
	ListRuns(ctx context.Context, taskID string) ([]string, error)

	// Close releases all resources held by the store.
	Close() error
}

// JSONLStore implements TraceStore by writing one JSON object per line to a
// file per (taskID, runID) pair. Files are stored under a configurable root
// directory.
//
// File layout:
//
//	<root>/
//	  <taskID>/
//	    <runID>.jsonl
//
// Each line is a JSON-serialised TraceLine. Seq is assigned by an in-process
// atomic counter seeded from the file length at open time.
//
// Concurrency model: s.mu protects the open map and closed flag; per-file
// mu serialises seq allocation + file write for each (taskID, runID) pair
// so that a single file's lines are written atomically and in seq order.
type JSONLStore struct {
	root   string
	mu     sync.Mutex
	closed bool
	open   map[string]*jsonlFile
}

type jsonlFile struct {
	mu   sync.Mutex
	f    *os.File
	seq  atomic.Int64 // next seq to assign; starts at 1
	enc  *json.Encoder
	path string
}

// key returns the store-internal map key for a (taskID, runID) pair.
func key(taskID, runID string) string { return taskID + "/" + runID }

// openFile opens (or re-opens) the JSONL file for the given taskID/runID,
// seeking to the end and seeding the sequence counter from the current
// line count so seq remains monotonic across daemon restarts.
func (s *JSONLStore) openFile(taskID, runID string) (*jsonlFile, error) {
	dir := filepath.Join(s.root, taskID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create trace dir: %w", err)
	}
	p := filepath.Join(dir, runID+".jsonl")

	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open trace file: %w", err)
	}

	// Seed seq from existing line count so seq remains monotonic across
	// store restarts (daemon restarts between task executions).
	count, err := countLines(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("count trace lines: %w", err)
	}

	jf := &jsonlFile{
		f:    f,
		path: p,
		enc:  json.NewEncoder(f),
	}
	// Disable HTML-escaping so <, >, & appear verbatim in raw_payload.
	jf.enc.SetEscapeHTML(false)
	jf.seq.Store(int64(count))

	return jf, nil
}

// countLines counts the number of newline-terminated lines in the file
// starting from the current read position. The file position is restored
// after counting.
func countLines(r io.ReadSeeker) (int, error) {
	pos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	defer func() { _, _ = r.Seek(pos, io.SeekStart) }()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, MaxContentLen), MaxContentLen*2)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

// NewJSONLStore creates a JSONLStore rooted at root. The directory is created
// if it does not exist.
func NewJSONLStore(root string) (*JSONLStore, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("create trace root: %w", err)
	}
	return &JSONLStore{
		root: root,
		open: make(map[string]*jsonlFile),
	}, nil
}

// Append writes a trace line. The line's Seq and Timestamp fields are assigned
// by the store; caller-provided values are overwritten.
func (s *JSONLStore) Append(ctx context.Context, line TraceLine) (int64, error) {
	if line.TaskID == "" || line.RunID == "" {
		return 0, fmt.Errorf("task_id and run_id are required")
	}

	k := key(line.TaskID, line.RunID)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, ErrStoreClosed
	}
	jf, ok := s.open[k]
	if !ok {
		var err error
		jf, err = s.openFile(line.TaskID, line.RunID)
		if err != nil {
			s.mu.Unlock()
			return 0, err
		}
		s.open[k] = jf
	}
	s.mu.Unlock()

	jf.mu.Lock()
	seq := jf.seq.Add(1)
	line.Seq = seq
	line.Timestamp = time.Now().UTC()
	line = line.truncateContent()
	if err := jf.enc.Encode(line); err != nil {
		jf.mu.Unlock()
		return 0, fmt.Errorf("encode trace line: %w", err)
	}
	jf.mu.Unlock()
	return seq, nil
}

// ListSince returns all lines for (taskID, runID) with seq > afterSeq.
func (s *JSONLStore) ListSince(ctx context.Context, taskID, runID string, afterSeq int64) ([]TraceLine, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, ErrStoreClosed
	}

	lines, err := s.readAll(taskID, runID)
	if err != nil {
		return nil, err
	}

	// Binary search for the first line with seq > afterSeq.
	idx := sort.Search(len(lines), func(i int) bool {
		return lines[i].Seq > afterSeq
	})
	if idx >= len(lines) {
		return nil, nil
	}
	return lines[idx:], nil
}

// Tail returns the last n lines for (taskID, runID).
func (s *JSONLStore) Tail(ctx context.Context, taskID, runID string, n int) ([]TraceLine, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, ErrStoreClosed
	}

	lines, err := s.readAll(taskID, runID)
	if err != nil {
		return nil, err
	}
	if n <= 0 || n >= len(lines) {
		return lines, nil
	}
	return lines[len(lines)-n:], nil
}

// ListRuns returns the run IDs recorded for taskID, sorted by newest file
// modification time first. If the task has no trace directory, returns nil.
func (s *JSONLStore) ListRuns(ctx context.Context, taskID string) ([]string, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, ErrStoreClosed
	}
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	dir := filepath.Join(s.root, taskID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read trace task dir: %w", err)
	}

	type runFile struct {
		id      string
		modTime time.Time
	}
	var runs []runFile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		runs = append(runs, runFile{
			id:      strings.TrimSuffix(entry.Name(), ".jsonl"),
			modTime: info.ModTime(),
		})
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].modTime.Equal(runs[j].modTime) {
			return runs[i].id > runs[j].id
		}
		return runs[i].modTime.After(runs[j].modTime)
	})

	out := make([]string, 0, len(runs))
	for _, run := range runs {
		out = append(out, run.id)
	}
	return out, nil
}

// readAll reads and parses all trace lines from the JSONL file for the given
// task/run pair. If no file exists, returns nil, nil (not an error).
func (s *JSONLStore) readAll(taskID, runID string) ([]TraceLine, error) {
	p := filepath.Join(s.root, taskID, runID+".jsonl")

	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open trace file for read: %w", err)
	}
	defer f.Close()

	var lines []TraceLine
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, MaxContentLen), MaxContentLen*2)
	for scanner.Scan() {
		var line TraceLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			// Skip malformed lines; they may be from a crashed write.
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return lines, fmt.Errorf("read trace file: %w", err)
	}
	return lines, nil
}

// Close marks the store as closed and closes all open trace files.
// Subsequent calls to Append, ListSince, or Tail return ErrStoreClosed.
func (s *JSONLStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}
	s.closed = true

	var firstErr error
	for k, jf := range s.open {
		if err := jf.f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(s.open, k)
	}
	return firstErr
}

// compile-time interface check.
var _ TraceStore = (*JSONLStore)(nil)
