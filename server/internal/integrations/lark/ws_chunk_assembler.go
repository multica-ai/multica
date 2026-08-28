package lark

import (
	"strconv"
	"sync"
	"time"
)

// chunkAssembler buffers multi-frame Lark data payloads keyed by
// message_id and returns the concatenated bytes once every chunk has
// arrived. Lark splits large event payloads across multiple binary
// Frames with the headers:
//
//   sum        — total number of chunks (>=2 means multi-frame)
//   seq        — 0-based index of THIS chunk within the message
//   message_id — common key across the N chunks
//
// The SDK reference (larksuite/oapi-sdk-go/v3/ws/client.go combine())
// uses a 5-second TTL on partial state — anything older than that is
// considered abandoned and dropped. Without TTL, a Lark-side packet
// drop on an intermediate chunk would leak the buffered bytes forever.
//
// The assembler is goroutine-safe: a single instance serves every
// supervisor goroutine. State lives in-process only — Frame chunks do
// not arrive across server restarts (Lark re-sends the full event on
// reconnect), so durability is not required.
type chunkAssembler struct {
	ttl time.Duration
	now func() time.Time

	mu  sync.Mutex
	buf map[string]*chunkEntry
}

type chunkEntry struct {
	chunks   [][]byte // indexed by seq; nil slots = still missing
	received int      // count of non-empty slots
	deadline time.Time
}

// newChunkAssembler returns an assembler with the given partial-state
// TTL. A non-positive ttl falls back to the SDK default (5s).
func newChunkAssembler(ttl time.Duration, now func() time.Time) *chunkAssembler {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &chunkAssembler{
		ttl: ttl,
		now: now,
		buf: make(map[string]*chunkEntry),
	}
}

// admit records a single chunk and returns:
//
//   - (payload, true)  — every chunk has now arrived; payload is the
//     concatenated bytes in seq order and the per-message entry has
//     been removed.
//   - (nil, false)     — partial state; caller should NOT emit yet and
//     SHOULD NOT ACK this frame (mirroring SDK behaviour where ACK only
//     fires after full assembly so the server can retry the whole event).
//
// admit rejects malformed inputs (sum<=0, seq<0, seq>=sum, duplicate
// seq) by returning (nil, false) and treating the chunk as ignored.
// In production these conditions never fire because Lark enforces them
// server-side, but the function stays defensive — one malformed header
// must not corrupt the buffer for the next event.
func (a *chunkAssembler) admit(messageID string, sum, seq int, payload []byte) ([]byte, bool) {
	if messageID == "" || sum <= 0 || seq < 0 || seq >= sum {
		return nil, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	// Lazy GC every admit: cheap (single map walk) and avoids needing a
	// separate sweeper goroutine. Bounded by the live message_id count,
	// which is small (Lark caps in-flight chunked events per connection).
	a.gcExpiredLocked()

	entry, ok := a.buf[messageID]
	if !ok {
		entry = &chunkEntry{
			chunks:   make([][]byte, sum),
			deadline: a.now().Add(a.ttl),
		}
		a.buf[messageID] = entry
	}
	// Duplicate chunk (network retry / out-of-order Lark resend): we
	// silently overwrite. Lark guarantees the payload bytes are stable
	// for a given (message_id, seq), so re-admitting cannot change the
	// final assembled output.
	if entry.chunks[seq] == nil {
		entry.received++
	}
	entry.chunks[seq] = append([]byte(nil), payload...)
	// Sliding deadline: every fresh chunk extends the per-message TTL
	// because Lark might pace a multi-frame event across several
	// hundred ms; the static 5s is for "we got chunk 0 and then
	// nothing", not "chunks 0..N-1 arrived steadily over 4.9s".
	entry.deadline = a.now().Add(a.ttl)

	if entry.received < len(entry.chunks) {
		return nil, false
	}

	total := 0
	for _, c := range entry.chunks {
		total += len(c)
	}
	out := make([]byte, 0, total)
	for _, c := range entry.chunks {
		out = append(out, c...)
	}
	delete(a.buf, messageID)
	return out, true
}

// gcExpired removes entries whose deadline has passed. Exposed for
// tests; production path runs it lazily in admit.
func (a *chunkAssembler) gcExpired() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.gcExpiredLocked()
}

func (a *chunkAssembler) gcExpiredLocked() int {
	now := a.now()
	n := 0
	for id, e := range a.buf {
		if now.After(e.deadline) {
			delete(a.buf, id)
			n++
		}
	}
	return n
}

// pendingCount reports the number of partially-assembled messages
// currently buffered. Used by tests; useful for ops dashboards.
func (a *chunkAssembler) pendingCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.buf)
}

// parseChunkHeaders extracts the chunking metadata from a Frame's
// headers. Missing or unparseable headers yield (sum=0, seq=0, ""),
// which the connector reads as "single-frame event" and bypasses the
// assembler entirely. Lark's docs state sum is omitted (effectively 1)
// for non-chunked events; SDK's GetInt returns 0 on missing header.
func parseChunkHeaders(f *Frame) (sum, seq int, messageID string) {
	if f == nil {
		return 0, 0, ""
	}
	if s := f.HeaderValue(FrameHeaderSumKey); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			sum = n
		}
	}
	if s := f.HeaderValue(FrameHeaderSeqKey); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			seq = n
		}
	}
	messageID = f.HeaderValue(FrameHeaderMessageIDKey)
	return
}
