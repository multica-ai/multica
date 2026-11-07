package agentcli

import (
	"os"
	"path/filepath"
	"strings"
)

// ChannelFromPath classifies an executable path from its location alone.
// It returns "" when the path has no npm or Homebrew marker; callers that
// know the file exists should then treat it as a direct binary.
//
// npm: the path (or its symlink target) contains node_modules or an npm
// global directory. Homebrew: Cellar, homebrew, or linuxbrew.
func ChannelFromPath(path string) string {
	p := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	if p == "" || p == "." {
		return ""
	}
	if strings.Contains(p, "node_modules") || strings.Contains(p, "/.npm/") || strings.Contains(p, "/npm-global/") {
		return ChannelNPM
	}
	if strings.Contains(p, "/cellar/") || strings.Contains(p, "/homebrew/") || strings.Contains(p, "linuxbrew") {
		return ChannelBrew
	}
	return ""
}

// HasNPMPrefix reports whether path looks like an npm global bin shim:
// <prefix>/bin/<cli> with <prefix>/lib/node_modules, or the Windows npm
// layout <prefix>/npm/<cli> with <prefix>/npm/node_modules. stat is os.Stat
// in production and a fake in tests.
func HasNPMPrefix(path string, stat func(string) (os.FileInfo, error)) bool {
	if stat == nil {
		return false
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	dir := filepath.Dir(path)
	base := filepath.Base(dir)
	var candidates []string
	switch {
	case strings.EqualFold(base, "bin"):
		prefix := filepath.Dir(dir)
		candidates = []string{
			filepath.Join(prefix, "lib", "node_modules"),
			filepath.Join(prefix, "node_modules"),
		}
	case strings.EqualFold(base, "npm"):
		candidates = []string{filepath.Join(dir, "node_modules")}
	default:
		return false
	}
	for _, candidate := range candidates {
		info, err := stat(candidate)
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}
