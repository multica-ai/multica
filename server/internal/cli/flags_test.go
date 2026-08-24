package cli

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

// newFlagOrEnvArrayCmd builds a minimal cobra command with a StringArray
// flag pre-registered under flagName. Tests set/unset the flag directly via
// cmd.Flags().Set so they don't depend on cobra's parsing edge cases.
func newFlagOrEnvArrayCmd(t *testing.T, flagName string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().StringArray(flagName, nil, "")
	return cmd
}

func TestFlagOrEnvArray(t *testing.T) {
	cases := []struct {
		name    string
		flagSet bool
		flag    []string
		env     string
		want    []string
	}{
		{
			name:    "flag set, single value",
			flagSet: true,
			flag:    []string{"X-A: 1"},
			want:    []string{"X-A: 1"},
		},
		{
			name:    "flag set, multiple values",
			flagSet: true,
			flag:    []string{"X-A: 1", "X-B: 2"},
			want:    []string{"X-A: 1", "X-B: 2"},
		},
		{
			name:    "flag wins over env",
			flagSet: true,
			flag:    []string{"X-A: 1"},
			env:     "X-A: env\nX-B: env",
			want:    []string{"X-A: 1"},
		},
		{
			name: "env multi-line",
			env:  "X-A: 1\nX-B: 2\nX-C: 3",
			want: []string{"X-A: 1", "X-B: 2", "X-C: 3"},
		},
		{
			name: "env multi-line CRLF (Windows-edited env var)",
			env:  "X-A: 1\r\nX-B: 2\r\n",
			want: []string{"X-A: 1", "X-B: 2"},
		},
		{
			name: "env strips blank and trailing-newline entries",
			env:  "\nX-A: 1\n\nX-B: 2\n",
			want: []string{"X-A: 1", "X-B: 2"},
		},
		{
			name: "env single value",
			env:  "X-A: 1",
			want: []string{"X-A: 1"},
		},
		{
			name: "empty everywhere",
			want: nil,
		},
		{
			name: "env only whitespace",
			env:  "  \n  \n",
			want: nil,
		},
		{
			name:    "flag with empty entries are dropped",
			flagSet: true,
			flag:    []string{"", "X-A: 1", "  "},
			want:    []string{"X-A: 1"},
		},
		{
			name:    "flag all-empty entries yield nil",
			flagSet: true,
			flag:    []string{"", "  "},
			want:    nil,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cmd := newFlagOrEnvArrayCmd(t, "extra-header")
			if tc.flagSet {
				for _, v := range tc.flag {
					if err := cmd.Flags().Set("extra-header", v); err != nil {
						t.Fatalf("set flag: %v", err)
					}
				}
			}
			t.Setenv("MULTICA_TEST_EXTRA", tc.env)

			got := FlagOrEnvArray(cmd, "extra-header", "MULTICA_TEST_EXTRA")
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("FlagOrEnvArray() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestFlagOrEnvArray_FlagUnsetIgnoresEmptyEnv pins the precedence contract:
// when the flag is untouched, a whitespace-only env var must not poison
// the result with a single empty entry.
func TestFlagOrEnvArray_FlagUnsetIgnoresEmptyEnv(t *testing.T) {
	cmd := newFlagOrEnvArrayCmd(t, "extra-header")
	t.Setenv("MULTICA_TEST_EXTRA", " ")

	if got := FlagOrEnvArray(cmd, "extra-header", "MULTICA_TEST_EXTRA"); got != nil {
		t.Errorf("FlagOrEnvArray() = %v, want nil for whitespace-only env", got)
	}
}

// TestFlagOrEnvArray_PreservesDeclarationOrder pins the implementation's
// "flag wins, declaration order preserved" rule. Re-ordering matters for
// callers that consume the slice as an ordered spec (e.g. the daemon
// parses each entry with ExtraHeaderFromFlag in order, and a misordered
// spec could swap two values into the wrong header).
func TestFlagOrEnvArray_PreservesDeclarationOrder(t *testing.T) {
	cmd := newFlagOrEnvArrayCmd(t, "extra-header")
	values := []string{"X-A: 1", "X-B: 2", "X-C: 3", "X-D: 4"}
	for _, v := range values {
		if err := cmd.Flags().Set("extra-header", v); err != nil {
			t.Fatalf("set flag: %v", err)
		}
	}

	got := FlagOrEnvArray(cmd, "extra-header", "MULTICA_TEST_EXTRA")
	if !reflect.DeepEqual(got, values) {
		t.Errorf("FlagOrEnvArray() = %v, want %v (declaration order must survive)", got, values)
	}
}

// TestFlagOrEnvArray_EnvWhitespaceOnlyAfterNewline trims a literal "\n"
// spec (not just whitespace-only env). An accidental env like
// MULTICA_EXTRA_HEADERS="\nX-A: 1" must surface "X-A: 1" alone — the
// leading blank line is dropped, not turned into an empty entry.
func TestFlagOrEnvArray_EnvWhitespaceOnlyAfterNewline(t *testing.T) {
	cmd := newFlagOrEnvArrayCmd(t, "extra-header")
	t.Setenv("MULTICA_TEST_EXTRA", "\nX-A: 1")

	got := FlagOrEnvArray(cmd, "extra-header", "MULTICA_TEST_EXTRA")
	want := []string{"X-A: 1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FlagOrEnvArray() = %v, want %v", got, want)
	}
}

// TestFlagOrEnvArray_EnvKeepsTrailingSpaces ensures a value like
// `X-Bearer:  abc def` round-trips with its internal spaces intact: the
// env-line parser only strips CR and skips blank lines, never the value
// itself. Operators deliberately type "Bearer abc def" tokens and a
// premature TrimSpace would silently collapse them into "Bearer abc def".
// (Trailing whitespace is not portable across `t.Setenv` implementations,
// so we exercise the more meaningful internal-spaces case here.)
func TestFlagOrEnvArray_EnvKeepsTrailingSpaces(t *testing.T) {
	cmd := newFlagOrEnvArrayCmd(t, "extra-header")
	t.Setenv("MULTICA_TEST_EXTRA", "X-Bearer:  abc def")

	got := FlagOrEnvArray(cmd, "extra-header", "MULTICA_TEST_EXTRA")
	if len(got) != 1 || got[0] != "X-Bearer:  abc def" {
		t.Errorf("FlagOrEnvArray() = %v, want %q (internal spaces must survive)", got, "X-Bearer:  abc def")
	}
}
