package webfetchpolicy

import "testing"

func TestHostMatchesRule(t *testing.T) {
	cases := []struct {
		host, rule string
		want       bool
	}{
		{"github.com", "github.com", true},
		{"api.github.com", "github.com", true},   // bare host matches subdomains
		{"github.com.evil.com", "github.com", false},
		{"notgithub.com", "github.com", false},   // must be exact or dot-bounded suffix
		{"api.github.com", "*.github.com", true}, // wildcard matches subdomains
		{"github.com", "*.github.com", false},    // wildcard does NOT match apex
		{"firtal.com", ".firtal.com", true},      // legacy leading-dot form still works
		{"shop.firtal.com", "firtal.com", true},
		{"docs.anthropic.com", "docs.anthropic.com", true},
		{"mydocs.anthropic.com", "docs.anthropic.com", false}, // stricter than legacy substring suffix
		{"", "github.com", false},
		{"github.com", "", false},
	}
	for _, c := range cases {
		if got := HostMatchesRule(c.host, c.rule); got != c.want {
			t.Errorf("HostMatchesRule(%q,%q)=%v want %v", c.host, c.rule, got, c.want)
		}
	}
}

func TestPolicyAllows(t *testing.T) {
	allow := Policy{Mode: ModeAllow, Rules: []string{"github.com", "docs.anthropic.com"}}
	if !allow.Allows("api.github.com") {
		t.Error("allow-list should permit a listed host")
	}
	if allow.Allows("example.com") {
		t.Error("allow-list should reject an unlisted host")
	}

	disallow := Policy{Mode: ModeDisallow, Rules: []string{"evil.com"}}
	if disallow.Allows("evil.com") {
		t.Error("disallow-list should block a listed host")
	}
	if !disallow.Allows("example.com") {
		t.Error("disallow-list should permit an unlisted host")
	}
}

func TestDefaultNoRegression(t *testing.T) {
	// The seeded default must keep permitting exactly the legacy hardcoded hosts.
	d := Default()
	if d.Mode != ModeAllow {
		t.Fatalf("default mode = %q, want allow", d.Mode)
	}
	for _, h := range []string{"firtal.com", "shop.firtal.com", "docs.anthropic.com"} {
		if !d.Allows(h) {
			t.Errorf("default should permit legacy host %q", h)
		}
	}
	if d.Allows("github.com") {
		t.Error("default should not permit github.com")
	}
}

func TestNormalize(t *testing.T) {
	p := Normalize("weird", []string{" GitHub.com ", "github.com", "", "  "})
	if p.Mode != ModeAllow {
		t.Errorf("unknown mode should default to allow, got %q", p.Mode)
	}
	if len(p.Rules) != 1 || p.Rules[0] != "github.com" {
		t.Errorf("normalize should trim/lowercase/dedupe/drop-empty, got %v", p.Rules)
	}
}
