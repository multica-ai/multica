package statusnotify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type stubStore struct {
	subs []Subscription
	err  error
}

func (s stubStore) SubscriptionsFor(context.Context, string) ([]Subscription, error) {
	return s.subs, s.err
}

type stubChannel struct {
	caps     channel.Capability
	sent     []channel.OutboundMessage
	sendErr  error
	sendCall int
}

func (c *stubChannel) Type() channel.Type               { return channel.TypeFeishu }
func (c *stubChannel) Connect(context.Context) error    { return nil }
func (c *stubChannel) Disconnect(context.Context) error { return nil }
func (c *stubChannel) Capabilities() channel.Capability { return c.caps }
func (c *stubChannel) Send(_ context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	c.sendCall++
	if c.sendErr != nil {
		return channel.SendResult{}, c.sendErr
	}
	c.sent = append(c.sent, out)
	return channel.SendResult{MessageID: "m1"}, nil
}

func newNotifier(store Store, ch channel.Channel) *Notifier {
	return &Notifier{
		Store:    store,
		Renderer: TextRenderer{Labels: map[string]string{CategoryBlocked: "Blocked"}},
		Channels: func(context.Context, Subscription) (channel.Channel, error) { return ch, nil },
		AppURL:   "https://multica.example.com",
		WorkspaceSlug: func(context.Context, string) (string, error) {
			return "demo", nil
		},
	}
}

func blockedChange() Change {
	return Change{
		IssueID:     "id-1",
		WorkspaceID: "ws-1",
		Identifier:  "ENG-42",
		Title:       "Login fails behind proxy",
		Status:      CategoryBlocked,
		Category:    CategoryBlocked,
		PrevStatus:  "in_progress",
	}
}

func TestNotifyDeliversToSubscribersOfThatCategory(t *testing.T) {
	ch := &stubChannel{caps: channel.CapText}
	n := newNotifier(stubStore{subs: []Subscription{
		{WorkspaceID: "ws-1", Target: "oc_team", Categories: []string{CategoryBlocked}},
	}}, ch)

	if err := n.Notify(context.Background(), blockedChange()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(ch.sent) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(ch.sent))
	}
	if ch.sent[0].ChatID != "oc_team" {
		t.Errorf("ChatID = %q, want oc_team", ch.sent[0].ChatID)
	}
	if !strings.Contains(ch.sent[0].Text, "ENG-42") {
		t.Errorf("text missing identifier: %q", ch.sent[0].Text)
	}
}

