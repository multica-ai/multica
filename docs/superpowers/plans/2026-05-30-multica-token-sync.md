# `multica-token-sync` — Local Keychain ⇆ Cluster Broker Sync — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the "single OAuth grant collision" the broker exposes. Today, the cluster-side `multica-claude-broker` is the authoritative writer for the Anthropic refresh chain; every time it rotates the refresh_token, the operator's local Keychain entry goes stale and breaks the local `claude` CLI until they run `claude /login` again. This tool inverts the direction: a small daemon on the operator's laptop polls the broker's state Secret over `kubectl`-style auth, transforms the bytes into the shape the local CLI expects, and upserts the macOS Keychain. The broker becomes the single writer, the laptop becomes a read-only follower.

**Architecture:** Tiny Go binary (~250 LOC), single static executable, `client-go` to read the cluster Secret, shells out to `/usr/bin/security` to write the Keychain. Runs as a launchd agent on a 30-minute interval; supports manual `--once` and long-running `--interval` modes for testing.

**Tech stack:** Go (standard library + `k8s.io/client-go` from existing controller code). No CGo, no Keychain.framework bindings — the `security` CLI is the supported Apple interface and survives macOS upgrades better than direct framework calls.

**Source spec:** Broker plan §"Single-grant caveat" (`docs/superpowers/plans/2026-05-29-multica-claude-broker.md`) flagged this as the long-term operational headache; this plan closes it.

**Builds on:** Plan F.2 (broker — `multica-claude-oauth-broker` Secret with `access_token`/`refresh_token`/`expires_at` keys, populated and refreshed automatically).

---

## Key facts established by design

| Fact | Evidence |
|---|---|
| Claude Code on macOS stores credentials in the Keychain at service `Claude Code-credentials`, account `$USER` | Verified via `security find-generic-password -s 'Claude Code-credentials'` returning a JSON blob during broker bootstrap |
| The stored value is a JSON blob with the schema `{claudeAiOauth: {accessToken, refreshToken, expiresAt, scopes[], subscriptionType}, organizationUuid}` | Decoded via `jq '.claudeAiOauth | keys'` during broker bootstrap; full key set: `accessToken, expiresAt, rateLimitTier, refreshToken, scopes, subscriptionType` |
| The local CLI re-reads the Keychain at the start of every CLI invocation | No in-process caching survives between `claude` command runs. Verified by overwriting the Keychain mid-session and observing next invocation use the new value |
| `security add-generic-password -U` updates an existing entry in-place without prompting | Apple documented behavior; `-U` flag = upsert |
| `client-go`'s `clientcmd.NewNonInteractiveDeferredLoadingClientConfig` is the standard way to load kubeconfig + current context | Used by every CLI tool that wraps `kubectl` (`stern`, `kubectx`, etc.) |

The broker exposes the access+refresh tokens in `multica-claude-oauth-broker` (state Secret, three keys) — the tool reads this directly, NOT the access-token-only mirror (`multica-claude-broker-access-token`) which only contains the bearer.

---

## File structure

### Created by this plan

```
server/cmd/multica-token-sync/
├── main.go                                    # CREATE: entry + signal handling + flag parsing
├── config.go                                  # CREATE: CLI flag → Config struct
├── config_test.go                             # CREATE
├── cluster.go                                 # CREATE: read broker Secret via kubeconfig
├── cluster_test.go                            # CREATE
├── keychain.go                                # CREATE: macOS Keychain abstraction + stub for tests
├── keychain_test.go                           # CREATE
├── sync.go                                    # CREATE: orchestrate read → diff → write
└── sync_test.go                               # CREATE

packaging/launchd/
├── com.multica.token-sync.plist               # CREATE: macOS scheduled agent
└── install.sh                                 # CREATE: install/uninstall script
```

### Modified by this plan

```
packaging/README.md                            # +operator section (install, schedule, troubleshooting)
```

### Reused unchanged

- `multica-claude-oauth-broker` Secret in the cluster — the canonical OAuth state the tool reads from.
- The local Keychain entry `Claude Code-credentials/$USER` — the destination of every successful sync.

---

## Prerequisites

