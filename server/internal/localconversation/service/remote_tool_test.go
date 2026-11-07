package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

type remoteToolFixture struct {
	Messages []any `json:"messages"`
	Events   []struct {
		TurnID    string         `json:"turn_id"`
		ItemID    string         `json:"item_id"`
		Type      string         `json:"type"`
		Final     bool           `json:"final"`
		CreatedAt time.Time      `json:"created_at"`
		Payload   map[string]any `json:"payload"`
	} `json:"events"`
}

func readRemoteToolFixture(t *testing.T) remoteToolFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/remote-tool-events.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture remoteToolFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// The GUI regression consumes these same events; derive them from raw cloud
// results without input so a hand-written completion cannot hide input loss.
func TestRemoteToolEventFixture(t *testing.T) {
	fixture := readRemoteToolFixture(t)
	for _, after := range []int64{0, 2, 4} {
		events, last := remoteEvents("turn", fixture.Messages, after, fixture.Events[0].CreatedAt)
		want := fixture.Events[after:]
		if last != 4 || len(events) != len(want) {
			t.Fatalf("after %d: last=%d events=%d; want last=4 events=%d", after, last, len(events), len(want))
		}
		for i, event := range events {
			var payload map[string]any
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if event.TurnID != want[i].TurnID || event.ItemID != want[i].ItemID || event.Type != want[i].Type || event.Final != want[i].Final || !event.CreatedAt.Equal(want[i].CreatedAt) || !reflect.DeepEqual(payload, want[i].Payload) {
				t.Errorf("after %d, event %d = %+v; want %+v", after, i, event, want[i])
			}
		}
	}
}

func TestRemoteToolResultMatchesOnlyItsOwnCall(t *testing.T) {
	input := map[string]any{"command": "original"}
	for _, tc := range []struct {
		name   string
		result map[string]any
		want   any
	}{
		{"absent input", map[string]any{"call_id": "call"}, input},
		{"null input", map[string]any{"call_id": "call", "input": nil}, input},
		{"explicit input", map[string]any{"call_id": "call", "input": map[string]any{}}, map[string]any{}},
		{"unknown call", map[string]any{"call_id": "other"}, nil},
		{"no call id", map[string]any{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := map[string]any{"seq": float64(1), "type": "tool_use", "call_id": "call", "tool": "Bash", "input": input}
			tc.result["seq"], tc.result["type"], tc.result["output"] = float64(2), "tool_result", "done"
			events, _ := remoteEvents("turn", []any{start, tc.result}, 1, time.Now())
			if len(events) != 1 {
				t.Fatalf("events = %+v; want one new result", events)
			}
			var payload map[string]any
			if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(payload["input"], tc.want) {
				t.Errorf("input = %+v; want %+v", payload["input"], tc.want)
			}
			if tc.result["call_id"] == "call" && payload["tool"] != "Bash" {
				t.Errorf("tool = %v; want Bash from the matching call", payload["tool"])
			}
		})
	}
}

func TestRemoteToolInputsSurviveResultsAndRestart(t *testing.T) {
	fixture := readRemoteToolFixture(t)
	for _, split := range []bool{false, true} {
		name := "same poll"
		if split {
			name = "result after restart"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			cloud := &fakeCloud{bodies: map[string]any{}, taskMessages: fixture.Messages, replied: true}
			if split {
				cloud.taskMessages, cloud.replied = fixture.Messages[:2], false
			}
			path := filepath.Join(t.TempDir(), "c.sqlite")
			opts := Options{Cloud: func(store.Scope, string) contextmcp.Cloud { return cloud }}
			f := newServiceFixtureWith(t, path, opts)
			f.create(t, "conv-1", "claude")
			result, err := f.svc.SendToAgents(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "inspect"}, []RemoteAgent{{ID: "agent-1", Name: "Inspector"}})
			if err != nil {
				t.Fatal(err)
			}
			turnID := result.TurnIDs[0]
			svc := f.svc
			if split {
				eventually(t, "both tool starts to persist", func() bool {
					turn, err := svc.Store().Turn(ctx, "conv-1", turnID)
					remote := remoteOf(turn)
					return err == nil && remote != nil && remote.MirroredSeq == 2
				})
				if err := svc.Close(ctx); err != nil {
					t.Fatal(err)
				}
				cloud.mu.Lock()
				cloud.taskMessages, cloud.replied = fixture.Messages, true
				cloud.mu.Unlock()
				svc = newServiceFixtureWith(t, path, opts).svc
			}
			eventually(t, "the cloud turn to finish", func() bool {
				turn, err := svc.Store().Turn(ctx, "conv-1", turnID)
				return err == nil && turn.Status == "completed"
			})
			checkSnapshot := func(svc *Service) {
				t.Helper()
				snapshot, err := svc.Store().Snapshot(ctx, "conv-1", 10)
				if err != nil {
					t.Fatal(err)
				}
				found := 0
				for _, turn := range snapshot.Turns {
					for _, event := range turn.Events {
						for _, want := range fixture.Events {
							if !want.Final || event.ItemID.String != strings.Replace(want.ItemID, "remote-turn-", "remote-"+turnID+"-", 1) {
								continue
							}
							found++
							var payload map[string]any
							if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
								t.Fatal(err)
							}
							if event.Type != "item.completed" || event.Final != 1 || !reflect.DeepEqual(payload, want.Payload) {
								t.Errorf("final tool %s = %s; want %+v", event.ItemID.String, event.Payload, want.Payload)
							}
						}
					}
				}
				if found != 2 {
					t.Errorf("snapshot has %d final tools; want 2", found)
				}
			}
			checkSnapshot(svc)
			if err := svc.Close(ctx); err != nil {
				t.Fatal(err)
			}
			checkSnapshot(newServiceFixtureWith(t, path, opts).svc)
			if sends := cloud.count("POST /api/chat/sessions/chat-1/messages"); sends != 1 {
				t.Errorf("restarts sent the message again (%d sends)", sends)
			}
		})
	}
}
