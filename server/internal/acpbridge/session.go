package acpbridge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/interaction"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type session struct {
	id       string
	ctx      context.Context
	done     chan struct{}
	cancel   context.CancelFunc
	op       sync.Mutex
	mu       sync.Mutex
	run      run
	record   *interaction.Record
	err      error
	seq      map[string]int
	humans   map[string]bool
	tools    map[string]bool
	state    string
	sent     sync.Map
	replayed map[string]bool
}

func (b *Bridge) load(ctx context.Context, id string) (any, error) {
	b.loadMu.Lock()
	defer b.loadMu.Unlock()
	if _, _, err := splitID(id, b.workspace); err != nil {
		return nil, err
	}
	runs, err := b.runs(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err = currentRun(runs); err != nil {
		return nil, err
	}
	child, cancel := context.WithCancel(ctx)
	s := &session{id: id, ctx: child, done: make(chan struct{}), cancel: cancel, seq: map[string]int{}, humans: map[string]bool{}, tools: map[string]bool{}, replayed: map[string]bool{}}
	b.mu.Lock()
	if len(b.sessions) >= 8 && b.sessions[id] == nil {
		b.mu.Unlock()
		cancel()
		return nil, errors.New("at most eight conversations may be attached")
	}
	if old := b.sessions[id]; old != nil {
		b.mu.Unlock()
		old.cancel()
		<-old.done
		b.mu.Lock()
	}
	b.sessions[id] = s
	b.mu.Unlock()
	if err = b.refresh(child, s); err != nil {
		cancel()
		b.mu.Lock()
		delete(b.sessions, id)
		b.mu.Unlock()
		return nil, err
	}
	commands := []map[string]string{
		{"name": "interrupt", "description": "Interrupt the current turn; retain the run and provider process"},
		{"name": "cancel-run", "description": "Cancel this entire Multica run"},
		{"name": "finish-run", "description": "Successfully finish an idle run (does not mark the issue done)"},
		{"name": "status", "description": "Show the current run state and deadline"},
		{"name": "follow-up", "description": "Start a new run from the latest finished run: /follow-up <message>"},
	}
	b.update(id, map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": commands})
	b.pollers.Add(1)
	go func() {
		defer b.pollers.Done()
		defer close(s.done)
		ticker := time.NewTicker(b.poll)
		defer ticker.Stop()
		for {
			select {
			case <-child.Done():
				return
			case <-ticker.C:
				err := b.refresh(child, s)
				s.mu.Lock()
				previous := s.err
				s.err = err
				s.mu.Unlock()
				if err != nil && previous == nil {
					b.text(id, "Multica connection unavailable: "+err.Error()+". Commands will not be silently replayed.")
				}
			}
		}
	}()
	return map[string]any{}, nil
}

// Poll the authoritative store with a sequence cursor. No socket event is
// required for delivery, and opening never invokes a model or creates a run.
func (b *Bridge) refresh(ctx context.Context, s *session) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	runs, err := b.runs(ctx, s.id)
	if err != nil {
		return err
	}
	selected, err := currentRun(runs)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if r.ID != selected.ID && r.terminal() && s.replayed[r.ID] {
			continue
		}
		// This endpoint is also the per-run issue/private-agent access check.
		record, err := b.record(ctx, r)
		if err != nil {
			return err
		}
		messages, err := b.messages(ctx, r, s.seq[r.ID])
		if err != nil {
			return err
		}
		// Preserve native sequence order, inserting human messages by timestamp.
		type human struct{ key, text, at string }
		humans := []human{}
		if record != nil {
			if record.OpeningInput != "" && !s.humans[r.ID+":opening"] {
				humans = append(humans, human{r.ID + ":opening", record.OpeningInput, r.CreatedAt})
			}
			for _, c := range record.Commands {
				key := r.ID + ":" + c.ID
				if !s.humans[key] {
					_, own := s.sent.Load(c.ID)
					if c.Kind == "input" && !own {
						humans = append(humans, human{key, c.Text, c.CreatedAt.Format(time.RFC3339Nano)})
					}
					if own || c.Kind != "input" {
						s.humans[key] = true
					}
				}
			}
		}
		sort.SliceStable(humans, func(i, j int) bool { return humans[i].at < humans[j].at })
		emitHuman := func() { h := humans[0]; b.human(s.id, h.text); s.humans[h.key] = true; humans = humans[1:] }
		for _, m := range messages {
			for len(humans) > 0 && (m.CreatedAt == "" || humans[0].at <= m.CreatedAt) {
				emitHuman()
			}
			if m.Seq > s.seq[r.ID] {
				b.message(s, m)
				s.seq[r.ID] = m.Seq
			}
		}
		for len(humans) > 0 {
			emitHuman()
		}
		if r.terminal() {
			s.replayed[r.ID] = true
		}
		if r.ID == selected.ID {
			s.mu.Lock()
			s.run = r
			s.record = record
			s.mu.Unlock()
			state := r.Status
			if record != nil && !r.terminal() {
				state = string(record.State.State)
			}
			key := r.ID + ":" + state
			if s.state != key {
				b.text(s.id, fmt.Sprintf("[Multica run %s: %s]", r.ID, state))
				s.state = key
			}
		}
	}
	return nil
}