1. **Plan F.2 broker live in the cluster.** The tool reads `multica-claude-oauth-broker`, which exists only when the broker is deployed.
2. **`kubectl` works against the cluster** from the operator's laptop with at least `get secrets/multica-claude-oauth-broker` permission in the broker's namespace. The tool reuses kubeconfig — if `kubectl -n multica get secret multica-claude-oauth-broker` works, so will the tool.
3. **macOS.** v1 is macOS-only; the Keychain backend is shelled-out `security` CLI. A Linux backend via `secret-tool` (libsecret) is a future extension.
4. **Go 1.26+** installed for build (same as the rest of the project).

---

## Task 1: CLI skeleton + flag parsing

**Files:**
- Create: `server/cmd/multica-token-sync/main.go`
- Create: `server/cmd/multica-token-sync/config.go`
- Create: `server/cmd/multica-token-sync/config_test.go`

### Design notes

The tool's CLI surface is small and stable — no subcommands, just flags. Use `flag` from the stdlib rather than `spf13/cobra` (the rest of the project uses cobra in places, but this binary has one command). Pulling cobra in for one command isn't worth the binary-size and dependency weight.

`--once` is the default (matches launchd's invocation model). `--interval` flips to daemon mode for human-driven testing.

### Implementation

- [ ] **Step 1: Failing test for `LoadConfig` defaults**

```go
package main

import (
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := ParseFlags([]string{})
	if err != nil { t.Fatalf("ParseFlags: %v", err) }
	if !cfg.Once { t.Errorf("Once default = %v, want true", cfg.Once) }
	if cfg.Interval != 30*time.Minute { t.Errorf("Interval default = %v", cfg.Interval) }
	if cfg.Namespace != "multica" { t.Errorf("Namespace default = %q", cfg.Namespace) }
	if cfg.SecretName != "multica-claude-oauth-broker" { t.Errorf("SecretName default = %q", cfg.SecretName) }
	if cfg.KeychainService != "Claude Code-credentials" { t.Errorf("KeychainService default = %q", cfg.KeychainService) }
	if cfg.DryRun { t.Error("DryRun must default to false") }
}

func TestParseFlags_DaemonMode(t *testing.T) {
	cfg, err := ParseFlags([]string{"--interval", "5m"})
	if err != nil { t.Fatalf("ParseFlags: %v", err) }
	if cfg.Once { t.Error("--interval must disable --once") }
	if cfg.Interval != 5*time.Minute { t.Errorf("Interval = %v", cfg.Interval) }
}
```

- [ ] **Step 2: Implement `config.go`**

```go
package main

import (
	"flag"
	"fmt"
	"time"
)

type Config struct {
	Once            bool          // exit after one sync
	Interval        time.Duration // daemon mode tick interval (only used if Once=false)
	Context         string        // kubeconfig context (empty = current)
	Namespace       string        // cluster namespace holding the broker state Secret
	SecretName      string        // broker state Secret name
	KeychainService string        // macOS keychain service name to write
	KeychainAccount string        // macOS keychain account (default $USER)
	DryRun          bool          // print intended Keychain write, don't perform it
	Verbose         bool          // slog level → debug
}

// ParseFlags accepts the args slice (NOT os.Args[1:]) so tests can drive it.
// Returns Config with defaults applied. --interval implies --once=false.
func ParseFlags(args []string) (*Config, error) {
	fs := flag.NewFlagSet("multica-token-sync", flag.ContinueOnError)
	cfg := &Config{}
	fs.BoolVar(&cfg.Once, "once", true, "run a single sync and exit (default)")
	fs.DurationVar(&cfg.Interval, "interval", 30*time.Minute, "daemon mode: sync every interval")
	fs.StringVar(&cfg.Context, "context", "", "kubeconfig context (default: current)")
	fs.StringVar(&cfg.Namespace, "namespace", "multica", "cluster namespace")
	fs.StringVar(&cfg.SecretName, "secret", "multica-claude-oauth-broker", "broker state Secret name")
	fs.StringVar(&cfg.KeychainService, "keychain-service", "Claude Code-credentials", "macOS Keychain service")
	fs.StringVar(&cfg.KeychainAccount, "keychain-account", "", "macOS Keychain account (default: $USER)")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "print intended Keychain write, don't perform")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "slog level → debug")
	if err := fs.Parse(args); err != nil { return nil, err }

	// --interval (when set explicitly by user) implies daemon mode.
	// Detection: check if --interval differs from the default OR was on the cmdline.
	intervalSet := false
	fs.Visit(func(f *flag.Flag) { if f.Name == "interval" { intervalSet = true } })
	if intervalSet { cfg.Once = false }

	if cfg.KeychainAccount == "" {
		cfg.KeychainAccount = mustUsername()
	}
	if !cfg.Once && cfg.Interval < 10*time.Second {
		return nil, fmt.Errorf("--interval must be ≥ 10s (got %v)", cfg.Interval)
	}
	return cfg, nil
}

func mustUsername() string {
	// os/user.Current() works without CGo on modern Go (uses USER env on macOS).
	import_we_handle_in_actual_file := ""
	_ = import_we_handle_in_actual_file
	return ""
}
```

(Real `mustUsername` uses `os/user`.`Current().Username`.)

- [ ] **Step 3: `main.go` — entry point + signal wiring**

```go
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	cfg, err := ParseFlags(os.Args[1:])
	if err != nil {
		exitWithUsage(err)
	}
	logger := newLogger(cfg.Verbose)
	if err := run(cfg, logger); err != nil {
		logger.Error("sync failed", "error", err)
		os.Exit(1)
	}
}

func run(cfg *Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// Sync impl arrives in Task 4.
	_ = ctx
	_ = time.Duration(0)
	return nil
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose { level = slog.LevelDebug }
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func exitWithUsage(err error) {
	if err != nil { _, _ = os.Stderr.WriteString(err.Error() + "\n") }
	os.Exit(2)
}
```

- [ ] **Step 4: Test + commit**

```bash
cd /Users/cjs/dev/multica/server
go build -o /tmp/multica-token-sync ./cmd/multica-token-sync
/tmp/multica-token-sync --help 2>&1 | head -20
go test ./cmd/multica-token-sync/ -v 2>&1 | tail -5
git add server/cmd/multica-token-sync/{main,config,config_test}.go
git commit -m "feat(token-sync): CLI skeleton + flag parsing"
```

---

## Task 2: Keychain abstraction

**Files:**
- Create: `server/cmd/multica-token-sync/keychain.go`
- Create: `server/cmd/multica-token-sync/keychain_test.go`

### Design notes

`security`-CLI shell-out is the only realistic backend for macOS Keychain without CGo. We model it behind a small interface so tests can stub it:

```go
type Keychain interface {
	Read(service, account string) ([]byte, error)
	Write(service, account string, data []byte) error
}
```

Read returns the raw value bytes; the upsert behavior is in Write (use `-U`). Both methods log only metadata (service, byte length) — never raw values.

`exec.Command` with explicit args (no shell), stdin-piped value so the token never appears in the process argv (where `ps` would expose it).

### Implementation

- [ ] **Step 1: Failing tests via a stub backend**

```go
package main

import "testing"

func TestKeychainStub_RoundTrip(t *testing.T) {
	kc := &stubKeychain{data: map[string][]byte{}}
	if err := kc.Write("svc", "acct", []byte("hello")); err != nil { t.Fatal(err) }
	got, err := kc.Read("svc", "acct")
	if err != nil { t.Fatal(err) }
	if string(got) != "hello" { t.Errorf("read = %q", got) }
}

func TestKeychainStub_MissingReadIsErr(t *testing.T) {
	kc := &stubKeychain{data: map[string][]byte{}}
	if _, err := kc.Read("svc", "acct"); err == nil {
		t.Error("expected error reading absent entry")
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
	defer kc.Delete(svc, acct) // cleanup
	if err := kc.Write(svc, acct, []byte(payload)); err != nil { t.Fatal(err) }
	got, err := kc.Read(svc, acct)
	if err != nil { t.Fatal(err) }
	if string(got) != payload { t.Errorf("got %q", got) }
}
```

- [ ] **Step 2: Implement `keychain.go`**

```go
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Keychain interface {
	Read(service, account string) ([]byte, error)
	Write(service, account string, data []byte) error
}

// macOSKeychain shells out to /usr/bin/security. Token bytes flow via stdin
// (never via argv) so `ps` can't see them.
type macOSKeychain struct{}

func (m *macOSKeychain) Read(service, account string) ([]byte, error) {
	cmd := exec.Command("/usr/bin/security", "find-generic-password", "-s", service, "-a", account, "-w")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			// macOS exits 44 for "not found"; surface as a sentinel for callers.
			return nil, fmt.Errorf("keychain read: %s: %w", strings.TrimSpace(string(exit.Stderr)), err)
		}
		return nil, fmt.Errorf("keychain read: %w", err)
	}
	// `-w` includes a trailing newline.
	return bytes.TrimRight(out, "\n"), nil
}

func (m *macOSKeychain) Write(service, account string, data []byte) error {
	// -U upserts (update if exists, create if not). -w reads value from arg or stdin.
	// We pass empty -w and write via stdin so the token bytes never hit argv.
	cmd := exec.Command("/usr/bin/security", "add-generic-password",
		"-s", service, "-a", account, "-U", "-w")
	cmd.Stdin = bytes.NewReader(data)
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
	if !ok { return nil, fmt.Errorf("stub: not found: %s/%s", service, account) }
	return v, nil
}

func (s *stubKeychain) Write(service, account string, data []byte) error {
	s.data[keychainKey(service, account)] = append([]byte(nil), data...)
	return nil
}
```

- [ ] **Step 3: Test + commit**

```bash
go test ./cmd/multica-token-sync/ -run TestKeychain -v 2>&1 | tail -5
# Optional integration check:
MULTICA_KEYCHAIN_TEST=1 go test ./cmd/multica-token-sync/ -run TestMacOSKeychain -v 2>&1 | tail -5
git add server/cmd/multica-token-sync/{keychain,keychain_test}.go
git commit -m "feat(token-sync): Keychain abstraction (macOS security CLI + stub)"
```

---

## Task 3: Cluster reader

**Files:**
- Create: `server/cmd/multica-token-sync/cluster.go`
- Create: `server/cmd/multica-token-sync/cluster_test.go`

### Design notes

Use `clientcmd.NewNonInteractiveDeferredLoadingClientConfig` to load kubeconfig the same way `kubectl` does — respects `$KUBECONFIG`, `--context`, and the standard `~/.kube/config` fallback. Returns a `kubernetes.Interface` we can hand to the rest of the tool.

Reads the Secret with three keys we expect: `access_token`, `refresh_token`, `expires_at` (RFC3339). Failing/missing keys → return a descriptive error so launchd's log shows the operator what's wrong.

`BrokerState` is what we return — same struct as the broker's `TokenState` but lives in this package because we don't want a cross-binary import dependency.

### Implementation

- [ ] **Step 1: Failing test using fake clientset**

```go
package main

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestClusterReader_ReadBrokerState(t *testing.T) {
	exp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "multica-claude-oauth-broker", Namespace: "multica"},
		Data: map[string][]byte{
			"access_token":  []byte("ACCESS"),
			"refresh_token": []byte("REFRESH"),
			"expires_at":    []byte(exp.Format(time.RFC3339)),
		},
	}
	k := fake.NewSimpleClientset(sec)
	state, err := ReadBrokerState(context.Background(), k, "multica", "multica-claude-oauth-broker")
	if err != nil { t.Fatalf("ReadBrokerState: %v", err) }
	if state.AccessToken != "ACCESS" || state.RefreshToken != "REFRESH" {
		t.Errorf("state = %+v", state)
	}
	if !state.ExpiresAt.Equal(exp) {
		t.Errorf("expires = %v, want %v", state.ExpiresAt, exp)
	}
}
```

(Add similar tests for missing-secret + malformed-expires_at.)

- [ ] **Step 2: Implement `cluster.go`**

```go
package main

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type BrokerState struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// LoadClusterClient reads kubeconfig the same way kubectl does (KUBECONFIG env,
// then ~/.kube/config). If contextName is non-empty, overrides the current
// context.
func LoadClusterClient(contextName string) (kubernetes.Interface, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" { overrides.CurrentContext = contextName }
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	restCfg, err := cc.ClientConfig()
	if err != nil { return nil, fmt.Errorf("load kubeconfig: %w", err) }
	return kubernetes.NewForConfig(restCfg)
}

func ReadBrokerState(ctx context.Context, k kubernetes.Interface, namespace, name string) (*BrokerState, error) {
	sec, err := k.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil { return nil, fmt.Errorf("get secret %s/%s: %w", namespace, name, err) }
	state := &BrokerState{
		AccessToken:  string(sec.Data["access_token"]),
		RefreshToken: string(sec.Data["refresh_token"]),
	}
	if rawExp, ok := sec.Data["expires_at"]; ok && len(rawExp) > 0 {
		t, err := time.Parse(time.RFC3339, string(rawExp))
		if err != nil { return nil, fmt.Errorf("parse expires_at %q: %w", rawExp, err) }
		state.ExpiresAt = t
	}
	if state.AccessToken == "" || state.RefreshToken == "" {
		return nil, fmt.Errorf("secret %s/%s missing access_token or refresh_token (broker may not have reloaded yet)", namespace, name)
	}
	return state, nil
}
```

- [ ] **Step 3: Test + commit**

```bash
go test ./cmd/multica-token-sync/ -run TestClusterReader -v 2>&1 | tail -5
git add server/cmd/multica-token-sync/{cluster,cluster_test}.go
git commit -m "feat(token-sync): cluster reader (kubeconfig + broker Secret)"
```

---

## Task 4: Sync orchestration

**Files:**
- Create: `server/cmd/multica-token-sync/sync.go`
- Create: `server/cmd/multica-token-sync/sync_test.go`

### Design notes

Three steps per sync:

1. Read cluster state.
2. Build the JSON blob the local CLI expects (see schema below). Hard-code `scopes` and `subscriptionType` — these don't change per-rotation and aren't in the broker Secret.
3. Compute SHA-256 of the new blob and compare against SHA-256 of the existing Keychain blob. Skip the write if equal. Logs the resourceVersion + the "wrote/skipped" outcome.

If the existing Keychain entry is missing, treat that as "always write." Tools that haven't been bootstrapped need a first run to populate.

JSON schema written to Keychain (matches Claude Code's expected `.credentials.json` shape):

```json
{
  "claudeAiOauth": {
    "accessToken":      "sk-ant-oat01-…",
    "refreshToken":     "sk-ant-ort01-…",
    "expiresAt":        1780129288996,
    "scopes":           ["user:profile","user:inference","user:sessions:claude_code","user:mcp_servers"],
    "subscriptionType": "max"
  }
}
```

`expiresAt` is millis since epoch (matches what claude itself writes).

### Implementation

- [ ] **Step 1: Failing tests for transform + dedup**

```go
package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSync_WritesKeychainWhenMissing(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "multica"},
		Data: map[string][]byte{
			"access_token":  []byte("ACCESS"),
			"refresh_token": []byte("REFRESH"),
			"expires_at":    []byte("2026-06-01T00:00:00Z"),
		},
	}
	k := fake.NewSimpleClientset(sec)
	kc := &stubKeychain{data: map[string][]byte{}}
	cfg := &Config{Namespace: "multica", SecretName: "broker", KeychainService: "claude", KeychainAccount: "u"}
	res, err := SyncOnce(context.Background(), cfg, k, kc, discardLogger())
	if err != nil { t.Fatalf("SyncOnce: %v", err) }
	if !res.Wrote { t.Error("expected write on first sync") }

	// Verify Keychain payload shape.
	raw, _ := kc.Read("claude", "u")
	var got struct {
		ClaudeAiOauth struct {
			AccessToken      string   `json:"accessToken"`
			RefreshToken     string   `json:"refreshToken"`
			ExpiresAt        int64    `json:"expiresAt"`
			Scopes           []string `json:"scopes"`
			SubscriptionType string   `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &got); err != nil { t.Fatalf("payload not valid JSON: %v", err) }
	if got.ClaudeAiOauth.AccessToken != "ACCESS" || got.ClaudeAiOauth.RefreshToken != "REFRESH" {
		t.Errorf("payload tokens wrong: %+v", got.ClaudeAiOauth)
	}
	if got.ClaudeAiOauth.SubscriptionType != "max" {
		t.Errorf("subscriptionType = %q", got.ClaudeAiOauth.SubscriptionType)
	}
	if len(got.ClaudeAiOauth.Scopes) != 4 {
		t.Errorf("scopes = %v", got.ClaudeAiOauth.Scopes)
	}
}

