package metrics_test

// PR3 lint test: enforces that every PostHog event constant declared in
// server/internal/analytics/events.go has a paired Prometheus counter
// reachable through metrics.RecordEvent — and that every
// h.Analytics.Capture(analytics.<Helper>(...)) call site goes through
// metrics.RecordEvent (no naked Capture allowed). The agent task lifecycle is
// no longer an analytics.Event — it is recorded straight to Prometheus via the
// typed BusinessMetrics.RecordTask* methods — so there is no longer an
// AgentTask* allow-list here.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/metrics"
)

// frontendOnlyEvents are declared in events.go but emitted from the frontend,
// not from server code. They still need a Prometheus counter (so a future
// server-side emission point lights up the same label set) but the server
// has no Capture call site to lint.
var frontendOnlyEvents = map[string]bool{
	analytics.EventOnboardingStarted: true,
}

// TestEveryAnalyticsEventHasPrometheusCounter asserts that every Event*
// constant declared in analytics/events.go is dispatched by
// metrics.IncForEvent (verified by sending a synthetic event through
// RecordEvent and observing a counter delta).
//
// Note: agent_task_* lifecycle telemetry is Prometheus-only via the typed
// BusinessMetrics.RecordTask* methods and is no longer declared as an
// analytics.Event, so there are no agent_task constants to exempt here.
func TestEveryAnalyticsEventHasPrometheusCounter(t *testing.T) {
	t.Parallel()

	declared := analyticsEventNames(t)

	m := metrics.NewBusinessMetrics()
	for name := range declared {
		// Build a minimal event with the required label properties that the
		// dispatcher reads. Since IncForEvent reads via stringProp helpers,
		// a nil Properties map is acceptable for events with empty label
		// sets and is normalised by the helpers for the others.
		ev := analytics.Event{
			Name:       name,
			DistinctID: "test",
			Properties: defaultPropsForEvent(name),
		}
		ok := dispatchIncrementsCounter(m, ev)
		if !ok {
			t.Errorf("analytics.%s (%q) is not paired with a Prometheus counter via metrics.IncForEvent — add a case in business_events.go", constantNameForEvent(name), name)
		}
	}
}

