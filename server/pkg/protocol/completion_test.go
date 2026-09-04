// @vitest-environment node — N/A (Go). Pure parsing, no DB: this is the
// canonical layer for the v1 envelope's boundary matrix. Handler and service
// tests exercise wiring, not these cases again.
package protocol

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCompletionResultValidV1(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantSummary string
		wantIDs     []string
	}{
		{"summary and artifacts", `{"version":1,"summary":"done","artifact_ids":["a","b"]}`, "done", []string{"a", "b"}},
		// Empty summary is legal: a tool-only chat turn produces no prose.
		{"empty summary is legal", `{"version":1,"summary":""}`, "", []string{}},
		{"artifact_ids missing", `{"version":1,"summary":"s"}`, "s", []string{}},
		{"artifact_ids null", `{"version":1,"summary":"s","artifact_ids":null}`, "s", []string{}},
		{"artifact_ids empty", `{"version":1,"summary":"s","artifact_ids":[]}`, "s", []string{}},
		// Unknown fields are ignored, not rejected: a newer producer may add
		// fields, and refusing them would turn an additive change into an outage.
		{"unknown field ignored", `{"version":1,"summary":"s","future":42}`, "s", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCompletionResult([]byte(tc.raw))
			if err != nil {
				t.Fatalf("ParseCompletionResult(%s) error = %v, want nil", tc.raw, err)
			}
			if got.Summary != tc.wantSummary {
				t.Errorf("Summary = %q, want %q", got.Summary, tc.wantSummary)
			}
			if got.Version != CompletionResultVersion1 {
				t.Errorf("Version = %d, want %d", got.Version, CompletionResultVersion1)
			}
			if got.ArtifactIDs == nil {
				t.Fatal("ArtifactIDs is nil; missing/null/[] must all normalize to a non-nil slice")
			}
			if len(got.ArtifactIDs) != len(tc.wantIDs) {
				t.Fatalf("ArtifactIDs = %v, want %v", got.ArtifactIDs, tc.wantIDs)
			}
			for i := range tc.wantIDs {
				if got.ArtifactIDs[i] != tc.wantIDs[i] {
					t.Errorf("ArtifactIDs[%d] = %q, want %q", i, got.ArtifactIDs[i], tc.wantIDs[i])
				}
			}
		})
	}
}

func TestParseCompletionResultLegacyIsNotAnError(t *testing.T) {
	// No `version` key: a pre-v1 daemon or a historical row. The caller must be
	// able to tell this apart from corruption, so it gets its own sentinel.
	for _, raw := range []string{`{"output":"hello"}`, `{}`, `{"pr_url":"https://x/1","output":"hi"}`} {
		_, err := ParseCompletionResult([]byte(raw))
		if !errors.Is(err, ErrLegacyCompletionResult) {
			t.Errorf("ParseCompletionResult(%s) error = %v, want ErrLegacyCompletionResult", raw, err)
		}
	}
}

func TestParseCompletionResultRejectsMalformedV1(t *testing.T) {
	// Every case here declares v1 and then breaks it. None may fall back to
	// legacy: a mis-parsed payload persisted under a v1 label is undetectable
	// downstream, which is the exact failure this strictness exists to prevent.
	tests := []struct {
		name string
		raw  string
	}{
		{"missing summary", `{"version":1}`},
		{"missing summary with artifacts", `{"version":1,"artifact_ids":["a"]}`},
		{"summary wrong type", `{"version":1,"summary":42}`},
		{"summary null", `{"version":1,"summary":null}`},
		{"unknown version", `{"version":2,"summary":"s"}`},
		{"version zero", `{"version":0,"summary":"s"}`},
		{"version not an integer", `{"version":"1","summary":"s"}`},
		{"artifact_ids not an array", `{"version":1,"summary":"s","artifact_ids":"a"}`},
		{"artifact_ids of wrong element type", `{"version":1,"summary":"s","artifact_ids":[1,2]}`},
		{"artifact_ids with empty element", `{"version":1,"summary":"s","artifact_ids":[""]}`},
		{"artifact_ids with NUL", "{\"version\":1,\"summary\":\"s\",\"artifact_ids\":[\"a\\u0000b\"]}"},
		{"not an object", `["version"]`},
		{"not JSON", `{oops`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseCompletionResult([]byte(tc.raw))
			if err == nil {
				t.Fatalf("ParseCompletionResult(%s) error = nil, want a rejection", tc.raw)
			}
			if errors.Is(err, ErrLegacyCompletionResult) {
				t.Fatalf("ParseCompletionResult(%s) fell back to legacy; malformed v1 must fail loud", tc.raw)
			}
		})
	}
}

