package agent

import (
	"bytes"
	"io"
	"sync"

	"github.com/multica-ai/multica/server/pkg/redact"
)

const secretSafeDiagnosticTailBytes = 16 * 1024

type secretSafeDiagnosticWriter struct {
	mu      sync.Mutex
	sinks   []io.Writer
	pending []byte
}

func newSecretSafeDiagnosticWriter(sinks ...io.Writer) *secretSafeDiagnosticWriter {
	return &secretSafeDiagnosticWriter{sinks: sinks}
}

func (w *secretSafeDiagnosticWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pending = append(w.pending, p...)
	for {
		newline := bytes.IndexByte(w.pending, '\n')
		if newline < 0 {
			break
		}
		w.emitSafe(w.pending[:newline+1])
		w.pending = w.pending[newline+1:]
	}
	if len(w.pending) > secretSafeDiagnosticTailBytes {
		w.pending = append([]byte(nil), w.pending[len(w.pending)-secretSafeDiagnosticTailBytes:]...)
	}
	return len(p), nil
}

func (w *secretSafeDiagnosticWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return
	}
	w.emitSafe(w.pending)
	w.pending = nil
}

func (w *secretSafeDiagnosticWriter) emitSafe(raw []byte) {
	safe := []byte(redact.Text(string(raw)))
	for _, sink := range w.sinks {
		_, _ = sink.Write(safe)
	}
}
