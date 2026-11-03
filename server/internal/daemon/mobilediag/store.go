package mobilediag

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// Log sources. Each has its own files and caps, so a noisy one cannot push out another.
const (
	SourceApp   = "app"
	SourceLink  = "link"
	SourceMarks = "marks"
)

const (
	maxFileBytes  = 1 << 20
	retentionDays = 7
	dayFormat     = "2006-01-02"
	accountFile   = "account"
	readOffFile   = "remote-read-disabled"
	runtimeFile   = "runtime.json"
	exportsDir    = "exports"
)

var sourceCaps = map[string]int64{SourceApp: 10 << 20, SourceLink: 10 << 20, SourceMarks: 5 << 20}

var logFileName = regexp.MustCompile(`^(app|link|marks)-(\d{4}-\d{2}-\d{2})(?:\.(\d+))?\.jsonl$`)

// Store is a phone's one log, written by the link and by the app. It outlives any
// link, so what happens before a link starts, or when one fails to, is kept too.
// Diagnostics must not change the app's behavior, so write failures are dropped.
type Store struct {
	dir string
	now func() time.Time

	mu      sync.Mutex
	writers map[string]*sourceWriter
}

type sourceWriter struct {
	file *os.File
	day  string
	seq  int
	size int64
}

type logFile struct {
	path string
	day  string
	seq  int
	size int64
}

// Open returns the store under dir, removing what has expired.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, now: time.Now, writers: map[string]*sourceWriter{}}
	_ = os.RemoveAll(filepath.Join(dir, exportsDir))
	s.mu.Lock()
	defer s.mu.Unlock()
	for source := range sourceCaps {
		s.pruneLocked(source)
	}
	return s, nil
}

// Append adds complete JSON lines from the app to source.
func (s *Store) Append(source string, lines []byte) {
	if _, known := sourceCaps[source]; !known || len(lines) == 0 {
		return
	}
	if lines[len(lines)-1] != '\n' {
		lines = append(slices.Clone(lines), '\n')
	}
	s.write(source, lines)
}

func (s *Store) write(source string, lines []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	day := s.now().UTC().Format(dayFormat)
	w := s.writers[source]
	if w != nil && (w.day != day || w.size+int64(len(lines)) > maxFileBytes) {
		_ = w.file.Close()
		seq := w.seq + 1
		if w.day != day {
			seq = 0
		}
		delete(s.writers, source)
		if w = s.openLocked(source, day, seq); w == nil {
			return
		}
		s.writers[source] = w
		s.pruneLocked(source)
	}
	if w == nil {
		if w = s.openLocked(source, day, s.lastSeqLocked(source, day)); w == nil {
			return
		}
		s.writers[source] = w
	}
	written, _ := w.file.Write(lines)
	w.size += int64(written)
}

