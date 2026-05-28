package cli

import "testing"

func TestIsForkVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"standard fork version", "v0.3.6-767-g16a0ca0a1", true},
		{"with v prefix", "v0.3.6-767-gabcdef0", true},
		{"without v prefix", "0.3.6-767-gabcdef0", true},
		{"stacked describe", "v0.3.6-767-g16a0ca0a1-14-g12d314f5c", true},
		{"dirty build", "v0.3.6-767-gabcdef0-dirty", false},
		{"clean semver", "v0.3.6", false},
		{"clean semver no v", "0.3.6", false},
		{"empty", "", false},
		{"garbage", "garbage", false},
		{"only major minor", "v0.3", false},
		{"zero commit count", "v0.3.6-0-gabcdef0", true},
		{"whitespace trimmed", "  v0.3.6-767-gabcdef0  ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsForkVersion(tt.in); got != tt.want {
				t.Fatalf("IsForkVersion(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsNewerForkVersion(t *testing.T) {
	tests := []struct {
		name            string
		latest, current string
		want            bool
	}{
		{"higher commit count", "v0.3.6-780-gabcdef0", "v0.3.6-767-g1234567", true},
		{"same commit count", "v0.3.6-767-gabcdef0", "v0.3.6-767-g1234567", false},
		{"lower commit count", "v0.3.6-750-gabcdef0", "v0.3.6-767-g1234567", false},
		{"base version bump", "v0.3.7-5-gabcdef0", "v0.3.6-780-g1234567", true},
		{"minor bump", "v0.4.0-5-gabcdef0", "v0.3.6-780-g1234567", true},
		{"major bump", "v1.0.0-1-gabcdef0", "v0.99.99-999-g1234567", true},
		{"stacked newer", "v0.3.6-767-g16a0ca0a1-20-g12d314f5c", "v0.3.6-767-g16a0ca0a1-14-g12d314f5c", true},
		{"stacked same", "v0.3.6-767-g16a0ca0a1-14-g12d314f5c", "v0.3.6-767-g16a0ca0a1-14-gabcdef0", false},
		{"stacked lower", "v0.3.6-767-g16a0ca0a1-10-g12d314f5c", "v0.3.6-767-g16a0ca0a1-14-gabcdef0", false},
		{"latest not fork", "v0.3.6", "v0.3.6-767-gabcdef0", false},
		{"current not fork", "v0.3.6-767-gabcdef0", "v0.3.6", false},
		{"both not fork", "v0.3.6", "v0.3.7", false},
		{"dirty latest", "v0.3.6-780-gabcdef0-dirty", "v0.3.6-767-g1234567", false},
		{"dirty current", "v0.3.6-780-gabcdef0", "v0.3.6-767-g1234567-dirty", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNewerForkVersion(tt.latest, tt.current); got != tt.want {
				t.Fatalf("IsNewerForkVersion(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

func TestIsUpdatableVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"clean semver", "v0.3.6", true},
		{"fork version", "v0.3.6-767-gabcdef0", true},
		{"dirty dev build", "v0.3.6-767-gabcdef0-dirty", false},
		{"empty", "", false},
		{"garbage", "garbage", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsUpdatableVersion(tt.in); got != tt.want {
				t.Fatalf("IsUpdatableVersion(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsNewerUpdatableVersion(t *testing.T) {
	tests := []struct {
		name            string
		latest, current string
		want            bool
	}{
		{"both release newer", "v0.3.7", "v0.3.6", true},
		{"both release same", "v0.3.6", "v0.3.6", false},
		{"both release older", "v0.3.5", "v0.3.6", false},
		{"both fork newer", "v0.3.6-780-gabcdef0", "v0.3.6-767-g1234567", true},
		{"both fork same", "v0.3.6-767-gabcdef0", "v0.3.6-767-g1234567", false},
		{"mismatch release vs fork", "v0.3.7", "v0.3.6-767-gabcdef0", false},
		{"mismatch fork vs release", "v0.3.7-5-gabcdef0", "v0.3.6", false},
		{"latest garbage", "garbage", "v0.3.6", false},
		{"current garbage", "v0.3.7", "garbage", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNewerUpdatableVersion(tt.latest, tt.current); got != tt.want {
				t.Fatalf("IsNewerUpdatableVersion(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}
