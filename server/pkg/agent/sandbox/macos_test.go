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

func TestGenerate_AllowsBroadHomeRead(t *testing.T) {
	out, err := Generate(Profile{
		Workdir: "/Users/sandbox/work",
		Home:    "/Users/sandbox",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := `(allow file-read* (subpath "/Users/sandbox"))`
	if !strings.Contains(out, want) {
		t.Errorf("profile missing broad home read rule\nprofile:\n%s", out)
	}
}

// TestGenerate_WritablePathsEmittedBeforeDenies guards JEH-405: agent CLIs
// (claude, cursor, gemini, …) read+write per-user state files outside the
// workdir (~/.claude.json, ~/.cursor, …). Without those allows the CLI
// silently exits with empty output the moment it tries to persist its
// session. The allows MUST sit before the sensitive-home denies so an
// accidentally overlapping path still gets sealed by "last match wins".
func TestGenerate_WritablePathsEmittedBeforeDenies(t *testing.T) {
	out, err := Generate(Profile{
		Workdir: "/Users/sandbox/work",
		Home:    "/Users/sandbox",
		WritablePaths: []string{
			"/Users/sandbox/.claude",
			"/Users/sandbox/.claude.json",
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	allow := `(allow file-read* file-write* (subpath "/Users/sandbox/.claude"))`
	if !strings.Contains(out, allow) {
		t.Errorf("profile missing writable-path allow %q\nprofile:\n%s", allow, out)
	}
	idxAllow := strings.Index(out, allow)
	idxDeny := strings.Index(out, "Sensitive home subpaths")
	if idxAllow < 0 || idxDeny < 0 {
		t.Fatalf("expected both writable allow and sensitive deny block; allow@%d deny@%d", idxAllow, idxDeny)
	}
	if idxAllow > idxDeny {
		t.Errorf("writable-path allow must come before sensitive denies (allow@%d deny@%d)", idxAllow, idxDeny)
	}
}

// TestGenerate_KeychainReadAllowedWriteDenied guards JEH-405's second
// blocker: claude (and the other agent CLIs) authenticate via the
// macOS Keychain. securityd serves keychain items over XPC but
// requires the calling process to have file-read access on the
// keychain DB. The previous full-deny on Library/Keychains broke
// auth — claude exited with "Not logged in · Please run /login".
//
// Reads must be allowed (covered by the broad $HOME read); writes
// must still be denied so a sandboxed agent cannot corrupt the DB
// or smuggle items in (legitimate mutations go through securityd
// which runs unsandboxed).
func TestGenerate_KeychainReadAllowedWriteDenied(t *testing.T) {
	out, err := Generate(Profile{
		Workdir: "/Users/sandbox/work",
		Home:    "/Users/sandbox",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// Must not appear in the read+write deny block.
	bothDenied := `(deny file-read* file-write* (subpath "/Users/sandbox/Library/Keychains"))`
	if strings.Contains(out, bothDenied) {
		t.Errorf("keychain DB must not be in the read+write deny block:\n%s", out)
	}
	// Must appear in the write-only deny block.
	writeDenied := `(deny file-write* (subpath "/Users/sandbox/Library/Keychains"))`
	if !strings.Contains(out, writeDenied) {
		t.Errorf("expected write-only deny on keychain DB, profile:\n%s", out)
	}
	// Read is granted via the broad $HOME read; the write-only deny
	// must be emitted AFTER that broad allow so "last match wins"
	// gives us read but not write.
	idxBroadHome := strings.Index(out, `(allow file-read* (subpath "/Users/sandbox"))`)
	idxWriteDeny := strings.Index(out, writeDenied)
	if idxBroadHome < 0 || idxWriteDeny < 0 {
		t.Fatalf("missing required rules; broadHome@%d writeDeny@%d", idxBroadHome, idxWriteDeny)
	}
	if idxWriteDeny < idxBroadHome {
		t.Errorf("write-deny on keychain must come after broad $HOME read (deny@%d allow@%d)", idxWriteDeny, idxBroadHome)
	}
}

// TestGenerate_WritablePathOverlappingSensitiveStillDenied verifies the
// "last match wins" guarantee: even if a caller passes a writable path
// that overlaps a sensitive subpath (a misconfiguration we do not want
// to silently honour), the trailing deny rule still seals it.
func TestGenerate_WritablePathOverlappingSensitiveStillDenied(t *testing.T) {
	out, err := Generate(Profile{
		Workdir: "/Users/sandbox/work",
		Home:    "/Users/sandbox",
		// Pretend a misconfiguration tries to make ~/.ssh writable.
		WritablePaths: []string{"/Users/sandbox/.ssh"},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	deny := `(deny file-read* file-write* (subpath "/Users/sandbox/.ssh"))`
	if !strings.Contains(out, deny) {
		t.Fatalf("profile missing sensitive deny rule\nprofile:\n%s", out)
	}
	idxAllow := strings.Index(out, `(allow file-read* file-write* (subpath "/Users/sandbox/.ssh"))`)
	idxDeny := strings.Index(out, deny)
	if idxAllow >= 0 && idxAllow > idxDeny {
		t.Errorf("an overlapping writable allow must NOT come after the sensitive deny (allow@%d deny@%d)", idxAllow, idxDeny)
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
	// Public hostnames collapse to a port-only rule (Seatbelt cannot match
	// arbitrary hostnames at the kernel layer); loopback IPs map to
	// "localhost:<port>" so the daemon health port stays loopback-scoped.
	out, err := Generate(Profile{
		Workdir: "/tmp/work",
		Home:    "/Users/sandbox",
		AllowedHosts: []string{
			"api.anthropic.com:443",
			"127.0.0.1:19514",
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wants := []string{
		`(allow network-outbound (remote tcp "localhost:19514"))`,
		`(allow network-outbound (remote tcp "*:443"))`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("profile missing rule %q\nprofile:\n%s", w, out)
		}
	}
}

// TestGenerate_NoLiteralHostsInRemoteTCP guards against the regression that
// motivated this translation: any (remote tcp ...) rule with a host other
// than "*" or "localhost" causes sandbox-exec to reject the profile and
// exit 65 before the wrapped process runs (JEH-354). Hostname/IP literals
// must never appear in the generated profile.
func TestGenerate_NoLiteralHostsInRemoteTCP(t *testing.T) {
	out, err := Generate(Profile{
		Workdir: "/tmp/work",
		Home:    "/Users/sandbox",
		AllowedHosts: []string{
			"api.anthropic.com:443",
			"statsig.anthropic.com:443",
			"multica.example.com:443",
			"127.0.0.1:19514",
			"localhost:19514",
			"[::1]:8080",
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "(remote tcp ") {
			continue
		}
		// Acceptable: "*:<port>" or "localhost:<port>".
		if strings.Contains(line, `"*:`) || strings.Contains(line, `"localhost:`) {
			continue
		}
		t.Errorf("disallowed remote tcp host literal in rule: %s", line)
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

func TestTranslateAllowlistToTCPRules(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "empty input returns nil",
			in:   nil,
			want: nil,
		},
		{
			name: "loopback variants collapse to one localhost rule",
			in:   []string{"127.0.0.1:19514", "localhost:19514", "[::1]:19514"},
			want: []string{
				`(allow network-outbound (remote tcp "localhost:19514"))`,
			},
		},
		{
			name: "different public hosts on same port collapse to one wildcard rule",
			in:   []string{"api.anthropic.com:443", "statsig.anthropic.com:443"},
			want: []string{
				`(allow network-outbound (remote tcp "*:443"))`,
			},
		},
		{
			name: "loopback and public on same port produce two distinct rules",
			in:   []string{"127.0.0.1:443", "api.anthropic.com:443"},
			want: []string{
				`(allow network-outbound (remote tcp "localhost:443"))`,
				`(allow network-outbound (remote tcp "*:443"))`,
			},
		},
		{
			name: "loopback rules sort before public; ports sort lexicographically within group",
			in:   []string{"api.example.com:80", "api.example.com:443", "localhost:9000", "localhost:80"},
			want: []string{
				`(allow network-outbound (remote tcp "localhost:80"))`,
				`(allow network-outbound (remote tcp "localhost:9000"))`,
				`(allow network-outbound (remote tcp "*:443"))`,
				`(allow network-outbound (remote tcp "*:80"))`,
			},
		},
		{
			name: "garbage entries are dropped",
			in:   []string{"", "  ", "not-a-host-port", ":443", "host:", "api.anthropic.com:443"},
			want: []string{
				`(allow network-outbound (remote tcp "*:443"))`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := translateAllowlistToTCPRules(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("translateAllowlistToTCPRules(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("rule[%d] = %q, want %q (full: %v)", i, got[i], tc.want[i], got)
				}
			}
		})
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
		"":           `""`,
		"foo":        `"foo"`,
		`with"quote`: `"with\"quote"`,
		`with\back`:  `"with\\back"`,
		"/Users/foo": `"/Users/foo"`,
		"æøå":        `"æøå"`,
	}
	for in, want := range cases {
		if got := schemeString(in); got != want {
			t.Errorf("schemeString(%q) = %q, want %q", in, got, want)
		}
	}
}
