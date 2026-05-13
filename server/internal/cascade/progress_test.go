package cascade

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProgress_Validate(t *testing.T) {
	merged := time.Date(2026, 5, 13, 11, 30, 0, 0, time.UTC)

	tests := []struct {
		name    string
		p       Progress
		wantErr bool
	}{
		{
			name:    "valid full",
			p:       Progress{TotalPRs: 8, CurrentStep: 3, LastPRNumber: 1234, LastPRMergedAt: &merged, LastEventType: "pr_merged"},
			wantErr: false,
		},
		{
			name:    "valid minimal (post-atomic-init, no PR yet)",
			p:       Progress{TotalPRs: 1, CurrentStep: 1},
			wantErr: false,
		},
		{
			name:    "total_prs zero",
			p:       Progress{TotalPRs: 0, CurrentStep: 1},
			wantErr: true,
		},
		{
			name:    "current_step zero (must be 1-indexed)",
			p:       Progress{TotalPRs: 5, CurrentStep: 0},
			wantErr: true,
		},
		{
			name:    "current_step exceeds total",
			p:       Progress{TotalPRs: 3, CurrentStep: 4},
			wantErr: true,
		},
		{
			name:    "current_step negative",
			p:       Progress{TotalPRs: 5, CurrentStep: -1},
			wantErr: true,
		},
		{
			name:    "last_pr_number negative (corruption)",
			p:       Progress{TotalPRs: 5, CurrentStep: 2, LastPRNumber: -1},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, ErrProgressInvalid) {
				t.Fatalf("Validate() err %v should wrap ErrProgressInvalid", err)
			}
		})
	}
}

func TestProgress_IsComplete(t *testing.T) {
	merged := time.Date(2026, 5, 13, 11, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		p    Progress
		want bool
	}{
		{
			name: "all PRs merged and final timestamp set",
			p:    Progress{TotalPRs: 3, CurrentStep: 3, LastPRNumber: 42, LastPRMergedAt: &merged},
			want: true,
		},
		{
			name: "current at total but no merge timestamp (PR open, awaiting CI)",
			p:    Progress{TotalPRs: 3, CurrentStep: 3, LastPRNumber: 42},
			want: false,
		},
		{
			name: "mid-cascade",
			p:    Progress{TotalPRs: 6, CurrentStep: 3, LastPRMergedAt: &merged},
			want: false,
		},
		{
			name: "zero-state",
			p:    Progress{},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.IsComplete(); got != tc.want {
				t.Errorf("IsComplete() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProgress_MarshalRoundTrip(t *testing.T) {
	merged := time.Date(2026, 5, 13, 11, 30, 0, 0, time.UTC)
	in := Progress{
		TotalPRs:       8,
		CurrentStep:    3,
		LastPRNumber:   1234,
		LastPRMergedAt: &merged,
		LastEventType:  "pr_merged",
	}

	raw, err := in.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := UnmarshalProgress(raw)
	if err != nil {
		t.Fatalf("UnmarshalProgress: %v", err)
	}

	if got.TotalPRs != in.TotalPRs || got.CurrentStep != in.CurrentStep || got.LastPRNumber != in.LastPRNumber || got.LastEventType != in.LastEventType {
		t.Fatalf("round-trip mismatch:\n  in:  %+v\n  got: %+v", in, got)
	}
	if got.LastPRMergedAt == nil || !got.LastPRMergedAt.Equal(merged) {
		t.Fatalf("LastPRMergedAt mismatch: got %v want %v", got.LastPRMergedAt, merged)
	}
}

func TestProgress_MarshalRejectsInvalid(t *testing.T) {
	// total_prs=0 → Validate fails before json.Marshal sees the value
	_, err := Progress{TotalPRs: 0, CurrentStep: 1}.Marshal()
	if err == nil {
		t.Fatal("Marshal should reject invalid Progress, got nil err")
	}
	if !errors.Is(err, ErrProgressInvalid) {
		t.Fatalf("err %v should wrap ErrProgressInvalid", err)
	}
}

func TestUnmarshalProgress_EmptyOrNil(t *testing.T) {
	// NULL cascade_progress → empty bytes from pgx → zero Progress, no error.
	for _, name := range []string{"nil", "empty"} {
		t.Run(name, func(t *testing.T) {
			var raw []byte
			if name == "empty" {
				raw = []byte{}
			}
			p, err := UnmarshalProgress(raw)
			if err != nil {
				t.Fatalf("err = %v, want nil for NULL JSONB", err)
			}
			if (p != Progress{}) {
				t.Fatalf("got %+v, want zero", p)
			}
		})
	}
}

func TestUnmarshalProgress_MalformedJSON(t *testing.T) {
	_, err := UnmarshalProgress([]byte(`{not json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode progress") {
		t.Fatalf("err %q should mention decode progress", err)
	}
}

func TestUnmarshalProgress_ValidJSONButInvariantViolation(t *testing.T) {
	// JSON parses fine, but current_step > total_prs.
	raw := []byte(`{"total_prs":3,"current_step":4}`)
	_, err := UnmarshalProgress(raw)
	if err == nil {
		t.Fatal("expected error for invariant violation")
	}
	if !errors.Is(err, ErrProgressInvalid) {
		t.Fatalf("err %v should wrap ErrProgressInvalid", err)
	}
}

func TestProgress_JSONFieldNames(t *testing.T) {
	// The frontend dashboard (PR7) reads /api/cascades, which surfaces
	// these fields directly. Snake-case stability matters — pin the
	// keys against a literal so an accidental field rename in Go does
	// not silently break the API contract.
	merged := time.Date(2026, 5, 13, 11, 30, 0, 0, time.UTC)
	p := Progress{TotalPRs: 8, CurrentStep: 3, LastPRNumber: 1234, LastPRMergedAt: &merged, LastEventType: "pr_merged"}

	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(raw)

	required := []string{
		`"total_prs":8`,
		`"current_step":3`,
		`"last_pr_number":1234`,
		`"last_pr_merged_at":`,
		`"last_event_type":"pr_merged"`,
	}
	for _, want := range required {
		if !strings.Contains(s, want) {
			t.Errorf("JSON missing %q\n  full: %s", want, s)
		}
	}
}
