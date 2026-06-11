package agentvault

import "testing"

func TestParseProxyCredential(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
		ok     bool
	}{
		{"empty", "", "", false},
		{"no scheme", "abc", "", false},
		{"bearer", "Bearer av_agt_xyz", "av_agt_xyz", true},
		{"bearer case-insensitive", "bearer tok", "tok", true},
		{"bearer blank", "Bearer ", "", false},
		// base64("av_agt_xyz:multica")
		{"basic user", "Basic YXZfYWd0X3h5ejptdWx0aWNh", "av_agt_xyz", true},
		{"basic garbage", "Basic !!!notb64", "", false},
		{"unknown scheme", "Digest x", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseProxyCredential(c.header)
			if ok != c.ok || got != c.want {
				t.Fatalf("ParseProxyCredential(%q) = (%q,%v), want (%q,%v)", c.header, got, ok, c.want, c.ok)
			}
		})
	}
}