func (b *Bridge) human(id, text string) {
	b.update(id, map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]string{"type": "text", "text": text}})
}

func (b *Bridge) message(s *session, m protocol.TaskMessagePayload) {
	kind := "agent_message_chunk"
	switch m.Type {
	case "thinking":
		kind = "agent_thought_chunk"
	case "tool_use", "tool_result", "tool_progress":
		id := m.TaskID + ":" + m.CallID
		if m.CallID == "" {
			id = fmt.Sprintf("%s:seq:%d", m.TaskID, m.Seq)
		}
		update := "tool_call_update"
		if !s.tools[id] {
			update = "tool_call"
			s.tools[id] = true
		}
		status := "in_progress"
		if m.Type == "tool_result" {
			status = "completed"
		}
		v := map[string]any{"sessionUpdate": update, "toolCallId": id, "status": status}
		if update == "tool_call" {
			v["title"] = m.Tool
			v["kind"] = "other"
		}
		if m.Input != nil {
			v["rawInput"] = m.Input
		}
		if m.Output != "" {
			v["content"] = []any{map[string]any{"type": "content", "content": map[string]string{"type": "text", "text": m.Output}}}
		}
		b.update(s.id, v)
		return
	case "text", "error":
	default:
		return
	}
	if m.Content != "" {
		b.update(s.id, map[string]any{"sessionUpdate": kind, "content": map[string]string{"type": "text", "text": m.Content}})
	}
}

func (b *Bridge) control(ctx context.Context, s *session, kind, text string) (*interaction.Record, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	runs, err := b.runs(ctx, s.id)
	if err != nil {
		return nil, err
	}
	r, err := currentRun(runs)
	if err != nil {
		return nil, err
	}
	if r.terminal() {
		return nil, errors.New("run has ended; use /follow-up <message> to explicitly start another")
	}
	record, err := b.record(ctx, r)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, errors.New("this run is not interactive; enable Interactive task sessions before starting a new run")
	}
	command := agent.InteractionCommand{ID: uuid.NewString(), Kind: kind, Text: text, Activity: record.State.Activity}
	if kind == "input" {
		s.sent.Store(command.ID, true)
	}
	var accepted interaction.Record
	body := struct {
		agent.InteractionCommand
		Owner string `json:"owner"`
	}{command, record.Owner}
	if err = b.api.PostJSON(ctx, "/api/tasks/"+r.ID+"/interaction", body, &accepted); err != nil {
		return nil, fmt.Errorf("command %s submission was not confirmed: %w; inspect /status before resending", command.ID, err)
	}
	ticker := time.NewTicker(b.poll)
	defer ticker.Stop()
	for {
		for _, c := range accepted.Commands {
			if c.ID == command.ID {
				if c.Error != "" {
					return nil, errors.New(c.Error)
				}
				if c.Receipt != nil {
					// Return this command's acknowledged activity, not a possibly
					// older polling snapshot that preceded native acceptance.
					accepted.State = c.Receipt.Snapshot
					accepted.Commands = []interaction.Input{c}
					return &accepted, nil
				}
			}
		}
		select {
		case <-s.ctx.Done():
			return nil, errors.New("editor detached; Multica run remains open")
		case <-ctx.Done():
			return nil, fmt.Errorf("command %s outcome unconfirmed; inspect /status: %w", command.ID, ctx.Err())
		case <-ticker.C:
		}
		next, err := b.record(ctx, r)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return nil, errors.New("run interaction record disappeared")
		}
		accepted = *next
	}
}

