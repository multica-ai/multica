//go:build !windows

package daemon

import (
	"path/filepath"
	"testing"
)

func TestIsNameDispatchingAgentShim_RequiresExactDispatcherName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "Volta", path: filepath.Join("manager", "volta-shim"), want: true},
		{name: "Vite Plus", path: filepath.Join("manager", "vp"), want: true},
		{name: "case insensitive", path: filepath.Join("manager", "VP"), want: true},
		{name: "short name prefix", path: filepath.Join("manager", "vpn"), want: false},
		{name: "short name word", path: filepath.Join("manager", "vproxy"), want: false},
		{name: "extension is not stripped", path: filepath.Join("manager", "volta-shim.exe"), want: false},
		{name: "ordinary version target", path: filepath.Join("manager", "claude-2.1.216"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNameDispatchingAgentShim(tt.path); got != tt.want {
				t.Fatalf("isNameDispatchingAgentShim(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
