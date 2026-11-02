package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

var (
	// ErrInteractionAnswered rejects an answer that differs from the one
	// already accepted: the first answer wins (plan §8.3).
	ErrInteractionAnswered = errors.New("local conversation: this question already has a different answer")
	// ErrInteractionClosed rejects an answer to a question that is no
	// longer waiting.
	ErrInteractionClosed = errors.New("local conversation: this question is no longer waiting for an answer")
)

const questionTool = "ask_user_question"

// interactions tracks questions a running execution is waiting on.
type interactions struct {
	mu      sync.Mutex
	waiting map[string]*waitingQuestion
}

type waitingQuestion struct {
	conversationID string
	turnID         string
	questions      userQuestions
	done           chan questionOutcome
}

type questionOutcome struct {
	answers map[string]any
	err     error
}

func questionInteractionID(turnID, requestID string) string {
	return "question:" + turnID + ":" + requestID
}

// questionToolFor is ask_user_question for one provider process; current
// reports the running turn.
func (s *Service) questionToolFor(conv sqlitedb.Conversation, current func() map[string]any) contextmcp.Tool {
	return contextmcp.Tool{
		Name:        questionTool,
		ReadOnly:    true,
		Description: "Ask the user 1–4 clarification questions in Multica's conversation UI and wait for their answers before continuing this same execution. Supports single choice, multiple choice and free text. Use stable question IDs and reuse request_id for a retry of the same question; use a new request_id for a new question. Do not invent an answer or ask for secrets. This tool does not grant command permissions or approve other tool calls.",
		InputSchema: json.RawMessage(`{"type":"object","required":["request_id","questions"],"additionalProperties":false,"properties":{"request_id":{"type":"string","minLength":1,"maxLength":128,"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_-]*$"},"questions":{"type":"array","minItems":1,"maxItems":4,"items":{"type":"object","required":["id","question"],"additionalProperties":false,"properties":{"id":{"type":"string","minLength":1,"maxLength":80},"header":{"type":"string","maxLength":40},"question":{"type":"string","minLength":1,"maxLength":4000},"options":{"type":"array","maxItems":8,"items":{"type":"object","required":["label"],"additionalProperties":false,"properties":{"label":{"type":"string","minLength":1,"maxLength":200},"description":{"type":"string","maxLength":1000}}}},"multiSelect":{"type":"boolean","default":false},"allowFreeText":{"type":"boolean","default":true}}}}}}`),
		Call: func(ctx context.Context, raw json.RawMessage) (contextmcp.Result, error) {
			turnID, _ := current()["turn_id"].(string)
			if turnID == "" {
				return contextmcp.Result{}, errors.New("this execution has stopped")
			}
			answers, err := s.ask(ctx, conv.ID, turnID, raw)
			if err != nil {
				return contextmcp.Result{}, err
			}
			return contextmcp.Result{Value: map[string]any{"answersByQuestionId": answers}}, nil
		},
	}
}

// ask records a question and waits for its answer. Retrying the same
// request_id returns the same outcome; a different question under it is
// rejected.
func (s *Service) ask(ctx context.Context, conversationID, turnID string, raw json.RawMessage) (map[string]any, error) {
	qs, err := parseUserQuestions(raw)
	if err != nil {
		return nil, err
	}
	id := questionInteractionID(turnID, qs.RequestID)
	request, _ := json.Marshal(qs)

	s.questions.mu.Lock()
	w := s.questions.waiting[id]
	if w == nil {
		w = &waitingQuestion{conversationID: conversationID, turnID: turnID, questions: qs, done: make(chan questionOutcome, 1)}
		s.questions.waiting[id] = w
		s.questions.mu.Unlock()
		if err := s.openQuestion(ctx, conversationID, turnID, id, qs, request); err != nil {
			s.questions.mu.Lock()
			delete(s.questions.waiting, id)
			s.questions.mu.Unlock()
			return nil, err
		}
	} else {
		s.questions.mu.Unlock()
		if !sameQuestions(w.questions, qs) {
			return nil, errors.New("this request_id already belongs to a different question")
		}
	}

	select {
	case out := <-w.done:
		w.done <- out // later retries of the same request see the same outcome
		return out.answers, out.err
	case <-ctx.Done():
		s.cancelQuestions(conversationID, []string{id})
		return nil, errors.New("the question's execution ended before an answer was received")
	}
}

// openQuestion persists a new question, unless the request id was already
// used in this turn: an answered one returns its answers through the
// waiter, a closed one is rejected.
func (s *Service) openQuestion(ctx context.Context, conversationID, turnID, id string, qs userQuestions, request json.RawMessage) error {
	if row, err := s.store.Interaction(ctx, id); err == nil {
		var previous userQuestions
		_ = json.Unmarshal([]byte(row.Payload), &previous)
		if !sameQuestions(previous, qs) {
			return errors.New("this request_id already belongs to a different question")
		}
		if row.Status == "answered" {
			var answers map[string]any
			_ = json.Unmarshal([]byte(row.Answer.String), &answers)
			s.deliver(id, questionOutcome{answers: answers})
			return nil
		}
		return errors.New("this question is no longer active; use a new request_id")
	}
	now := s.opts.Now()
	payload, _ := json.Marshal(map[string]any{"interaction_id": id, "kind": "question", "request": json.RawMessage(request)})
	_, err := s.batcher.AppendWait(ctx, store.Batch{
		ConversationID: conversationID,
		Interactions:   []store.NewInteraction{{ID: id, TurnID: turnID, ProviderRequestID: qs.RequestID, Kind: "question", Request: request, CreatedAt: now}},
		Events:         []store.NewEvent{{TurnID: turnID, Type: "interaction.requested", Payload: payload, CreatedAt: now}},
	})
	return err
}

