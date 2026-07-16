package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestTaskIsolationPolicyRejectsSymlinkEscapeAndForbiddenOverlap(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ownerConfig := filepath.Join(root, "owner", ".multica")
	if err := os.MkdirAll(ownerConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	taskRoot := filepath.Join(root, "task")
	if err := os.Mkdir(taskRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(taskRoot, "escape")
	if err := os.Symlink(ownerConfig, escape); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		policy TaskIsolationPolicy
	}{
		{
			name: "writable symlink escape",
			policy: TaskIsolationPolicy{
				WritableRoots:  []string{escape},
				ForbiddenRoots: []string{ownerConfig},
			},
		},
		{
			name: "read-only forbidden child",
			policy: TaskIsolationPolicy{
				ReadOnlyRoots:  []string{filepath.Join(root, "owner")},
				ForbiddenRoots: []string{ownerConfig},
			},
		},
		{
			name: "writable forbidden parent",
			policy: TaskIsolationPolicy{
				WritableRoots:  []string{root},
				ForbiddenRoots: []string{ownerConfig},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.policy.Validated(); err == nil {
				t.Fatal("expected forbidden-root overlap to fail")
			}
		})
	}
}

func TestTaskIsolationPolicyRejectsRelativeDotDotAndMissingRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	tests := []TaskIsolationPolicy{
		{WritableRoots: []string{"relative"}},
		{WritableRoots: []string{root + "/child/.."}},
		{WritableRoots: []string{missing}},
	}
	for _, policy := range tests {
		if _, err := policy.Validated(); err == nil {
			t.Fatalf("Validated(%#v) unexpectedly succeeded", policy)
		}
	}
}