func TestSync_SkipsWhenUnchanged(t *testing.T) {
	exp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "multica"},
		Data: map[string][]byte{
			"access_token": []byte("A"), "refresh_token": []byte("R"),
			"expires_at": []byte(exp.Format(time.RFC3339)),
		},
	}
	k := fake.NewSimpleClientset(sec)
	kc := &stubKeychain{data: map[string][]byte{}}
	cfg := &Config{Namespace: "multica", SecretName: "broker", KeychainService: "claude", KeychainAccount: "u"}

	// First sync: writes.
	r1, _ := SyncOnce(context.Background(), cfg, k, kc, discardLogger())
	if !r1.Wrote { t.Error("expected write on first") }
	// Second sync with no change: skips.
	r2, _ := SyncOnce(context.Background(), cfg, k, kc, discardLogger())
	if r2.Wrote { t.Error("expected no-op on second sync") }
}

func TestSync_DryRunDoesNotWrite(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "multica"},
		Data: map[string][]byte{
			"access_token": []byte("A"), "refresh_token": []byte("R"),
			"expires_at": []byte("2026-06-01T00:00:00Z"),
		},
	}
	k := fake.NewSimpleClientset(sec)
	kc := &stubKeychain{data: map[string][]byte{}}
	cfg := &Config{Namespace: "multica", SecretName: "broker", KeychainService: "claude", KeychainAccount: "u", DryRun: true}
	res, err := SyncOnce(context.Background(), cfg, k, kc, discardLogger())
	if err != nil { t.Fatalf("SyncOnce: %v", err) }
	if res.Wrote { t.Error("dry-run must not write") }
	if _, err := kc.Read("claude", "u"); err == nil { t.Error("keychain should still be empty") }
}
```

- [ ] **Step 2: Implement `sync.go`**

```go
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/client-go/kubernetes"
)

