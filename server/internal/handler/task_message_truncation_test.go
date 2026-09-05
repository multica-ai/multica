package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The three states must survive every hop: request decode, batch parameter
// encoding, column storage and response serialisation. Collapsing "unknown"
// into "complete" anywhere in that chain reintroduces the silent truncation
// this metadata exists to expose, and the damage is permanent — the original
// size is not recoverable once the row is written.
func TestEncodeNullableBool(t *testing.T) {
	tr, fa := true, false
	tests := []struct {
		name string
		in   *bool
		want string
	}{
		{"unknown stays empty", nil, ""},
		{"explicit false is preserved", &fa, "false"},
		{"true", &tr, "true"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := encodeNullableBool(tc.in); got != tc.want {
				t.Errorf("encodeNullableBool = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEncodeNullableInt64(t *testing.T) {
	zero, big, neg := int64(0), int64(9007199254740993), int64(-1)
	tests := []struct {
		name string
		in   *int64
		want string
	}{
		{"unknown", nil, ""},
		{"zero is a real size", &zero, "0"},
		{"beyond float64 precision", &big, "9007199254740993"},
		// A byte count cannot be negative; a reporter sending one is confused,
		// and showing an absurd size is worse than showing none.
		{"negative is dropped", &neg, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := encodeNullableInt64(tc.in); got != tc.want {
				t.Errorf("encodeNullableInt64 = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTaskMessageToPayloadTruncationStates(t *testing.T) {
	tests := []struct {
		name          string
		truncated     pgtype.Bool
		originalBytes pgtype.Int8
		wantJSONHas   []string
		wantJSONLacks []string
	}{
		{
			// Rows written before these columns existed, and rows from a daemon
			// too old to report. The field must be absent, not false: a client
			// reading false would tell the user the output is complete.
			name:          "null columns omit the fields entirely",
			truncated:     pgtype.Bool{},
			originalBytes: pgtype.Int8{},
			wantJSONLacks: []string{"output_truncated", "output_original_bytes"},
		},
		{
			name:          "explicit false is carried",
			truncated:     pgtype.Bool{Bool: false, Valid: true},
			originalBytes: pgtype.Int8{Int64: 512, Valid: true},
			wantJSONHas:   []string{`"output_truncated":false`, `"output_original_bytes":512`},
		},
		{
			name:          "true with size",
			truncated:     pgtype.Bool{Bool: true, Valid: true},
			originalBytes: pgtype.Int8{Int64: 1048576, Valid: true},
			wantJSONHas:   []string{`"output_truncated":true`, `"output_original_bytes":1048576`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := taskMessageToPayload(db.TaskMessage{
				Seq:                 1,
				Type:                "tool_result",
				Output:              pgtype.Text{String: "some output", Valid: true},
				OutputTruncated:     tc.truncated,
				OutputOriginalBytes: tc.originalBytes,
			}, "task-1", "issue-1")

			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := string(encoded)
			for _, want := range tc.wantJSONHas {
				if !strings.Contains(got, want) {
					t.Errorf("payload JSON missing %s\ngot: %s", want, got)
				}
			}
			for _, unwanted := range tc.wantJSONLacks {
				if strings.Contains(got, unwanted) {
					t.Errorf("payload JSON should omit %s for an unknown column\ngot: %s", unwanted, got)
				}
			}
		})
	}
}

// A daemon that predates the fields sends neither. Decoding must leave them
// nil rather than defaulting to false, which is why the request struct uses
// pointers.
func TestTaskMessageRequestDecodesAbsentTruncationAsUnknown(t *testing.T) {
	var req TaskMessageRequest
	if err := json.Unmarshal([]byte(`{"seq":1,"type":"tool_result","output":"x"}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.OutputTruncated != nil {
		t.Error("absent output_truncated decoded to a value; unknown must stay nil")
	}
	if req.OutputOriginalBytes != nil {
		t.Error("absent output_original_bytes decoded to a value; unknown must stay nil")
	}

	var explicit TaskMessageRequest
	if err := json.Unmarshal([]byte(`{"seq":1,"type":"tool_result","output":"x","output_truncated":false,"output_original_bytes":0}`), &explicit); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if explicit.OutputTruncated == nil || *explicit.OutputTruncated {
		t.Error("explicit false did not survive decoding")
	}
	if explicit.OutputOriginalBytes == nil || *explicit.OutputOriginalBytes != 0 {
		t.Error("explicit zero did not survive decoding")
	}
}
