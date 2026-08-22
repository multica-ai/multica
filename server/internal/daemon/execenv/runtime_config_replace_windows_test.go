package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWriteRuntimeConfigFilePreservesWindowsDACL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("user rules\n"), 0o644); err != nil {
		t.Fatalf("seed runtime config: %v", err)
	}

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("get current token user: %v", err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, nil)
	if err != nil {
		t.Fatalf("build protected DACL: %v", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		t.Fatalf("set protected DACL: %v", err)
	}
	before := runtimeConfigDACLString(t, path)

	if err := writeRuntimeConfigFile(path, "runtime brief"); err != nil {
		t.Fatalf("inject runtime config: %v", err)
	}
	if after := runtimeConfigDACLString(t, path); after != before {
		t.Fatalf("Windows DACL changed during atomic replacement\nbefore: %s\n after: %s", before, after)
	}
}

func TestRuntimeConfigRoundTripSupportsExistingWindowsLongPath(t *testing.T) {
	dir := t.TempDir()
	for len(filepath.Join(dir, "AGENTS.md")) <= 280 {
		dir = filepath.Join(dir, strings.Repeat("nested", 6))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create long-path directory: %v", err)
	}
	path := filepath.Join(dir, "AGENTS.md")
	if len(path) <= 260 {
		t.Fatalf("test path length = %d, want > 260: %s", len(path), path)
	}
	original := []byte("user instructions at a long Windows path\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("seed long-path runtime config: %v", err)
	}

	if err := writeRuntimeConfigFile(path, "runtime brief"); err != nil {
		t.Fatalf("inject at long path: %v", err)
	}
	if err := CleanupRuntimeConfig(dir, "codex"); err != nil {
		t.Fatalf("cleanup at long path: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read long-path target after round trip: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("long-path round trip changed user content\n got: %q\nwant: %q", got, original)
	}
}

func runtimeConfigDACLString(t *testing.T, path string) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("get DACL for %s: %v", path, err)
	}
	return descriptor.String()
}
