// Package expr evaluates arithmetic and logical expressions.
//
// Grammar, loosest to tightest: || && (comparisons) + - * / % unary(- !) ^.
// ^ is right-associative. Comparisons and logic yield 1 or 0, any non-zero
// value is true, and && / || short-circuit.
package expr

import "errors"

// Env holds variable values.
type Env map[string]float64

// ErrDivByZero is returned for division or modulo by zero.
var ErrDivByZero = errors.New("division by zero")

// SyntaxError reports malformed input at byte offset Pos.
type SyntaxError struct {
	Pos int
	Msg string
}

func (e *SyntaxError) Error() string { panic("not implemented") }

// Eval parses and evaluates src. Builtin functions: min, max (one or more
// arguments), abs, sqrt (exactly one argument).
func Eval(src string, env Env) (float64, error) { panic("not implemented") }