func TestTaskIsolationPolicyValidatesExactReadOnlyFiles(t *testing.T) {
	root := t.TempDir()
	writable := filepath.Join(root, "task")
	private := filepath.Join(root, "daemon-private")
	forbidden := filepath.Join(root, "owner")
	if err := os.MkdirAll(writable, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(forbidden, 0o700); err != nil {
		t.Fatal(err)
	}
	authority := filepath.Join(private, "task-authority.json")
	if err := os.WriteFile(authority, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	validated, err := (TaskIsolationPolicy{
		WritableRoots: []string{writable},
		ReadOnlyFiles: []ReadOnlyFileMount{{Source: authority, Target: "/run/multica/task-authority.json"}},
	}).Validated()
	if err != nil {
		t.Fatalf("valid exact file rejected: %v", err)
	}
	if len(validated.ReadOnlyFiles) != 1 || validated.ReadOnlyFiles[0].Source != authority || validated.ReadOnlyFiles[0].Target != "/run/multica/task-authority.json" {
		t.Fatalf("validated exact files = %#v", validated.ReadOnlyFiles)
	}

	writableAuthority := filepath.Join(writable, "task-authority.json")
	if err := os.WriteFile(writableAuthority, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (TaskIsolationPolicy{
		WritableRoots: []string{writable},
		ReadOnlyFiles: []ReadOnlyFileMount{{Source: writableAuthority, Target: "/run/multica/task-authority.json"}},
	}).Validated(); err == nil {
		t.Fatal("read-only file source inside writable root unexpectedly accepted")
	}

	directory := filepath.Join(writable, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(writable, "authority-link")
	if err := os.Symlink(authority, symlink); err != nil {
		t.Fatal(err)
	}
	forbiddenFile := filepath.Join(forbidden, "authority.json")
	if err := os.WriteFile(forbiddenFile, []byte("forbidden"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		source string
		target string
	}{
		{name: "missing", source: filepath.Join(writable, "missing"), target: "/run/multica/task-authority.json"},
		{name: "directory", source: directory, target: "/run/multica/task-authority.json"},
		{name: "symlink", source: symlink, target: "/run/multica/task-authority.json"},
		{name: "inside forbidden root", source: forbiddenFile, target: "/run/multica/task-authority.json"},
		{name: "relative target", source: authority, target: "run/multica/task-authority.json"},
		{name: "proc target", source: authority, target: "/proc/task-authority.json"},
		{name: "dev target", source: authority, target: "/dev/task-authority.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := (TaskIsolationPolicy{
				WritableRoots:  []string{writable},
				ReadOnlyFiles:  []ReadOnlyFileMount{{Source: tt.source, Target: tt.target}},
				ForbiddenRoots: []string{forbidden},
			}).Validated()
			if err == nil {
				t.Fatalf("exact file mount %#v unexpectedly accepted", tt)
			}
		})
	}

	assertFIFOReadOnlyFileRejected(t, writable)

	if _, err := (TaskIsolationPolicy{
		WritableRoots: []string{writable},
		ReadOnlyFiles: []ReadOnlyFileMount{
			{Source: authority, Target: "/run/multica/task-authority.json"},
			{Source: forbiddenFile, Target: "/run/multica/task-authority.json"},
		},
	}).Validated(); err == nil {
		t.Fatal("duplicate exact-file target unexpectedly accepted")
	}
}

func TestTaskIsolationPolicyRejectsReadOnlyFileTargetWritableOverlap(t *testing.T) {
	root := t.TempDir()
	writable := filepath.Join(root, "task")
	private := filepath.Join(root, "daemon-private")
	if err := os.MkdirAll(writable, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(private, 0o700); err != nil {
		t.Fatal(err)
	}
	authority := filepath.Join(private, "task-authority.json")
	if err := os.WriteFile(authority, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		target string
	}{
		{name: "target inside writable root", target: filepath.Join(writable, "task-authority.json")},
		{name: "target equals writable root", target: writable},
		{name: "target contains writable root", target: root},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := (TaskIsolationPolicy{
				WritableRoots: []string{writable},
				ReadOnlyFiles: []ReadOnlyFileMount{{Source: authority, Target: tt.target}},
			}).Validated()
			if err == nil {
				t.Fatalf("read-only target %q overlapping writable root %q unexpectedly accepted", tt.target, writable)
			}
		})
	}
}

func TestStableSystemPathAliasesArePlatformBoundAndExact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		goos     string
		path     string
		resolved string
		want     bool
	}{
		{name: "linux bin", goos: "linux", path: "/bin", resolved: "/usr/bin", want: true},
		{name: "linux bin child", goos: "linux", path: "/bin/sh", resolved: "/usr/bin/sh", want: true},
		{name: "linux lib", goos: "linux", path: "/lib", resolved: "/usr/lib", want: true},
		{name: "linux lib64", goos: "linux", path: "/lib64", resolved: "/usr/lib64", want: true},
		{name: "linux unexpected target", goos: "linux", path: "/lib", resolved: "/opt/lib"},
		{name: "linux reverse alias", goos: "linux", path: "/usr/bin", resolved: "/bin"},
		{name: "linux extended target", goos: "linux", path: "/bin", resolved: "/usr/bin/extra"},
		{name: "linux alias prefix confusion", goos: "linux", path: "/library", resolved: "/usr/library"},
		{name: "linux bin boundary confusion", goos: "linux", path: "/binx", resolved: "/usr/binx"},
		{name: "linux lib64 boundary confusion", goos: "linux", path: "/lib64evil", resolved: "/usr/lib64evil"},
		{name: "linux canonical child mismatch", goos: "linux", path: "/bin/sh", resolved: "/usr/bin/bash"},
		{name: "linux unlisted alias", goos: "linux", path: "/sbin", resolved: "/usr/sbin"},
		{name: "darwin var", goos: "darwin", path: "/var", resolved: "/private/var", want: true},
		{name: "darwin var child", goos: "darwin", path: "/var/db", resolved: "/private/var/db", want: true},
		{name: "darwin alias on linux", goos: "linux", path: "/var", resolved: "/private/var"},
		{name: "linux alias on darwin", goos: "darwin", path: "/bin", resolved: "/usr/bin"},
		{name: "unsupported platform", goos: "windows", path: "/bin", resolved: "/usr/bin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isStableSystemPathAliasForOS(tt.goos, tt.path, tt.resolved); got != tt.want {
				t.Fatalf("isStableSystemPathAliasForOS(%q, %q, %q) = %v, want %v", tt.goos, tt.path, tt.resolved, got, tt.want)
			}
		})
	}
}

