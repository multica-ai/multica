// Package statusnotify delivers workspace-scoped issue status changes to an
// IM channel.
//
// This is a different shape from the per-agent conversational bindings in
// internal/integrations/{lark,slack,...}: those bind one bot to one agent and
// carry a two-way chat, keyed on a chat session. Issue-driven work has no chat
// session — lark/outbound.go returns early on exactly that — so a status feed
// cannot reuse that path.
//
// What it does instead: subscribe to EventIssueUpdated once, filter to the
// status categories a workspace asked for, and render through the existing
// channel.Channel contract. Adapters already declare CapRichCard / CapText, so
// a platform that cannot render a card degrades to text without the core
// growing a branch per platform — the same "register a factory, never edit the
// core" rule the channel registry states.
package statusnotify

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// Change is one status transition worth notifying about, extracted from an
// EventIssueUpdated payload.
type Change struct {
	IssueID     string
	WorkspaceID string
	Identifier  string
	Title       string
	Description string
	// Status is the raw status key, which may be a custom one.
	Status string
	// Category is the canonical status this transition lands in. Filtering
	// happens on this rather than Status: a custom status carries an opaque
	// key ("in_review_2") but always reports one of the known categories.
	Category string
	// PrevStatus is empty when the payload did not carry one.
	PrevStatus string
}

// Subscription is a workspace's standing request to be told about status
// changes.
//
// Deliberately NOT keyed on agent_id. That is the one field whose absence
// makes this a workspace feed rather than another per-agent binding, and it is
// why this cannot be expressed with the existing channel_installation row,
// which is unique on (workspace_id, agent_id, channel_type).
type Subscription struct {
	WorkspaceID string
	// Target is the platform's conversation identifier (a Feishu chat id, a
	// Slack channel id, ...). Its format is the adapter's business.
	Target string
	// Categories lists the status categories to notify on. Empty means the
	// caller wants nothing, not everything: a subscription that silently
	// broadcast every transition would be a surprising default for a feed
	// whose whole point is to carry only what a human must act on.
	Categories []string
}

// wants reports whether this subscription asked about category.
func (s Subscription) wants(category string) bool {
	for _, want := range s.Categories {
		if strings.EqualFold(want, category) {
			return true
		}
	}
	return false
}

// Store supplies the subscriptions for a workspace. Kept as an interface so
// the listener can be tested without a database, and so a deployment can hold
// subscriptions wherever it already keeps this kind of config.
type Store interface {
	SubscriptionsFor(ctx context.Context, workspaceID string) ([]Subscription, error)
}

// Renderer turns a Change into an outbound message for a specific channel.
//
// It receives the target Channel's capabilities rather than the Channel
// itself: rendering needs to know whether a card is available, and nothing
// else. Passing the whole Channel would let a renderer start sending.
type Renderer interface {
	Render(change Change, issueURL string, caps channel.Capability) (channel.OutboundMessage, error)
}

// Notifier fans one status change out to a workspace's subscribed channels.
type Notifier struct {
	Store    Store
	Renderer Renderer
	// Channels resolves the channel to deliver on. Returning an error for a
	// target is not fatal to the other targets.
	Channels func(ctx context.Context, sub Subscription) (channel.Channel, error)
	// AppURL is the deployment's web address, used to build the issue link
	// carried in the notification.
	AppURL string
	// WorkspaceSlug resolves the slug that appears in issue URLs. Nil falls
	// back to omitting the link rather than emitting a broken one.
	WorkspaceSlug func(ctx context.Context, workspaceID string) (string, error)
	Logger        *slog.Logger
}

// Notify delivers change to every subscription that asked for its category.
//
// One failing target does not stop the others: these are independent
// deliveries, and letting a single unreachable channel suppress the rest would
// turn a partial outage into a total one.
func (n *Notifier) Notify(ctx context.Context, change Change) error {
	subs, err := n.Store.SubscriptionsFor(ctx, change.WorkspaceID)
	if err != nil {
		return fmt.Errorf("statusnotify: load subscriptions: %w", err)
	}

	issueURL := n.issueURL(ctx, change)

	var failures int
	for _, sub := range subs {
		if !sub.wants(change.Category) {
			continue
		}
		if err := n.deliver(ctx, sub, change, issueURL); err != nil {
			failures++
			n.log().Warn("statusnotify: delivery failed",
				"workspace_id", change.WorkspaceID,
				"issue", change.Identifier,
				"category", change.Category,
				"error", err,
			)
		}
	}
	if failures > 0 {
		return fmt.Errorf("statusnotify: %d of %d deliveries failed", failures, len(subs))
	}
	return nil
}

func (n *Notifier) deliver(ctx context.Context, sub Subscription, change Change, issueURL string) error {
	ch, err := n.Channels(ctx, sub)
	if err != nil {
		return fmt.Errorf("resolve channel: %w", err)
	}
	out, err := n.Renderer.Render(change, issueURL, ch.Capabilities())
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}
	out.ChatID = sub.Target
	if _, err := ch.Send(ctx, out); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}

// issueURL builds the link back to the issue, or returns "" when the slug
// cannot be resolved. An empty link is a renderer's problem to handle; a
// wrong link is worse than none.
func (n *Notifier) issueURL(ctx context.Context, change Change) string {
	if n.AppURL == "" || n.WorkspaceSlug == nil || change.Identifier == "" {
		return ""
	}
	slug, err := n.WorkspaceSlug(ctx, change.WorkspaceID)
	if err != nil || slug == "" {
		n.log().Debug("statusnotify: workspace slug unavailable; omitting issue link",
			"workspace_id", change.WorkspaceID, "error", err)
		return ""
	}
	return fmt.Sprintf("%s/%s/issues/%s", strings.TrimRight(n.AppURL, "/"), slug, change.Identifier)
}

func (n *Notifier) log() *slog.Logger {
	if n.Logger != nil {
		return n.Logger
	}
	return slog.Default()
}
