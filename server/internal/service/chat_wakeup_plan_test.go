package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestChatWakeupPlan(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	cond := json.RawMessage(`{"type":"run","task_id":"x"}`)
	at := now.Add(3 * time.Hour)
	past := now.Add(-time.Minute)
	farExpiry := now.Add(8 * 24 * time.Hour)

	for _, tc := range []struct {
		name    string
		in      ChatWakeupInput
		wantErr string
		check   func(t *testing.T, p chatWakeupPlan)
	}{
		{name: "nothing set", in: ChatWakeupInput{}, wantErr: "exactly one"},
		{name: "condition and reminder", in: ChatWakeupInput{Condition: cond, AfterSeconds: 60}, wantErr: "exactly one"},
		{
			name: "condition waits a day by default and checks now",
			in:   ChatWakeupInput{Condition: cond},
			check: func(t *testing.T, p chatWakeupPlan) {
				if p.kind != "condition" || !p.nextEval.Equal(now) || !p.expiresAt.Equal(now.Add(24*time.Hour)) {
					t.Fatalf("plan = %+v", p)
				}
			},
		},
		{
			name: "reminder ends when it fires",
			in:   ChatWakeupInput{At: &at},
			check: func(t *testing.T, p chatWakeupPlan) {
				if p.kind != "at" || !p.fireAt.Time.Equal(at) || !p.expiresAt.Equal(at) || !p.nextEval.Equal(at) {
					t.Fatalf("plan = %+v", p)
				}
			},
		},
		{name: "reminder rejects a deadline", in: ChatWakeupInput{AfterSeconds: 60, ExpiresInSeconds: 3600}, wantErr: "ends when it fires"},
		{name: "reminder in the past", in: ChatWakeupInput{At: &past}, wantErr: "future"},
		{name: "reminder too far out", in: ChatWakeupInput{AfterSeconds: int64(31 * 24 * 3600)}, wantErr: "Autopilot"},
		{name: "check too often", in: ChatWakeupInput{IntervalSeconds: 300}, wantErr: "10 minutes"},
		{name: "check too many times", in: ChatWakeupInput{IntervalSeconds: 600}, wantErr: "at most 100"},
		{
			name: "check starts one interval out",
			in:   ChatWakeupInput{IntervalSeconds: 1800, ExpiresInSeconds: 6 * 3600},
			check: func(t *testing.T, p chatWakeupPlan) {
				if p.kind != "check" || !p.nextEval.Equal(now.Add(30*time.Minute)) || !p.expiresAt.Equal(now.Add(6*time.Hour)) {
					t.Fatalf("plan = %+v", p)
				}
			},
		},
		{name: "wait longer than a week", in: ChatWakeupInput{Condition: cond, ExpiresAt: &farExpiry}, wantErr: "7 days"},
		{name: "deadline in the past", in: ChatWakeupInput{Condition: cond, ExpiresAt: &past}, wantErr: "future"},
		{name: "both deadlines", in: ChatWakeupInput{Condition: cond, ExpiresAt: &at, ExpiresInSeconds: 60}, wantErr: "not both"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := tc.in.plan(now)
			if tc.wantErr != "" {
				if !errors.Is(err, ErrChatWakeupInput) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want input error containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, p)
		})
	}
}
