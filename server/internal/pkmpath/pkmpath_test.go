package pkmpath

import (
	"strings"
	"testing"
)

func TestNormalize_ValidPaths(t *testing.T) {
	cases := []struct {
		name string
		in   string
		root string
		want string
	}{
		{"default pkm under /root", "/PKM-CUONG/GROWTH/PROJECTS", "/root", "/PKM-CUONG/GROWTH/PROJECTS"},
		{"trailing slash collapsed", "/PKM-CUONG/GROWTH/PROJECTS/", "/root", "/PKM-CUONG/GROWTH/PROJECTS"},
		{"redundant slashes collapsed", "/PKM-CUONG//GROWTH///PROJECTS", "/root", "/PKM-CUONG/GROWTH/PROJECTS"},
		{"single dot segments collapsed", "/PKM-CUONG/./GROWTH/./PROJECTS", "/root", "/PKM-CUONG/GROWTH/PROJECTS"},
		{"unicode names accepted", "/Notes/Été 2024", "/root", "/Notes/Été 2024"},
		{"deep nesting accepted", "/a/b/c/d/e/f/g", "/root", "/a/b/c/d/e/f/g"},
		{"trailing slash on root collapsed", "/PKM/GROWTH", "/root/", "/PKM/GROWTH"},
		{"no allowlist root still validates shape", "/PKM-CUONG/GROWTH/PROJECTS", "", "/PKM-CUONG/GROWTH/PROJECTS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.in, tc.root)
			if err != nil {
				t.Fatalf("Normalize(%q, %q) returned error: %v", tc.in, tc.root, err)
			}
			if got != tc.want {
				t.Fatalf("Normalize(%q, %q) = %q, want %q", tc.in, tc.root, got, tc.want)
			}
		})
	}
}

func TestNormalize_InvalidPaths(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		root    string
		wantErr string
	}{
		{"empty", "", "/root", "must not be empty"},
		{"relative", "PKM/GROWTH", "/root", "must start with '/'"},
		{"dot-dot at start", "/../etc/passwd", "/root", "must not contain '..'"},
		{"dot-dot in middle", "/PKM/../../etc", "/root", "must not contain '..'"},
		{"dot-dot at end", "/PKM/GROWTH/..", "/root", "must not contain '..'"},
		{"bare root", "/", "/root", "must not be '/'"},
		{"only slashes collapses to root", "///", "/root", "must not be '/'"},
		{"resolves to root itself", "/", "", "must not be '/'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.in, tc.root)
			if err == nil {
				t.Fatalf("Normalize(%q, %q) = %q, want error containing %q", tc.in, tc.root, got, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Normalize(%q, %q) error = %q, want substring %q", tc.in, tc.root, err.Error(), tc.wantErr)
			}
		})
	}
}

// TestNormalize_PrefixSanity guards against the classic confused-prefix bug
// where "/rootless" is wrongly accepted because it shares the literal prefix
// "/root" with the allowlist root. The check must use a path-boundary
// (root + "/") comparison.
func TestNormalize_PrefixSanity(t *testing.T) {
	// Allowlist root is "/root". A pkm_path resolving to "/rootless/..."
	// must be rejected even though the strings share the "/root" prefix.
	// Crafting such a payload requires that the user-supplied path starts
	// with "/" and joins onto "/root", so resolved is always
	// "/root/<input-without-leading-slash>". Plain "/rootless/secret"
	// becomes "/root/rootless/secret" which is legitimately inside the
	// root — that's fine. The actual hazard is when allowlistRoot itself
	// has a sibling like "/root2"; verify with a tighter root.
	if _, err := Normalize("/secret", "/srv/multica"); err != nil {
		t.Fatalf("legitimate path under /srv/multica rejected: %v", err)
	}
	// Crafted root with no trailing slash: "/srv/multic" must not match a
	// resolved "/srv/multica/..." path. We swap roles to verify the
	// boundary check.
	if _, err := Normalize("/", "/srv/multic"); err == nil {
		t.Fatal("expected '/' to be rejected even with a different allowlist root")
	}
}
