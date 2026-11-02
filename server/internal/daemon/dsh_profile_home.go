package daemon

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type dshProfileState int

const (
	dshProfileUnknown dshProfileState = iota
	dshProfileMissing
	dshProfilePresent
	dshProfileNeedsMigration
)

// The discovery tick only reads the selected launcher's manifest; it never
// boots a profile or invokes `plugin`/`--dump-config`, which can create files.
func dshMulticaProfileState(executablePath string) dshProfileState {
	dshHome := dshLauncherHome(executablePath)
	if dshHome == "" {
		return dshProfileUnknown
	}
	path := filepath.Join(dshHome, "profiles", dshMulticaProfileName, "package.json")
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return dshProfileMissing
	}
	if err != nil || !info.Mode().IsRegular() {
		return dshProfileUnknown
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return dshProfileUnknown
	}
	var manifest dshProfileManifest
	if json.Unmarshal(data, &manifest) != nil {
		return dshProfileUnknown
	}
	if dshMulticaProfileNeedsMigration(manifest) {
		return dshProfileNeedsMigration
	}
	if !dshProfileNamesRuntime(manifest) {
		return dshProfileMissing
	}
	return dshProfilePresent
}

var dshHomeAssignment = regexp.MustCompile(`(?i)\bDSH_HOME\s*=`)

// dshLauncherHome recognizes a literal DSH_HOME in a shell/batch launcher's
// prologue, before any command or control flow. Desktop shims own that value;
// the daemon's inherited environment must not override it. Dynamic assignments,
// unreadable launchers and opaque binaries have no reliable filesystem answer.
// Absence of an assignment does not prove inheritance: a launcher can source
// its environment or delegate to another wrapper without naming DSH_HOME.
// We do not execute or expand shell text to discover one. A successful protocol
// probe remains authoritative even when this cheap filesystem check is unknown.
func dshLauncherHome(executablePath string) string {
	info, err := os.Stat(executablePath)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	f, err := os.Open(executablePath)
	if err != nil {
		return ""
	}
	defer f.Close()
	const maxLauncherBytes = 64 * 1024
	data, err := io.ReadAll(io.LimitReader(f, maxLauncherBytes+1))
	if err != nil || len(data) > maxLauncherBytes || strings.ContainsRune(string(data), '\x00') {
		return ""
	}
	script := string(data)
	removeReferences := strings.NewReplacer("${DSH_HOME}", "", "$DSH_HOME", "", "%DSH_HOME%", "")
	assignments := dshHomeAssignment.FindAllStringIndex(script, -1)
	if len(assignments) != 0 {
		if len(assignments) != 1 {
			return ""
		}
		for _, line := range strings.Split(script, "\n") {
			line = strings.TrimSpace(line)
			lower := strings.ToLower(line)
			if line == "" || strings.HasPrefix(line, "#") || lower == "@echo off" || lower == "setlocal" {
				continue
			}
			remaining := removeReferences.Replace(strings.Replace(script, line, "", 1))
			if strings.Contains(strings.ToUpper(remaining), "DSH_HOME") {
				return ""
			}
			var value string
			switch {
			case strings.HasPrefix(line, "export DSH_HOME="):
				value = strings.TrimPrefix(line, "export DSH_HOME=")
			case strings.HasPrefix(lower, `set "dsh_home=`) && strings.HasSuffix(line, `"`):
				value = line[len(`set "DSH_HOME=`) : len(line)-1]
				if strings.ContainsAny(value, `%!^"`) {
					return ""
				}
				return absoluteDshHome(value)
			default:
				return ""
			}
			if len(value) < 2 || value[0] != value[len(value)-1] {
				return ""
			}
			quote := value[0]
			value = value[1 : len(value)-1]
			if (quote != '\'' && quote != '"') || strings.ContainsRune(value, rune(quote)) ||
				(quote == '"' && strings.ContainsAny(value, "$`\\")) {
				return ""
			}
			return absoluteDshHome(value)
		}
		return ""
	}
	return ""
}

func absoluteDshHome(home string) string {
	if !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Clean(home)
}