func TestNotifySkipsCategoriesNobodySubscribedTo(t *testing.T) {
	ch := &stubChannel{caps: channel.CapText}
	n := newNotifier(stubStore{subs: []Subscription{
		{WorkspaceID: "ws-1", Target: "oc_team", Categories: []string{CategoryInReview}},
	}}, ch)

	if err := n.Notify(context.Background(), blockedChange()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if ch.sendCall != 0 {
		t.Fatalf("expected no delivery for an unsubscribed category, got %d", ch.sendCall)
	}
}

// An empty Categories list must mean "nothing", not "everything". A
// subscription that broadcast every transition by default would flood a
// channel whose purpose is to carry only what a human must act on.
func TestEmptyCategoriesDeliversNothing(t *testing.T) {
	ch := &stubChannel{caps: channel.CapText}
	n := newNotifier(stubStore{subs: []Subscription{
		{WorkspaceID: "ws-1", Target: "oc_team"},
	}}, ch)

	if err := n.Notify(context.Background(), blockedChange()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if ch.sendCall != 0 {
		t.Fatalf("empty Categories delivered %d messages, want 0", ch.sendCall)
	}
}

// One unreachable target must not suppress the others: these are independent
// deliveries, and failing them together turns a partial outage into a total one.
func TestOneFailingTargetDoesNotStopTheRest(t *testing.T) {
	good := &stubChannel{caps: channel.CapText}
	bad := &stubChannel{caps: channel.CapText, sendErr: errors.New("rate limited")}

	n := &Notifier{
		Store: stubStore{subs: []Subscription{
			{WorkspaceID: "ws-1", Target: "bad", Categories: []string{CategoryBlocked}},
			{WorkspaceID: "ws-1", Target: "good", Categories: []string{CategoryBlocked}},
		}},
		Renderer: TextRenderer{},
		Channels: func(_ context.Context, sub Subscription) (channel.Channel, error) {
			if sub.Target == "bad" {
				return bad, nil
			}
			return good, nil
		},
	}

	err := n.Notify(context.Background(), blockedChange())
	if err == nil {
		t.Fatal("expected an error reporting the partial failure")
	}
	if len(good.sent) != 1 {
		t.Fatalf("healthy target got %d deliveries, want 1", len(good.sent))
	}
}

func TestIssueURLIsBuiltFromSlugAndIdentifier(t *testing.T) {
	ch := &stubChannel{caps: channel.CapText}
	n := newNotifier(stubStore{subs: []Subscription{
		{WorkspaceID: "ws-1", Target: "oc_team", Categories: []string{CategoryBlocked}},
	}}, ch)

	if err := n.Notify(context.Background(), blockedChange()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	want := "https://multica.example.com/demo/issues/ENG-42"
	if !strings.Contains(ch.sent[0].Text, want) {
		t.Errorf("text missing issue URL %q: %q", want, ch.sent[0].Text)
	}
}

// A wrong link is worse than none: when the slug cannot be resolved the
// notification still goes out, without a link that would 404.
func TestUnresolvableSlugOmitsTheLinkButStillDelivers(t *testing.T) {
	ch := &stubChannel{caps: channel.CapText}
	n := newNotifier(stubStore{subs: []Subscription{
		{WorkspaceID: "ws-1", Target: "oc_team", Categories: []string{CategoryBlocked}},
	}}, ch)
	n.WorkspaceSlug = func(context.Context, string) (string, error) {
		return "", errors.New("workspace gone")
	}

	if err := n.Notify(context.Background(), blockedChange()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(ch.sent) != 1 {
		t.Fatalf("expected delivery despite missing slug, got %d", len(ch.sent))
	}
	if strings.Contains(ch.sent[0].Text, "multica.example.com") {
		t.Errorf("expected no link, got %q", ch.sent[0].Text)
	}
}

func TestExtractChangeRequiresAStatusTransition(t *testing.T) {
	e := events.Event{
		Type:        "issue:updated",
		WorkspaceID: "ws-1",
		Payload: map[string]any{
			"status_changed": false,
			"issue":          handler.IssueResponse{ID: "id-1", Identifier: "ENG-1"},
		},
	}
	if _, ok := ExtractChange(e); ok {
		t.Fatal("a non-status update must not produce a Change")
	}
}

// StatusCategory carries omitempty and its doc comment tells consumers to fall
// back to Status. Without the fallback a custom status would match no
// subscription and the notification would vanish silently.
func TestExtractChangeFallsBackToStatusWhenCategoryIsBlank(t *testing.T) {
	e := events.Event{
		Type:        "issue:updated",
		WorkspaceID: "ws-1",
		Payload: map[string]any{
			"status_changed": true,
			"issue": handler.IssueResponse{
				ID: "id-1", WorkspaceID: "ws-1", Identifier: "ENG-1",
				Title: "t", Status: CategoryBlocked,
			},
		},
	}
	change, ok := ExtractChange(e)
	if !ok {
		t.Fatal("expected a Change")
	}
	if change.Category != CategoryBlocked {
		t.Errorf("Category = %q, want %q", change.Category, CategoryBlocked)
	}
}

func TestRenderClipsOnRuneBoundaries(t *testing.T) {
	change := blockedChange()
	change.Title = strings.Repeat("登", 200)

	out, err := TextRenderer{}.Render(change, "", channel.CapText)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Byte truncation would split a multi-byte rune and emit U+FFFD.
	if strings.ContainsRune(out.Text, '\uFFFD') {
		t.Error("clip split a multi-byte rune")
	}
	if !strings.Contains(out.Text, "…") {
		t.Error("expected an ellipsis marking the clip")
	}
}