// keychainPayload mirrors Claude Code's expected .credentials.json shape.
type keychainPayload struct {
	ClaudeAiOauth oauthBlob `json:"claudeAiOauth"`
}

type oauthBlob struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"` // millis since epoch
	Scopes           []string `json:"scopes"`
	SubscriptionType string   `json:"subscriptionType"`
}

// claude's scopes are stable across rotations — hard-coded here, matching the
// values the OAuth-constants extractor pulls from the binary.
var defaultScopes = []string{
	"user:profile",
	"user:inference",
	"user:sessions:claude_code",
	"user:mcp_servers",
}

type SyncResult struct {
	Wrote          bool
	OldFingerprint string // sha256 hex (empty if no prior entry)
	NewFingerprint string // sha256 hex
}

// SyncOnce executes the full pipeline once and returns the outcome.
func SyncOnce(ctx context.Context, cfg *Config, k kubernetes.Interface, kc Keychain, logger *slog.Logger) (*SyncResult, error) {
	state, err := ReadBrokerState(ctx, k, cfg.Namespace, cfg.SecretName)
	if err != nil { return nil, err }

	payload := keychainPayload{ClaudeAiOauth: oauthBlob{
		AccessToken:      state.AccessToken,
		RefreshToken:     state.RefreshToken,
		ExpiresAt:        state.ExpiresAt.UnixMilli(),
		Scopes:           defaultScopes,
		SubscriptionType: "max",
	}}
	newBytes, err := json.Marshal(payload)
	if err != nil { return nil, fmt.Errorf("marshal: %w", err) }
	newFP := fingerprint(newBytes)

	result := &SyncResult{NewFingerprint: newFP}
	if existing, err := kc.Read(cfg.KeychainService, cfg.KeychainAccount); err == nil {
		result.OldFingerprint = fingerprint(existing)
		if result.OldFingerprint == newFP {
			logger.Info("keychain already current", "fingerprint", newFP)
			return result, nil
		}
		logger.Info("keychain out of date, rotating", "from", result.OldFingerprint, "to", newFP)
	} else {
		logger.Info("keychain entry missing, creating", "to", newFP)
	}

	if cfg.DryRun {
		logger.Info("dry-run; not writing keychain")
		return result, nil
	}
	if err := kc.Write(cfg.KeychainService, cfg.KeychainAccount, newBytes); err != nil {
		return nil, fmt.Errorf("keychain write: %w", err)
	}
	result.Wrote = true
	logger.Info("keychain updated",
		"service", cfg.KeychainService,
		"account", cfg.KeychainAccount,
		"expires_at", state.ExpiresAt.Format(time.RFC3339))
	return result, nil
}

