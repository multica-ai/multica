//go:build darwin

package localenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// runningAppsScript reports, for each pid argument, the app it is when its
// activation policy is regular: it has a Dock icon and windows of its own.
// AppKit through the JXA bridge needs neither Accessibility nor Automation
// permission.
const runningAppsScript = `ObjC.import("AppKit");
function run(argv) {
  const out = {};
  for (const arg of argv) {
    const app = $.NSRunningApplication.runningApplicationWithProcessIdentifier(Number(arg));
    if (app.isNil() || app.activationPolicy !== $.NSApplicationActivationPolicyRegular) continue;
    out[arg] = {
      name: app.localizedName.isNil() ? "" : app.localizedName.js,
      bundle: app.bundleURL.isNil() ? "" : app.bundleURL.path.js,
    };
  }
  return JSON.stringify(out);
}`

// focusScript prepares the app with pid argv[0] to come forward, provided it
// still is the app at bundle argv[1]: a pid reused since detection is someone
// else's window.
//
// Since macOS 14 an activation request from a background process (the daemon)
// is accepted and then ignored, and activation never restores minimized
// windows. LaunchServices opening the bundle does both, the same as a Dock
// click, but it names an app by bundle, so the script answers "open" only when
// this process is the one running instance of its bundle. Otherwise it asks
// AppKit by pid and answers "requested"; whether that took is only visible
// from another process.
const focusScript = `ObjC.import("AppKit");
function run(argv) {
  const pid = Number(argv[0]);
  const app = $.NSRunningApplication.runningApplicationWithProcessIdentifier(pid);
  if (app.isNil() || app.activationPolicy !== $.NSApplicationActivationPolicyRegular) return "gone";
  const bundle = app.bundleURL.isNil() ? "" : app.bundleURL.path.js;
  if (bundle !== argv[1]) return "gone";
  app.unhide;
  const twins = bundle === "" ? 0 : ObjC.unwrap($.NSWorkspace.sharedWorkspace.runningApplications)
    .filter((other) => !other.bundleURL.isNil() && other.bundleURL.path.js === bundle).length;
  if (twins === 1) return "open";
  app.activateWithOptions($.NSApplicationActivateAllWindows);
  return "requested";
}`

const frontmostScript = `ObjC.import("AppKit");
$.NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier`

func inspectProcesses(ctx context.Context, pids []int) map[int]Process {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	list := make([]string, len(pids))
	for i, pid := range pids {
		list[i] = strconv.Itoa(pid)
	}
	out, err := matchOutput(exec.CommandContext(ctx, "ps", "-ww", "-o", "pid=,args=", "-p", strings.Join(list, ",")))
	if err != nil {
		return nil
	}
	procs := parsePsArgs(out)
	var candidates []string
	for pid, p := range procs {
		// Only an executable inside an app bundle can be an app; asking AppKit
		// about anything else would start osascript for nothing.
		if strings.Contains(p.Args, ".app/Contents/MacOS/") {
			candidates = append(candidates, strconv.Itoa(pid))
		}
	}
	if len(candidates) == 0 {
		return procs
	}
	// Until AppKit names a candidate as an app the answer is not final: it
	// may still be starting, or osascript may have failed this once.
	for _, c := range candidates {
		pid, _ := strconv.Atoi(c)
		p := procs[pid]
		p.Unsettled = true
		procs[pid] = p
	}
	raw, err := exec.CommandContext(ctx, "osascript", append([]string{"-l", "JavaScript", "-e", runningAppsScript}, candidates...)...).Output()
	if err != nil {
		return procs
	}
	var apps map[string]struct {
		Name   string `json:"name"`
		Bundle string `json:"bundle"`
	}
	if json.Unmarshal(raw, &apps) != nil {
		return procs
	}
	for key, app := range apps {
		pid, _ := strconv.Atoi(key)
		if p, ok := procs[pid]; ok && app.Name != "" {
			p.App, p.Bundle, p.Unsettled = app.Name, app.Bundle, false
			procs[pid] = p
		}
	}
	return procs
}

func focusApp(ctx context.Context, pid int, bundle string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", focusScript, strconv.Itoa(pid), bundle).Output()
	if err != nil {
		return fmt.Errorf("bring the app forward: %w", err)
	}
	switch strings.TrimSpace(string(out)) {
	case "gone":
		return ErrAppGone
	case "open":
		if err := exec.CommandContext(ctx, "/usr/bin/open", bundle).Run(); err != nil {
			return fmt.Errorf("open the app's bundle: %w", err)
		}
	case "requested":
	default:
		return fmt.Errorf("bring the app forward: unexpected answer %q", out)
	}
	// macOS keeps the user's current app in front while they are using it, and
	// says so to nobody; only the frontmost app, read afresh, tells.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(400 * time.Millisecond):
	}
	front, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", frontmostScript).Output()
	if err != nil {
		return fmt.Errorf("check the frontmost app: %w", err)
	}
	if strings.TrimSpace(string(front)) != strconv.Itoa(pid) {
		return errors.New("macOS kept another app in front")
	}
	return nil
}
