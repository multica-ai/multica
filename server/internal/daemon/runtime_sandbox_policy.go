package daemon

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

const sandboxNetworkModeDefault = "default"
const sandboxNetworkModeCustom = "custom"

type RuntimeSandboxPolicy struct {
	NetworkMode        string   `json:"network_mode,omitempty"`
	AllowedHosts       []string `json:"allowed_hosts,omitempty"`
	ExtraWritablePaths []string `json:"extra_writable_paths,omitempty"`
	DeniedPaths        []string `json:"denied_paths,omitempty"`
	DenyShell          bool     `json:"deny_shell,omitempty"`
	DeniedExecutables  []string `json:"denied_executables,omitempty"`
}

func parseRuntimeSandboxPolicy(raw json.RawMessage) RuntimeSandboxPolicy {
	if len(raw) == 0 {
		return RuntimeSandboxPolicy{}
	}
	var policy RuntimeSandboxPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return RuntimeSandboxPolicy{}
	}
	policy.NetworkMode = normalizeNetworkMode(policy.NetworkMode)
	policy.AllowedHosts = normalizeHostPorts(policy.AllowedHosts)
	policy.ExtraWritablePaths = normalizeAbsolutePolicyPaths(policy.ExtraWritablePaths)
	policy.DeniedPaths = normalizeAbsolutePolicyPaths(policy.DeniedPaths)
	policy.DeniedExecutables = normalizeAbsolutePolicyPaths(policy.DeniedExecutables)
	return policy
}

func normalizeNetworkMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case sandboxNetworkModeCustom:
		return sandboxNetworkModeCustom
	default:
		return sandboxNetworkModeDefault
	}
}

func normalizeAbsolutePolicyPaths(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" || !filepath.IsAbs(raw) {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	return out
}

func normalizeHostPorts(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	return out
}

func runtimeShellDenyExecutables(policy RuntimeSandboxPolicy) []string {
	out := append([]string{}, policy.DeniedExecutables...)
	if policy.DenyShell {
		out = append(out,
			"/bin/bash",
			"/bin/sh",
			"/bin/zsh",
			"/usr/bin/osascript",
			"/usr/bin/script",
			"/usr/bin/ssh",
		)
	}
	return normalizeAbsolutePolicyPaths(out)
}
