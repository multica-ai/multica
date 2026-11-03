package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// shadowingCLIOnPath returns the first `multica` on pathList that is not the
// same file as selfBin, or "" when every copy is the daemon's own binary.
//
// The daemon prepends its own directory to the agent's PATH, but agent shells
// source the user's rc files (`brew shellenv` and friends) before every
// command, which puts directories such as /opt/homebrew/bin back in front. Any
// other copy on the daemon's PATH can therefore win inside the agent, whatever
// its position here. That copy is often an older release, while the prompt is
// written by this daemon and may use flags only this build has (MUL-8519).
func shadowingCLIOnPath(selfBin, pathList string) string {
	selfInfo, err := os.Stat(selfBin)
	if err != nil {
		return ""
	}
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, "multica")
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		if os.SameFile(selfInfo, info) {
			continue
		}
		return candidate
	}
	return ""
}

// WithShadowedCLI tells the agent to invoke the daemon's own CLI by absolute
// path because another `multica` on PATH may shadow it.
func WithShadowedCLI(selfBin, shadow string) PromptOption {
	return func(o *promptOpts) {
		o.cliSelf = selfBin
		o.cliShadow = shadow
	}
}

var posixShellSafe = regexp.MustCompile(`^[A-Za-z0-9_./@%+=:,-]+$`)

func posixShellQuote(s string) string {
	if posixShellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildShadowedCLIBlock is rendered only on POSIX hosts: the invocation it
// prints is POSIX-quoted, and PowerShell needs a different call syntax.
func buildShadowedCLIBlock(selfBin, shadow string) string {
	if selfBin == "" || shadow == "" || strings.ContainsAny(selfBin+shadow, "`\r\n") {
		return ""
	}
	cmd := posixShellQuote(selfBin)
	var b strings.Builder
	b.WriteString("## Multica CLI binary\n\n")
	fmt.Fprintf(&b, "This daemon's Multica CLI is `%s`. Another copy is installed at `%s`, and your shell may resolve the bare `multica` name to it. That copy can be older than this daemon and reject flags or commands these instructions rely on (for example `unknown flag: --assignee-picker`).\n\n", selfBin, shadow)
	fmt.Fprintf(&b, "Run every Multica CLI command in this run through `%s` instead of the bare name, keeping the arguments exactly as instructed — for example `%s issue create ...` where the instructions say `multica issue create ...`.\n\n", cmd, cmd)
	return b.String()
}
