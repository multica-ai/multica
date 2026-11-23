//go:build !windows

package agent

import (
	"os"
	"strings"
	"testing"
)

func tomcatFixtureEnv(home string) []string {
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "SHELL=/bin/sh", "NO_PROXY=127.0.0.1,localhost", "TOMCAT__UI__LANGUAGE=en"}
	return env
}

func tomcatHostEnv(fixtureEnv []string) map[string]string {
	environment := map[string]string{}
	// A separate host inherits only the fixture's explicit values.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		environment[key] = ""
	}
	for _, entry := range fixtureEnv {
		key, value, _ := strings.Cut(entry, "=")
		environment[key] = value
	}
	return environment
}

func TestTomcatFixtureEnvironment(t *testing.T) {
	for _, active := range []string{"1", ""} {
		t.Run("parent-active="+active, func(t *testing.T) {
			t.Setenv("TOMCAT_AGENT_ACTIVE", active)
			t.Setenv("TOMCAT__LLM__DEFAULT_MODEL", "host-model")
			t.Setenv("OPENAI_API_KEY", "host-sentinel")
			t.Setenv("HOME", "/host-home")
			home := t.TempDir()
			env := tomcatFixtureEnv(home)
			for name, final := range map[string][]string{"cli": env, "backend": buildEnv(tomcatHostEnv(env))} {
				values := map[string]string{}
				for _, entry := range final {
					key, value, _ := strings.Cut(entry, "=")
					values[key] = value
				}
				if values["HOME"] != home {
					t.Errorf("%s HOME = %q, want private %q", name, values["HOME"], home)
				}
				for _, key := range []string{"TOMCAT_AGENT_ACTIVE", "TOMCAT__LLM__DEFAULT_MODEL", "OPENAI_API_KEY"} {
					if values[key] != "" {
						t.Errorf("%s inherited host key %s", name, key)
					}
				}
			}
			if os.Getenv("HOME") != "/host-home" || os.Getenv("TOMCAT_AGENT_ACTIVE") != active {
				t.Fatal("fixture changed parent environment")
			}
		})
	}
}
