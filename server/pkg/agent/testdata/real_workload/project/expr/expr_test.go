package expr

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestEvalArithmetic(t *testing.T) {
	cases := []struct {
		src  string
		want float64
	}{
		{"1", 1},
		{"1.5 + 2.25", 3.75},
		{"2 + 3 * 4", 14},
		{"(2 + 3) * 4", 20},
		{"10 - 4 - 3", 3},
		{"20 / 4 / 5", 1},
		{"7 % 4", 3},
		{"2 ^ 3 ^ 2", 512},
		{"-2 ^ 2", -4},
		{"(-2) ^ 2", 4},
		{"--3", 3},
		{"-(1 + 2) * 3", -9},
		{"  4\t*\n2 ", 8},
	}
	for _, c := range cases {
		got, err := Eval(c.src, nil)
		if err != nil {
			t.Fatalf("Eval(%q): %v", c.src, err)
		}
		if math.Abs(got-c.want) > 1e-9 {
			t.Fatalf("Eval(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

func TestEvalVariablesAndFunctions(t *testing.T) {
	env := Env{"x": 3, "rate_2": 0.5}
	cases := []struct {
		src  string
		want float64
	}{
		{"x * x", 9},
		{"rate_2 * 10", 5},
		{"min(4, x, 7)", 3},
		{"max(1, x * 3, 2)", 9},
		{"abs(-x)", 3},
		{"sqrt(16) + 1", 5},
		{"max(min(1, 2), abs(-5))", 5},
	}
	for _, c := range cases {
		got, err := Eval(c.src, env)
		if err != nil {
			t.Fatalf("Eval(%q): %v", c.src, err)
		}
		if math.Abs(got-c.want) > 1e-9 {
			t.Fatalf("Eval(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

func TestEvalLogic(t *testing.T) {
	cases := []struct {
		src  string
		want float64
	}{
		{"1 < 2", 1},
		{"2 <= 1", 0},
		{"3 == 3", 1},
		{"3 != 3", 0},
		{"1 < 2 && 2 < 3", 1},
		{"1 > 2 || 0", 0},
		{"!0", 1},
		{"!5", 0},
		{"0 && 1 / 0", 0},
		{"1 || unknown", 1},
		{"1 + 1 == 2 && 2 * 2 >= 4", 1},
	}
	for _, c := range cases {
		got, err := Eval(c.src, nil)
		if err != nil {
			t.Fatalf("Eval(%q): %v", c.src, err)
		}
		if got != c.want {
			t.Fatalf("Eval(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

func TestEvalErrors(t *testing.T) {
	if _, err := Eval("1 / (2 - 2)", nil); !errors.Is(err, ErrDivByZero) {
		t.Fatalf("division by zero error = %v, want ErrDivByZero", err)
	}
	if _, err := Eval("5 % 0", nil); !errors.Is(err, ErrDivByZero) {
		t.Fatalf("modulo by zero error = %v, want ErrDivByZero", err)
	}
	if _, err := Eval("y + 1", Env{"x": 1}); err == nil || !strings.Contains(err.Error(), "unknown variable y") {
		t.Fatalf("unknown variable error = %v", err)
	}
	if _, err := Eval("nope(1)", nil); err == nil || !strings.Contains(err.Error(), "unknown function nope") {
		t.Fatalf("unknown function error = %v", err)
	}
	if _, err := Eval("abs(1, 2)", nil); err == nil || !strings.Contains(err.Error(), "abs") {
		t.Fatalf("arity error = %v", err)
	}
	if _, err := Eval("min()", nil); err == nil {
		t.Fatal("min() with no arguments must fail")
	}
}

func TestEvalSyntaxErrorPositions(t *testing.T) {
	cases := []struct {
		src string
		pos int
	}{
		{"1 + * 2", 4},
		{"(1 + 2", 6},
		{"1 2", 2},
		{"3 $ 4", 2},
		{"", 0},
		{"max(1,", 6},
	}
	for _, c := range cases {
		_, err := Eval(c.src, nil)
		var se *SyntaxError
		if !errors.As(err, &se) {
			t.Fatalf("Eval(%q) error = %v, want *SyntaxError", c.src, err)
		}
		if se.Pos != c.pos {
			t.Fatalf("Eval(%q) syntax error at %d (%s), want %d", c.src, se.Pos, se.Msg, c.pos)
		}
		if !strings.HasPrefix(se.Error(), "syntax error at ") {
			t.Fatalf("SyntaxError.Error() = %q", se.Error())
		}
	}
}
