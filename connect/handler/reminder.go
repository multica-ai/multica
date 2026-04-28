package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/multica-ai/multica-connect/channel"
	"github.com/multica-ai/multica-connect/triage"
)

// Reminder is a scheduled one-time notification.
type Reminder struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"thread_id"`
	Channel   string    `json:"channel"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	DeliverAt time.Time `json:"deliver_at"`
	Delivered bool      `json:"delivered"`
	CreatedAt time.Time `json:"created_at"`
}

// ReminderStore persists reminders to a JSON file.
type ReminderStore struct {
	mu       sync.Mutex
	path     string
	items    []Reminder
	onChange func(r Reminder) // called when a reminder fires
}

func NewReminderStore(path string, onFire func(r Reminder)) *ReminderStore {
	s := &ReminderStore{path: path, onChange: onFire}
	s.load()
	return s
}

func (s *ReminderStore) Add(r Reminder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, r)
	s.save()
}

func (s *ReminderStore) CheckAndFire() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	changed := false
	for i := range s.items {
		if !s.items[i].Delivered && now.After(s.items[i].DeliverAt) {
			s.items[i].Delivered = true
			changed = true
			if s.onChange != nil {
				s.onChange(s.items[i])
			}
		}
	}
	if changed {
		s.save()
	}
}

// StartScheduler runs a ticker that checks for due reminders.
func (s *ReminderStore) StartScheduler(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.CheckAndFire()
		}
	}
}

func (s *ReminderStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &s.items)
}

func (s *ReminderStore) save() {
	data, _ := json.MarshalIndent(s.items, "", "  ")
	if err := os.WriteFile(s.path, data, 0644); err != nil {
		slog.Error("failed to save reminders", "error", err)
	}
}

// ReminderHandler processes reminder-classified messages.
type ReminderHandler struct {
	Store *ReminderStore
}

func (h *ReminderHandler) Handle(ctx context.Context, msg channel.Message, result *triage.Result) (*Response, error) {
	// Parse the remind_at time. Accept ISO 8601 or fall back to "soon".
	deliverAt := time.Now().Add(1 * time.Hour) // default: 1 hour from now
	if result.RemindAt != "" {
		if t, err := time.Parse(time.RFC3339, result.RemindAt); err == nil {
			deliverAt = t
		} else if t, err := time.Parse("2006-01-02T15:04:05", result.RemindAt); err == nil {
			deliverAt = t
		}
		// TODO: natural language time parsing (e.g. "friday at 10")
	}

	r := Reminder{
		ID:        fmt.Sprintf("r_%d", time.Now().UnixNano()),
		ThreadID:  msg.ThreadID,
		Channel:   msg.Source,
		Title:     result.Title,
		Body:      msg.Text,
		DeliverAt: deliverAt,
		CreatedAt: time.Now(),
	}
	h.Store.Add(r)

	timeStr := deliverAt.Format("Mon 2 Jan 15:04")
	text := fmt.Sprintf("⏰ Reminder sat: *%s*\n%s", result.Title, timeStr)

	return &Response{Text: text, ParseMode: "Markdown"}, nil
}
