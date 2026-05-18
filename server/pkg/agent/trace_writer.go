package agent

type traceWriter struct {
	channel string
	trace   TraceCallback
}

func newTraceWriter(channel string, trace TraceCallback) *traceWriter {
	return &traceWriter{channel: channel, trace: trace}
}

func (w *traceWriter) Write(p []byte) (int, error) {
	if w.trace != nil && len(p) > 0 {
		w.trace(w.channel, string(p), "")
	}
	return len(p), nil
}
