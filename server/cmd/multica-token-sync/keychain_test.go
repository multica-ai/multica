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

func TestMacOSKeychain_RejectsNewlinePayload(t *testing.T) {
	kc := &macOSKeychain{}
	if err := kc.Write("svc", "acct", []byte("a\nb")); err == nil {
		t.Error("expected error for payload containing newline")
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
