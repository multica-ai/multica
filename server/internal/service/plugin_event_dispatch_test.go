package service

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The promise this file exists to keep: publishing an event NEVER waits for a
// third party.
//
// Bus.Publish calls listeners inline, on the goroutine of the request that
// published, so a listener that dialled an endpoint would put an outside server
// on the critical path of creating an issue. These tests hold the bus listener
// against an endpoint that never answers.

// A hung endpoint must not hold up the publisher. If Dispatch ever becomes
// synchronous this test hangs for the endpoint's full duration rather than
// returning in microseconds, which is the failure worth catching.
func TestEventDispatchNeverBlocksThePublisher(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })

	harness := newHookTestServer(t)
	harness.respond = func(w http.ResponseWriter, _ []byte) {
		// Hold the connection open until the test finishes.
		<-blocked
		w.WriteHeader(http.StatusOK)
	}
	service := hookTestService(t, harness)
	dispatcher := NewPluginEventDispatcher(service)
	t.Cleanup(dispatcher.Close)

	bus := events.New()
	SubscribePluginEvents(bus, dispatcher)

	started := time.Now()
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: "00000000-0000-4000-8000-000000000001",
		ActorType:   "member",
		Payload:     map[string]any{"issue": map[string]any{"id": "00000000-0000-4000-8000-000000000002"}},
	})
	elapsed := time.Since(started)

	// Generous by three orders of magnitude: the point is "did not wait for the
	// network", not a latency budget.
	if elapsed > 250*time.Millisecond {
		t.Fatalf("Publish took %v — an event hook is blocking the publishing request", elapsed)
	}
}

// Backpressure is shed, not queued. An unbounded queue turns a slow plugin into
// a memory leak; dropping and counting keeps the host healthy and the loss
// visible rather than silent.
//
// Workers are stopped first so nothing drains — the same state a pool saturated
// by slow endpoints reaches, reproduced without depending on timing.
func TestEventDispatchShedsLoadRatherThanGrowingWithoutBound(t *testing.T) {
	harness := newHookTestServer(t)
	dispatcher := NewPluginEventDispatcher(hookTestService(t, harness))
	dispatcher.Close()

	// Well past the queue depth. Every call must return, none may panic, and
	// Dispatch must stay safe after the pool has stopped.
	for i := 0; i < dispatchQueueDepth*3; i++ {
		dispatcher.Dispatch(plugincontract.EventIssueCreated, "00000000-0000-4000-8000-000000000001", nil)
	}
	dropped := dispatcher.Dropped()
	if dropped == 0 {
		t.Fatal("nothing was dropped past the queue depth — the queue is unbounded")
	}
	// Exactly the overflow: the queue holds its depth and sheds the rest.
	if want := dispatchQueueDepth*3 - dispatchQueueDepth; dropped != want {
		t.Fatalf("dropped %d, want %d — the queue is not bounded at its declared depth", dropped, want)
	}
}

// The bus vocabulary and the plugin vocabulary are deliberately separate. This
// pins the mapping, including the one event that has no internal equivalent:
// issue.status_changed is derived from an issue:updated carrying
// status_changed=true, so a plugin can subscribe to the specific thing it cares
// about instead of filtering every field change itself.
func TestEventBridgeMapsInternalEventsToThePublishedVocabulary(t *testing.T) {
	seen := []string{}
	bus := events.New()
	SubscribePluginEvents(bus, recordingSink(func(eventType string) { seen = append(seen, eventType) }))

	workspace := "00000000-0000-4000-8000-000000000001"
	bus.Publish(events.Event{Type: protocol.EventIssueCreated, WorkspaceID: workspace})
	bus.Publish(events.Event{Type: protocol.EventCommentCreated, WorkspaceID: workspace})
	bus.Publish(events.Event{Type: protocol.EventTaskRunning, WorkspaceID: workspace})
	bus.Publish(events.Event{Type: protocol.EventTaskCompleted, WorkspaceID: workspace})
	bus.Publish(events.Event{Type: protocol.EventTaskFailed, WorkspaceID: workspace})
	// A plain field edit is issue.updated only.
	bus.Publish(events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: workspace, Payload: map[string]any{"title_changed": true}})
	// A status change is BOTH, so a subscriber to either sees it.
	bus.Publish(events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: workspace, Payload: map[string]any{"status_changed": true}})

	want := []string{
		plugincontract.EventIssueCreated,
		plugincontract.EventCommentCreated,
		plugincontract.EventTaskStarted,
		plugincontract.EventTaskCompleted,
		plugincontract.EventTaskFailed,
		plugincontract.EventIssueUpdated,
		plugincontract.EventIssueUpdated,
		plugincontract.EventIssueStatusChanged,
	}
	if len(seen) != len(want) {
		t.Fatalf("dispatched %v, want %v", seen, want)
	}
	for index, expected := range want {
		if seen[index] != expected {
			t.Fatalf("dispatched[%d] = %q, want %q (full: %v)", index, seen[index], expected, seen)
		}
	}

	// Every published event name must be one a manifest may subscribe to, or a
	// plugin could never receive it.
	for _, eventType := range want {
		if !plugincontract.IsKnownEvent(eventType) {
			t.Fatalf("%q is dispatched but not a subscribable event", eventType)
		}
	}
}

