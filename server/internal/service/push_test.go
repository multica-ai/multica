package service

import "testing"

// CEREBRO-PATCH(vapid-mailto-fix): regression test for JEH-563 — keep the
// normalisation idempotent against the webpush-go auto-prepend behaviour.
func TestNormalizeVAPIDSubject(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty defaults to bare email", "", "noreply@multica.ai"},
		{"mailto: prefix stripped", "mailto:foo@bar.com", "foo@bar.com"},
		{"plain email passes through", "foo@bar.com", "foo@bar.com"},
		{"https url preserved", "https://multica.ai", "https://multica.ai"},
		{"https url with path preserved", "https://multica.ai/about", "https://multica.ai/about"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeVAPIDSubject(tc.in)
			if got != tc.want {
				t.Errorf("normalizeVAPIDSubject(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
