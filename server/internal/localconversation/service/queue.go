package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

const (
	maxQueueItems     = 100
	maxQueueItemBytes = 2 << 20
)

var (
	// ErrQueueItemStarted rejects changing a queued message that already
	// started.
	ErrQueueItemStarted = errors.New("local conversation: this queued message has already started")
	// ErrQueueFull rejects a message for a full queue.
	ErrQueueFull = errors.New("local conversation: this conversation queue is full")
	// ErrArchived rejects new work for an archived conversation.
	ErrArchived = errors.New("local conversation: archived conversations are read-only")
)

// queueItem is the part of a client's queued message the daemon acts on;
// the rest, such as how the composer displays it, is stored as given.
type queueItem struct {
	RequestID       string          `json:"requestId"`
	Content         string          `json:"content"`
	Prompt          *string         `json:"prompt"`
	Model           json.RawMessage `json:"model"`
	ReasoningEffort json.RawMessage `json:"reasoningEffort"`
	PermissionMode  *string         `json:"permissionMode"`
	AttachmentIDs   []string        `json:"attachmentIds"`
	References      []Reference     `json:"references"`
	Agents          []RemoteAgent   `json:"agents"`
	Display         struct {
		ID             string `json:"id"`
		ClientSubmitID string `json:"clientSubmitId"`
	} `json:"display"`
}

func parseQueueItem(raw json.RawMessage) (queueItem, error) {
	var item queueItem
	if len(raw) > maxQueueItemBytes {
		return item, fmt.Errorf("%w: the queued message is too large", ErrInvalidRequest)
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return item, fmt.Errorf("%w: invalid queued message", ErrInvalidRequest)
	}
	switch {
	case item.RequestID == "" || len(item.RequestID) > 160:
		return item, fmt.Errorf("%w: queued request id", ErrInvalidRequest)
	case item.Content == "":
		return item, fmt.Errorf("%w: the queued message is empty", ErrInvalidRequest)
	case item.Display.ID != item.RequestID || (item.Display.ClientSubmitID != "" && item.Display.ClientSubmitID != item.RequestID):
		return item, fmt.Errorf("%w: invalid queued message display", ErrInvalidRequest)
	case len(item.AttachmentIDs) > maxMessageAttachments:
		return item, fmt.Errorf("%w: attach up to %d files per message", ErrInvalidRequest, maxMessageAttachments)
	case validReferences(item.References) != nil:
		return item, validReferences(item.References)
	case !referencedAgents(item):
		return item, fmt.Errorf("%w: invalid queued agents", ErrInvalidRequest)
	case item.PermissionMode != nil && !validPermissionModes[*item.PermissionMode]:
		return item, fmt.Errorf("%w: permission mode", ErrInvalidSettings)
	}
	return item, nil
}

// QueueView is a conversation's queue as clients show it.
type QueueView struct {
	Revision    int64             `json:"revision"`
	Paused      bool              `json:"paused"`
	Error       string            `json:"error,omitempty"`
	Dispatching string            `json:"dispatching_request_id,omitempty"`
	Items       []json.RawMessage `json:"items"`
}

// queues serializes queue mutations and dispatch per conversation.
type queues struct {
	mu          sync.Mutex
	locks       map[string]*sync.Mutex
	dispatching map[string]string
	// once names a message to send although its queue is paused. In memory on
	// purpose: after a restart the queue waits for the user, this message included.
	once map[string]string
}

func (q *queues) setOnce(conversationID, requestID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.once == nil {
		q.once = map[string]string{}
	}
	if requestID == "" {
		delete(q.once, conversationID)
	} else {
		q.once[conversationID] = requestID
	}
}

func (q *queues) onceFor(conversationID string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.once[conversationID]
}

