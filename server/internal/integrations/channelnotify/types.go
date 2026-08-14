package channelnotify

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Notification is the durable Inbox content that a platform sender may
// deliver. The dispatcher receives it only after Inbox policy has created the
// item and published inbox:new.
type Notification struct {
	InboxItemID pgtype.UUID
	WorkspaceID pgtype.UUID
	RecipientID pgtype.UUID
	IssueID     pgtype.UUID
	Type        string
	Severity    string
	Title       string
	Body        string
	Details     json.RawMessage
}

// Target identifies the exact installation and external user that should
// receive one private Channel delivery.
type Target struct {
	InstallationID pgtype.UUID
	AgentID        pgtype.UUID
	ChannelType    channel.Type
	ChannelUserID  string
	WorkspaceSlug  string
}

// Sender delivers one Inbox notification through one platform installation.
// Platform credentials, API calls, and rendering remain behind this boundary.
type Sender interface {
	SendInbox(context.Context, Target, Notification) error
}

// Registry stores proactive Inbox senders independently from the existing
// Channel registry, whose Send method targets an already-known ChatID.
type Registry struct {
	mu      sync.RWMutex
	senders map[channel.Type]Sender
}

func NewRegistry() *Registry {
	return &Registry{senders: make(map[channel.Type]Sender)}
}

func (r *Registry) Register(t channel.Type, sender Sender) {
	if r == nil || t == "" || sender == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.senders[t] = sender
}

func (r *Registry) Lookup(t channel.Type) (Sender, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	sender, ok := r.senders[t]
	return sender, ok
}

func (r *Registry) Types() []channel.Type {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]channel.Type, 0, len(r.senders))
	for t := range r.senders {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}

// ParseNotification validates the stable inbox:new envelope and converts its
// map payload into a platform-neutral value. Normal ineligible events return
// ok=false instead of creating a second error path for Inbox policy.
func ParseNotification(e events.Event) (Notification, bool) {
	if e.Type != protocol.EventInboxNew {
		return Notification{}, false
	}
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return Notification{}, false
	}
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return Notification{}, false
	}
	if recipientType, _ := item["recipient_type"].(string); recipientType != "member" {
		return Notification{}, false
	}

	inboxItemID, ok := parseUUID(item["id"])
	if !ok {
		return Notification{}, false
	}
	workspaceID, ok := parseUUID(item["workspace_id"])
	if !ok {
		return Notification{}, false
	}
	recipientID, ok := parseUUID(item["recipient_id"])
	if !ok {
		return Notification{}, false
	}
	issueID, ok := parseUUID(item["issue_id"])
	if !ok {
		return Notification{}, false
	}

	n := Notification{
		InboxItemID: inboxItemID,
		WorkspaceID: workspaceID,
		RecipientID: recipientID,
		IssueID:     issueID,
		Type:        stringValue(item["type"]),
		Severity:    stringValue(item["severity"]),
		Title:       stringValue(item["title"]),
		Body:        nullableStringValue(item["body"]),
	}
	if details := item["details"]; details != nil {
		switch value := details.(type) {
		case json.RawMessage:
			n.Details = append(json.RawMessage(nil), value...)
		case []byte:
			n.Details = append(json.RawMessage(nil), value...)
		default:
			if encoded, err := json.Marshal(details); err == nil {
				n.Details = encoded
			}
		}
	}
	return n, true
}

func parseUUID(value any) (pgtype.UUID, bool) {
	switch value := value.(type) {
	case pgtype.UUID:
		return value, value.Valid
	case string:
		uuid, err := util.ParseUUID(value)
		return uuid, err == nil
	case *string:
		if value == nil {
			return pgtype.UUID{}, false
		}
		uuid, err := util.ParseUUID(*value)
		return uuid, err == nil
	default:
		return pgtype.UUID{}, false
	}
}

func stringValue(value any) string {
	if value, ok := value.(string); ok {
		return value
	}
	return ""
}

func nullableStringValue(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case *string:
		if value != nil {
			return *value
		}
	}
	return ""
}