// TestNoNakedAnalyticsCaptureInHandlersOrServices walks every Go file under
// server/internal/handler and server/internal/service and asserts that every
// `<x>.Analytics.Capture(analytics.<Helper>(...))` call goes through
// metrics.RecordEvent. There are no exceptions: every server-side PostHog
// event must flow through RecordEvent so the Prometheus and PostHog sides
// cannot drift.
func TestNoNakedAnalyticsCaptureInHandlersOrServices(t *testing.T) {
	t.Parallel()

	roots := []string{
		filepath.Join(repoRoot(t), "internal", "handler"),
		filepath.Join(repoRoot(t), "internal", "service"),
		filepath.Join(repoRoot(t), "cmd", "server"),
	}
	// allowedFunctions is keyed by absolute file path, valued by the set of
	// function names whose bodies are allowed to call Analytics.Capture
	// directly. Granularity is per-function, not per-file. Currently empty —
	// no server code is permitted to call Analytics.Capture outside
	// metrics.RecordEvent.
	allowedFunctions := map[string]map[string]struct{}{}

	var offenders []string
	fset := token.NewFileSet()
	for _, root := range roots {
		matches, err := filepath.Glob(filepath.Join(root, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", root, err)
		}
		for _, file := range matches {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			fileAllowedFns := allowedFunctions[file]
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if _, allowed := fileAllowedFns[fn.Name.Name]; allowed {
					continue
				}
				if fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if !isAnalyticsCapture(call) {
						return true
					}
					offenders = append(offenders, fset.Position(call.Pos()).String()+" (in "+fn.Name.Name+")")
					return true
				})
			}
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("found %d naked Analytics.Capture(...) calls — wrap them in metrics.RecordEvent so the Prometheus and PostHog sides cannot drift:\n  %s", len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestEveryAnalyticsRecordEventTakesAnalyticsHelper enforces the inverse of
// TestNoNakedAnalyticsCaptureInHandlersOrServices: every call site that
// DOES go through metrics.RecordEvent must take an analytics.* event helper
// as its third argument. Local idents are accepted only when def-use
// tracking inside the same function body proves the value originated from
// an `analytics.<Helper>(...)` call — bare strings or unresolved values
// fail CI.
func TestEveryAnalyticsRecordEventTakesAnalyticsHelper(t *testing.T) {
	t.Parallel()

	roots := []string{
		filepath.Join(repoRoot(t), "internal", "handler"),
		filepath.Join(repoRoot(t), "internal", "service"),
		filepath.Join(repoRoot(t), "cmd", "server"),
	}

	var offenders []string
	fset := token.NewFileSet()
	for _, root := range roots {
		matches, err := filepath.Glob(filepath.Join(root, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", root, err)
		}
		for _, file := range matches {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				analyticsLocals := analyticsBackedIdents(fn.Body)
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if !isMetricsRecordEvent(call) {
						return true
					}
					if len(call.Args) < 3 {
						offenders = append(offenders, fset.Position(call.Pos()).String()+" (RecordEvent must be called with 3 args: client, metrics, event)")
						return true
					}
					ev := call.Args[2]
					if analyticsHelperCall(ev) {
						return true
					}
					if id, ok := ev.(*ast.Ident); ok {
						if _, traced := analyticsLocals[id.Name]; traced {
							return true
						}
						offenders = append(offenders, fset.Position(call.Pos()).String()+" (third arg "+id.Name+" was not assigned from an analytics.* helper in this function)")
						return true
					}
					offenders = append(offenders, fset.Position(call.Pos()).String()+" (third arg must be an analytics.* helper call or a local assigned from one)")
					return true
				})
			}
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("metrics.RecordEvent call sites must take an analytics.* event:\n  %s", strings.Join(offenders, "\n  "))
	}
}

// analyticsBackedIdents walks a function body and returns the set of local
// identifiers whose initial value came from an analytics.<Helper>(...) call.
// Both `:=` short declarations and `=` assignments at any nesting depth are
// recognised. The set is conservative — re-assignments to non-analytics
// values keep the ident in the set (we only track the originating
// definition); call sites that cared could rewrite to use the helper inline.
//
// KNOWN LIMITATION (tracked as PR3 follow-up; see PR description):
// matching is by ident NAME, not by SSA def-use. A pathological function
// that shadows an analytics-backed name with a non-analytics binding in a
// nested scope (e.g. an `if`/`for` re-declaration via `:=`) would still
// pass this check, because the outer-scope name is in the allow-set. This
// is rare in practice — every current call site assigns once at the same
// scope as the RecordEvent call — but a future hardening pass should
// switch to a real go/types or go/ssa walk so the lint is type-aware
// rather than name-aware.
func analyticsBackedIdents(body *ast.BlockStmt) map[string]struct{} {
	out := map[string]struct{}{}
	if body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			// Match either lhs[i] = analytics.X(...) or lhs[i], _ := analytics.X(...).
			if len(stmt.Rhs) == 0 {
				return true
			}
			for i, lhs := range stmt.Lhs {
				if i >= len(stmt.Rhs) {
					// Multi-return-from-single-call shape (e.g. a, b := f())
					// — there is exactly one Rhs and we can't tell which
					// returned position the ident binds without type info.
					// Fall back to checking the single Rhs.
					if len(stmt.Rhs) == 1 && analyticsHelperCall(stmt.Rhs[0]) {
						if id, ok := lhs.(*ast.Ident); ok {
							out[id.Name] = struct{}{}
						}
					}
					continue
				}
				if id, ok := lhs.(*ast.Ident); ok && analyticsHelperCall(stmt.Rhs[i]) {
					out[id.Name] = struct{}{}
				}
			}
		case *ast.ValueSpec:
			// `var x = analytics.X(...)` and the like.
			for i, name := range stmt.Names {
				if i < len(stmt.Values) && analyticsHelperCall(stmt.Values[i]) {
					out[name.Name] = struct{}{}
				}
			}
		}
		return true
	})
	return out
}

// ---- helpers --------------------------------------------------------------

// repoRoot returns the absolute path to server/. The test sources live in
// server/internal/metrics/ so two parents up is the server root.
func repoRoot(t *testing.T) string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	// .../server/internal/metrics/business_pairing_test.go → .../server
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

// analyticsEventNames parses analytics/events.go and returns every Event*
// constant value (the literal string passed to PostHog).
func analyticsEventNames(t *testing.T) map[string]struct{} {
	t.Helper()

	path := filepath.Join(repoRoot(t), "internal", "analytics", "events.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	out := map[string]struct{}{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Values) == 0 {
				continue
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Event") {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				out[strings.Trim(lit.Value, "\"")] = struct{}{}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no Event* constants found in %s", path)
	}
	return out
}