func (b *Bridge) prompt(ctx context.Context, s *session, text string) (any, error) {
	ctx, cancelPrompt := context.WithCancel(ctx)
	stopPrompt := context.AfterFunc(s.ctx, cancelPrompt)
	defer stopPrompt()
	defer cancelPrompt()
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("message is empty")
	}
	if !s.op.TryLock() {
		return nil, errors.New("another prompt is active; use Stop to interrupt it first")
	}
	defer s.op.Unlock()
	result := func() (any, error) { return map[string]string{"stopReason": "end_turn"}, nil }
	if text == "/status" {
		runs, err := b.runs(ctx, s.id)
		if err != nil {
			return nil, err
		}
		r, err := currentRun(runs)
		if err != nil {
			return nil, err
		}
		record, err := b.record(ctx, r)
		if err != nil {
			return nil, err
		}
		status := fmt.Sprintf("Run %s: %s. Working directory: %s", r.ID, r.Status, r.WorkDir)
		if record != nil {
			status += fmt.Sprintf("\nActivity: %d; state: %s; deadline: %s", record.State.Activity, record.State.State, record.Deadline.Format(time.RFC3339))
		}
		b.text(s.id, status)
		return result()
	}
	if text == "/cancel-run" {
		runs, err := b.runs(ctx, s.id)
		if err != nil {
			return nil, err
		}
		r, err := currentRun(runs)
		if err != nil {
			return nil, err
		}
		if !r.terminal() {
			if err = b.api.PostJSON(ctx, "/api/tasks/"+r.ID+"/cancel", map[string]any{}, nil); err != nil {
				return nil, err
			}
		}
		b.text(s.id, "Run cancellation requested.")
		return result()
	}
	if text == "/interrupt" || text == "/finish-run" {
		kind := "interrupt"
		if text == "/finish-run" {
			kind = "finish"
		}
		call, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		if _, err := b.control(call, s, kind, ""); err != nil {
			return nil, err
		}
		b.text(s.id, "Multica acknowledged "+kind+".")
		return result()
	}
	if strings.HasPrefix(text, "/follow-up ") {
		message := strings.TrimSpace(strings.TrimPrefix(text, "/follow-up "))
		if message == "" {
			return nil, errors.New("provide a follow-up message")
		}
		runs, err := b.runs(ctx, s.id)
		if err != nil {
			return nil, err
		}
		r, err := currentRun(runs)
		if err != nil {
			return nil, err
		}
		if !r.terminal() {
			return nil, errors.New("a run is already open; send a normal message")
		}
		var next struct {
			RunID string `json:"run_id"`
		}
		id := uuid.NewString()
		if err = b.api.PostJSON(ctx, "/api/tasks/"+r.ID+"/interaction/follow-up", map[string]string{"id": id, "text": message}, &next); err != nil {
			return nil, fmt.Errorf("follow-up %s outcome unconfirmed: %w; inspect /status before retrying", id, err)
		}
		b.text(s.id, "Follow-up run queued: "+next.RunID+". Output will stream here; use /status or /interrupt while attached.")
		return result()
	}
	if strings.HasPrefix(text, "/") {
		return nil, errors.New("unknown command; supported: /status, /interrupt, /finish-run, /cancel-run, /follow-up <message>")
	}
	call, cancel := context.WithTimeout(ctx, 45*time.Second)
	accepted, err := b.control(call, s, "input", text)
	cancel()
	if err != nil {
		return nil, err
	}
	activity := accepted.State.Activity
	owner := accepted.Owner
	commandID := accepted.Commands[0].ID
	ticker := time.NewTicker(b.poll)
	defer ticker.Stop()
	for {
		s.mu.Lock()
		r, record, pollErr := s.run, s.record, s.err
		s.mu.Unlock()
		if pollErr != nil {
			return nil, pollErr
		}
		if record != nil && record.Owner != owner {
			return nil, errors.New("run changed while awaiting the reply; inspect /status")
		}
		if record != nil && record.Owner == owner && record.State.Activity >= activity {
			observed := false
			for _, c := range record.Commands {
				if c.ID == commandID && c.Receipt != nil {
					observed = true
					break
				}
			}
			if !observed {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-ticker.C:
				}
				continue
			}
			if r.terminal() || record.State.State == agent.InteractionAwaitingInput {
				// A confirmed native interrupt is a cancelled ACP turn, not a
				// cancelled Multica run. An ordinary reply ends only this prompt.
				for i := len(record.Commands) - 1; i >= 0; i-- {
					c := record.Commands[i]
					if c.Kind == "interrupt" && c.Activity == record.State.Activity && c.Receipt != nil && c.Receipt.Outcome == "applied" {
						return map[string]string{"stopReason": "cancelled"}, nil
					}
				}
				return result()
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
