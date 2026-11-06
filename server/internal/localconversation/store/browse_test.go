package store

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestSessionPageOrdersFractionalSendTimes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		a, b  time.Duration
		want  []string
		empty bool
	}{
		{"whole second and tenths", 0, 100 * time.Millisecond, []string{"b", "a"}, false},
		{"different fractional widths", 10 * time.Millisecond, 100 * time.Millisecond, []string{"b", "a"}, false},
		{"whole second and nanosecond", 0, time.Nanosecond, []string{"b", "a"}, false},
		{"within a millisecond", 100 * time.Nanosecond, 900 * time.Nanosecond, []string{"b", "a"}, false},
		{"across seconds", 999999999 * time.Nanosecond, time.Second, []string{"b", "a"}, false},
		{"equal instants use ID", 100 * time.Millisecond, 100 * time.Millisecond, []string{"a", "b"}, false},
		{"empty history uses creation time", 0, 100 * time.Millisecond, []string{"b", "a"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, _ := openTestStore(t)
			ctx := context.Background()
			scopeID, err := s.EnsureScope(ctx, Scope{APIOrigin: "https://api.multica.ai", UserID: "user", WorkspaceID: "workspace"})
			if err != nil {
				t.Fatal(err)
			}
			w := s.NewWriter(WriterOptions{})
			defer w.Close()
			for id, delta := range map[string]time.Duration{"a": tc.a, "b": tc.b} {
				sent := testTime.Add(delta)
				if _, err := s.CreateConversation(ctx, NewConversation{ID: id, ScopeID: scopeID, Provider: "claude", CreatedAt: sent}); err != nil {
					t.Fatal(err)
				}
				if tc.empty {
					continue
				}
				_, err := w.Submit(ctx, Batch{ConversationID: id, StartTurn: &NewTurn{ID: id + "-turn", StartedAt: sent},
					Messages: []NewMessage{{ID: id + "-user", TurnID: id + "-turn", Role: "user", CreatedAt: sent}},
					Summary:  &SummaryPatch{UpdatedAt: sent}})
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, size := range []int{1, 2} {
				var got []string
				cursor := SessionBrowsePosition{}
				for {
					rows, _, more, err := s.SessionPage(ctx, scopeID, SessionBrowseFilter{}, cursor, size)
					if err != nil {
						t.Fatal(err)
					}
					for _, row := range rows {
						got = append(got, row.ID)
						cursor = SessionBrowsePosition{ID: row.ID, Pinned: row.Pinned, SortAt: row.LastTurnSentAt}
						if cursor.SortAt == "" {
							cursor.SortAt = row.CreatedAt
						}
					}
					if !more {
						break
					}
					if len(got) > 2 || len(rows) == 0 {
						t.Fatalf("cursor did not advance: %v", got)
					}
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("page size %d returned %v, want %v", size, got, tc.want)
				}
			}
		})
	}
}