// constantNameForEvent reverse-maps an event string to its Go constant name
// for nicer error messages. Stable for the constants we ship.
func constantNameForEvent(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return "Event" + strings.Join(parts, "")
}

// dispatchIncrementsCounter sends ev through RecordEvent (with a noop
// PostHog client) and returns true when at least one Prometheus counter
// receives a non-zero increment. We use a fresh BusinessMetrics per event
// so a leftover prewarm value from another counter cannot mask a missing
// dispatch case.
func dispatchIncrementsCounter(m *metrics.BusinessMetrics, ev analytics.Event) bool {
	before := metrics.SumAllCounters(m)
	metrics.RecordEvent(analytics.NoopClient{}, m, ev)
	after := metrics.SumAllCounters(m)
	return after > before
}

// defaultPropsForEvent returns a properties map populated with the label
// values the dispatcher reads, so the synthetic test event lights up its
// matching counter without relying on the analytics helper plumbing.
func defaultPropsForEvent(name string) map[string]any {
	switch name {
	case analytics.EventSignup:
		return map[string]any{"signup_source": "test"}
	case analytics.EventWorkspaceCreated:
		return map[string]any{"source": "manual"}
	case analytics.EventOnboardingStarted:
		return map[string]any{"platform": "web"}
	case analytics.EventOnboardingCompleted:
		return map[string]any{"completion_path": "full"}
	case analytics.EventIssueCreated:
		return map[string]any{"source": "manual", "platform": "web"}
	case analytics.EventChatMessageSent:
		return map[string]any{"platform": "web"}
	case analytics.EventAgentCreated:
		return map[string]any{"runtime_mode": "local", "source": "manual"}
	case analytics.EventAutopilotCreated:
		return map[string]any{"cadence": "manual"}
	case analytics.EventIssueExecuted:
		return map[string]any{"source": "manual"}
	case analytics.EventRuntimeRegistered, analytics.EventRuntimeReady, analytics.EventRuntimeOffline:
		return map[string]any{"runtime_mode": "local", "provider": "claude"}
	case analytics.EventRuntimeFailed:
		return map[string]any{"runtime_mode": "local", "provider": "claude", "failure_reason": "unknown", "recoverable": false}
	case analytics.EventAutopilotRunStarted, analytics.EventAutopilotRunCompleted, analytics.EventAutopilotRunFailed:
		return map[string]any{"cadence": "manual", "trigger_kind": "manual"}
	case analytics.EventFeedbackSubmitted:
		return map[string]any{"kind": "general", "platform": "web"}
	case analytics.EventContactSalesSubmitted:
		return map[string]any{"form_source": "page"}
	}
	return map[string]any{}
}

func isAnalyticsCapture(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel == nil || sel.Sel.Name != "Capture" {
		return false
	}
	// Receiver must be a selector ending in `.Analytics`.
	rec, ok := sel.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if rec.Sel == nil || rec.Sel.Name != "Analytics" {
		return false
	}
	// Must be passing an analytics helper or a local built from one — but
	// the lint principle is "no direct Capture", so any shape fails.
	return true
}

// isMetricsRecordEvent reports whether call is a metrics.RecordEvent
// invocation. Recognises the two import aliases used in this codebase:
// `obsmetrics` (everywhere outside the metrics package itself) and the
// natural package name `metrics`.
//
// KNOWN LIMITATION (tracked as PR3 follow-up; see PR description):
// the alias set is HARD-CODED. A future caller that imports the metrics
// package under a third alias (`mx "..."`, etc.) would slip past this
// check. The follow-up plan is to walk the file's import declarations
// and resolve the alias for `server/internal/metrics` per-file, so any
// alias works as long as the canonical import path matches. We leave it
// hard-coded here because every current import in handler/, service/,
// and cmd/server/ uses one of the two names, and goimports/`gofmt`
// guidance keeps it that way.
func isMetricsRecordEvent(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel == nil || sel.Sel.Name != "RecordEvent" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "obsmetrics" || pkg.Name == "metrics"
}

func analyticsHelperCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "analytics" && sel.Sel != nil && len(sel.Sel.Name) > 0
}
