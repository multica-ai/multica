// CEL evaluator for the Expr escape hatch on a Condition (FIR-1609). The common
// condition cases are plain structured fields (host-allowlist, action-list);
// Expr exists only for genuine dynamics ("only in business hours"). CEL is
// Google's purpose-built, non-Turing-complete expression language for exactly
// this — a bounded predicate over request attributes, not a general program — so
// a malicious or buggy expression cannot loop or escape. The evaluator is built
// here and injected as an ExprEvaluator so the pure condition core (condition.go)
// carries no CEL dependency and stays unit-testable without it.
package toolpolicy

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// FlagPolicyCEL is the workspace feature-flag key that enables the CEL Expr
// escape hatch on Conditions. Default OFF (packages/cerebro-feature-flags/
// registry.ts). Both gates read it under this single constant so the gateway and
// daemon paths can never drift to different key strings. Kept in sync with the TS
// registry.
const FlagPolicyCEL = "cerebro_policy_cel"

// celVarHost and celVarAction are the request attributes an Expr may reference.
// They mirror the structured RequestContext fields one-for-one, so an Expr is a
// strict superset of what HostAllowlist/Actions already express — anything new it
// reads must be added both here and to RequestContext.
const (
	celVarHost   = "host"
	celVarAction = "action"
)

// CELEvaluator compiles and evaluates Condition expressions against a request
// context. Programs are compiled once per distinct expression and cached, since
// the same handful of authored rules are evaluated on every matching tool call.
// The zero value is not usable; construct with NewCELEvaluator.
type CELEvaluator struct {
	env   *cel.Env
	mu    sync.RWMutex
	progs map[string]cel.Program // expr → compiled program (nil entry = compile error, cached below)
	errs  map[string]error       // expr → compile error, so a bad expr is not recompiled every call
}

// NewCELEvaluator builds an evaluator whose environment exposes the request
// attributes (host, action) as typed string variables. It fails only if the
// fixed environment itself is invalid, which is a programming error.
func NewCELEvaluator() (*CELEvaluator, error) {
	env, err := cel.NewEnv(
		cel.Variable(celVarHost, cel.StringType),
		cel.Variable(celVarAction, cel.StringType),
	)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: build CEL env: %w", err)
	}
	return &CELEvaluator{
		env:   env,
		progs: map[string]cel.Program{},
		errs:  map[string]error{},
	}, nil
}

// Eval is the ExprEvaluator the resolver injects. It compiles (once) and runs the
// expression against the request context, requiring a bool result. A compile
// error, a non-bool result, or a runtime error is returned to the caller, which
// — per ConditionedSetting — fails closed by the rule's effect (an Allow with an
// undecidable Expr is dropped; a Deny/Ask is kept). It never panics and never
// silently treats an error as "matches".
func (e *CELEvaluator) Eval(expr string, ctx RequestContext) (bool, error) {
	prg, err := e.program(expr)
	if err != nil {
		return false, err
	}
	out, _, err := prg.Eval(map[string]any{
		celVarHost:   ctx.Host,
		celVarAction: ctx.Action,
	})
	if err != nil {
		return false, fmt.Errorf("toolpolicy: eval CEL expr: %w", err)
	}
	return celBool(out)
}

// program returns the compiled program for an expression, compiling and caching
// it on first use. A compile error is cached too, so a malformed authored rule
// costs one compile, not one per request.
func (e *CELEvaluator) program(expr string) (cel.Program, error) {
	e.mu.RLock()
	if prg, ok := e.progs[expr]; ok {
		e.mu.RUnlock()
		return prg, nil
	}
	if cerr, ok := e.errs[expr]; ok {
		e.mu.RUnlock()
		return nil, cerr
	}
	e.mu.RUnlock()

	prg, err := e.compile(expr)

	e.mu.Lock()
	defer e.mu.Unlock()
	if err != nil {
		e.errs[expr] = err
		return nil, err
	}
	e.progs[expr] = prg
	return prg, nil
}

// compile type-checks the expression and requires it to yield a bool — a
// condition is a predicate, so an expr that returns a string or int is a config
// error caught here, not a silent mismatch at the gate.
func (e *CELEvaluator) compile(expr string) (cel.Program, error) {
	ast, iss := e.env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("toolpolicy: compile CEL expr: %w", iss.Err())
	}
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("toolpolicy: CEL expr must yield bool, got %s", ast.OutputType())
	}
	prg, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: build CEL program: %w", err)
	}
	return prg, nil
}

// sharedEval is the process-wide evaluator both gates inject when the CEL flag is
// on. One instance is correct and preferable: the environment is fixed and the
// compile cache is concurrency-safe, so sharing it lets a rule authored once
// compile once for the whole fleet. Built lazily so importing the package costs
// nothing until a gate actually needs it.
var (
	sharedEvalOnce sync.Once
	sharedEval     *CELEvaluator
	sharedEvalErr  error
)

// SharedCELEvaluator returns the process-wide CEL evaluator, building it on first
// use. The error is only ever a programming error in the fixed environment; a
// caller that gets one should treat the evaluator as unavailable (leave Query.Eval
// nil, so Expr conditions fail closed) rather than crash the gate.
func SharedCELEvaluator() (*CELEvaluator, error) {
	sharedEvalOnce.Do(func() {
		sharedEval, sharedEvalErr = NewCELEvaluator()
	})
	return sharedEval, sharedEvalErr
}

// celBool narrows a CEL result to a Go bool, treating any non-bool (including a
// CEL error value that surfaced as the result) as an error rather than a match.
func celBool(out ref.Val) (bool, error) {
	if out == nil {
		return false, fmt.Errorf("toolpolicy: CEL expr produced no value")
	}
	if out.Type() != types.BoolType {
		return false, fmt.Errorf("toolpolicy: CEL expr produced %s, want bool", out.Type())
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("toolpolicy: CEL bool result was not a Go bool")
	}
	return b, nil
}
