package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type antigravityThinkingScope struct {
	conversationID string
	turnStart      int
	valid          bool
}

// Both stdout and native thinking use this writer, so paragraph separators
// follow actual message order without padding the end of each thinking block.
type antigravityMessages struct {
	mu           sync.Mutex
	messages     chan<- Message
	lastThinking bool
}

func (s *antigravityMessages) send(ctx context.Context, msg Message, reliable bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg.Type == MessageThinking {
		msg.Content = strings.TrimSpace(msg.Content)
		if msg.Content == "" {
			return true
		}
		if s.lastThinking {
			msg.Content = "\n\n" + msg.Content
		}
	}
	if reliable {
		select {
		case s.messages <- msg:
		case <-ctx.Done():
			return false
		}
	} else if !trySend(s.messages, msg) {
		return false
	}
	switch msg.Type {
	case MessageThinking, MessageText, MessageToolUse, MessageToolResult, MessageError:
		s.lastThinking = msg.Type == MessageThinking
	}
	return true
}

// Headless stdout carries response text, but not the thinking text available
// in agy's native transcript. Read only settled records after the USER_INPUT
// index announced by this process; a resumed conversation also holds history.
type antigravityThinkingProgress struct {
	logPath string
	send    func(Message) bool

	mu       sync.Mutex
	scope    antigravityThinkingScope
	injected map[int]bool

	reading antigravityThinkingScope
	path    string
	offset  int64
	seen    map[int]bool
}

func (p *antigravityThinkingProgress) setScope(conversationID string, start *int, injected map[int]bool) {
	scope := antigravityThinkingScope{conversationID: conversationID}
	if conversationID != "" && start != nil && *start >= 0 {
		scope.turnStart, scope.valid = *start, true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// Stdout only adds indices to the turn. Share a fresh snapshot when the
	// set grows so the reader never races with a stdout update.
	if p.scope != scope || len(p.injected) != len(injected) {
		p.scope = scope
		p.injected = maps.Clone(injected)
	}
}

func (p *antigravityThinkingProgress) follow(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.poll()
		case <-ctx.Done():
			p.poll()
			return
		}
	}
}

// poll is called by the follower only. Incomplete trailing lines stay unread
// until the writer appends their newline. Missing files never fail execution.
func (p *antigravityThinkingProgress) poll() {
	p.mu.Lock()
	scope, injected := p.scope, p.injected
	p.mu.Unlock()
	if !scope.valid {
		return
	}
	if p.reading != scope {
		p.reading, p.path, p.offset = scope, "", 0
		p.seen = make(map[int]bool)
	}
	if p.path == "" {
		root := readAntigravityAppDataDir(p.logPath)
		if root == "" {
			return
		}
		p.path = filepath.Join(root, "brain", scope.conversationID, ".system_generated", "logs", "transcript.jsonl")
	}
	f, err := os.Open(p.path)
	if err != nil {
		return
	}
	defer f.Close()
	if stat, err := f.Stat(); err == nil && stat.Size() < p.offset {
		p.offset = 0
	}
	if _, err := f.Seek(p.offset, io.SeekStart); err != nil {
		return
	}
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var record struct {
			Type      string `json:"type"`
			Source    string `json:"source"`
			Status    string `json:"status"`
			StepIndex *int   `json:"step_index"`
			Thinking  string `json:"thinking"`
		}
		if json.Unmarshal(line, &record) == nil && record.StepIndex != nil && *record.StepIndex > scope.turnStart {
			index := *record.StepIndex
			if record.Type == "USER_INPUT" && !injected[index] {
				// The transcript can get ahead of stdout. Leave this record
				// unread until stdout recognizes it as an in-turn injection.
				return
			}
			if record.Type == "PLANNER_RESPONSE" && record.Source == "MODEL" && record.Status == "DONE" &&
				!p.seen[index] && strings.TrimSpace(record.Thinking) != "" {
				if !p.send(Message{Type: MessageThinking, Content: record.Thinking}) {
					return // Retry this record on the next poll if the consumer fell behind.
				}
				p.seen[index] = true
			}
		}
		p.offset += int64(len(line))
	}
}
