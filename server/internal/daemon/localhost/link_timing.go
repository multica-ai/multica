package localhost

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// TimingReportCapability tells a remote client it may report its marks here.
const TimingReportCapability = "timing-report-v1"

const (
	reportBodyBytes  = 1 << 20
	reportMaxRecords = 500
	// A phone reports on reconnect and at each turn's end, far below this.
	reportBurst       = 12
	reportPerMinute   = 6
	reportSeenBatches = 64
	// migrationWindow is how long after a relayed WebSocket closes its client's
	// return on a direct stream still reads as the same move.
	migrationWindow = 2 * time.Minute
)

// reportLimits keeps one device's reports from filling the report file, and
// remembers recent batches so a retry after a lost acknowledgement stores nothing.
type reportLimits struct {
	now   func() time.Time
	mu    sync.Mutex
	peers map[string]*reportPeer
}

type reportPeer struct {
	tokens float64
	last   time.Time
	seen   []string
}

func newReportLimits(now func() time.Time) reportLimits {
	return reportLimits{now: now, peers: map[string]*reportPeer{}}
}

func (l *reportLimits) allow(peer string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	state := l.peers[peer]
	if state == nil {
		state = &reportPeer{tokens: reportBurst, last: now}
		l.peers[peer] = state
	}
	state.tokens = min(reportBurst, state.tokens+now.Sub(state.last).Minutes()*reportPerMinute)
	state.last = now
	if state.tokens < 1 {
		return false
	}
	state.tokens--
	return true
}

// firstSeen records batch and reports whether it is new.
func (l *reportLimits) firstSeen(peer, batch string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.peers[peer]
	for _, seen := range state.seen {
		if seen == batch {
			return false
		}
	}
	state.seen = append(state.seen, batch)
	if len(state.seen) > reportSeenBatches {
		state.seen = state.seen[1:]
	}
	return true
}

