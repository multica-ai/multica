//go:build linux

package main

import (
	"errors"
	"os"
	"testing"
)

// Every user's default profile hashes to the same health port, so on a shared
// machine another user's daemon answers it. Taking it for ours made a second
// user's daemon refuse to start ("already running").
func TestDaemonIdentityMismatchRefusesAnotherUsersDaemon(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("pid 1 belongs to the same user when running as root")
	}
	for _, health := range []map[string]any{
		{"pid": 1.0, "profile": ""},
		{"pid": 1.0}, // an older daemon that does not report its profile
	} {
		err := daemonIdentityMismatch(health, "", 19514)
		var mismatch *daemonProfileMismatchError
		if !errors.As(err, &mismatch) || !mismatch.OtherUser {
			t.Fatalf("daemonIdentityMismatch(%v) = %v, want another user's daemon", health, err)
		}
	}
	if err := daemonIdentityMismatch(map[string]any{"pid": float64(os.Getpid()), "profile": ""}, "", 19514); err != nil {
		t.Fatalf("own daemon = %v, want a match", err)
	}
}
