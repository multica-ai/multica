package note

import (
	"net/http/httptest"
	"testing"
)

func TestValidVisibility(t *testing.T) {
	for _, v := range []string{"private", "shared", "workspace"} {
		if !validVisibility[v] {
			t.Errorf("expected %q to be a valid visibility", v)
		}
	}
	for _, v := range []string{"", "public", "everyone", "PRIVATE"} {
		if validVisibility[v] {
			t.Errorf("expected %q to be rejected", v)
		}
	}
}

func TestPageParams(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantLimit  int32
		wantOffset int32
	}{
		{"defaults", "", defaultListLimit, 0},
		{"explicit", "?limit=10&offset=5", 10, 5},
		{"limit clamped to max", "?limit=9999", maxListLimit, 0},
		{"zero limit falls back to default", "?limit=0", defaultListLimit, 0},
		{"negative limit falls back to default", "?limit=-3", defaultListLimit, 0},
		{"garbage ignored", "?limit=abc&offset=xyz", defaultListLimit, 0},
		{"negative offset ignored", "?offset=-7", defaultListLimit, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/notes"+tc.query, nil)
			limit, offset := pageParams(r)
			if limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", limit, tc.wantLimit)
			}
			if offset != tc.wantOffset {
				t.Errorf("offset = %d, want %d", offset, tc.wantOffset)
			}
		})
	}
}

func TestRequireUserID(t *testing.T) {
	t.Run("missing header is rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/notes", nil)
		if _, ok := requireUserID(w, r); ok {
			t.Fatal("expected requireUserID to fail without X-User-ID")
		}
		if w.Code != 401 {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})
	t.Run("header is accepted", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/notes", nil)
		r.Header.Set("X-User-ID", "u-1")
		id, ok := requireUserID(w, r)
		if !ok || id != "u-1" {
			t.Errorf("requireUserID = %q, %v; want u-1, true", id, ok)
		}
	})
}
