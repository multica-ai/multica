package auth

import (
	"context"
	"testing"
)

func TestBootstrapSingleUser_DisabledIsNoop(t *testing.T) {
	t.Setenv("MULTICA_SINGLE_USER", "")
	// Passing nil pool would panic if the function ever reached the
	// non-no-op path; the early SingleUserMode() check protects it.
	if err := BootstrapSingleUser(context.Background(), nil); err != nil {
		t.Fatalf("expected no-op when single-user mode is off, got %v", err)
	}
}

func TestBootstrapSingleUser_NilPoolErrorsWhenEnabled(t *testing.T) {
	t.Setenv("MULTICA_SINGLE_USER", "true")
	if err := BootstrapSingleUser(context.Background(), nil); err == nil {
		t.Fatal("expected error when pool is nil and single-user mode is on")
	}
}

func TestSingleUserMode(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"", false},
		{"false", false},
		{"0", false},
		{"no", false},
		{"off", false},
		{"random", false},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"on", true},
		{"  true  ", true},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("MULTICA_SINGLE_USER", tc.raw)
			if got := SingleUserMode(); got != tc.want {
				t.Fatalf("SingleUserMode(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