// SyncLoop runs SyncOnce on an interval until ctx is cancelled. Errors are
// logged but never terminate the loop (transient cluster failures shouldn't
// kill a long-running daemon).
func SyncLoop(ctx context.Context, cfg *Config, k kubernetes.Interface, kc Keychain, logger *slog.Logger) {
	t := time.NewTicker(cfg.Interval); defer t.Stop()
	if _, err := SyncOnce(ctx, cfg, k, kc, logger); err != nil {
		logger.Error("initial sync failed", "error", err)
	}
	for {
		select {
		case <-ctx.Done(): return
		case <-t.C:
			if _, err := SyncOnce(ctx, cfg, k, kc, logger); err != nil {
				logger.Error("sync tick failed", "error", err)
			}
		}
	}
}

func fingerprint(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 3: Wire `run` in main.go**

Replace the stub `run` from Task 1:

```go
func run(cfg *Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	k, err := LoadClusterClient(cfg.Context)
	if err != nil { return err }
	kc := &macOSKeychain{}

	if cfg.Once {
		_, err := SyncOnce(ctx, cfg, k, kc, logger)
		return err
	}
	SyncLoop(ctx, cfg, k, kc, logger)
	return nil
}
```

- [ ] **Step 4: Test + commit**

```bash
go test ./cmd/multica-token-sync/ -v 2>&1 | tail -15
git add server/cmd/multica-token-sync/{sync,sync_test}.go server/cmd/multica-token-sync/main.go
git commit -m "feat(token-sync): sync orchestration (read → diff → write + dry-run)"
```

---

## Task 5: launchd plist + install script

**Files:**
- Create: `packaging/launchd/com.multica.token-sync.plist`
- Create: `packaging/launchd/install.sh`

### Design notes

**Where to install:**
- Binary: `/usr/local/bin/multica-token-sync` (user-installed, no root needed for `~/Library/LaunchAgents`).
- Plist: `~/Library/LaunchAgents/com.multica.token-sync.plist` (user agent, runs as the operator's UID — required because the Keychain it touches is the operator's).

**Schedule:** `StartInterval = 1800` (30 min). The cluster access token is good for hours; a 30-min cadence gives a comfortable margin without hammering the cluster.

**Log destination:** `~/Library/Logs/multica-token-sync.log`. Rotated by launchd's `StandardErrorPath` semantics (writes append until you `> log` it).

**Reload on plist change:** `launchctl bootstrap` (modern) replaces the older `launchctl load -w`. The installer handles bootstrap + bootout cycle.

### Implementation

- [ ] **Step 1: Plist**

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.multica.token-sync</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/multica-token-sync</string>
    <string>--once</string>
  </array>
  <key>StartInterval</key><integer>1800</integer>
  <key>RunAtLoad</key><true/>
  <key>StandardOutPath</key><string>__USER_HOME__/Library/Logs/multica-token-sync.log</string>
  <key>StandardErrorPath</key><string>__USER_HOME__/Library/Logs/multica-token-sync.log</string>
</dict>
</plist>
```

(The `__USER_HOME__` placeholder is rewritten by the installer.)

- [ ] **Step 2: Installer / uninstaller**

```bash
#!/usr/bin/env bash
# packaging/launchd/install.sh — install or uninstall the multica-token-sync
# launchd agent on macOS. Run as the operator (no sudo).

set -euo pipefail

CMD="${1:-install}"
LABEL="com.multica.token-sync"
PLIST_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/${LABEL}.plist"
PLIST_DST="$HOME/Library/LaunchAgents/${LABEL}.plist"
BIN_DST="/usr/local/bin/multica-token-sync"

ensure_binary() {
  if [[ ! -x "$BIN_DST" ]]; then
    echo "error: $BIN_DST not found or not executable" >&2
    echo "Build with: cd server && go build -o $BIN_DST ./cmd/multica-token-sync" >&2
    exit 1
  fi
}

case "$CMD" in
  install)
    ensure_binary
    mkdir -p "$HOME/Library/Logs" "$HOME/Library/LaunchAgents"
    sed "s|__USER_HOME__|$HOME|g" "$PLIST_SRC" > "$PLIST_DST"
    launchctl bootout "gui/$(id -u)/${LABEL}" 2>/dev/null || true
    launchctl bootstrap "gui/$(id -u)" "$PLIST_DST"
    echo "Installed. Status: $(launchctl print "gui/$(id -u)/${LABEL}" 2>&1 | head -3 | tail -1)"
    echo "Logs: $HOME/Library/Logs/multica-token-sync.log"
    ;;
  uninstall)
    launchctl bootout "gui/$(id -u)/${LABEL}" 2>/dev/null || true
    rm -f "$PLIST_DST"
    echo "Uninstalled."
    ;;
  status)
    launchctl print "gui/$(id -u)/${LABEL}" 2>&1 | head -20
    ;;
  *)
    echo "usage: $0 [install|uninstall|status]" >&2
    exit 2
    ;;