// timingReport stores a device's own marks apart from this device's, so they can
// never displace them. received_at is this device's clock: export filters and sorts
// by it because the reporter's clock can be arbitrarily wrong.
func (v *V2) timingReport(w http.ResponseWriter, r *http.Request, scopeID int64) {
	c := callerOf(r.Context())
	if v.reports == nil || c.clientKind != timing.ClientMobile {
		writeV2Error(w, AppError("forbidden", "This caller cannot report timing here"))
		return
	}
	if !v.reportLimits.allow(c.peer) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"kind": "rate_limited", "message": "Too many timing reports"}})
		return
	}
	var body struct {
		BatchID string            `json:"batch_id"`
		Records []json.RawMessage `json:"records"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, reportBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !timing.ValidID(body.BatchID) || len(body.Records) > reportMaxRecords {
		writeV2Error(w, AppError("invalid_request", "A timing report needs a batch id and at most 500 records"))
		return
	}
	if !v.reportLimits.firstSeen(c.peer, body.BatchID) {
		writeJSON(w, http.StatusOK, map[string]any{"accepted": 0, "rejected": 0, "duplicate": true})
		return
	}
	received := timing.FormatAt(time.Now())
	inScope := map[string]bool{}
	accepted, rejected := 0, 0
	for _, raw := range body.Records {
		var record timing.Record
		if json.Unmarshal(raw, &record) != nil || record.Source != timing.SourceMobile || !timing.Reportable(record.Stage) || !v.reportInScope(r, scopeID, record.ConversationID, inScope) {
			rejected++
			continue
		}
		record.Version = timing.Version
		record.Reporter, record.ReceivedAt = c.peer, received
		record.Origin, record.ClientKind = timing.OriginP2P, timing.ClientMobile
		clean, ok := timing.Sanitize(record)
		if !ok {
			rejected++
			continue
		}
		v.reports.Write(clean)
		accepted++
	}
	writeJSON(w, http.StatusOK, map[string]any{"accepted": accepted, "rejected": rejected, "duplicate": false})
}

func (v *V2) reportInScope(r *http.Request, scopeID int64, conversationID string, known map[string]bool) bool {
	if conversationID == "" {
		return true
	}
	allowed, ok := known[conversationID]
	if !ok {
		conv, err := v.svc.Store().Conversation(r.Context(), conversationID)
		allowed = err == nil && conv.ScopeID == scopeID
		known[conversationID] = allowed
	}
	return allowed
}

// observeRemote records a device's request arriving and the first byte of the
// answer. A report's own traffic is left out, or each report would produce records
// for the next one, and so is a WebSocket, whose writer must stay hijackable.
func (v *V2) observeRemote(w http.ResponseWriter, r *http.Request, c caller) (http.ResponseWriter, func()) {
	path := strings.TrimPrefix(r.URL.Path, "/local/v2")
	if v.marks == nil || path == timing.ReportsPath {
		return w, func() {}
	}
	began := v.marks.Now()
	id := timing.RequestIDOf(r.Header)
	base := timing.Record{RequestID: id, Peer: c.peer, Role: timing.RoleExecutor, Connection: c.connection, Origin: timing.OriginP2P, ClientKind: c.clientKind}
	write := func(stage string, duration bool) {
		record := base
		record.Stage = stage
		if duration {
			record.DurationMs = timing.Ms(v.marks.Now().Sub(began))
		}
		v.marks.Write(record)
	}
	write(timing.StageProxyRequest, false)
	if id != "" {
		write(timing.StageLinkRequestReceived, false)
	}
	done := func() {
		if id != "" {
			write(timing.StageLinkResponseSent, true)
		}
	}
	if path == "/ws" {
		return w, done
	}
	return &firstByteWriter{ResponseWriter: w, first: func() { write(timing.StageProxyFirstByte, true) }}, done
}

// firstByteWriter calls first before the response starts.
type firstByteWriter struct {
	http.ResponseWriter
	first func()
	once  sync.Once
}

func (f *firstByteWriter) WriteHeader(status int) {
	f.once.Do(f.first)
	f.ResponseWriter.WriteHeader(status)
}

func (f *firstByteWriter) Write(data []byte) (int, error) {
	f.once.Do(f.first)
	return f.ResponseWriter.Write(data)
}

func (f *firstByteWriter) Flush() {
	if flusher, ok := f.ResponseWriter.(http.Flusher); ok {
		f.once.Do(f.first)
		flusher.Flush()
	}
}

func (f *firstByteWriter) Unwrap() http.ResponseWriter { return f.ResponseWriter }

// writeSnapshot answers with a snapshot and, for another device, records how long
// reading it took and its size. Opening a conversation is observed, not optimized.
func (v *V2) writeSnapshot(w http.ResponseWriter, r *http.Request, id string, began time.Time, body map[string]any) {
	c := callerOf(r.Context())
	if v.marks == nil || c.kind != callerRemote {
		writeJSON(w, http.StatusOK, body)
		return
	}
	data, err := json.Marshal(body)
	if err != nil {
		writeV2Error(w, errors.New("snapshot could not be encoded"))
		return
	}
	record := timing.Record{Stage: timing.StageSnapshotServerRead, ConversationID: id, Peer: c.peer, Role: timing.RoleExecutor, Connection: c.connection, Bytes: timing.N(int64(len(data)))}
	v.marks.Since(record, began, nil)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(data, '\n'))
}

// noteSocketClosed remembers a relayed WebSocket closing for noteResubscribed.
func (v *V2) noteSocketClosed(c *wsConn) {
	if v.marks == nil || c.caller.kind != callerRemote || c.caller.clientKey == "" || c.caller.connection != timing.ConnectionRelayed {
		return
	}
	now := v.marks.Now()
	v.mu.Lock()
	defer v.mu.Unlock()
	for key, at := range v.relayedClosed {
		if now.Sub(at) > migrationWindow {
			delete(v.relayedClosed, key)
		}
	}
	v.relayedClosed[c.caller.clientKey] = now
}

// noteResubscribed records the end of a migration to direct. Only the executor can
// see it for every client: an initiator's tunnel sees the socket come back but not
// the subscribe inside it, and a client cannot tell a migration from any other drop.
func (v *V2) noteResubscribed(c *wsConn) {
	if v.marks == nil || c.caller.clientKey == "" || c.caller.connection != timing.ConnectionDirect {
		return
	}
	v.mu.Lock()
	closed, ok := v.relayedClosed[c.caller.clientKey]
	delete(v.relayedClosed, c.caller.clientKey)
	v.mu.Unlock()
	if ok && v.marks.Now().Sub(closed) <= migrationWindow {
		v.marks.Since(timing.Record{Stage: timing.StageMigrateResubscribed, Peer: c.caller.peer, Role: timing.RoleExecutor, Connection: timing.ConnectionDirect}, closed, nil)
	}
}

// linkTraffic sums what one remote subscription was sent per turn. It is written
// when the turn ends or the subscription does, never per batch.
type linkTraffic struct {
	marks        *timing.Marks
	caller       caller
	conversation string
	turns        map[string]*turnTraffic
}

type turnTraffic struct {
	batches, bytes int64
	last           time.Time
	maxGap         time.Duration
}

func (v *V2) newLinkTraffic(c *wsConn, conversationID string) *linkTraffic {
	if v.marks == nil || c.caller.kind != callerRemote {
		return nil
	}
	return &linkTraffic{marks: v.marks, caller: c.caller, conversation: conversationID, turns: map[string]*turnTraffic{}}
}

func (t *linkTraffic) sent(events []store.CommittedEvent, size int64) {
	if t == nil {
		return
	}
	now := t.marks.Now()
	counted := map[string]bool{}
	for _, e := range events {
		if e.TurnID == "" {
			continue
		}
		turn := t.turns[e.TurnID]
		if turn == nil {
			turn = &turnTraffic{}
			t.turns[e.TurnID] = turn
		}
		if !counted[e.TurnID] {
			counted[e.TurnID] = true
			if !turn.last.IsZero() {
				turn.maxGap = max(turn.maxGap, now.Sub(turn.last))
			}
			turn.last = now
			turn.batches++
			// A batch nearly always holds one turn; when it does not, its size
			// stays with the first so nothing is counted twice.
			turn.bytes += size
			size = 0
		}
		if e.Type == "turn.completed" {
			t.write(e.TurnID, turn)
			delete(t.turns, e.TurnID)
		}
	}
}

func (t *linkTraffic) write(turnID string, turn *turnTraffic) {
	t.marks.Write(timing.Record{Stage: timing.StageLinkEventSent, ConversationID: t.conversation, TurnID: turnID, Peer: t.caller.peer, Role: timing.RoleExecutor, Connection: t.caller.connection,
		Count: timing.N(turn.batches), Bytes: timing.N(turn.bytes), MaxGapMs: timing.Ms(turn.maxGap)})
}

// flush writes turns still open when the subscription ends, so a socket lost mid
// turn still shows what it carried.
func (t *linkTraffic) flush() {
	if t == nil {
		return
	}
	for id, turn := range t.turns {
		t.write(id, turn)
	}
}
