package handler

import "testing"

func TestNormalizeProjectWorkdirPolicy(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{name: "empty defaults to none", input: "", want: "none", wantOK: true},
		{name: "none", input: "none", want: "none", wantOK: true},
		{name: "advisory", input: "advisory", want: "advisory", wantOK: true},
		{name: "unknown", input: "enforced", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeProjectWorkdirPolicy(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("policy = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeProjectCanonicalWorkdir(t *testing.T) {
	path := "  /Users/me/project  "
	got, err := normalizeProjectCanonicalWorkdir(&path)
	if err != nil {
		t.Fatalf("normalize path: %v", err)
	}
	if !got.Valid || got.String != "/Users/me/project" {
		t.Fatalf("canonical path = %#v", got)
	}

	empty := "   "
	got, err = normalizeProjectCanonicalWorkdir(&empty)
	if err != nil {
		t.Fatalf("normalize empty path: %v", err)
	}
	if got.Valid {
		t.Fatalf("empty path should become NULL, got %#v", got)
	}

	bad := "/tmp/project\nnext"
	if _, err = normalizeProjectCanonicalWorkdir(&bad); err == nil {
		t.Fatal("expected control character path to be rejected")
	}
}