esac
```

- [ ] **Step 3: Smoke check (don't install yet)**

```bash
chmod +x packaging/launchd/install.sh
# Validate the plist is well-formed:
plutil packaging/launchd/com.multica.token-sync.plist
git add packaging/launchd/
git commit -m "feat(token-sync): launchd agent + installer"
```

---

## Task 6: Local smoke test

- [ ] **Step 1: Build, dry-run against real cluster**

```bash
cd /Users/cjs/dev/multica/server
go build -o /tmp/multica-token-sync ./cmd/multica-token-sync
/tmp/multica-token-sync --dry-run --verbose 2>&1 | tail -10
```

Expected: logs show "keychain out of date, rotating from <hash> to <hash>" + "dry-run; not writing keychain". No error.

- [ ] **Step 2: Real run, verify Keychain mutated**

```bash
# Snapshot the current Keychain entry's first 12 bytes (token prefix).
BEFORE=$(security find-generic-password -s 'Claude Code-credentials' -w | jq -r .claudeAiOauth.accessToken | head -c 30)
echo "before: ${BEFORE}..."

/tmp/multica-token-sync --once --verbose 2>&1 | tail -5

AFTER=$(security find-generic-password -s 'Claude Code-credentials' -w | jq -r .claudeAiOauth.accessToken | head -c 30)
echo "after:  ${AFTER}..."

