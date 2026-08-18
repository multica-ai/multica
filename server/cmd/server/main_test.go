package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisClientName(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		suffix   string
		want     string
	}{
		{"empty_suffix_returns_existing", "multica-api:store", "", "multica-api:store"},
		{"empty_existing_uses_default_prefix", "", "store", "multica-api:store"},
		{"both_set_joins_with_colon", "custom", "store", "custom:store"},
		{"empty_both_returns_empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redisClientName(tt.existing, tt.suffix)
			if got != tt.want {
				t.Errorf("redisClientName(%q, %q) = %q, want %q", tt.existing, tt.suffix, got, tt.want)
			}
		})
	}
}

func TestChannelLeaseRedisURLFromEnvPrefersDedicatedInstance(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://shared:6379/0")
	t.Setenv("CHANNEL_WS_LEASE_REDIS_URL", "redis://leases:6379/0")
	if got := channelLeaseRedisURLFromEnv(); got != "redis://leases:6379/0" {
		t.Fatalf("channel lease Redis URL = %q", got)
	}
}

func TestChannelLeaseRedisURLFromEnvFallsBackToSharedRedis(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://shared:6379/0")
	t.Setenv("CHANNEL_WS_LEASE_REDIS_URL", "")
	if got := channelLeaseRedisURLFromEnv(); got != "redis://shared:6379/0" {
		t.Fatalf("channel lease Redis URL = %q", got)
	}
}

func TestNewNamedRedisClient_SetsClientName(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "")
	base := &redis.Options{Addr: "localhost:6379"}
	client := newNamedRedisClient(base, "store")
	defer client.Close()

	opts := client.Options()
	if opts.ClientName != "multica-api:store" {
		t.Errorf("ClientName = %q, want %q", opts.ClientName, "multica-api:store")
	}
}

func TestNewNamedRedisClient_DisableClientName(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "true")
	base := &redis.Options{Addr: "localhost:6379"}
	client := newNamedRedisClient(base, "store")
	defer client.Close()

	opts := client.Options()
	if opts.ClientName != "" {
		t.Errorf("ClientName = %q, want empty when REDIS_DISABLE_CLIENT_NAME=true", opts.ClientName)
	}
}

func TestNewNamedRedisClient_DisableClientName_ClearsPreExistingName(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "true")
	// Simulate REDIS_URL with ?client_name=foo — ParseURL sets ClientName.
	base := &redis.Options{Addr: "localhost:6379", ClientName: "foo"}
	client := newNamedRedisClient(base, "store")
	defer client.Close()

	opts := client.Options()
	if opts.ClientName != "" {
		t.Errorf("ClientName = %q, want empty: REDIS_DISABLE_CLIENT_NAME must clear pre-existing name from URL", opts.ClientName)
	}
}

func TestNewNamedRedisClient_DisableClientName_InvalidValue(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "not-a-bool")
	base := &redis.Options{Addr: "localhost:6379"}
	client := newNamedRedisClient(base, "store")
	defer client.Close()

	opts := client.Options()
	// Invalid value falls back to default (false), so ClientName IS set
	if opts.ClientName != "multica-api:store" {
		t.Errorf("ClientName = %q, want %q (invalid env should fall back to naming enabled)", opts.ClientName, "multica-api:store")
	}
}

// mainSourceFile is parsed by TestMainUsesRouterOwnedBackgroundServices. The
// test asserts the markers below are actually present so a future move of the
// background-worker wiring fails loudly instead of vacuously passing on a walk
// that matched nothing.
const mainSourceFile = "main.go"

// backgroundServiceConstructors must never be called from main(): they build a
// TaskService / AutopilotService that the router has not finished wiring.
var backgroundServiceConstructors = []string{
	"service.NewTaskService",
	"service.NewAutopilotService",
}