func (q *queues) lock(conversationID string) func() {
	q.mu.Lock()
	l := q.locks[conversationID]
	if l == nil {
		l = &sync.Mutex{}
		q.locks[conversationID] = l
	}
	q.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (q *queues) setDispatching(conversationID, requestID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if requestID == "" {
		delete(q.dispatching, conversationID)
	} else {
		q.dispatching[conversationID] = requestID
	}
}

func (q *queues) dispatchingFor(conversationID string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dispatching[conversationID]
}

// Queue returns the conversation's prompt queue.
func (s *Service) Queue(ctx context.Context, scopeID int64, conversationID string) (QueueView, error) {
	if _, err := s.scopedConversation(ctx, scopeID, conversationID); err != nil {
		return QueueView{}, err
	}
	state, err := s.store.Queue(ctx, conversationID)
	if err != nil {
		return QueueView{}, err
	}
	return s.queueView(conversationID, state), nil
}

func (s *Service) queueView(conversationID string, state store.QueueState) QueueView {
	view := QueueView{Revision: state.Revision, Paused: state.Paused, Error: state.Error, Items: []json.RawMessage{}}
	for _, item := range state.Items {
		view.Items = append(view.Items, item.Item)
	}
	if id := s.queues.dispatchingFor(conversationID); id != "" && ((!state.Paused && state.Error == "") || id == s.queues.onceFor(conversationID)) {
		view.Dispatching = id
	}
	return view
}

// Enqueue queues a message. Re-enqueuing the same item, or one already
// sent, is a no-op; a different item under the same request id is
// rejected. paused queues it without dispatching.
func (s *Service) Enqueue(ctx context.Context, scopeID int64, conversationID string, raw json.RawMessage, paused bool, origin TurnOrigin) (QueueView, error) {
	conv, err := s.scopedConversation(ctx, scopeID, conversationID)
	if err != nil {
		return QueueView{}, err
	}
	if conv.Archived == 1 {
		return QueueView{}, ErrArchived
	}
	item, err := parseQueueItem(raw)
	if err != nil {
		return QueueView{}, err
	}
	unlock := s.queues.lock(conversationID)
	state, err := s.store.Queue(ctx, conversationID)
	if err != nil {
		unlock()
		return QueueView{}, err
	}
	for _, existing := range state.Items {
		if existing.RequestID == item.RequestID {
			unlock()
			if !sameQueuedMessage(existing.Item, raw) {
				return QueueView{}, ErrRequestConflict
			}
			return s.queueView(conversationID, state), nil
		}
	}
	if _, err := s.store.Receipt(ctx, scopeID, item.RequestID); err == nil {
		unlock()
		return s.queueView(conversationID, state), nil
	}
	removed, err := s.store.QueueItemRemoved(ctx, conversationID, item.RequestID)
	if err != nil || removed {
		unlock()
		return s.queueView(conversationID, state), err
	}
	if len(state.Items) >= maxQueueItems {
		unlock()
		return QueueView{}, ErrQueueFull
	}
	change := store.QueueChange{Revision: state.Revision + 1, Paused: paused, Append: &store.QueueItem{RequestID: item.RequestID, Item: raw, OriginPeerID: origin.PeerID, OriginClientKind: origin.ClientKind}}
	if !paused {
		change.Error = ""
	} else {
		change.Error = state.Error
	}
	view, err := s.commitQueue(ctx, conversationID, change, len(state.Items)+1, "enqueued")
	unlock()
	if err == nil {
		if paused || len(state.Items) > 0 || conv.Status == "running" {
			s.traces.waited(item.RequestID)
		}
		go s.dispatchQueue(conversationID)
	}
	return view, err
}

// UpdateQueue applies pause, resume, remove, stop or send_now. stop and
// send_now also interrupt the running turn; send_now then moves the item
// to the front, so it runs as soon as that turn ends.
func (s *Service) UpdateQueue(ctx context.Context, scopeID int64, conversationID, action, requestID string) (QueueView, error) {
	if _, err := s.scopedConversation(ctx, scopeID, conversationID); err != nil {
		return QueueView{}, err
	}
	started := func() bool {
		if s.queues.dispatchingFor(conversationID) == requestID {
			return true
		}
		_, err := s.store.Receipt(ctx, scopeID, requestID)
		return err == nil
	}
	switch action {
	case "pause", "resume", "stop":
	case "remove", "send_now":
		if requestID == "" {
			return QueueView{}, fmt.Errorf("%w: request_id is required", ErrInvalidRequest)
		}
	default:
		return QueueView{}, fmt.Errorf("%w: unknown queue action %q", ErrInvalidRequest, action)
	}
	if action == "send_now" && started() {
		return s.Queue(ctx, scopeID, conversationID)
	}

	unlock := s.queues.lock(conversationID)
	state, err := s.store.Queue(ctx, conversationID)
	if err != nil {
		unlock()
		return QueueView{}, err
	}
	queued := false
	for _, item := range state.Items {
		queued = queued || item.RequestID == requestID
	}
	change := store.QueueChange{Revision: state.Revision + 1, Paused: state.Paused, Error: state.Error}
	count := len(state.Items)
	wasHeld, heldError := state.Paused || state.Error != "", state.Error
	reason := action
	switch action {
	case "pause", "stop":
		change.Paused = true
	case "resume":
		change.Paused, change.Error = false, ""
	case "remove":
		if !queued {
			unlock()
			if started() {
				return QueueView{}, ErrQueueItemStarted
			}
			return s.queueView(conversationID, state), nil
		}
		change.Remove = requestID
		change.RemovedAt = s.opts.Now()
		reason = "removed"
		count--
	case "send_now":
		if !queued {
			unlock()
			return QueueView{}, ErrQueueItemStarted
		}
		// Hold the queue until the running turn has stopped.
		change.Paused = true
	}
	view, err := s.commitQueue(ctx, conversationID, change, count, reason)
	unlock()
	if err != nil {
		return view, err
	}
	if action == "stop" || action == "send_now" {
		if err := s.Interrupt(ctx, scopeID, conversationID); err != nil && !errors.Is(err, ErrNoRunningTurn) {
			return view, err
		}
	}
	if action == "send_now" {
		unlock := s.queues.lock(conversationID)
		state, err := s.store.Queue(ctx, conversationID)
		if err == nil {
			// A queue the user was holding stays held: only this message goes.
			if wasHeld {
				s.queues.setOnce(conversationID, requestID)
			}
			front := store.QueueChange{Revision: state.Revision + 1, Front: requestID}
			if wasHeld {
				front.Paused, front.Error = true, heldError
			}
			view, err = s.commitQueue(ctx, conversationID, front, len(state.Items), "send_now")
		}
		unlock()
		if err != nil {
			return view, err
		}
	}
	go s.dispatchQueue(conversationID)
	return view, nil
}

// queuePausedByRestart explains a queue paused when the daemon started.
const queuePausedByRestart = "Multica restarted; resume the queue to send these messages"

// pauseQueueAfterRestart pauses a queue a previous daemon left with
// messages, so nothing is sent until the user resumes it.
func (s *Service) pauseQueueAfterRestart(ctx context.Context, conversationID string) error {
	unlock := s.queues.lock(conversationID)
	defer unlock()
	state, err := s.store.Queue(ctx, conversationID)
	if err != nil || state.Paused || len(state.Items) == 0 {
		return err
	}
	_, err = s.commitQueue(ctx, conversationID, store.QueueChange{Revision: state.Revision + 1, Paused: true, Error: queuePausedByRestart}, len(state.Items), "restart")
	return err
}

// commitQueue records a queue change with its queue.updated event and the
// summary's queue count. The caller holds the conversation's queue lock.
func (s *Service) commitQueue(ctx context.Context, conversationID string, change store.QueueChange, count int, reason string) (QueueView, error) {
	current, err := s.store.Queue(ctx, conversationID)
	if err != nil {
		return QueueView{}, err
	}
	// The order and the reason tell a client a message that went out from one
	// another device removed. No content: the items stay behind the queue route.
	payload := map[string]any{"revision": change.Revision, "paused": change.Paused, "count": count, "request_ids": requestIDsAfter(current, change), "reason": reason}
	if change.Error != "" {
		payload["error"] = change.Error
	}
	if id := s.queues.dispatchingFor(conversationID); id != "" {
		payload["dispatching_request_id"] = id
	}
	data, _ := json.Marshal(payload)
	n := int64(count)
	now := s.opts.Now()
	_, err = s.batcher.AppendWait(ctx, store.Batch{
		ConversationID: conversationID, Queue: &change,
		Events:  []store.NewEvent{{Type: "queue.updated", Payload: data, CreatedAt: now}},
		Summary: &store.SummaryPatch{QueueCount: &n, UpdatedAt: now},
	})
	if err != nil {
		return QueueView{}, err
	}
	state, err := s.store.Queue(ctx, conversationID)
	if err != nil {
		return QueueView{}, err
	}
	return s.queueView(conversationID, state), nil
}

// dispatchQueue sends the first queued message when the queue is running.
// A running turn leaves the item queued for the next turn end; any other
// failure pauses the queue with the error.
func (s *Service) dispatchQueue(conversationID string) {
	ctx := context.Background()
	unlock := s.queues.lock(conversationID)
	defer unlock()
	if s.isClosing() {
		return
	}
	state, err := s.store.Queue(ctx, conversationID)
	if err != nil || len(state.Items) == 0 {
		return
	}
	once := s.queues.onceFor(conversationID) == state.Items[0].RequestID
	if (state.Paused || state.Error != "") && !once {
		return
	}
	conv, err := s.store.Conversation(ctx, conversationID)
	if err != nil || conv.Archived == 1 {
		return
	}
	if busy, err := s.conversationBusy(ctx, conversationID); err != nil || busy {
		return
	}
	first := state.Items[0]
	item, err := parseQueueItem(first.Item)
	var trace *timing.Trace
	var dispatched time.Time
	if err == nil {
		trace = s.traces.dispatching(conv, item)
		dispatched = trace.Now()
		s.queues.setDispatching(conversationID, first.RequestID)
		err = s.sendQueued(ctx, conv, item, TurnOrigin{PeerID: first.OriginPeerID, ClientKind: first.OriginClientKind})
		s.queues.setDispatching(conversationID, "")
	}
	change := store.QueueChange{Revision: state.Revision + 1, Paused: state.Paused, Error: state.Error}
	count := len(state.Items)
	switch {
	case errors.Is(err, ErrTurnRunning) || errors.Is(err, ErrClosing) || errors.Is(err, ErrDraining):
		return
	}
	if once {
		s.queues.setOnce(conversationID, "")
	}
	// Recorded after the send because a dispatch that finds a running turn
	// stays queued and is not the one that counts.
	trace.Completed(timing.StageQueueDispatch, dispatched, dispatched.Sub(trace.Started()), err)
	switch {
	case err != nil && !errors.Is(err, ErrOutcomeUnknown):
		change.Paused, change.Error = true, err.Error()
	default:
		change.Remove = first.RequestID
		count--
	}
	reason := "dispatched"
	if change.Remove == "" {
		reason = "failed"
	}
	if _, err := s.commitQueue(ctx, conversationID, change, count, reason); err != nil {
		s.opts.Logger.Warn("local conversations: could not record queue progress", "conversation_id", conversationID, "error", err)
	}
}

func (s *Service) sendQueued(ctx context.Context, conv sqlitedb.Conversation, item queueItem, origin TurnOrigin) error {
	update := SettingsUpdate{PermissionMode: item.PermissionMode}
	var err error
	if update.Model, update.ClearModel, err = rawOptionalString(item.Model); err != nil {
		return err
	}
	if update.ReasoningEffort, update.ClearReasoningEffort, err = rawOptionalString(item.ReasoningEffort); err != nil {
		return err
	}
	if update.Model != nil || update.ClearModel || update.ReasoningEffort != nil || update.ClearReasoningEffort || update.PermissionMode != nil {
		if err := s.UpdateSettings(ctx, conv.ScopeID, conv.ID, update); err != nil {
			return err
		}
	}
	cmd := SendCommand{ScopeID: conv.ScopeID, Origin: origin, ConversationID: conv.ID, RequestID: item.RequestID, Text: item.Content, AttachmentIDs: item.AttachmentIDs, References: item.References}
	if item.Prompt != nil {
		cmd.Prompt = *item.Prompt
	}
	if len(item.Agents) > 0 {
		_, err = s.SendToAgents(ctx, cmd, item.Agents)
		return err
	}
	cmd.RequestID, err = s.queuedAttemptID(ctx, conv, item.RequestID)
	if err != nil {
		return err
	}
	_, err = s.Send(ctx, cmd)
	return err
}

// queuedAttemptID is the request id to send a queued message under. A send
// records its receipt before reaching the provider, so an id whose attempt the
// provider refused would replay as done and the queue would drop the message
// unsent. Those attempts stay in the conversation as failed turns, and the
// message goes out under a new id. An attempt that may have been delivered
// keeps its id, so its receipt stops it running twice.
func (s *Service) queuedAttemptID(ctx context.Context, conv sqlitedb.Conversation, requestID string) (string, error) {
	id := requestID
	for attempt := 1; ; attempt++ {
		receipt, err := s.store.Receipt(ctx, conv.ScopeID, id)
		if errors.Is(err, store.ErrNotFound) {
			return id, nil
		}
		if err != nil {
			return "", err
		}
		if receipt.Uncertain != 0 {
			return id, nil
		}
		turn, err := s.store.Turn(ctx, conv.ID, "turn-"+id)
		if errors.Is(err, store.ErrNotFound) {
			return id, nil
		}
		if err != nil {
			return "", err
		}
		if turn.Status != "failed" || (turn.ErrorKind != "provider_unavailable" && turn.ErrorKind != "provider_error") {
			return id, nil
		}
		id = fmt.Sprintf("%s-retry-%d", requestID, attempt)
	}
}

// requestIDsAfter is the queue's order once a change applies.
func requestIDsAfter(state store.QueueState, change store.QueueChange) []string {
	ids := []string{}
	for _, item := range state.Items {
		if item.RequestID != change.Remove && item.RequestID != change.Front {
			ids = append(ids, item.RequestID)
		}
	}
	if change.Front != "" {
		ids = append([]string{change.Front}, ids...)
	}
	if change.Append != nil {
		ids = append(ids, change.Append.RequestID)
	}
	return ids
}

// sameQueuedMessage compares two queued items apart from how they display: a
// reloaded client rebuilds the display, with a new time, for the same message.
func sameQueuedMessage(a, b json.RawMessage) bool {
	withoutDisplay := func(raw json.RawMessage) any {
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			return string(raw)
		}
		delete(value, "display")
		return value
	}
	return reflect.DeepEqual(withoutDisplay(a), withoutDisplay(b))
}

