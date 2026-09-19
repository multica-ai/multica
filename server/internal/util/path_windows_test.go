//go:build windows

package util

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestClassifyTarget pins the link-target grammar on pure string inputs, with
// no filesystem fixture: every branch is a classification decision the walk
// relies on, and the ordering between them is load-bearing — VolumeName is
// non-empty for drive-absolute paths too, so the IsAbs branch must be asked
// before the drive-relative one or a junction's absolute target resolves
// against the working directory (which is exactly how a junction escape read
// as inside the workdir before this table existed).
func TestClassifyTarget(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	vol := filepath.VolumeName(cwd)
	sep := string(filepath.Separator)

	cases := []struct {
		name         string
		target       string
		wantBase     string
		wantTail     string
		wantVerbatim bool
		wantOK       bool
	}{
		{
			name:     "drive-absolute target is walked from its own volume root",
			target:   vol + `\outside\dir`,
			wantBase: vol + sep,
			wantTail: `outside\dir`,
			wantOK:   true,
		},
		{
			name:     "UNC target is walked from the share root",
			target:   `\\srv\share\dir`,
			wantBase: `\\srv\share` + sep,
			wantTail: `dir`,
			wantOK:   true,
		},
		{
			name:     "root-relative target resolves from the link's volume root, not the link's directory",
			target:   `\outside\dir`,
			wantBase: vol + sep,
			wantTail: `outside\dir`,
			wantOK:   true,
		},
		{
			name:     "drive-relative target on the current drive resolves against the working directory",
			target:   vol + `outside\dir`,
			wantBase: vol + sep,
			// The working directory's components are part of the walked tail,
			// not of the root the walk starts from.
			wantTail: strings.TrimPrefix(cwd, vol+sep) + sep + `outside` + sep + `dir`,
			wantOK:   true,
		},
		{
			name:   "drive-relative target on another drive is unobservable",
			target: `Q:outside\dir`,
			wantOK: false,
		},
		{
			name:     "plain-relative target keeps the link's directory as base",
			target:   `relative\dir`,
			wantBase: cwd,
			wantTail: `relative\dir`,
			wantOK:   true,
		},
		{
			name:     "extended-length drive path reduces to its Win32 spelling",
			target:   `\\?\` + vol + `\outside\dir`,
			wantBase: vol + sep,
			wantTail: `outside\dir`,
			wantOK:   true,
		},
		{
			name:     "extended-length UNC path reduces to its Win32 spelling",
			target:   `\\?\UNC\srv\share\dir`,
			wantBase: `\\srv\share` + sep,
			wantTail: `dir`,
			wantOK:   true,
		},
		{
			name:         "volume-GUID target has no Win32 spelling and stays verbatim",
			target:       `\\?\Volume{01234567-89ab-cdef-0123-456789abcdef}\dir`,
			wantBase:     `\\?\Volume{01234567-89ab-cdef-0123-456789abcdef}\dir`,
			wantVerbatim: true,
			wantOK:       true,
		},
		{
			name:     "object-manager root is stripped before classification",
			target:   `\??\` + vol + `\outside\dir`,
			wantBase: vol + sep,
			wantTail: `outside\dir`,
			wantOK:   true,
		},
		{
			name:     "object-manager UNC target becomes a Win32 UNC path",
			target:   `\??\UNC\srv\share\dir`,
			wantBase: `\\srv\share` + sep,
			wantTail: `dir`,
			wantOK:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, segs, verbatim, ok := classifyTarget(tc.target, cwd)
			if ok != tc.wantOK {
				t.Fatalf("classifyTarget(%q) ok = %v, want %v", tc.target, ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if verbatim != tc.wantVerbatim {
				t.Fatalf("classifyTarget(%q) verbatim = %v, want %v", tc.target, verbatim, tc.wantVerbatim)
			}
			if base != tc.wantBase {
				t.Errorf("classifyTarget(%q) base = %q, want %q", tc.target, base, tc.wantBase)
			}
			if tc.wantVerbatim {
				return
			}
			if got := filepath.Join(segs...); !strings.EqualFold(got, tc.wantTail) {
				t.Errorf("classifyTarget(%q) segs = %q, want %q", tc.target, got, tc.wantTail)
			}
		})
	}

	t.Run("a drive-absolute target is never misread as drive-relative", func(t *testing.T) {
		// The regression the table exists to catch, spelled out: VolumeName is
		// non-empty for `C:\dir` too, so if the IsAbs branch is not asked
		// first, a junction's absolute target resolves against the working
		// directory and an escape reads as inside the workdir.
		base, _, _, ok := classifyTarget(vol+`\outside\dir`, cwd)
		if !ok {
			t.Fatal("classifyTarget reported !ok for a drive-absolute target")
		}
		if !strings.EqualFold(base, vol+sep) {
			t.Fatalf("classifyTarget(%q) base = %q, want the volume root %q", vol+`\outside\dir`, base, vol+sep)
		}
	})
}