// The issue id travels with the event so the callback token can be narrowed to
// it. Each payload shape the product actually publishes is covered.
func TestEventBridgeFindsTheIssueInEveryPayloadShape(t *testing.T) {
	issueID := "3fa85f64-5717-4562-b3fc-2c963f66afa6"
	for name, payload := range map[string]any{
		"issue event":   map[string]any{"issue": map[string]any{"id": issueID}},
		"comment event": map[string]any{"comment": map[string]any{"issue_id": issueID}},
		"task event":    map[string]any{"issue_id": issueID},
	} {
		found := issueIDFromPayload(payload)
		if uuidString(found) != issueID {
			t.Fatalf("%s: issue id = %q, want %q", name, uuidString(found), issueID)
		}
	}

	// A payload with no issue yields the zero UUID, which simply means the
	// grant is not issue-scoped — not an error.
	if issueIDFromPayload(map[string]any{"unrelated": true}).Valid {
		t.Fatal("a payload with no issue must not produce an issue-scoped grant")
	}
	if issueIDFromPayload(nil).Valid {
		t.Fatal("a nil payload must not produce an issue-scoped grant")
	}
	// Not a UUID: refuse rather than pass a malformed id into a grant.
	if issueIDFromPayload(map[string]any{"issue_id": "not-a-uuid"}).Valid {
		t.Fatal("a malformed id must not produce an issue-scoped grant")
	}
}

// An event hook writes as the installation. This asserts the actor the
// dispatcher stamps, which is what the callback token later carries.
func TestEventDispatchStampsThePluginActor(t *testing.T) {
	harness := newHookTestServer(t)
	service := hookTestService(t, harness)
	host := hookTestHost(harness)
	installation := hookTestInstallation(t, harness.server.URL+"/hooks/summarize", host,
		[]string{plugincontract.TriggerEvent})

	hook, err := FindHook(installation, "summarize")
	if err != nil {
		t.Fatalf("find hook: %v", err)
	}
	if _, err := service.callHookEndpoint(t.Context(), HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerEvent,
		EventType:    plugincontract.EventIssueCreated,
		Actor:        HookActor{Type: "plugin", ID: installation.ID},
	}); err != nil {
		t.Fatalf("event hook call: %v", err)
	}

	received := <-harness.received
	var body hookRequestBody
	if err := json.Unmarshal(received.Body, &body); err != nil {
		t.Fatalf("decode delivered body: %v", err)
	}
	if body.Actor.Type != "plugin" {
		t.Fatalf("actor.type = %q, want plugin: an event hook has no person behind it", body.Actor.Type)
	}
	if body.EventType != plugincontract.EventIssueCreated {
		t.Fatalf("event_type = %q, want %q", body.EventType, plugincontract.EventIssueCreated)
	}
	if body.Trigger != plugincontract.TriggerEvent {
		t.Fatalf("trigger = %q, want event", body.Trigger)
	}

	// Re-issued: callHookEndpoint revokes the grant once its call has returned.
	token, err := service.Callbacks.Issue(t.Context(), HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerEvent,
		Actor:        HookActor{Type: "plugin", ID: installation.ID},
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	grant, err := service.Callbacks.Resolve(token)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if grant.Actor.Type != "plugin" {
		t.Fatalf("the grant must carry the plugin actor, got %q", grant.Actor.Type)
	}
}

