package savedfilters

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestNormalizeFilterState(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantOK  bool
		wantOut string
	}{
		{"empty defaults to object", "", true, "{}"},
		{"null defaults to object", "null", true, "{}"},
		{"valid object passes through", `{"projectFilters":["a"]}`, true, `{"projectFilters":["a"]}`},
		{"whitespace object", "  {}  ", true, "{}"},
		{"array rejected", `["a","b"]`, false, ""},
		{"scalar rejected", `42`, false, ""},
		{"garbage rejected", `{not json`, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			out, ok := normalizeFilterState(w, json.RawMessage(c.in))
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v (body: %s)", ok, c.wantOK, w.Body.String())
			}
			if ok && string(out) != c.wantOut {
				t.Fatalf("out = %q, want %q", string(out), c.wantOut)
			}
			if !ok && w.Code != 400 {
				t.Fatalf("expected 400 on reject, got %d", w.Code)
			}
		})
	}
}
