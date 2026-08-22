package channelnotify

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type dispatcherResolverStub struct {
	mu    sync.Mutex
	calls []channel.Type
}

func (r *dispatcherResolverStub) Resolve(_ context.Context, _ Notification, channelType channel.Type) (Target, bool, error) {
	r.mu.Lock()
	r.calls = append(r.calls, channelType)
	r.mu.Unlock()
	return Target{ChannelType: channelType, ChannelUserID: "recipient"}, true, nil
}

func (r *dispatcherResolverStub) callCount(channelType channel.Type) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, called := range r.calls {
		if called == channelType {
			count++
		}
	}
	return count
}

type senderFunc func(context.Context, Target, Notification) error

func (f senderFunc) SendInbox(ctx context.Context, target Target, notification Notification) error {
	return f(ctx, target, notification)
}

func newDispatcherForTest(resolver notificationResolver, registry *Registry, mutate func(*Config)) *Dispatcher {
	config := Config{
		Enabled:       []channel.Type{channel.TypeFeishu},
		QueueSize:     8,
		Workers:       1,
		DeliveryLimit: time.Second,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if mutate != nil {
		mutate(&config)
	}
	return NewDispatcher(resolver, registry, config)
}

func closeDispatcher(t *testing.T, dispatcher *Dispatcher) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := dispatcher.Close(ctx); err != nil {
		t.Fatalf("close dispatcher: %v", err)
	}
}

func TestDispatcherReturnsFromSynchronousPublishBeforeSendCompletes(t *testing.T) {
	resolver := &dispatcherResolverStub{}
	registry := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	registry.Register(channel.TypeFeishu, senderFunc(func(context.Context, Target, Notification) error {
		close(started)
		<-release
		return nil
	}))
	dispatcher := newDispatcherForTest(resolver, registry, nil)
	bus := events.New()
	dispatcher.Register(bus)

	published := make(chan struct{})
	go func() {
		bus.Publish(dispatcherInboxEvent("11111111-1111-4111-8111-111111111111"))
		close(published)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sender did not start")
	}
	select {
	case <-published:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("synchronous event publication waited for Channel delivery")
	}
	close(release)
	closeDispatcher(t, dispatcher)
}

func TestDispatcherDeliversOncePerEnabledRegisteredChannel(t *testing.T) {
	resolver := &dispatcherResolverStub{}
	registry := NewRegistry()
	var mu sync.Mutex
	counts := map[channel.Type]int{}
	for _, channelType := range []channel.Type{channel.TypeFeishu, "slack"} {
		channelType := channelType
		registry.Register(channelType, senderFunc(func(context.Context, Target, Notification) error {
			mu.Lock()
			counts[channelType]++
			mu.Unlock()
			return nil
		}))
	}
	dispatcher := newDispatcherForTest(resolver, registry, func(config *Config) {
		config.Enabled = []channel.Type{channel.TypeFeishu, "slack"}
	})
	bus := events.New()
	dispatcher.Register(bus)
	bus.Publish(dispatcherInboxEvent("11111111-1111-4111-8111-111111111112"))
	closeDispatcher(t, dispatcher)

	mu.Lock()
	defer mu.Unlock()
	if counts[channel.TypeFeishu] != 1 || counts["slack"] != 1 {
		t.Fatalf("delivery counts = %v, want one per Channel", counts)
	}
}

func TestDispatcherSkipsUnregisteredConfiguredChannel(t *testing.T) {
	resolver := &dispatcherResolverStub{}
	registry := NewRegistry()
	registry.Register(channel.TypeFeishu, senderFunc(func(context.Context, Target, Notification) error { return nil }))
	dispatcher := newDispatcherForTest(resolver, registry, func(config *Config) {
		config.Enabled = []channel.Type{channel.TypeFeishu, "slack"}
	})
	bus := events.New()
	dispatcher.Register(bus)
	bus.Publish(dispatcherInboxEvent("11111111-1111-4111-8111-111111111113"))
	closeDispatcher(t, dispatcher)

	if got := resolver.callCount("slack"); got != 0 {
		t.Fatalf("unregistered slack resolver calls = %d, want 0", got)
	}
}

func TestDispatcherContinuesAfterOneChannelFails(t *testing.T) {
	resolver := &dispatcherResolverStub{}
	registry := NewRegistry()
	registry.Register(channel.TypeFeishu, senderFunc(func(context.Context, Target, Notification) error {
		return errors.New("feishu unavailable")
	}))
	slackDelivered := make(chan struct{}, 1)
	registry.Register("slack", senderFunc(func(context.Context, Target, Notification) error {
		slackDelivered <- struct{}{}
		return nil
	}))
	dispatcher := newDispatcherForTest(resolver, registry, func(config *Config) {
		config.Enabled = []channel.Type{channel.TypeFeishu, "slack"}
	})
	bus := events.New()
	dispatcher.Register(bus)
	bus.Publish(dispatcherInboxEvent("11111111-1111-4111-8111-111111111114"))
	closeDispatcher(t, dispatcher)

	select {
	case <-slackDelivered:
	default:
		t.Fatal("slack delivery did not run after Feishu failure")
	}
}