func TestParseCompletionResultRejectsOversizedArtifactID(t *testing.T) {
	raw := `{"version":1,"summary":"s","artifact_ids":["` + strings.Repeat("a", maxArtifactIDLen+1) + `"]}`
	if _, err := ParseCompletionResult([]byte(raw)); err == nil {
		t.Fatal("oversized artifact id was accepted, want rejection")
	}
}

func TestParseCompletionResultPreservesNULInSummary(t *testing.T) {
	// Summary is prose and is sanitized at the STORAGE boundary, not here:
	// this package must not depend on storage rules. Parsing must therefore
	// accept it rather than reject or silently rewrite it.
	got, err := ParseCompletionResult([]byte("{\"version\":1,\"summary\":\"a\\u0000b\"}"))
	if err != nil {
		t.Fatalf("error = %v, want nil (summary sanitizing belongs to the caller)", err)
	}
	if !strings.ContainsRune(got.Summary, 0) {
		t.Error("parser stripped the NUL from summary; that is the storage layer's job")
	}
}

func TestReadStoredResultLegacyRow(t *testing.T) {
	// A row written before v1 existed must still read back as canonical v1.
	got, ok := ReadStoredResult([]byte(`{"output":"legacy answer","work_dir":"/Users/a/p","session_id":"ses_1"}`))
	if !ok {
		t.Fatal("ok = false, want true: a legacy row is readable, not corrupt")
	}
	if got.Summary != "legacy answer" {
		t.Errorf("Summary = %q, want %q", got.Summary, "legacy answer")
	}
	if got.Version != CompletionResultVersion1 {
		t.Errorf("Version = %d, want normalization to v1", got.Version)
	}
	if got.ArtifactIDs == nil {
		t.Error("ArtifactIDs is nil, want an empty slice")
	}
}

func TestReadStoredResultV1Row(t *testing.T) {
	got, ok := ReadStoredResult([]byte(`{"version":1,"summary":"v1 answer","artifact_ids":["x"]}`))
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got.Summary != "v1 answer" || len(got.ArtifactIDs) != 1 {
		t.Errorf("got %+v, want summary=%q with 1 artifact", got, "v1 answer")
	}
}

func TestReadStoredResultDegradesBadRow(t *testing.T) {
	// The run is over; the only options are degrade or lie. A malformed v1 row
	// must NOT be re-read under legacy rules, and must not be echoed back out.
	for _, raw := range []string{
		`{"version":1}`,
		`{"version":9,"summary":"s"}`,
		`{"version":1,"summary":42}`,
		`not json`,
		``,
	} {
		if _, ok := ReadStoredResult([]byte(raw)); ok {
			t.Errorf("ReadStoredResult(%q) ok = true, want degradation to false", raw)
		}
	}
}

func TestReadStoredResultDropsTransportFields(t *testing.T) {
	// The regression that motivated the envelope: the old code marshalled the
	// whole request into result, so session_id and absolute paths reached every
	// UI caller. Parsing to a typed struct is what structurally prevents that.
	got, ok := ReadStoredResult([]byte(`{"version":1,"summary":"s","session_id":"ses_leak","work_dir":"/Users/alice/p"}`))
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got.Summary != "s" {
		t.Errorf("Summary = %q, want %q", got.Summary, "s")
	}
	// CompletionResultV1 has no field to hold them, so this is structural
	// rather than a filter someone can forget to apply.
}

func TestNewLegacyCompletionResultIsTotal(t *testing.T) {
	got := NewLegacyCompletionResult("")
	if got.Version != CompletionResultVersion1 || got.Summary != "" || got.ArtifactIDs == nil {
		t.Errorf("got %+v, want v1 with empty summary and non-nil artifact ids", got)
	}
}