func TestValidatedCanonicalizesOnlyStableSystemAliases(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux usr-merge aliases")
	}
	resolved, err := filepath.EvalSymlinks("/bin")
	if err != nil || resolved == "/bin" {
		t.Skip("host does not use /bin as a symlink alias")
	}
	root := t.TempDir()
	policy, err := (TaskIsolationPolicy{
		WritableRoots: []string{root},
		SystemRoots:   []string{"/bin"},
	}).Validated()
	if err != nil {
		t.Fatalf("Validated: %v", err)
	}
	if !reflect.DeepEqual(policy.SystemRoots, []string{resolved}) {
		t.Fatalf("system roots = %#v, want %#v", policy.SystemRoots, []string{resolved})
	}

	alias := filepath.Join(root, "alias")
	if err := os.Symlink(resolved, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := (TaskIsolationPolicy{WritableRoots: []string{alias}}).Validated(); err == nil {
		t.Fatal("user-controlled alias unexpectedly accepted")
	}
}

func TestDarwinProfileRenderingIsDeterministicAndQuotesPaths(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX paths")
	}
	root := t.TempDir()
	spaceRoot := filepath.Join(root, "task root")
	if err := os.Mkdir(spaceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	policy := TaskIsolationPolicy{
		WritableRoots: []string{spaceRoot},
		ReadOnlyRoots: []string{root},
		SystemRoots:   existingSystemRootsForTest(t),
		Network:       NetworkAccessPublicAndLoopback,
	}
	first, err := renderDarwinProfile(policy)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	second, err := renderDarwinProfile(policy)
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if first != second {
		t.Fatalf("profile rendering is nondeterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if !strings.Contains(first, `(subpath "`+escapeSandboxString(spaceRoot)+`")`) {
		t.Fatalf("profile does not contain escaped writable root %q:\n%s", spaceRoot, first)
	}
	if !strings.Contains(first, "(allow network-outbound)") || !strings.Contains(first, "(allow network-inbound") {
		t.Fatalf("profile does not explicitly allow public + loopback networking:\n%s", first)
	}
}

func TestDarwinProfileAllowsOnlyExplicitMachServices(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	profile, err := renderDarwinProfile(TaskIsolationPolicy{WritableRoots: []string{root}})
	if err != nil {
		t.Fatalf("renderDarwinProfile: %v", err)
	}
	if strings.Contains(profile, "(allow mach-lookup)\n") {
		t.Fatalf("profile contains unrestricted mach-lookup authority:\n%s", profile)
	}
	wantServices := []string{
		"com.apple.SystemConfiguration.configd",
		"com.apple.cfprefsd.agent",
		"com.apple.system.opendirectoryd.libinfo",
		"com.apple.trustd.agent",
	}
	for _, service := range wantServices {
		want := `(allow mach-lookup (global-name "` + service + `"))`
		if !strings.Contains(profile, want) {
			t.Fatalf("profile does not allow required service %q:\n%s", service, profile)
		}
	}
	for _, line := range strings.Split(profile, "\n") {
		if strings.Contains(line, "mach-lookup") && !containsAnyString(line, wantServices) {
			t.Fatalf("profile contains unreviewed mach service rule %q", line)
		}
	}
}

func TestLinuxArgsUseEmptyNamespaceWithoutHostRootBind(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	policy := TaskIsolationPolicy{
		WritableRoots: []string{root},
		SystemRoots:   existingSystemRootsForTest(t),
		Network:       NetworkAccessPublicAndLoopback,
	}
	args, err := renderLinuxBubblewrapArgs(policy, "/usr/bin/tool", []string{"arg"})
	if err != nil {
		t.Fatalf("renderLinuxBubblewrapArgs: %v", err)
	}
	if isolationContainsAdjacent(args, "--ro-bind", "/") || isolationContainsAdjacent(args, "--bind", "/") {
		t.Fatalf("bubblewrap args expose host root: %#v", args)
	}
	if !isolationContainsAdjacent(args, "--bind", root) {
		t.Fatalf("bubblewrap args do not bind writable task root: %#v", args)
	}
	if !isolationContainsString(args, "--unshare-all") || !isolationContainsString(args, "--share-net") {
		t.Fatalf("bubblewrap args do not isolate namespaces while preserving public/loopback network: %#v", args)
	}
	if !isolationContainsAdjacent(args, "--dev", "/dev") {
		t.Fatalf("bubblewrap args do not create a minimal device namespace: %#v", args)
	}
	if !isolationContainsAdjacent(args, "--proc", "/proc") {
		t.Fatalf("bubblewrap args do not mount proc for the isolated PID namespace: %#v", args)
	}
	for _, hostRoot := range []string{"/dev", "/proc"} {
		if isolationContainsAdjacent(args, "--bind", hostRoot) || isolationContainsAdjacent(args, "--ro-bind", hostRoot) {
			t.Fatalf("bubblewrap args bind host %s instead of creating an isolated mount: %#v", hostRoot, args)
		}
	}
	wantTail := []string{"--", "/usr/bin/tool", "arg"}
	if len(args) < len(wantTail) || !reflect.DeepEqual(args[len(args)-len(wantTail):], wantTail) {
		t.Fatalf("bubblewrap argv tail = %#v, want %#v", args, wantTail)
	}
}

func TestLinuxArgsRejectHostDeviceAndProcRootBindings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, exposed := range []string{"/", "/dev", "/proc"} {
		t.Run(exposed, func(t *testing.T) {
			_, err := renderLinuxBubblewrapArgs(TaskIsolationPolicy{
				WritableRoots: []string{root},
				ReadOnlyRoots: []string{exposed},
			}, "/usr/bin/tool", nil)
			if err == nil {
				t.Fatalf("host pseudo-filesystem root %q unexpectedly accepted", exposed)
			}
		})
	}
}