func (s *Store) openLocked(source, day string, seq int) *sourceWriter {
	name := source + "-" + day + ".jsonl"
	if seq > 0 {
		name = source + "-" + day + "." + strconv.Itoa(seq) + ".jsonl"
	}
	file, err := os.OpenFile(filepath.Join(s.dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	w := &sourceWriter{file: file, day: day, seq: seq}
	if info, err := file.Stat(); err == nil {
		w.size = info.Size()
	}
	return w
}

func (s *Store) lastSeqLocked(source, day string) int {
	last := 0
	for _, f := range s.files(source) {
		if f.day == day && f.seq > last {
			last = f.seq
		}
	}
	return last
}

// files lists source's files, oldest first.
func (s *Store) files(source string) []logFile {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	files := []logFile{}
	for _, entry := range entries {
		parts := logFileName.FindStringSubmatch(entry.Name())
		if parts == nil || parts[1] != source {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		seq, _ := strconv.Atoi(parts[3])
		files = append(files, logFile{path: filepath.Join(s.dir, entry.Name()), day: parts[2], seq: seq, size: info.Size()})
	}
	slices.SortFunc(files, func(a, b logFile) int {
		if a.day != b.day {
			if a.day < b.day {
				return -1
			}
			return 1
		}
		return a.seq - b.seq
	})
	return files
}

// pruneLocked removes files past the retention, then the oldest while over the
// cap. The file being written stays, so a read always has the latest records.
func (s *Store) pruneLocked(source string) {
	files := s.files(source)
	cutoff := s.now().UTC().AddDate(0, 0, -retentionDays).Format(dayFormat)
	var total int64
	kept := files[:0:0]
	for _, f := range files {
		if f.day < cutoff {
			_ = os.Remove(f.path)
			continue
		}
		total += f.size
		kept = append(kept, f)
	}
	for len(kept) > 1 && total > sourceCaps[source] {
		_ = os.Remove(kept[0].path)
		total -= kept[0].size
		kept = kept[1:]
	}
}

func (s *Store) closeWritersLocked() {
	for source, w := range s.writers {
		_ = w.file.Close()
		delete(s.writers, source)
	}
}

// SetAccount names whose logs these are. What was written with no account known
// becomes theirs; another account's are removed first, so no read can see them.
func (s *Store) SetAccount(userID string) {
	if userID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, _ := os.ReadFile(filepath.Join(s.dir, accountFile))
	if string(current) == userID {
		return
	}
	if len(current) > 0 {
		s.clearLocked()
	}
	_ = os.WriteFile(filepath.Join(s.dir, accountFile), []byte(userID), 0o600)
}

// Clear removes every log and the account, keeping this device's own settings.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearLocked()
}

func (s *Store) clearLocked() {
	s.closeWritersLocked()
	entries, err := os.ReadDir(s.dir)
	if err == nil {
		for _, entry := range entries {
			if logFileName.MatchString(entry.Name()) {
				_ = os.Remove(filepath.Join(s.dir, entry.Name()))
			}
		}
	}
	_ = os.Remove(filepath.Join(s.dir, accountFile))
	_ = os.RemoveAll(filepath.Join(s.dir, exportsDir))
}

// Close releases the open files; a later write reopens them.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeWritersLocked()
}

// RemoteReadAllowed reports whether other devices may read these logs. A missing
// file means allowed, and the file is read each time so a change applies at once.
func (s *Store) RemoteReadAllowed() bool {
	_, err := os.Stat(filepath.Join(s.dir, readOffFile))
	return errors.Is(err, os.ErrNotExist)
}

// SetRemoteReadAllowed records the user's choice.
func (s *Store) SetRemoteReadAllowed(allowed bool) {
	path := filepath.Join(s.dir, readOffFile)
	if allowed {
		_ = os.Remove(path)
		return
	}
	_ = os.WriteFile(path, nil, 0o600)
}

// SetRuntime records what describes this app and device in every read's first line.
func (s *Store) SetRuntime(runtime []byte) {
	if json.Valid(runtime) {
		_ = os.WriteFile(filepath.Join(s.dir, runtimeFile), runtime, 0o600)
	}
}

// slogWriter gives slog's complete lines to the link source.
type slogWriter struct{ store *Store }

func (w slogWriter) Write(line []byte) (int, error) {
	w.store.write(SourceLink, line)
	return len(line), nil
}

// Logger writes the link's own logs here, including its dial failures at Debug.
func (s *Store) Logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(slogWriter{s}, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return attr
			}
			switch attr.Key {
			case slog.TimeKey:
				attr.Key = "at"
			case slog.MessageKey:
				attr.Key = "message"
			}
			return attr
		},
	})).With("source", SourceLink)
}

// markSink passes each mark on and keeps a copy here: the sink forgets a mark once
// an executor has it, and a later read of this phone must still see it.
type markSink struct {
	next  timing.Sink
	store *Store
}

// RetainMarks returns a sink that also keeps marks here.
func (s *Store) RetainMarks(next timing.Sink) timing.Sink {
	return markSink{next: next, store: s}
}

func (m markSink) Write(record timing.Record) {
	m.next.Write(record)
	line, err := MarkLine(record.At, record)
	if err == nil {
		m.store.write(SourceMarks, line)
	}
}

// MarkLine is one mark as the store keeps it. A record's own source names who
// wrote it, so the record is nested rather than merged into the line.
func MarkLine(at string, record any) ([]byte, error) {
	line, err := json.Marshal(map[string]any{"at": at, "source": SourceMarks, "record": record})
	if err != nil {
		return nil, fmt.Errorf("mark line: %w", err)
	}
	return append(line, '\n'), nil
}
