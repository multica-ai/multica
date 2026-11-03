package timing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultMaxBytes  = 5 << 20
	defaultQueueSize = 1024
	atLayout         = "2006-01-02T15:04:05.000Z"
)

// Options tunes a Log; zero values use 5 MiB per file and a 1024-record queue.
type Options struct {
	MaxBytes  int64
	QueueSize int
	// beforeWrite lets tests stall the disk.
	beforeWrite func()
}

// Log appends records to one file from a single goroutine, so a slow disk
// costs a dropped record and never a send. A nil Log discards everything.
type Log struct {
	path     string
	maxBytes int64
	before   func()
	queue    chan Record
	flushes  chan chan struct{}
	stop     chan struct{}
	stopped  chan struct{}
	closing  sync.Once
	dropped  atomic.Int64

	file *os.File
	size int64
}

// Open starts a log writing to path, created on the first write. An empty
// path returns nil: timing is disabled.
func Open(path string, opts Options) *Log {
	if path == "" {
		return nil
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultMaxBytes
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = defaultQueueSize
	}
	l := &Log{path: path, maxBytes: opts.MaxBytes, before: opts.beforeWrite, queue: make(chan Record, opts.QueueSize), flushes: make(chan chan struct{}), stop: make(chan struct{}), stopped: make(chan struct{})}
	go l.run()
	return l
}

// Write queues r without blocking. A full queue counts as a dropped write;
// the next successful write records the gap so an export reports it.
func (l *Log) Write(r Record) {
	if l == nil {
		return
	}
	r.Version = Version
	if r.Source == "" {
		r.Source = SourceDaemon
	}
	if r.At == "" {
		r.At = formatAt(time.Now())
	}
	clean, ok := Sanitize(r)
	if !ok {
		return
	}
	select {
	case l.queue <- clean:
	default:
		l.dropped.Add(1)
	}
}

// Flush waits until everything queued so far is on disk.
func (l *Log) Flush() {
	if l == nil {
		return
	}
	done := make(chan struct{})
	select {
	case l.flushes <- done:
		<-done
	case <-l.stopped:
	}
}

// Close writes what is queued and stops. Later writes are discarded.
func (l *Log) Close() {
	if l == nil {
		return
	}
	l.closing.Do(func() { close(l.stop) })
	<-l.stopped
}

func (l *Log) run() {
	defer close(l.stopped)
	defer func() {
		if l.file != nil {
			_ = l.file.Close()
		}
	}()
	for {
		select {
		case r := <-l.queue:
			l.append(r)
		case done := <-l.flushes:
			l.drain()
			close(done)
		case <-l.stop:
			l.drain()
			return
		}
	}
}

func (l *Log) drain() {
	for {
		select {
		case r := <-l.queue:
			l.append(r)
		default:
			l.reportDropped()
			return
		}
	}
}

func (l *Log) append(r Record) {
	if l.before != nil {
		l.before()
	}
	l.reportDropped()
	if !l.writeLine(r) {
		l.dropped.Add(1)
	}
}

func (l *Log) reportDropped() {
	n := l.dropped.Swap(0)
	if n == 0 {
		return
	}
	if !l.writeLine(Record{Version: Version, Source: SourceDaemon, At: formatAt(time.Now()), Stage: StageTimingDropped, Event: EventMark, Count: &n}) {
		l.dropped.Add(n)
	}
}

func (l *Log) writeLine(r Record) bool {
	line, err := json.Marshal(r)
	if err != nil {
		return false
	}
	line = append(line, '\n')
	if !l.ensureFile() {
		return false
	}
	if l.size > 0 && l.size+int64(len(line)) > l.maxBytes {
		_ = l.file.Close()
		l.file = nil
		if err := os.Rename(l.path, l.path+".1"); err != nil {
			return false
		}
		if !l.ensureFile() {
			return false
		}
	}
	n, err := l.file.Write(line)
	l.size += int64(n)
	if err != nil {
		_ = l.file.Close()
		l.file = nil
		return false
	}
	return true
}

func (l *Log) ensureFile() bool {
	if l.file != nil {
		return true
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return false
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return false
	}
	l.file, l.size = f, info.Size()
	return true
}

func formatAt(t time.Time) string { return t.UTC().Format(atLayout) }