func TestIsolationHelperRejectsUserWritableAndSymlinkPaths(t *testing.T) {
	t.Parallel()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temporary root: %v", err)
	}
	helper := filepath.Join(root, "sandbox-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateIsolationHelper(helper); err == nil {
		t.Fatal("user-owned isolation helper unexpectedly trusted")
	} else if !strings.Contains(err.Error(), "want root") {
		t.Fatalf("user-owned helper rejected for the wrong reason: %v", err)
	}

	symlink := filepath.Join(root, "sandbox-helper-link")
	if err := os.Symlink(helper, symlink); err != nil {
		t.Fatal(err)
	}
	if err := validateIsolationHelper(symlink); err == nil {
		t.Fatal("symlinked isolation helper unexpectedly trusted")
	} else if !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("symlinked helper rejected for the wrong reason: %v", err)
	}
}

func TestSystemIsolationHelperHasTrustedStableChain(t *testing.T) {
	t.Parallel()

	var helper string
	switch runtime.GOOS {
	case "darwin":
		helper = "/usr/bin/sandbox-exec"
	case "linux":
		helper = "/usr/bin/bwrap"
	default:
		t.Skip("no supported platform isolation helper")
	}
	if _, err := os.Lstat(helper); os.IsNotExist(err) {
		t.Skipf("platform helper %s is not installed", helper)
	} else if err != nil {
		t.Fatalf("lstat platform helper: %v", err)
	}
	if err := validateIsolationHelper(helper); err != nil {
		t.Fatalf("trusted platform helper rejected: %v", err)
	}
}

func TestPlatformIsolationFailsClosedForMissingHelperAndUnsupportedOS(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, platform := range []platformIsolation{
		newDarwinIsolation(filepath.Join(root, "missing-sandbox-exec")),
		newLinuxIsolation(filepath.Join(root, "missing-bwrap")),
		newUnsupportedIsolation("plan9"),
	} {
		executable := pathIdentity{Path: "/usr/bin/tool"}
		cwd := pathIdentity{Path: root}
		if _, _, _, err := platform.WrapBound(nil, executable, cwd, nil, 0); err == nil {
			t.Fatalf("%T unexpectedly accepted unavailable isolation", platform)
		}
	}
}

func isolationContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func isolationContainsAdjacent(values []string, first, second string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == first && values[i+1] == second {
			return true
		}
	}
	return false
}

func containsAnyString(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
