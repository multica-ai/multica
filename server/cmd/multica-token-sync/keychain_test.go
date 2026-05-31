package main

import (
	"os"
	"testing"
)

func TestKeychainStub_RoundTrip(t *testing.T) {
	kc := &stubKeychain{data: map[string][]byte{}}
	if err := kc.Write("svc", "acct", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := kc.Read("svc", "acct")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("read = %q", got)
	}
}

func TestKeychainStub_MissingReadIsErr(t *testing.T) {
	kc := &stubKeychain{data: map[string][]byte{}}
	if _, err := kc.Read("svc", "acct"); err == nil {
		t.Error("expected error reading absent entry")
	}
}

func TestKeychainStub_WriteIsCopy(t *testing.T) {
	kc := &stubKeychain{data: map[string][]byte{}}
	src := []byte("v1")
	if err := kc.Write("s", "a", src); err != nil {
		t.Fatal(err)
	}
	src[0] = 'X' // mutate after write; stored copy must be untouched
	got, _ := kc.Read("s", "a")
	if string(got) != "v1" {
		t.Errorf("stub did not copy on write; got %q", got)
	}
}

// macOS-only integration test, opt-in via env to avoid touching CI's Keychain.
func TestMacOSKeychain_RoundTrip(t *testing.T) {
	if os.Getenv("MULTICA_KEYCHAIN_TEST") == "" {
		t.Skip("set MULTICA_KEYCHAIN_TEST=1 to opt into macOS Keychain integration test")
	}
	kc := &macOSKeychain{}
	const svc = "multica-token-sync-test"
	const acct = "test"
	const payload = `{"test":"value"}`
	defer func() { _ = kc.Delete(svc, acct) }()
	if err := kc.Write(svc, acct, []byte(payload)); err != nil {
		t.Fatal(err)
	}
	got, err := kc.Read(svc, acct)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Errorf("got %q want %q", got, payload)
	}
}

// Regression: an earlier version piped the value via stdin to keep it out of
// argv, but `security -w` reads stdin through getpass(3) which silently
// truncates at PASS_MAX (~128 bytes). Real Claude Code credentials are
// hundreds of bytes; verify round-trip survives.
func TestMacOSKeychain_LongPayloadRoundTrip(t *testing.T) {
	if os.Getenv("MULTICA_KEYCHAIN_TEST") == "" {
		t.Skip("set MULTICA_KEYCHAIN_TEST=1 to opt into macOS Keychain integration test")
	}
	kc := &macOSKeychain{}
	const svc = "multica-token-sync-test-long"
	const acct = "test"
	payload := make([]byte, 512)
	for i := range payload {
		payload[i] = byte('A' + i%26)
	}
	defer func() { _ = kc.Delete(svc, acct) }()
	if err := kc.Write(svc, acct, payload); err != nil {
		t.Fatal(err)
	}
	got, err := kc.Read(svc, acct)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Errorf("round-trip mismatch (got %d bytes, want %d)", len(got), len(payload))
	}
}