# Verify local claude CLI accepts the new credentials.
echo "say ok" | claude -p --model claude-haiku-4-5-20251001 --output-format text
```

Expected: prefix may or may not change (depends on whether broker has rotated since your last `/login`), but `claude -p` returns "ok" with no auth errors.

- [ ] **Step 3: Install launchd agent + verify next-tick fires**

```bash
sudo install -m 0755 /tmp/multica-token-sync /usr/local/bin/multica-token-sync
./packaging/launchd/install.sh install
sleep 2
./packaging/launchd/install.sh status
tail -20 ~/Library/Logs/multica-token-sync.log
```

Expected: status shows the agent is loaded; log shows the initial RunAtLoad sync ran successfully.

---

## Task 7: Operator docs

**Files:**
- Modify: `packaging/README.md`

Add a "Local token sync (macOS)" section right after the "Claude OAuth broker" section. Cover:

- What it is (eliminates `/login` ceremonies, follows the broker as authoritative).
- One-line install:
  ```bash
  cd server && go build -o /tmp/multica-token-sync ./cmd/multica-token-sync
  sudo install -m 0755 /tmp/multica-token-sync /usr/local/bin/multica-token-sync
  ./packaging/launchd/install.sh install
  ```
- Verify install: `./packaging/launchd/install.sh status` + checking `~/Library/Logs/multica-token-sync.log`.
- Manual force-sync: `multica-token-sync --once --verbose`.
- Disable / uninstall: `./packaging/launchd/install.sh uninstall`.
- Caveat: a long-running interactive `claude` session holds tokens in memory; broker rotations land at the next CLI invocation, not mid-session.

- [ ] **Commit**

```bash
git add packaging/README.md
git commit -m "docs(packaging): multica-token-sync operator guide"
```

---

## Task 8: Final regression

- [ ] `cd server && go vet ./... && go test ./cmd/multica-token-sync/ -v` clean.
- [ ] `plutil packaging/launchd/com.multica.token-sync.plist` reports OK.
- [ ] One full sync cycle observed in `~/Library/Logs/multica-token-sync.log` after install.
- [ ] `claude -p "echo ok"` against the local CLI succeeds (proves the synced Keychain entry is valid).

---

## What's next

- **Linux backend** — if/when an operator runs `claude` locally on Linux, add `secret-tool` (libsecret) as a second `Keychain` implementation behind the existing interface. Trivial extension.
- **Windows backend** — DPAPI / `wincred` could be a third backend. Same shape.
- **`--watch` mode** — replace the polling loop with a K8s `watch` on the Secret to react instantly to rotations. Probably not worth the complexity given 30-min polling is fine for hour-long token lifetimes.
- **Distribute as a Homebrew formula** — once Multica is open-source enough, a tap that installs both the binary and the launchd plist in one command.
- **Audit log** — write a tiny line to `~/Library/Logs/multica-token-sync.log` per Keychain mutation summarizing fingerprint change + cluster resourceVersion. Useful for forensics if the operator ever loses access and wants to know when it happened.

## What this enables

- One OAuth grant, one writer (the cluster broker), N read-only followers (the operator's laptops). The single-grant collision the broker README currently flags as a long-term operational headache is closed.
- Operator can `/login` once during initial bootstrap and never again, even across broker rotations.
- Multi-machine support comes for free — each laptop polls independently, converges to the broker, doesn't need to coordinate with other laptops.
