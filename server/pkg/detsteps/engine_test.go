package detsteps

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/dettools"
	"github.com/traefik/yaegi/stdlib"
)

func run(t *testing.T, src string, input map[string]any) dettools.Result {
	t.Helper()
	return Run(context.Background(), src, input, Options{Timeout: 3 * time.Second})
}

const okStep = `
package step

import "strings"

func Run(input map[string]any) map[string]any {
	name, _ := input["name"].(string)
	return map[string]any{
		"status":       "ok",
		"summary":      "hello " + strings.ToUpper(name),
		"machine_data": map[string]any{"length": len(name)},
	}
}
`

func TestRunOK(t *testing.T) {
	res := run(t, okStep, map[string]any{"name": "world"})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	if res.Summary != "hello WORLD" {
		t.Errorf("summary = %q, want %q", res.Summary, "hello WORLD")
	}
	if got := res.MachineData["length"]; got != 5 {
		t.Errorf("machine_data.length = %v, want 5", got)
	}
}

func TestRunDefaultsStatusToOK(t *testing.T) {
	src := `package step
func Run(input map[string]any) map[string]any {
	return map[string]any{"summary": "no explicit status"}
}`
	res := run(t, src, nil)
	if res.Status != dettools.StatusOK {
		t.Errorf("status = %q, want ok (missing status should default to ok)", res.Status)
	}
}

func TestRunErrorStatusMapsThrough(t *testing.T) {
	src := `package step
func Run(input map[string]any) map[string]any {
	return map[string]any{"status": "error", "error_code": "POLICY_FAILURE", "summary": "nope"}
}`
	res := run(t, src, nil)
	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodePolicyFailure {
		t.Fatalf("got status=%q code=%q, want error/POLICY_FAILURE", res.Status, res.ErrorCode)
	}
}

func TestRunCompileError(t *testing.T) {
	res := run(t, `package step
func Run(input map[string]any) map[string]any { this is not go }`, nil)
	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodeInvalidInput {
		t.Fatalf("got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}
	if !strings.Contains(res.Summary, "compile error") {
		t.Errorf("summary = %q, want it to mention a compile error", res.Summary)
	}
}

func TestRunMissingEntrypoint(t *testing.T) {
	res := run(t, `package step
func NotRun(input map[string]any) map[string]any { return nil }`, nil)
	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodeInvalidInput {
		t.Fatalf("got status=%q code=%q, want error/INVALID_INPUT for missing entrypoint", res.Status, res.ErrorCode)
	}
}

func TestRunWrongSignature(t *testing.T) {
	res := run(t, `package step
func Run(x int) string { return "" }`, nil)
	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodeInvalidInput {
		t.Fatalf("got status=%q code=%q, want error/INVALID_INPUT for wrong signature", res.Status, res.ErrorCode)
	}
}

func TestRunPanicIsContained(t *testing.T) {
	src := `package step
func Run(input map[string]any) map[string]any {
	var s []int
	_ = s[5] // index out of range -> panic
	return nil
}`
	res := run(t, src, nil)
	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodeInternal {
		t.Fatalf("got status=%q code=%q, want error/INTERNAL_ERROR for a panicking step", res.Status, res.ErrorCode)
	}
}

// TestSandboxBlocksHostAccess is the security contract: a step cannot import a
// host-touching package. None of these are in the whitelist and the empty source
// filesystem stops yaegi from interpreting them from GOROOT, so every import must
// fail rather than run.
func TestSandboxBlocksHostAccess(t *testing.T) {
	for _, pkg := range []string{"os", "os/exec", "net/http", "io", "syscall", "path/filepath"} {
		src := "package step\nimport \"" + pkg + "\"\nfunc Run(input map[string]any) map[string]any { return nil }"
		res := run(t, src, nil)
		if res.Status != dettools.StatusError {
			t.Errorf("import %q was NOT blocked — sandbox leak", pkg)
		}
	}
}

func TestRunTimeout(t *testing.T) {
	src := `package step
func Run(input map[string]any) map[string]any {
	for { _ = 1 }
}`
	res := Run(context.Background(), src, nil, Options{Timeout: 200 * time.Millisecond})
	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodeTimeout {
		t.Fatalf("got status=%q code=%q, want error/TIMEOUT for an infinite loop", res.Status, res.ErrorCode)
	}
}

// TestWhitelistedPackagesResolve guards against a typo or a yaegi version bump
// dropping a package we advertise as available: every allowed package must
// resolve to a real symbol set.
func TestWhitelistedPackagesResolve(t *testing.T) {
	for _, pkg := range allowedPackages {
		key := symbolKey(pkg)
		if _, ok := stdlib.Symbols[key]; !ok {
			t.Errorf("allowed package %q (key %q) is not in yaegi stdlib.Symbols", pkg, key)
		}
	}
}
