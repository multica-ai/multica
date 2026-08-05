package daemon

// CEREBRO-PATCH(daemon-claude-keychain-account-test): FIR-4520 regression
// guard. The keychain lookup used to ask for the service name only, so on a
// Mac carrying both a current Claude Code item and a stale pre-2.x one the
// `security` command returned the stale item and the daemon reported an
// expired token forever — the account's 5h/7d usage windows stayed empty
// with no failing test anywhere.

import (
	"errors"
	"reflect"
	"testing"
)

func stubSecurityLookup(t *testing.T, fn func(args ...string) ([]byte, error)) *[][]string {
	t.Helper()
	original := runSecurityFindGenericPassword
	t.Cleanup(func() { runSecurityFindGenericPassword = original })

	var calls [][]string
	runSecurityFindGenericPassword = func(args ...string) ([]byte, error) {
		calls = append(calls, args)
		return fn(args...)
	}
	return &calls
}

func TestReadClaudeCredentialsKeychainAsksForAccountScopedItem(t *testing.T) {
	t.Setenv("USER", "hvejsel")
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	calls := stubSecurityLookup(t, func(...string) ([]byte, error) {
		return []byte(`{"claudeAiOauth":{"accessToken":"tok"}}`), nil
	})

	data, err := readClaudeCredentialsKeychain()
	if err != nil {
		t.Fatalf("readClaudeCredentialsKeychain: %v", err)
	}
	if string(data) != `{"claudeAiOauth":{"accessToken":"tok"}}` {
		t.Fatalf("unexpected credentials payload: %s", data)
	}

	want := []string{"-a", "hvejsel", "-w", "-s", "Claude Code-credentials"}
	if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
		t.Fatalf("keychain lookup args = %v, want exactly one call %v", *calls, want)
	}
}

func TestReadClaudeCredentialsKeychainScopesServiceToConfigDir(t *testing.T) {
	t.Setenv("USER", "hvejsel")
	t.Setenv("CLAUDE_CONFIG_DIR", "/Users/hvejsel/.claude-account-17")

	calls := stubSecurityLookup(t, func(...string) ([]byte, error) {
		return []byte("{}"), nil
	})

	if _, err := readClaudeCredentialsKeychain(); err != nil {
		t.Fatalf("readClaudeCredentialsKeychain: %v", err)
	}

	want := []string{"-a", "hvejsel", "-w", "-s", "Claude Code-credentials-13c6d484"}
	if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
		t.Fatalf("keychain lookup args = %v, want exactly one call %v", *calls, want)
	}
}

func TestReadClaudeCredentialsKeychainFallsBackToLegacyItem(t *testing.T) {
	t.Setenv("USER", "hvejsel")
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	calls := stubSecurityLookup(t, func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "-a" {
			return nil, errors.New("item not found")
		}
		return []byte("legacy"), nil
	})

	data, err := readClaudeCredentialsKeychain()
	if err != nil {
		t.Fatalf("readClaudeCredentialsKeychain: %v", err)
	}
	if string(data) != "legacy" {
		t.Fatalf("expected the legacy item, got %s", data)
	}

	want := []string{"-w", "-s", "Claude Code-credentials"}
	if len(*calls) != 2 || !reflect.DeepEqual((*calls)[1], want) {
		t.Fatalf("fallback lookup args = %v, want second call %v", *calls, want)
	}
}
