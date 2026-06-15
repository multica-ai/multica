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

// CEREBRO-PATCH(push-device-split): TECH-3548 — mobile vs desktop classification
// drives independent push channels. Unknown/empty agents must fall back to
// desktop so a push is never silently dropped for an unrecognised device.
func TestDeviceClassFromUserAgent(t *testing.T) {
	cases := []struct {
		ua   string
		want string
	}{
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) Safari", DeviceClassMobile},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) Chrome Mobile", DeviceClassMobile},
		{"Mozilla/5.0 (iPad; CPU OS 17_4 like Mac OS X) Safari", DeviceClassMobile},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/124 Safari", DeviceClassDesktop},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/124", DeviceClassDesktop},
		{"", DeviceClassDesktop},
		{"some-unknown-agent", DeviceClassDesktop},
	}
	for _, c := range cases {
		if got := DeviceClassFromUserAgent(c.ua); got != c.want {
			t.Errorf("DeviceClassFromUserAgent(%q) = %q, want %q", c.ua, got, c.want)
		}
	}
}
