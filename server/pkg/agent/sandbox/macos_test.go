package sandbox

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update sandbox profile golden files")

func TestGenerate_Golden(t *testing.T) {
	got, err := Generate(Profile{
		Workdir: "/Users/sandbox/work",
		Home:    "/Users/sandbox",
		TempDir: "/Users/sandbox/work/.tmp",
		AllowedHosts: []string{
			"api.anthropic.com:443",
			"127.0.0.1:19514",
			"multica.example.com:443",
			// duplicate + whitespace get normalized away
			"  api.anthropic.com:443  ",
			// bogus entry is dropped
			"not-a-host-port",
			"",
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	goldenPath := filepath.Join("testdata", "default.golden.sb")
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update once): %v", err)
	}
	if got != string(want) {
		t.Errorf("profile differs from golden file %s\n--- want ---\n%s\n--- got ---\n%s",
			goldenPath, string(want), got)
	}
}

func TestGenerate_RequiresWorkdir(t *testing.T) {
	if _, err := Generate(Profile{Home: "/Users/sandbox"}); err == nil {
		t.Fatal("expected error when workdir is empty")
	}
}

func TestGenerate_RequiresHome(t *testing.T) {
	if _, err := Generate(Profile{Workdir: "/tmp/work"}); err == nil {
		t.Fatal("expected error when home is empty")
	}
}

func TestGenerate_DeniesSensitivePaths(t *testing.T) {
	out, err := Generate(Profile{
		Workdir: "/Users/sandbox/work",
		Home:    "/Users/sandbox",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, sub := range []string{
		"/Users/sandbox/.ssh",
		"/Users/sandbox/.aws",
		"/Users/sandbox/.gcloud",
		"/Users/sandbox/Library/Application Support",
		"/Users/sandbox/Library/Cookies",
	} {
		needle := "(deny file-read* file-write* (subpath \"" + sub + "\"))"
		if !strings.Contains(out, needle) {
			t.Errorf("profile missing deny rule for %q\nprofile:\n%s", sub, out)
		}
	}
}

func TestGenerate_AllowsWorkdirReadWrite(t *testing.T) {
	out, err := Generate(Profile{
		Workdir: "/Users/sandbox/work",
		Home:    "/Users/sandbox",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "(allow file-read* file-write* (subpath \"/Users/sandbox/work\"))"
	if !strings.Contains(out, want) {
		t.Errorf("profile missing workdir read/write rule\nprofile:\n%s", out)
	}
}

func TestGenerate_AllowedHostsBecomeNetworkRules(t *testing.T) {
	out, err := Generate(Profile{
		Workdir:      "/tmp/work",
		Home:         "/Users/sandbox",
		AllowedHosts: []string{"api.anthropic.com:443"},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := `(allow network-outbound (remote tcp "api.anthropic.com:443"))`
	if !strings.Contains(out, want) {
		t.Errorf("profile missing allowed host rule\nprofile:\n%s", out)
	}
}

func TestGenerate_EmptyAllowlistDeniesAllOutbound(t *testing.T) {
	out, err := Generate(Profile{
		Workdir: "/tmp/work",
		Home:    "/Users/sandbox",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(out, "(allow network-outbound (remote tcp ") {
		t.Errorf("expected no remote-tcp allow rules, got:\n%s", out)
	}
	// DNS is still allowed so allowlisted hostnames could resolve once added.
	if !strings.Contains(out, "(allow network-outbound (remote udp \"*:53\"))") {
		t.Error("expected DNS allow rule")
	}
}

func TestGenerate_DenyDefaultIsFirstRule(t *testing.T) {
	out, err := Generate(Profile{
		Workdir: "/tmp/work",
		Home:    "/Users/sandbox",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	idxDeny := strings.Index(out, "(deny default)")
	idxAllow := strings.Index(out, "(allow ")
	if idxDeny < 0 {
		t.Fatal("missing (deny default)")
	}
	if idxAllow < 0 {
		t.Fatal("missing any allow rule")
	}
	if idxDeny > idxAllow {
		t.Errorf("(deny default) must come before any allow rule (deny@%d, allow@%d)", idxDeny, idxAllow)
	}
}

func TestNormalizeHosts(t *testing.T) {
	in := []string{
		"api.anthropic.com:443",
		"  api.anthropic.com:443  ",
		"127.0.0.1:19514",
		"",
		"not-a-host-port",
		":443",
		"host:",
		"[::1]:8080",
	}
	got := normalizeHosts(in)
	want := []string{
		"127.0.0.1:19514",
		"[::1]:8080",
		"api.anthropic.com:443",
	}
	if len(got) != len(want) {
		t.Fatalf("normalizeHosts: got %d entries, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("normalizeHosts[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestSchemeString(t *testing.T) {
	cases := map[string]string{
		"":            `""`,
		"foo":         `"foo"`,
		`with"quote`:  `"with\"quote"`,
		`with\back`:   `"with\\back"`,
		"/Users/foo":  `"/Users/foo"`,
		"æøå":         `"æøå"`,
	}
	for in, want := range cases {
		if got := schemeString(in); got != want {
			t.Errorf("schemeString(%q) = %q, want %q", in, got, want)
		}
	}
}
