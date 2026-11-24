package expr

import (
	"errors"
	"testing"
)

func TestTernary(t *testing.T) {
	cases := []struct {
		src  string
		env  Env
		want float64
	}{
		{"1 ? 2 : 3", nil, 2},
		{"0 ? 2 : 3", nil, 3},
		{"1 < 2 ? 10 : 20", nil, 10},
		{"0 ? 1 : 0 ? 2 : 3", nil, 3},
		{"1 ? 0 ? 4 : 5 : 6", nil, 5},
		{"x > 0 ? x : -x", Env{"x": -4}, 4},
		{"1 ? 2 : 1 / 0", nil, 2},
		{"0 ? missing : 7", nil, 7},
		{"(1 ? 2 : 3) * 10", nil, 20},
		{"1 || 0 ? 8 : 9", nil, 8},
	}
	for _, c := range cases {
		got, err := Eval(c.src, c.env)
		if err != nil {
			t.Fatalf("Eval(%q): %v", c.src, err)
		}
		if got != c.want {
			t.Fatalf("Eval(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

func TestTernarySyntaxErrors(t *testing.T) {
	for src, pos := range map[string]int{"1 ? 2": 5, "1 ? : 2": 4, "? 1 : 2": 0} {
		_, err := Eval(src, nil)
		var se *SyntaxError
		if !errors.As(err, &se) || se.Pos != pos {
			t.Fatalf("Eval(%q) error = %v, want syntax error at %d", src, err, pos)
		}
	}
}