// TestMainUsesRouterOwnedBackgroundServices guards the process wiring behind
// scheduled Autopilot dispatch and the runtime sweeper: both must take the
// router's fully wired services off *handler.Handler instead of constructing
// their own.
//
// The router — not NewTaskService — assigns h.TaskService.EmptyClaim. A second
// TaskService built inside main() therefore has EmptyClaim == nil, and because
// EmptyClaimCache is deliberately nil-safe the missed invalidation is silent:
// a scheduled dispatch still delivers the daemon wakeup while the claim path's
// cached "no queued task" verdict survives, so an idle runtime keeps returning
// empty claims until EmptyClaimCacheTTL expires.
//
// This parses main.go instead of asserting on backgroundServices' return
// values. A value-level assertion only proves the helper hands back h's fields,
// which stays true even when main() stops calling it and constructs its own
// services again — i.e. it cannot fail on the exact regression it names.
func TestMainUsesRouterOwnedBackgroundServices(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, mainSourceFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", mainSourceFile, err)
	}

	var mainFunc *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == "main" {
			mainFunc = fn
			break
		}
	}
	if mainFunc == nil || mainFunc.Body == nil {
		t.Fatalf("no func main() with a body found in %s — this guard must be pointed at the real wiring", mainSourceFile)
	}

	// Resolve the variable holding the router's *handler.Handler rather than
	// hardcoding "h": the argument identity is the whole point of the guard,
	// and reading it off the NewRouterWithOptions assignment keeps a rename of
	// that variable from silently weakening the check below.
	routerHandlerVar := routerHandlerIdent(mainFunc.Body)
	if routerHandlerVar == "" {
		t.Fatalf("could not find the *handler.Handler result of NewRouterWithOptions in main() — re-point this guard at the current router wiring")
	}

	forbidden := make(map[string]bool, len(backgroundServiceConstructors))
	for _, name := range backgroundServiceConstructors {
		forbidden[name] = true
	}

	var (
		offenders          []string
		badReuse           []string
		reusesRouter       bool
		registersScheduler bool
	)
	ast.Inspect(mainFunc.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch callee := calleeName(call); {
		case forbidden[callee]:
			offenders = append(offenders, fmt.Sprintf("%s at %s", callee, fset.Position(call.Pos())))
		case callee == "backgroundServices":
			// Matching the callee name alone would accept
			// backgroundServices(nil) — which compiles, reuses nothing, and
			// panics at startup. The argument must be the router's handler.
			if len(call.Args) == 1 {
				if arg, ok := call.Args[0].(*ast.Ident); ok && arg.Name == routerHandlerVar {
					reusesRouter = true
					return true
				}
			}
			badReuse = append(badReuse, fmt.Sprintf("%s at %s", exprText(fset, call), fset.Position(call.Pos())))
		case callee == "scheduler.AutopilotScheduleDispatchJob":
			registersScheduler = true
		}
		return true
	})

	// Anti-vacuity: the scheduled-dispatch registration is what makes the
	// unwired-service hazard reachable. If it moves out of main(), this test
	// no longer covers the path it claims to and must fail rather than pass.
	if !registersScheduler {
		t.Fatalf("scheduler.AutopilotScheduleDispatchJob is no longer registered in main() — re-point this guard at wherever the schedule job is now wired")
	}
	if len(offenders) > 0 {
		t.Errorf("main() constructs its own background services (%s); take them from the router via backgroundServices(%s) so EmptyClaim and the rest of the router wiring come along", strings.Join(offenders, ", "), routerHandlerVar)
	}
	if len(badReuse) > 0 {
		t.Errorf("main() calls backgroundServices with something other than the router handler %q (%s); background workers must reuse the services the router finished wiring", routerHandlerVar, strings.Join(badReuse, ", "))
	}
	if !reusesRouter {
		t.Errorf("main() no longer calls backgroundServices(%s); background workers must reuse the router-owned TaskService/AutopilotService", routerHandlerVar)
	}
}

// routerHandlerIdent returns the name of the variable that receives the
// *handler.Handler from NewRouterWithOptions (the `h` in `r, h := ...`), or ""
// when that assignment is no longer recognizable.
func routerHandlerIdent(body *ast.BlockStmt) string {
	var name string
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 || len(assign.Lhs) != 2 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok || calleeName(call) != "NewRouterWithOptions" {
			return true
		}
		if ident, ok := assign.Lhs[1].(*ast.Ident); ok {
			name = ident.Name
			return false
		}
		return true
	})
	return name
}

// exprText renders an expression back to source for error messages.
func exprText(fset *token.FileSet, expr ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, expr); err != nil {
		return "<unprintable expression>"
	}
	return buf.String()
}

// calleeName renders a call's target as "pkg.Func" or "Func" for matching.
func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if pkg, ok := fn.X.(*ast.Ident); ok {
			return pkg.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	}
	return ""
}

