package daemon

import (
	"strings"
	"testing"
)

func TestExtractSemverToken(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"my-cli 1.4.2 (Company Internal)", "1.4.2"},
		{"v0.3.18", "0.3.18"},
		{"agent build v2.10.10-235-gabc 2024-01", "2.10.10"},
		{"no version here", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := extractSemverToken(c.in); got != c.want {
			t.Errorf("extractSemverToken(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCompareSemverPasses(t *testing.T) {
	t.Parallel()
	if err := compareSemver("1.5.0", "1.0.0"); err != nil {
		t.Errorf("expected 1.5.0 ≥ 1.0.0 to pass, got %v", err)
	}
	if err := compareSemver("1.0.0", "1.0.0"); err != nil {
		t.Errorf("expected 1.0.0 ≥ 1.0.0 to pass, got %v", err)
	}
	if err := compareSemver("v2.0.0", "1.99.99"); err != nil {
		t.Errorf("expected v2.0.0 ≥ 1.99.99 to pass, got %v", err)
	}
}

func TestCompareSemverDetectsTooOld(t *testing.T) {
	t.Parallel()
	err := compareSemver("0.9.0", "1.0.0")
	if err == nil {
		t.Fatalf("expected 0.9.0 < 1.0.0 to fail")
	}
	if !strings.Contains(err.Error(), "below required minimum") {
		t.Errorf("error message changed shape: %v", err)
	}
}

func TestCompareSemverUnparseableIsLenient(t *testing.T) {
	t.Parallel()
	// If we can't parse the detected version we don't want to refuse to
	// load the manifest — fall through to nil so the operator sees the
	// runtime as available and can dig into the version themselves.
	if err := compareSemver("custom-build-sha", "1.0.0"); err != nil {
		t.Errorf("expected unparseable detected to pass, got %v", err)
	}
	// An unparseable minimum is a misconfiguration we can't act on either.
	if err := compareSemver("1.0.0", "abc"); err != nil {
		t.Errorf("expected unparseable minimum to pass, got %v", err)
	}
}
