package wecom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestMediaTempFileWindowsPrivacy(t *testing.T) {
	dir := t.TempDir()
	f, err := createMediaTempFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("plaintext DACL is not protected from inheritance: control=%v err=%v", control, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 1 {
		t.Fatalf("want exactly one private access entry: dacl=%v err=%v", dacl, err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE ||
		!(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(user.User.Sid) {
		t.Fatal("plaintext access is not restricted to the process user")
	}
	if other, err := os.Open(f.Name()); err == nil {
		other.Close()
		t.Fatal("another handle could open the plaintext file")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	assertMediaTempDirEmpty(t, dir)
}

func TestMediaTempFileWindowsLongPath(t *testing.T) {
	dir := t.TempDir()
	for len(dir) < 280 {
		dir = filepath.Join(dir, strings.Repeat("long-temp-", 5))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := createMediaTempFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	assertMediaTempDirEmpty(t, dir)
}
