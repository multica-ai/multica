// Package detsteps executes user-authored deterministic Go "steps" at runtime
// through an embedded, sandboxed Go interpreter (yaegi). Unlike the compiled
// dettools handlers, a step's source lives in workspace data and is written and
// tested from the web UI, so it must run without recompiling the multica binary
// and without granting the code access to the host.
//
// The interpreter is loaded with a whitelist of pure, deterministic stdlib
// packages only (see symbols.go) — no os, os/exec, io, net*, syscall, runtime,
// reflect, or unsafe — so a step can compute over its JSON input but cannot read
// files, spawn processes, or reach the network. Every failure mode (compile
// error, missing/mismatched entrypoint, panic in user code, timeout) is returned
// as a dettools error Result with a stable code; Run never panics into the
// caller. The returned envelope is the exact dettools.Result contract the
// compiled tool plane uses, so a step and a built-in tool are indistinguishable
// to the agent that eventually calls them.
package detsteps

import (
	"context"
	"io/fs"
	"time"

	"github.com/multica-ai/multica/server/pkg/dettools"
	"github.com/traefik/yaegi/interp"
)

// emptyFS is handed to the interpreter as its source filesystem so that an
// import can ONLY be satisfied by the whitelisted Use() symbols — never by
// yaegi falling back to interpreting a package from GOROOT/GOPATH source on the
// host. Without this, a host with Go source present could let a step import a
// package the whitelist omits.
type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

// The contract a step's source must satisfy: a package named `step` exposing
// `func Run(input map[string]any) map[string]any`. The returned map is mapped to
// the dettools.Result envelope by mapToResult.
const (
	entrypointPackage = "step"
	entrypointFunc    = "Run"
	entrypointSymbol  = entrypointPackage + "." + entrypointFunc
)

// DefaultTimeout bounds a single step execution. yaegi cannot be preempted, so
// the timeout returns control to the caller while the offending goroutine is
// abandoned — acceptable for the author/test loop; a hardened agent-time
// execution path would run each step in an isolated process.
const DefaultTimeout = 5 * time.Second

// Options configures one Run.
type Options struct {
	// Timeout overrides DefaultTimeout when > 0.
	Timeout time.Duration
}

// Run interprets source, invokes its Run(input) entrypoint in the sandbox, and
// returns the resulting dettools.Result. It is safe against arbitrary user code:
// compile failures, signature mismatches, and panics all become error Results.
func Run(ctx context.Context, source string, input map[string]any, opts Options) dettools.Result {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	done := make(chan dettools.Result, 1) // buffered so an abandoned goroutine can exit
	go func() {
		done <- runOnce(source, input)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return dettools.Errf(dettools.CodeTimeout, "step cancelled: %v", ctx.Err())
	case <-timer.C:
		return dettools.Errf(dettools.CodeTimeout, "step exceeded the %s time limit", timeout)
	case r := <-done:
		return r
	}
}

// runOnce performs the interpret-and-call cycle. The deferred recover converts a
// panic inside interpreted user code into an INTERNAL_ERROR Result instead of
// crashing the server process.
func runOnce(source string, input map[string]any) (res dettools.Result) {
	defer func() {
		if r := recover(); r != nil {
			res = dettools.Errf(dettools.CodeInternal, "step panicked during execution: %v", r)
		}
	}()

	i := interp.New(interp.Options{SourcecodeFilesystem: emptyFS{}})
	if err := i.Use(sandboxSymbols()); err != nil {
		return dettools.Errf(dettools.CodeInternal, "initialize interpreter: %v", err)
	}

	if _, err := i.Eval(source); err != nil {
		return dettools.Errf(dettools.CodeInvalidInput, "compile error: %v", err)
	}

	v, err := i.Eval(entrypointSymbol)
	if err != nil {
		return dettools.Errf(dettools.CodeInvalidInput,
			"missing entrypoint: define `func %s(input map[string]any) map[string]any` in `package %s` (%v)",
			entrypointFunc, entrypointPackage, err)
	}

	fn, ok := v.Interface().(func(map[string]any) map[string]any)
	if !ok {
		return dettools.Errf(dettools.CodeInvalidInput,
			"entrypoint %s has the wrong signature; it must be `func(input map[string]any) map[string]any`",
			entrypointSymbol)
	}

	if input == nil {
		input = map[string]any{}
	}
	return mapToResult(fn(input))
}

// mapToResult maps the step's returned map onto the dettools.Result envelope.
// Missing keys degrade gracefully: an absent status defaults to "ok" so a step
// that just returns data is treated as success.
func mapToResult(m map[string]any) dettools.Result {
	if m == nil {
		return dettools.Errf(dettools.CodeInternal, "step returned a nil result map")
	}
	status, _ := m["status"].(string)
	if status == "" {
		status = dettools.StatusOK
	}
	summary, _ := m["summary"].(string)
	code, _ := m["error_code"].(string)
	retryable, _ := m["retryable"].(bool)
	md, _ := m["machine_data"].(map[string]any)

	return dettools.Result{
		Status:      status,
		Summary:     summary,
		MachineData: md,
		ErrorCode:   code,
		Retryable:   retryable,
	}
}
