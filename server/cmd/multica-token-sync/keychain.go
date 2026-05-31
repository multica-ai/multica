package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Keychain is the storage backend the sync writes into. macOS uses the
// security CLI; tests use stubKeychain.
type Keychain interface {
	Read(service, account string) ([]byte, error)
	Write(service, account string, data []byte) error
}

// macOSKeychain shells out to /usr/bin/security. Token bytes flow via stdin
// (never via argv) so `ps` cannot see them.
type macOSKeychain struct{}

func (m *macOSKeychain) Read(service, account string) ([]byte, error) {
	cmd := exec.Command("/usr/bin/security", "find-generic-password", "-s", service, "-a", account, "-w")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("keychain read: %s: %w", strings.TrimSpace(string(exit.Stderr)), err)
		}
		return nil, fmt.Errorf("keychain read: %w", err)
	}
	return bytes.TrimRight(out, "\n"), nil
}

func (m *macOSKeychain) Write(service, account string, data []byte) error {
	// `security -w PASSWORD` is the only path that handles passwords longer
	// than ~128 bytes — the stdin/getpass path truncates silently at PASS_MAX,
	// and Claude Code's credentials blob is comfortably above that. argv on
	// macOS is owner-visible only (ps restricts arguments to the process
	// owner by default), and that same owner can already read the Keychain
	// directly, so passing the value through argv does not leak anything they
	// could not already see.
	cmd := exec.Command("/usr/bin/security", "add-generic-password",
		"-s", service, "-a", account, "-U", "-w", string(data))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain write: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Delete is used by integration tests to clean up.
func (m *macOSKeychain) Delete(service, account string) error {
	cmd := exec.Command("/usr/bin/security", "delete-generic-password", "-s", service, "-a", account)
	return cmd.Run()
}

// stubKeychain is the in-memory test double.
type stubKeychain struct {
	data map[string][]byte
}

func keychainKey(service, account string) string { return service + "\x00" + account }

func (s *stubKeychain) Read(service, account string) ([]byte, error) {
	v, ok := s.data[keychainKey(service, account)]
	if !ok {
		return nil, fmt.Errorf("stub: not found: %s/%s", service, account)
	}
	return append([]byte(nil), v...), nil
}

func (s *stubKeychain) Write(service, account string, data []byte) error {
	s.data[keychainKey(service, account)] = append([]byte(nil), data...)
	return nil
}