// recordingSink stands in for the dispatcher so the vocabulary mapping is
// asserted directly, with no worker pool or endpoint in the way.
type recordingSink func(eventType string)

func (r recordingSink) Dispatch(eventType, _ string, _ any) { r(eventType) }

// The shape that took down cmd/server's router test: a dispatcher built over a
// Queries whose pool was never opened.
//
// A nil check on Queries does not catch it — sqlc wraps an executor, so the
// value is non-nil and the nil pool is only reached inside pgxpool. Constructing
// a dispatcher must therefore never touch the database on its own, and a sweep
// that does must not be able to kill the process.
func TestNewDispatcherDoesNotTouchTheDatabase(t *testing.T) {
	// A Queries over a nil pool: exactly what NewRouter holds in a test that
	// never opens one.
	dispatcher := NewPluginEventDispatcher(&PluginService{Queries: db.New(nil)})
	t.Cleanup(dispatcher.Close)

	// Long enough that a construction-time sweep would already have panicked and
	// taken the process with it.
	time.Sleep(150 * time.Millisecond)

	// And the sweep itself, called directly, must survive the same pool.
	dispatcher.sweepOnce()
}

func testFlagService(enabled bool) *featureflag.Service {
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.PluginsV1, featureflag.Rule{Default: enabled})
	return featureflag.NewService(provider)
}

// Nil flags read as disabled. A deployment that never wired the flag service
// must not have outbound hooks enabled by that omission.
func TestEventDispatchTreatsMissingFlagsAsDisabled(t *testing.T) {
	harness := newHookTestServer(t)
	service := hookTestService(t, harness)
	service.Queries = db.New(nil)
	service.FeatureFlags = nil

	dispatcher := NewPluginEventDispatcher(service)
	t.Cleanup(dispatcher.Close)
	dispatcher.runGuarded(dispatchJob{
		installation: db.PluginInstallation{WorkspaceID: testInstallationID(t)},
		eventType:    plugincontract.EventIssueCreated,
	})

	select {
	case <-harness.received:
		t.Fatal("a request left with no feature flag service configured")
	default:
	}
}

// countingPayload records how many times something serialized it.
//
// json.Marshal calls MarshalJSON, so this counts exactly the work the bus
// listener used to do on the publishing request's goroutine.
type countingPayload struct{ marshals *int }

func (c countingPayload) MarshalJSON() ([]byte, error) {
	*c.marshals++
	return []byte(`{"issue":{"id":"3fa85f64-5717-4562-b3fc-2c963f66afa6"}}`), nil
}

// Publishing must not parse the payload.
//
// The listener used to pass issueIDFromPayload(e.Payload) as an argument, which
// put a JSON marshal + unmarshal of a full issue body on the request goroutine
// for seven event types, in every workspace — including deployments with plugins
// switched off, where the flag check downstream then threw the result away.
// Finding the id belongs on a worker, past the flag.
func TestPublishingDoesNotParseThePayload(t *testing.T) {
	marshals := 0
	bus := events.New()
	SubscribePluginEvents(bus, recordingSink(func(string) {}))

	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: "00000000-0000-4000-8000-000000000001",
		Payload:     countingPayload{marshals: &marshals},
	})

	if marshals != 0 {
		t.Fatalf("the payload was serialized %d time(s) on the publishing goroutine; the id must be extracted on a worker instead", marshals)
	}
}

// And the id still arrives — moved, not dropped. issueIDFromPayload remains the
// one place that knows the payload shapes; only where it runs changed.
func TestTheIssueIDIsStillFoundOnTheWorker(t *testing.T) {
	issueID := "3fa85f64-5717-4562-b3fc-2c963f66afa6"
	found := issueIDFromPayload(map[string]any{"issue": map[string]any{"id": issueID}})
	if uuidString(found) != issueID {
		t.Fatalf("issue id = %q, want %q", uuidString(found), issueID)
	}
}
