package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestOpenCodeClockExport(t *testing.T) {
	const now = int64(1_800_000_000_000)
	for _, tc := range []struct {
		name             string
		updated, created int64
		wantSkew         bool
	}{
		{"past", now - 1000, now - 1000, false},
		{"same millisecond", now - 1000, now, true},
		{"thirty seconds ahead", now - 1000, now + 30_000, true},
		{"six hours ahead", now - 1000, now + 21_600_000, true},
		{"session metadata ahead", now + 30_000, now - 1000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"info":{"id":"ses-test","time":{"updated":%d}},"messages":[{"info":{"time":{"created":%d}},"parts":[{"text":"sensitive body with time.created=9999999999999"}]}]}`, tc.updated, tc.created)
			err := checkOpenCodeClockExport(strings.NewReader(raw), "ses-test", now)
			if errors.Is(err, ErrOpenCodeClockSkew) != tc.wantSkew || (!tc.wantSkew && err != nil) {
				t.Fatalf("check = %v, want skew=%v", err, tc.wantSkew)
			}
			native := fmt.Sprintf(`{"info":{"id":"ses-test","time":{"updated":%d}},"messages":[{"id":"msg-test","type":"user","time":{"created":%d},"text":"sensitive body"}]}`, tc.updated, tc.created)
			err = checkOpenCodeClockExport(strings.NewReader(native), "ses-test", now)
			if errors.Is(err, ErrOpenCodeClockSkew) != tc.wantSkew || (!tc.wantSkew && err != nil) {
				t.Fatalf("native export check = %v, want skew=%v", err, tc.wantSkew)
			}
		})
	}
}

func TestOpenCodeClockExportRejectsUnverifiedData(t *testing.T) {
	for _, raw := range []string{
		`{}`, `null`, `{"messages":[]}`,
		`{"info":{"id":"other","time":{"updated":1}},"messages":[]}`,
		`{"info":{"id":"ses-test","time":{"updated":1}},"messages":[{}]}`,
		`{"info":{"id":"ses-test","time":{"updated":1}},"messages":[{"info":{"time":{"created":"secret"}}}]}`,
		`{"info":{"id":"ses-test","time":{"updated":1}},"messages":[{"time":{"created":"secret"}}]}`,
		`{"info":{"id":"ses-test","time":{"updated":1}},"messages":[{"time":{"created":1},"info":{"time":{"created":1}}}]}`,
		`{"info":{"id":"ses-test","time":{"updated":1}},"messages":[]} trailing secret`,
	} {
		err := checkOpenCodeClockExport(strings.NewReader(raw), "ses-test", 1000)
		if err == nil || errors.Is(err, ErrOpenCodeClockSkew) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("malformed export must be unverified without echoing content: %v", err)
		}
	}
}