func TestDispatcherDoesNotLogSenderErrorDetails(t *testing.T) {
	const (
		openIDMarker = "ou_sensitive_recipient"
		titleMarker  = "sensitive Inbox title"
		bodyMarker   = "sensitive Inbox body"
		secretMarker = "plaintext-app-secret"
	)

	resolver := &dispatcherResolverStub{}
	registry := NewRegistry()
	registry.Register(channel.TypeFeishu, senderFunc(func(context.Context, Target, Notification) error {
		return errors.New(strings.Join([]string{openIDMarker, titleMarker, bodyMarker, secretMarker}, " "))
	}))
	var logs bytes.Buffer
	dispatcher := newDispatcherForTest(resolver, registry, func(config *Config) {
		config.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	})
	bus := events.New()
	dispatcher.Register(bus)
	bus.Publish(dispatcherInboxEvent("11111111-1111-4111-8111-111111111120"))
	closeDispatcher(t, dispatcher)

	output := logs.String()
	for _, marker := range []string{openIDMarker, titleMarker, bodyMarker, secretMarker} {
		if strings.Contains(output, marker) {
			t.Errorf("sender failure log contains sensitive marker %q: %s", marker, output)
		}
	}
	for _, field := range []string{
		"channel_type=feishu",
		"inbox_item_id=11111111-1111-4111-8111-111111111120",
		"issue_id=44444444-4444-4444-8444-444444444444",
		"delivery_status=failed",
		"failure_category=sender_error",
	} {
		if !strings.Contains(output, field) {
			t.Errorf("sender failure log missing %q: %s", field, output)
		}
	}
}

func TestDispatcherDropsWithoutBlockingWhenQueueIsFull(t *testing.T) {
	resolver := &dispatcherResolverStub{}
	registry := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	sends := 0
	registry.Register(channel.TypeFeishu, senderFunc(func(context.Context, Target, Notification) error {
		mu.Lock()
		sends++
		mu.Unlock()
		once.Do(func() { close(started) })
		<-release
		return nil
	}))
	dispatcher := newDispatcherForTest(resolver, registry, func(config *Config) {
		config.QueueSize = 1
	})
	bus := events.New()
	dispatcher.Register(bus)

	bus.Publish(dispatcherInboxEvent("11111111-1111-4111-8111-111111111115"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first delivery did not start")
	}
	bus.Publish(dispatcherInboxEvent("11111111-1111-4111-8111-111111111116"))
	before := time.Now()
	bus.Publish(dispatcherInboxEvent("11111111-1111-4111-8111-111111111117"))
	if elapsed := time.Since(before); elapsed > 100*time.Millisecond {
		t.Fatalf("full queue blocked publisher for %s", elapsed)
	}
	close(release)
	closeDispatcher(t, dispatcher)

	mu.Lock()
	defer mu.Unlock()
	if sends != 2 {
		t.Fatalf("send count = %d, want 2 accepted jobs", sends)
	}
}

func TestDispatcherCloseDrainsAcceptedJobs(t *testing.T) {
	resolver := &dispatcherResolverStub{}
	registry := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	registry.Register(channel.TypeFeishu, senderFunc(func(context.Context, Target, Notification) error {
		close(started)
		<-release
		return nil
	}))
	dispatcher := newDispatcherForTest(resolver, registry, nil)
	bus := events.New()
	dispatcher.Register(bus)
	bus.Publish(dispatcherInboxEvent("11111111-1111-4111-8111-111111111118"))
	<-started

	closed := make(chan error, 1)
	go func() { closed <- dispatcher.Close(context.Background()) }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned before accepted delivery completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not return after accepted delivery completed")
	}
}

func TestDispatcherPublishAfterCloseIsSafe(t *testing.T) {
	resolver := &dispatcherResolverStub{}
	registry := NewRegistry()
	registry.Register(channel.TypeFeishu, senderFunc(func(context.Context, Target, Notification) error {
		t.Fatal("sender called after dispatcher closed")
		return nil
	}))
	dispatcher := newDispatcherForTest(resolver, registry, nil)
	bus := events.New()
	dispatcher.Register(bus)
	closeDispatcher(t, dispatcher)

	for i := 0; i < 100; i++ {
		bus.Publish(dispatcherInboxEvent("11111111-1111-4111-8111-111111111119"))
	}
	if got := resolver.callCount(channel.TypeFeishu); got != 0 {
		t.Fatalf("resolver calls after close = %d, want 0", got)
	}
}

func dispatcherInboxEvent(itemID string) events.Event {
	return inboxEvent(map[string]any{
		"id":             itemID,
		"workspace_id":   "22222222-2222-4222-8222-222222222222",
		"recipient_type": "member",
		"recipient_id":   "33333333-3333-4333-8333-333333333333",
		"issue_id":       "44444444-4444-4444-8444-444444444444",
		"type":           "mentioned",
		"severity":       "attention",
		"title":          "Please review",
	})
}
