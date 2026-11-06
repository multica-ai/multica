package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

const SubmissionLifecycleCapability = "submission-lifecycle-v1"

var errSubmissionStopped = errors.New("local conversation: submission stopped")

type SubmissionResult struct {
	StoreGeneration string           `json:"store_generation"`
	SnapshotSeq     int64            `json:"snapshot_seq"`
	QueueRevision   int64            `json:"queue_revision"`
	Submission      store.Submission `json:"submission"`
	Queue           QueueView        `json:"queue"`
}

type SubmissionStatuses struct {
	StoreGeneration string             `json:"store_generation"`
	SnapshotSeq     int64              `json:"snapshot_seq"`
	QueueRevision   int64              `json:"queue_revision"`
	Submissions     []store.Submission `json:"submissions"`
	// Absence is only proven within this database, which retains submission identities.
	KnownHistory bool `json:"known_history"`
}

type submissionRun struct {
	attemptID string
	cancel    context.CancelCauseFunc
}

func attemptID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "attempt-" + hex.EncodeToString(b)
}

func (s *Service) Submissions(ctx context.Context, scopeID int64, id string, requestIDs []string) (SubmissionStatuses, error) {
	if _, err := s.scopedConversation(ctx, scopeID, id); err != nil {
		return SubmissionStatuses{}, err
	}
	if len(requestIDs) > maxQueueItems {
		return SubmissionStatuses{}, fmt.Errorf("%w: too many submission IDs", ErrInvalidRequest)
	}
	seq, queue, views, err := s.store.SubmissionState(ctx, id, requestIDs)
	return SubmissionStatuses{StoreGeneration: s.Generation(), SnapshotSeq: seq, QueueRevision: queue.Revision, Submissions: views, KnownHistory: true}, err
}

// Submit durably admits a message and reserves an idle execution before answering.
func (s *Service) Submit(ctx context.Context, scopeID int64, id string, raw json.RawMessage, origin TurnOrigin) (SubmissionResult, error) {
	conv, err := s.scopedConversation(ctx, scopeID, id)
	if err != nil {
		return SubmissionResult{}, err
	}
	item, err := parseQueueItem(raw)
	if err != nil {
		return SubmissionResult{}, err
	}
	ctx = context.WithoutCancel(ctx)
	unlock := s.queues.lock(id)
	_, _, known, err := s.store.SubmissionState(ctx, id, []string{item.RequestID})
	if err != nil {
		unlock()
		return SubmissionResult{}, err
	}
	if len(known) > 0 {
		if known[0].State != "removed" && string(known[0].Item) != "{}" && !sameQueuedMessage(known[0].Item, raw) {
			unlock()
			return SubmissionResult{}, ErrRequestConflict
		}
		result, err := s.submissionResult(ctx, id, item.RequestID)
		unlock()
		return result, err
	}
	// A request sent by an older client may already have a receipt without a submission row.
	if receipt, err := s.store.Receipt(ctx, scopeID, item.RequestID); err == nil {
		cmd := queuedCommand(conv, item, origin)
		if receipt.Fingerprint != sendFingerprint(cmd) {
			unlock()
			return SubmissionResult{}, ErrRequestConflict
		}
		_, err = s.batcher.AppendWait(ctx, store.Batch{ConversationID: id, Submissions: []store.SubmissionChange{{RequestID: item.RequestID, Item: raw, State: "attached"}}})
		if err != nil {
			unlock()
			return SubmissionResult{}, err
		}
		result, err := s.submissionResult(ctx, id, item.RequestID)
		unlock()
		return result, err
	} else if !errors.Is(err, store.ErrNotFound) {
		unlock()
		return SubmissionResult{}, err
	}
	if conv.Archived == 1 {
		unlock()
		return SubmissionResult{}, ErrArchived
	}
	state, err := s.store.Queue(ctx, id)
	if err != nil {
		unlock()
		return SubmissionResult{}, err
	}
	if len(state.Items) >= maxQueueItems {
		unlock()
		return SubmissionResult{}, ErrQueueFull
	}
	change := store.QueueChange{Revision: state.Revision + 1, Paused: state.Paused, Error: state.Error, Append: &store.QueueItem{RequestID: item.RequestID, Item: raw, OriginPeerID: origin.PeerID, OriginClientKind: origin.ClientKind}}
	// A fresh send resumes a user pause; failed or restored queues still need explicit recovery.
	if state.Error == "" {
		change.Paused = false
	}
	batch := s.queueBatch(id, state, change, len(state.Items)+1, "enqueued")
	var claim *store.SubmissionAttempt
	busy, err := s.executionBusy(ctx, id)
	if err != nil {
		unlock()
		return SubmissionResult{}, err
	}
	if !busy && s.acceptTurn() == nil && !change.Paused && change.Error == "" && len(state.Items) == 0 {
		claim, err = s.newClaim(ctx, conv, item)
		if err != nil {
			unlock()
			return SubmissionResult{}, err
		}
		batch.Submissions = []store.SubmissionChange{{RequestID: item.RequestID, Claim: claim}}
	}
	if _, err = s.batcher.AppendWait(ctx, batch); err != nil {
		unlock()
		return SubmissionResult{}, executionError(err)
	}
	result, err := s.submissionResult(ctx, id, item.RequestID)
	var runCtx context.Context
	var cancel context.CancelCauseFunc
	if claim != nil {
		runCtx, cancel = context.WithCancelCause(context.Background())
		s.setSubmissionRun(id, claim.AttemptID, cancel)
	}
	unlock()
	if claim != nil {
		s.goBackground(func() { s.runSubmission(runCtx, conv, item, raw, *claim, origin) })
	} else {
		s.goBackground(func() { s.dispatchQueue(id) })
	}
	return result, err
}

