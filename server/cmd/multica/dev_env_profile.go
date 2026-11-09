package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
)

// A local development environment started by scripts/dev-env.sh runs its own
// daemon on a profile the script created, signed in to that environment's own
// API. A checkout an agent works in sits below the daemon-task marker, so
// without the exemption here every daemon lifecycle command for that profile
// is refused, and the agent can test the API, Web and Desktop but never a real
// agent run.
//
// The exemption is narrow on every axis. It covers only the daemon commands
// dev-env and its Desktop run, only an explicit --profile that the dev-env
// registry assigns to an environment, only when the daemon would talk to that
// environment's API, and only from a process the task's identity was stripped
// from. A loopback URL alone proves nothing: a self-hosted install can run its
// real server on localhost.

const (
	devEnvHomeEnv = "MULTICA_DEV_HOME"
	devEnvNameEnv = "MULTICA_DEV_ENV"
)

// The registry manifest is written by `printf '%q'`, which leaves values made
// of these characters unquoted. Anything else is not a value this check reads.
var devEnvManifestValue = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// requireHumanLocalDaemonCommand is requireHumanLocalCommand for the daemon
// lifecycle commands a dev-env environment runs against its own profiles.
func requireHumanLocalDaemonCommand(cmd *cobra.Command, command string) error {
	err := requireHumanLocalCommand(command)
	if err != nil && devEnvOwnsDaemonProfile(cmd) {
		return nil
	}
	return err
}

// devEnvOwnsDaemonProfile reports whether the explicit --profile is the CLI
// profile of a registered dev-env environment, or the one its Desktop signs
// in, and the daemon for it would connect to that environment's API.
func devEnvOwnsDaemonProfile(cmd *cobra.Command) bool {
	profile := resolveProfile(cmd)
	if profile == "" || taskIdentityInEnv() {
		return false
	}
	port := devEnvBackendPortForProfile(profile)
	if port == "" {
		return false
	}
	serverURL, err := daemon.NormalizeServerBaseURL(resolveDaemonServerURL(cmd, profile))
	return err == nil && serverURL == "http://localhost:"+port
}

// taskIdentityInEnv reports whether the task's own identity is still in the
// environment. dev-env launches every component without it; a daemon started
// with it would hand the task's token and config root to the runs it hosts.
func taskIdentityInEnv() bool {
	return inAgentExecutionContext() ||
		strings.TrimSpace(os.Getenv(cli.TaskConfigRootEnv)) != "" ||
		strings.TrimSpace(os.Getenv("MULTICA_TOKEN")) != "" ||
		strings.TrimSpace(os.Getenv("MULTICA_DAEMON_PORT")) != ""
}

// devEnvBackendPortForProfile returns the backend port of the registered
// environment that owns profile, or "" when none does. An environment owns its
// PROFILE and the `desktop-localhost-<backend port>` profile its Desktop
// derives from the backend URL.
func devEnvBackendPortForProfile(profile string) string {
	root := devEnvHome()
	if root == "" {
		return ""
	}
	manifests, _ := filepath.Glob(filepath.Join(root, "envs", "*", "manifest.env"))
	for _, path := range manifests {
		manifest := readDevEnvManifest(path)
		if manifest["NAME"] != filepath.Base(filepath.Dir(path)) {
			continue
		}
		port := manifest["BACKEND_PORT"]
		if n, err := strconv.Atoi(port); err != nil || n <= 0 || n > 65535 {
			continue
		}
		if profile == manifest["PROFILE"] || profile == "desktop-localhost-"+port {
			return port
		}
	}
	return ""
}

// launchingDevEnv returns the name of the registered dev-env environment that
// launched this process, or "" when none did. dev-env run from an agent task
// exports devEnvNameEnv to everything it starts with that task's identity
// stripped. It only
// changes what a refusal says, never whether a command is refused, and a name
// the registry does not hold is ignored so a stray export cannot hide a real
// leftover marker.
func launchingDevEnv() string {
	name := strings.TrimSpace(os.Getenv(devEnvNameEnv))
	root := devEnvHome()
	if !devEnvManifestValue.MatchString(name) || root == "" {
		return ""
	}
	manifest := readDevEnvManifest(filepath.Join(root, "envs", name, "manifest.env"))
	if manifest["NAME"] != name {
		return ""
	}
	return name
}

// devEnvHome mirrors DEV_HOME in scripts/dev-env.sh.
func devEnvHome() string {
	if root := strings.TrimSpace(os.Getenv(devEnvHomeEnv)); root != "" {
		return root
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".multica", "dev")
}

func readDevEnvManifest(path string) map[string]string {
	values := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return values
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok && devEnvManifestValue.MatchString(value) {
			values[key] = value
		}
	}
	return values
}