// Answer accepts a user's answer to a waiting question. The first answer
// wins; repeating it is a no-op and a different one is rejected.
func (s *Service) Answer(ctx context.Context, scopeID int64, conversationID, interactionID string, raw json.RawMessage, connection string) error {
	if _, err := s.scopedConversation(ctx, scopeID, conversationID); err != nil {
		return err
	}
	row, err := s.store.Interaction(ctx, interactionID)
	if err != nil || row.ConversationID != conversationID {
		return store.ErrNotFound
	}
	var qs userQuestions
	if err := json.Unmarshal([]byte(row.Payload), &qs); err != nil {
		return err
	}
	answers, err := questionAnswers(qs, raw)
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(answers)
	if done, err := settledAnswer(row, encoded); done {
		return err
	}
	now := s.opts.Now()
	payload, _ := json.Marshal(map[string]any{"interaction_id": interactionID, "status": "answered", "answer": answers})
	_, err = s.batcher.AppendWait(ctx, store.Batch{
		ConversationID: conversationID,
		Resolutions:    []store.InteractionResolution{{ID: interactionID, Status: "answered", Answer: encoded, By: connection, At: now}},
		Events:         []store.NewEvent{{TurnID: row.TurnID, Type: "interaction.resolved", Final: true, Payload: payload, CreatedAt: now}},
	})
	if errors.Is(err, store.ErrInteractionClosed) {
		// Another answer won the race; compare with it.
		if row, err := s.store.Interaction(ctx, interactionID); err == nil {
			_, err := settledAnswer(row, encoded)
			return err
		}
	}
	if err != nil {
		return err
	}
	s.deliver(interactionID, questionOutcome{answers: answers})
	return nil
}

// settledAnswer reports the outcome for a question that is no longer
// pending: the same answer again succeeds, anything else is rejected.
func settledAnswer(row sqlitedb.InteractionRequest, answer []byte) (bool, error) {
	switch row.Status {
	case "pending":
		return false, nil
	case "answered":
		if jsonEqual([]byte(row.Answer.String), answer) {
			return true, nil
		}
		return true, ErrInteractionAnswered
	}
	return true, ErrInteractionClosed
}

// turnEnded cancels the turn's questions that are still waiting.
func (s *Service) turnEnded(conversationID, turnID string) {
	s.endComputer(conversationID)
	s.questions.mu.Lock()
	var ids []string
	for id, w := range s.questions.waiting {
		if w.conversationID == conversationID && w.turnID == turnID {
			ids = append(ids, id)
		}
	}
	s.questions.mu.Unlock()
	if len(ids) > 0 {
		s.cancelQuestions(conversationID, ids)
	}
	s.mu.Lock()
	ended := s.turnEnd
	s.mu.Unlock()
	if ended != nil {
		ended(conversationID)
	}
	go func() {
		s.dispatchQueue(conversationID)
		s.generateTitle(conversationID)
	}()
}

// cancelQuestions closes still-pending questions and wakes their waiters.
func (s *Service) cancelQuestions(conversationID string, ids []string) {
	now := s.opts.Now()
	batch := store.Batch{ConversationID: conversationID}
	for _, id := range ids {
		s.questions.mu.Lock()
		w := s.questions.waiting[id]
		s.questions.mu.Unlock()
		turnID := ""
		if w != nil {
			turnID = w.turnID
		}
		payload, _ := json.Marshal(map[string]any{"interaction_id": id, "status": "cancelled"})
		batch.Resolutions = append(batch.Resolutions, store.InteractionResolution{ID: id, Status: "cancelled", At: now, IgnoreClosed: true})
		batch.Events = append(batch.Events, store.NewEvent{TurnID: turnID, Type: "interaction.resolved", Final: true, Payload: payload, CreatedAt: now})
	}
	_ = s.batcher.Append(batch, true)
	for _, id := range ids {
		s.deliver(id, questionOutcome{err: errors.New("the question's execution ended before an answer was received")})
	}
}

func (s *Service) deliver(id string, out questionOutcome) {
	s.questions.mu.Lock()
	w := s.questions.waiting[id]
	delete(s.questions.waiting, id)
	s.questions.mu.Unlock()
	if w != nil {
		select {
		case w.done <- out:
		default:
		}
	}
}

func sameQuestions(a, b userQuestions) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ea, _ := json.Marshal(x)
	eb, _ := json.Marshal(y)
	return string(ea) == string(eb)
}