func (s *Service) submissionResult(ctx context.Context, id, requestID string) (SubmissionResult, error) {
	seq, queue, views, err := s.store.SubmissionState(ctx, id, []string{requestID})
	if err != nil {
		return SubmissionResult{}, err
	}
	if len(views) == 0 {
		return SubmissionResult{}, store.ErrNotFound
	}
	return SubmissionResult{StoreGeneration: s.Generation(), SnapshotSeq: seq, QueueRevision: queue.Revision, Submission: views[0], Queue: withRecoveryItems(s.queueView(id, queue), views)}, nil
}
func executionError(err error) error {
	if errors.Is(err, store.ErrExecutionBusy) {
		return ErrTurnRunning
	}
	return err
}
func (s *Service) executionBusy(ctx context.Context, id string) (bool, error) {
	if _, err := s.store.Execution(ctx, id); err == nil {
		return true, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	return s.conversationBusy(ctx, id)
}
func queuedCommand(conv sqlitedb.Conversation, item queueItem, origin TurnOrigin) SendCommand {
	cmd := SendCommand{ScopeID: conv.ScopeID, ConversationID: conv.ID, RequestID: item.RequestID, Text: item.Content, AttachmentIDs: item.AttachmentIDs, References: item.References, Origin: origin}
	if item.Prompt != nil {
		cmd.Prompt = *item.Prompt
	}
	return cmd
}
func (s *Service) newClaim(ctx context.Context, conv sqlitedb.Conversation, item queueItem) (*store.SubmissionAttempt, error) {
	req, err := s.queuedAttemptID(ctx, conv, item.RequestID)
	if err != nil {
		return nil, err
	}
	attempts, err := s.store.Attempts(ctx, conv.ID, item.RequestID)
	if err != nil {
		return nil, err
	}
	for _, prior := range attempts {
		if prior.RequestID == req {
			req = fmt.Sprintf("%s-retry-%d", item.RequestID, len(attempts))
			break
		}
	}
	settings := conversationSettings(conv)
	if item.Model != nil {
		value, clear, err := rawOptionalString(item.Model)
		if err != nil {
			return nil, err
		}
		if clear {
			settings.Model = nil
		} else {
			settings.Model = value
		}
	}
	if item.ReasoningEffort != nil {
		value, clear, err := rawOptionalString(item.ReasoningEffort)
		if err != nil {
			return nil, err
		}
		if clear {
			settings.ReasoningEffort = nil
		} else {
			settings.ReasoningEffort = value
		}
	}
	if item.PermissionMode != nil {
		settings.PermissionMode = *item.PermissionMode
	}
	encoded, _ := json.Marshal(settings)
	return &store.SubmissionAttempt{AttemptID: attemptID(), RequestID: req, TurnIDs: []string{}, Settings: encoded, Delivery: "not_sent"}, nil
}

func (s *Service) setSubmissionRun(id, attempt string, cancel context.CancelCauseFunc) {
	s.queues.mu.Lock()
	defer s.queues.mu.Unlock()
	if s.queues.preparing == nil {
		s.queues.preparing = map[string]submissionRun{}
	}
	s.queues.preparing[id] = submissionRun{attempt, cancel}
}
func (s *Service) cancelSubmissionRun(id string) {
	s.queues.mu.Lock()
	run := s.queues.preparing[id]
	s.queues.mu.Unlock()
	if run.cancel != nil {
		run.cancel(nil)
	}
}
func (s *Service) clearSubmissionRun(id, attempt string) {
	s.queues.mu.Lock()
	defer s.queues.mu.Unlock()
	if s.queues.preparing[id].attemptID == attempt {
		delete(s.queues.preparing, id)
	}
}

func (s *Service) runSubmission(ctx context.Context, conv sqlitedb.Conversation, item queueItem, raw json.RawMessage, claim store.SubmissionAttempt, origin TurnOrigin) {
	reschedule := false
	defer func() {
		s.clearSubmissionRun(conv.ID, claim.AttemptID)
		if reschedule {
			s.goBackground(func() { s.dispatchQueue(conv.ID) })
		}
	}()
	s.queues.setDispatching(conv.ID, item.RequestID)
	defer func() {
		s.queues.mu.Lock()
		defer s.queues.mu.Unlock()
		if s.queues.dispatching[conv.ID] == item.RequestID {
			delete(s.queues.dispatching, conv.ID)
		}
	}()
	trace := s.traces.dispatching(conv, item)
	finished := trace.Begin(timing.StageQueueDispatch)
	err := ctx.Err()
	if err == nil {
		err = s.sendClaimed(ctx, conv, item, claim, origin)
	}
	finished(err)
	unlock := s.queues.lock(conv.ID)
	defer unlock()
	state, e := s.store.Queue(context.Background(), conv.ID)
	if e != nil {
		return
	}
	change := store.SubmissionChange{RequestID: item.RequestID, ExpectedAttemptID: claim.AttemptID, Delivery: "confirmed"}
	if len(item.Agents) > 0 {
		change.Delivery = "intent_recorded"
	}
	batch := store.Batch{ConversationID: conv.ID}
	stopped := false
	if err != nil {
		if errors.Is(err, ErrOutcomeUnknown) {
			change.State = "recovery_required"
			change.Reason = err.Error()
			change.Delivery = "unknown"
		} else {
			change.State = "queued"
			change.Reason = err.Error()
			change.Delivery = "not_sent"
			change.Release = true
			q := store.QueueChange{Revision: state.Revision + 1, Paused: true, Error: err.Error()}
			reason := "failed"
			if errors.Is(context.Cause(ctx), errSubmissionStopped) {
				// Stop already committed the pause; a later send or pause owns the current state.
				q.Paused, q.Error = state.Paused, state.Error
				change.Reason = ""
				reason, stopped = "enqueued", true
			}
			found := false
			for _, v := range state.Items {
				found = found || v.RequestID == item.RequestID
			}
			if !found {
				q.Append = &store.QueueItem{RequestID: item.RequestID, Item: raw, OriginPeerID: origin.PeerID, OriginClientKind: origin.ClientKind}
				q.Front = item.RequestID
			}
			count := len(state.Items)
			if !found {
				count++
			}
			batch = s.queueBatch(conv.ID, state, q, count, reason)
		}
	}
	batch.Submissions = []store.SubmissionChange{change}
	if _, e := s.batcher.AppendWait(context.Background(), batch); e != nil {
		s.opts.Logger.Warn("could not record submission outcome", "conversation_id", conv.ID, "error", e)
		return
	}
	reschedule = stopped
}
func (s *Service) sendClaimed(ctx context.Context, conv sqlitedb.Conversation, item queueItem, claim store.SubmissionAttempt, origin TurnOrigin) error {
	var settings store.ConversationSettings
	if err := json.Unmarshal(claim.Settings, &settings); err != nil {
		return err
	}
	update := SettingsUpdate{Model: settings.Model, ClearModel: settings.Model == nil, ReasoningEffort: settings.ReasoningEffort, ClearReasoningEffort: settings.ReasoningEffort == nil, PermissionMode: &settings.PermissionMode}
	current, err := s.store.Conversation(ctx, conv.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(conversationSettings(current), settings) {
		if err := s.UpdateSettings(ctx, conv.ScopeID, conv.ID, update); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := queuedCommand(conv, item, origin)
	cmd.RequestID = claim.RequestID
	if len(item.Agents) > 0 {
		_, err := s.sendToAgents(ctx, cmd, item.Agents)
		return err
	}
	_, err = s.send(ctx, cmd)
	return err
}

// directExecution keeps legacy direct sends in the same foreground admission domain.
func (s *Service) directExecution(ctx context.Context, cmd SendCommand, remote ...bool) (context.Context, func(error), error) {
	unlock := s.queues.lock(cmd.ConversationID)
	if err := s.acceptTurn(); err != nil {
		unlock()
		return ctx, nil, err
	}
	busy, err := s.executionBusy(ctx, cmd.ConversationID)
	if err != nil || busy {
		unlock()
		if err == nil {
			err = ErrTurnRunning
		}
		return ctx, nil, err
	}
	claim := store.SubmissionAttempt{AttemptID: attemptID(), RequestID: cmd.RequestID}
	_, err = s.batcher.AppendWait(context.WithoutCancel(ctx), store.Batch{ConversationID: cmd.ConversationID, Submissions: []store.SubmissionChange{{RequestID: cmd.RequestID, Claim: &claim, Legacy: true}}})
	if err != nil {
		unlock()
		return ctx, nil, executionError(err)
	}
	runCtx, cancel := context.WithCancelCause(ctx)
	s.setSubmissionRun(cmd.ConversationID, claim.AttemptID, cancel)
	unlock()
	return runCtx, func(outcome error) {
		defer cancel(nil)
		s.clearSubmissionRun(cmd.ConversationID, claim.AttemptID)
		delivery := "confirmed"
		if len(remote) > 0 && remote[0] {
			delivery = "intent_recorded"
		}
		if outcome != nil {
			delivery = "not_sent"
		}
		unknown := errors.Is(outcome, ErrOutcomeUnknown)
		if unknown {
			delivery = "unknown"
		}
		_, err := s.batcher.AppendWait(context.Background(), store.Batch{ConversationID: cmd.ConversationID, Submissions: []store.SubmissionChange{{RequestID: cmd.RequestID, ExpectedAttemptID: claim.AttemptID, Legacy: true, Delivery: delivery, Release: outcome != nil && !unknown}}})
		if err != nil {
			s.opts.Logger.Warn("could not finish direct admission", "error", err)
		}
	}, nil
}

func (s *Service) recoverSubmissions(ctx context.Context) error {
	slots, err := s.store.Executions(ctx)
	if err != nil {
		return err
	}
	for _, slot := range slots {
		a, err := s.store.Attempt(ctx, slot.ConversationID, slot.AttemptID)
		if err != nil {
			return err
		}
		turns, err := s.store.ConversationTurns(ctx, slot.ConversationID)
		if err != nil {
			return err
		}
		ids := []string{}
		if err := json.Unmarshal([]byte(a.TurnIds), &ids); err != nil {
			return err
		}
		running, remoteConfirmed := false, len(ids) > 0
		for _, id := range ids {
			found := false
			for _, turn := range turns {
				if turn.ID != id {
					continue
				}
				found = true
				running = running || turn.Status == "running"
				remote := remoteOf(turn)
				remoteConfirmed = remoteConfirmed && remote != nil && (remote.TaskID != "" || (turn.Status != "running" && turn.ErrorKind != "outcome_unknown"))
			}
			remoteConfirmed = remoteConfirmed && found
		}
		change := store.SubmissionChange{RequestID: slot.RequestID, ExpectedAttemptID: slot.AttemptID, Legacy: a.SubmissionID == ""}
		switch a.Delivery {
		case "not_sent":
			change.State = "queued"
			change.Reason = queuePausedByRestart
			change.Release = true
		case "confirmed":
			change.State = "attached"
			change.Release = !running
		case "intent_recorded":
			if remoteConfirmed {
				change.State = "attached"
				change.Delivery = "confirmed"
				change.Release = !running
			} else {
				change.State = "recovery_required"
				change.Reason = "The previous execution may have received this message. Review it before retrying."
				change.Delivery = "unknown"
			}
		default:
			change.State = "recovery_required"
			change.Reason = "The previous execution may have received this message. Review it before retrying."
			change.Delivery = "unknown"
		}
		if _, err := s.batcher.AppendWait(ctx, store.Batch{ConversationID: slot.ConversationID, Submissions: []store.SubmissionChange{change}}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SnapshotQueue(id string, state store.QueueState, submissions []store.Submission) QueueView {
	return withRecoveryItems(s.queueView(id, state), submissions)
}
func withRecoveryItems(view QueueView, submissions []store.Submission) QueueView {
	for _, value := range submissions {
		if value.State != "recovery_required" || len(value.Item) == 0 {
			continue
		}
		found := false
		for _, raw := range view.Items {
			var item queueItem
			_ = json.Unmarshal(raw, &item)
			found = found || item.RequestID == value.RequestID
		}
		if !found {
			view.Items = append(view.Items, value.Item)
		}
		if view.Error == "" {
			view.Error = value.BlockedReason
		}
	}
	return view
}

// A delayed control can only stop the execution it observed, never its successor.
func (s *Service) interruptExecution(ctx context.Context, scopeID int64, id string, slot sqlitedb.ConversationExecution) error {
	if slot.AttemptID == "" {
		return nil
	}
	s.queues.mu.Lock()
	run := s.queues.preparing[id]
	s.queues.mu.Unlock()
	if run.attemptID == slot.AttemptID && run.cancel != nil {
		run.cancel(errSubmissionStopped)
	}
	a, err := s.store.Attempt(ctx, id, slot.AttemptID)
	if err != nil {
		return err
	}
	if found, err := s.cancelRemote(ctx, scopeID, id, a.RequestID); found || err != nil {
		return err
	}
	var turns []string
	if err := json.Unmarshal([]byte(a.TurnIds), &turns); err != nil {
		return err
	}
	if len(turns) == 0 {
		turns = []string{"turn-" + a.RequestID}
	}
	s.mu.Lock()
	r := s.runners[id]
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	for _, turn := range turns {
		if err := r.Interrupt(ctx, turn); err != nil {
			return err
		}
	}
	return nil
}