// TestNormalizeServerVersion covers the router-config wiring path (not just
// a hand-set handler.Config field): an unstamped "dev" build must not leak
// into /api/config's server_version, or the Help popover would render
// "Server version dev" instead of hiding the row.
func TestNormalizeServerVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"unstamped_dev_default_becomes_empty", "dev", ""},
		{"already_empty_stays_empty", "", ""},
		{"stamped_release_tag_passes_through", "v0.4.0", "v0.4.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeServerVersion(tt.in); got != tt.want {
				t.Errorf("normalizeServerVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		def   bool
		want  bool
	}{
		{"empty_returns_default_false", "TEST_ENV_BOOL_1", "", false, false},
		{"empty_returns_default_true", "TEST_ENV_BOOL_2", "", true, true},
		{"true_string", "TEST_ENV_BOOL_3", "true", false, true},
		{"false_string", "TEST_ENV_BOOL_4", "false", true, false},
		{"one_is_true", "TEST_ENV_BOOL_5", "1", false, true},
		{"zero_is_false", "TEST_ENV_BOOL_6", "0", true, false},
		{"invalid_returns_default", "TEST_ENV_BOOL_7", "maybe", false, false},
		{"invalid_returns_default_true", "TEST_ENV_BOOL_8", "maybe", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != "" {
				t.Setenv(tt.key, tt.value)
			} else {
				os.Unsetenv(tt.key)
			}
			got := envBool(tt.key, tt.def)
			if got != tt.want {
				t.Errorf("envBool(%q, %v) = %v, want %v", tt.key, tt.def, got, tt.want)
			}
		})
	}
}

func TestEnvNonNegativeDuration(t *testing.T) {
	tests := []struct {
		name  string
		value string
		def   time.Duration
		want  time.Duration
	}{
		{name: "unset returns default", def: 3 * time.Second, want: 3 * time.Second},
		{name: "empty returns default", value: "", def: 2 * time.Second, want: 2 * time.Second},
		{name: "bare zero disables hold", value: "0", def: time.Second, want: 0},
		{name: "zero duration disables hold", value: "0s", def: time.Second, want: 0},
		{name: "positive duration", value: "5m", want: 5 * time.Minute},
		{name: "invalid returns default", value: "later", def: 4 * time.Second, want: 4 * time.Second},
		{name: "negative returns default", value: "-1s", def: 4 * time.Second, want: 4 * time.Second},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_NON_NEGATIVE_DURATION_" + strconv.Itoa(i)
			if tt.name == "unset returns default" {
				os.Unsetenv(key)
			} else {
				t.Setenv(key, tt.value)
			}
			if got := envNonNegativeDuration(key, tt.def); got != tt.want {
				t.Fatalf("envNonNegativeDuration(%q, %s) = %s, want %s", key, tt.def, got, tt.want)
			}
		})
	}
}

func TestEnvNonNegativeInt(t *testing.T) {
	tests := []struct {
		name  string
		value string
		def   int
		want  int
	}{
		{name: "unset returns default", def: 10, want: 10},
		{name: "zero disables gate", value: "0", def: 10, want: 0},
		{name: "positive override", value: "25", def: 10, want: 25},
		{name: "invalid returns default", value: "many", def: 10, want: 10},
		{name: "negative returns default", value: "-1", def: 10, want: 10},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_NON_NEGATIVE_INT_" + strconv.Itoa(i)
			if tt.name == "unset returns default" {
				os.Unsetenv(key)
			} else {
				t.Setenv(key, tt.value)
			}
			if got := envNonNegativeInt(key, tt.def); got != tt.want {
				t.Fatalf("envNonNegativeInt(%q, %d) = %d, want %d", key, tt.def, got, tt.want)
			}
		})
	}
}

func TestHoldBeforeShutdown(t *testing.T) {
	const hold = 10 * time.Millisecond
	started := time.Now()
	holdBeforeShutdown(syscall.SIGTERM, nil, hold)
	if elapsed := time.Since(started); elapsed < hold {
		t.Fatalf("holdBeforeShutdown returned after %s, before configured hold %s", elapsed, hold)
	}
}

func TestHoldBeforeShutdownInterruptedBySecondSignal(t *testing.T) {
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGINT
	done := make(chan struct{})

	go func() {
		holdBeforeShutdown(syscall.SIGTERM, signals, time.Minute)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("holdBeforeShutdown did not return after a second signal")
	}
	if len(signals) != 0 {
		t.Fatal("holdBeforeShutdown did not consume the second signal")
	}
}

func TestHoldBeforeShutdownDisabled(t *testing.T) {
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGINT
	holdBeforeShutdown(syscall.SIGTERM, signals, 0)
	if len(signals) != 1 {
		t.Fatal("disabled hold should not consume another signal")
	}
}
