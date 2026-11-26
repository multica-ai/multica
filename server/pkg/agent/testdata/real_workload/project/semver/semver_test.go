package semver

import (
	"sort"
	"testing"
)

func mustParse(t *testing.T, s string) Version {
	t.Helper()
	v, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return v
}

func TestParseAndString(t *testing.T) {
	for _, s := range []string{"1.2.3", "0.0.0", "10.20.30", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-0.3.7", "1.0.0+build.5", "1.0.0-rc.1+sha.abc"} {
		if got := mustParse(t, s).String(); got != s {
			t.Fatalf("String(Parse(%q)) = %q", s, got)
		}
	}
	if got := mustParse(t, "v2.0.1").String(); got != "2.0.1" {
		t.Fatalf("leading v: %q", got)
	}
	for _, bad := range []string{"", "1", "1.2", "1.2.3.4", "01.2.3", "1.02.3", "1.2.3-", "1.2.3-01", "1.2.3-a..b", "a.b.c", "1.2.-3"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("Parse(%q) succeeded, want error", bad)
		}
	}
}

func TestPrecedence(t *testing.T) {
	ordered := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2",
		"1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0",
	}
	vs := make([]Version, len(ordered))
	for i, s := range ordered {
		vs[i] = mustParse(t, s)
	}
	shuffled := []Version{vs[7], vs[2], vs[10], vs[0], vs[5], vs[9], vs[1], vs[8], vs[4], vs[3], vs[6]}
	sort.Slice(shuffled, func(i, j int) bool { return Compare(shuffled[i], shuffled[j]) < 0 })
	for i := range shuffled {
		if shuffled[i].String() != ordered[i] {
			t.Fatalf("position %d = %s, want %s", i, shuffled[i], ordered[i])
		}
	}
	if Compare(mustParse(t, "1.0.0+a"), mustParse(t, "1.0.0+b")) != 0 {
		t.Fatal("build metadata must not affect precedence")
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		constraint string
		version    string
		want       bool
	}{
		{"1.2.3", "1.2.3", true},
		{"=1.2.3", "1.2.4", false},
		{">1.2.3", "1.2.4", true},
		{">=1.2.3 <2.0.0", "1.9.9", true},
		{">=1.2.3 <2.0.0", "2.0.0", false},
		{"<=1.0.0", "1.0.0", true},
		{"^1.2.3", "1.9.0", true},
		{"^1.2.3", "2.0.0", false},
		{"^1.2.3", "1.2.2", false},
		{"^0.2.3", "0.2.9", true},
		{"^0.2.3", "0.3.0", false},
		{"^0.0.3", "0.0.3", true},
		{"^0.0.3", "0.0.4", false},
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		{"~1.2", "1.2.0", true},
		{"~1.2", "1.3.0", false},
		{"^1.2.3 || ^3.0.0", "3.1.0", true},
		{"^1.2.3 || ^3.0.0", "2.5.0", false},
		{"^1.2.3", "1.5.0-beta", false},
		{">=1.2.3-alpha <1.3.0", "1.2.3-beta", true},
		{">=1.2.3-alpha <1.3.0", "1.2.4-beta", false},
	}
	for _, c := range cases {
		got, err := Match(c.constraint, mustParse(t, c.version))
		if err != nil {
			t.Fatalf("Match(%q, %s): %v", c.constraint, c.version, err)
		}
		if got != c.want {
			t.Fatalf("Match(%q, %s) = %v, want %v", c.constraint, c.version, got, c.want)
		}
	}
	for _, bad := range []string{"", ">>1.0.0", "^", "1.0.0 ||", "~x"} {
		if _, err := Match(bad, mustParse(t, "1.0.0")); err == nil {
			t.Fatalf("Match(%q) succeeded, want error", bad)
		}
	}
}