// Prompt statuses answer what became of a message a client lost track of.
const (
	PromptQueued      = "queued"
	PromptDispatching = "dispatching"
	PromptDelivered   = "delivered"
	// PromptFailed means no provider received it, so sending again is safe.
	PromptFailed = "failed"
	// PromptUnknown means it may have been delivered; only the user can judge.
	PromptUnknown = "unknown"
	PromptRemoved = "removed"
	PromptNone    = "none"
)

// PromptStatuses reports each message's fate, for a client reconciling after a
// reload: a dispatched message may be in a turn it has not loaded.
func (s *Service) PromptStatuses(ctx context.Context, scopeID int64, conversationID string, requestIDs []string) (map[string]string, error) {
	conv, err := s.scopedConversation(ctx, scopeID, conversationID)
	if err != nil {
		return nil, err
	}
	if len(requestIDs) > maxQueueItems {
		return nil, fmt.Errorf("%w: ask about up to %d messages", ErrInvalidRequest, maxQueueItems)
	}
	state, err := s.store.Queue(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	statuses := make(map[string]string, len(requestIDs))
	for _, item := range state.Items {
		statuses[item.RequestID] = PromptQueued
	}
	dispatching := s.queues.dispatchingFor(conversationID)
	result := make(map[string]string, len(requestIDs))
	for _, id := range requestIDs {
		switch {
		case id != "" && id == dispatching:
			result[id] = PromptDispatching
		case statuses[id] != "":
			result[id] = statuses[id]
		default:
			if result[id], err = s.settledPromptStatus(ctx, conv, id); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func (s *Service) settledPromptStatus(ctx context.Context, conv sqlitedb.Conversation, requestID string) (string, error) {
	removed, err := s.store.QueueItemRemoved(ctx, conv.ID, requestID)
	if err != nil {
		return "", err
	}
	if removed {
		return PromptRemoved, nil
	}
	status := PromptNone
	// Each attempt the provider refused was followed by a retry under a new id; the last decides.
	for attempt, id := 0, requestID; ; attempt, id = attempt+1, fmt.Sprintf("%s-retry-%d", requestID, attempt+1) {
		receipt, err := s.store.Receipt(ctx, conv.ScopeID, id)
		if errors.Is(err, store.ErrNotFound) {
			return status, nil
		}
		if err != nil {
			return "", err
		}
		if receipt.Uncertain != 0 {
			return PromptUnknown, nil
		}
		turn, err := s.store.Turn(ctx, conv.ID, "turn-"+id)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
		if err == nil && turn.Status == "failed" && (turn.ErrorKind == "provider_unavailable" || turn.ErrorKind == "provider_error") {
			status = PromptFailed
			continue
		}
		return PromptDelivered, nil
	}
}

// rawOptionalString decodes an optional setting: absent keeps it, null
// clears it.
func rawOptionalString(raw json.RawMessage) (*string, bool, error) {
	switch {
	case len(raw) == 0:
		return nil, false, nil
	case string(raw) == "null":
		return nil, true, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false, fmt.Errorf("%w: settings values must be strings or null", ErrInvalidSettings)
	}
	return &v, false, nil
}

func (s *Service) isClosing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing
}

// referencedAgents checks a queued item's agents: at most five, each also
// an @ agent reference of the message.
func referencedAgents(item queueItem) bool {
	if len(item.Agents) > maxReferences {
		return false
	}
	for _, a := range item.Agents {
		found := false
		for _, r := range item.References {
			found = found || (r.Kind == "agent" && r.ID == a.ID)
		}
		if !found || !nativeID.MatchString(a.ID) || a.Name == "" || len(a.Name) > 256 {
			return false
		}
	}
	return true
}
