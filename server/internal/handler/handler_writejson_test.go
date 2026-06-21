package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWriteMeasuredJSONByteIdenticalToWriteJSON locks the load-bearing assumption
// behind the F2 claim-observability patch: swapping writeJSON for writeMeasuredJSON
// at the /tasks/claim response sites must not change a single byte on the wire.
//
// writeJSON encodes via json.NewEncoder(w).Encode (which appends a trailing
// newline); writeMeasuredJSON marshals via json.Marshal and appends the newline
// by hand. Both HTML-escape by default, so the emitted bytes must match for every
// input. This table-driven test fails closed if that invariant ever drifts, so the
// "no wire-behavior change" claim is provable rather than reasoned.
func TestWriteMeasuredJSONByteIdenticalToWriteJSON(t *testing.T) {
	type skill struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Files       map[string]string `json:"files"`
	}
	type claimResp struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Skills []skill  `json:"skills"`
		Args   []string `json:"args"`
	}

	cases := []struct {
		name string
		v    any
	}{
		{"nil", nil},
		{"no_task", map[string]any{"task": nil}},
		{"empty_map", map[string]any{}},
		{"empty_slice", []string{}},
		{"scalar_string", "plain string"},
		{"scalar_bool", true},
		{"numbers", map[string]any{"i": 42, "f": 3.5, "neg": -17, "big": 1234567890123}},
		{"html_escapable", map[string]any{"s": `a<b> & "c" 'd' <script>`}},
		{"ampersand_lt_gt", map[string]any{"raw": "1 < 2 && 3 > 2"}},
		{"unicode_and_separators", map[string]any{"s": "héllo 世界 🚀   "}},
		{"nested", map[string]any{"a": []any{1, "two", true, nil}, "b": map[string]any{"c": []int{1, 2, 3}}}},
		{"large_claim_with_skills", map[string]any{"task": claimResp{
			ID:   "11111111-2222-3333-4444-555555555555",
			Name: "agent <CC> & friends",
			Skills: []skill{
				{Name: "multica-working-on-issues", Description: "do work <safely> & well", Files: map[string]string{"SKILL.md": "# Title\n<b>x</b> & y"}},
				{Name: "multica-mentioning", Description: "ping people", Files: map[string]string{"SKILL.md": "line1\nline2"}},
			},
			Args: []string{"--flag", "a<b", "c&d"},
		}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recEnc := httptest.NewRecorder()
			writeJSON(recEnc, http.StatusOK, tc.v)

			recMeasured := httptest.NewRecorder()
			n, err := writeMeasuredJSON(recMeasured, http.StatusOK, tc.v)
			if err != nil {
				t.Fatalf("writeMeasuredJSON returned error: %v", err)
			}

			encBody := recEnc.Body.Bytes()
			measuredBody := recMeasured.Body.Bytes()

			if !bytes.Equal(encBody, measuredBody) {
				t.Fatalf("wire bytes differ:\n writeJSON         = %q\n writeMeasuredJSON = %q", encBody, measuredBody)
			}
			if n != len(measuredBody) {
				t.Fatalf("reported payload bytes %d != actual body length %d", n, len(measuredBody))
			}
			if n != len(encBody) {
				t.Fatalf("reported payload bytes %d != writeJSON body length %d", n, len(encBody))
			}
			if recEnc.Code != recMeasured.Code {
				t.Fatalf("status code differs: writeJSON=%d writeMeasuredJSON=%d", recEnc.Code, recMeasured.Code)
			}
			if got, want := recMeasured.Header().Get("Content-Type"), recEnc.Header().Get("Content-Type"); got != want {
				t.Fatalf("Content-Type differs: writeMeasuredJSON=%q writeJSON=%q", got, want)
			}
		})
	}

	// Sanity guard: both encoders HTML-escape by default, so a literal '<' rune must
	// not survive into the body (it is emitted as the escaped form). This documents
	// the escaping behaviour that makes the byte-identity comparison meaningful,
	// without depending on the escaped literal appearing in source.
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"x": "<&>"})
	if bytes.ContainsRune(rec.Body.Bytes(), '<') {
		t.Fatalf("expected '<' to be HTML-escaped out of the body, got %q", rec.Body.String())
	}
}
