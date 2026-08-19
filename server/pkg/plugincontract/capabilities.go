package plugincontract

import (
	"fmt"
	"sort"
	"strings"
)

// Capabilities is what this host build can actually run. The manifest schema is
// defined in full from the first release so publishers never chase a moving
// contract, but the host lands the machinery one slice at a time. Anything a
// manifest declares that is not listed here fails installation loudly — a
// silently ignored contribution would look installed and never fire.
//
// Flip an entry on in the same change that lands its runtime.
type Capabilities struct {
	SurfaceTypes  map[string]bool
	HookTriggers  map[string]bool
	HookTransport map[string]bool
	ResourceTypes map[string]bool
}

// HostCapabilities is the currently shipped set.
//
// Action API + issue/sidebar surfaces land with the surface runtime; hooks land
// with the hook engine; the agent trigger, the mcp transport, and skill
// resources land with the agent integration.
func HostCapabilities() Capabilities {
	return Capabilities{
		// issue_panel only: PluginPanelSection is the one mount point that exists.
		// sidebar_panel has no host location yet, and enabling a surface the host
		// cannot render installs a plugin that silently never appears — precisely
		// what this gate exists to prevent.
		SurfaceTypes:  map[string]bool{SurfaceIssuePanel: true},
		HookTriggers:  map[string]bool{},
		HookTransport: map[string]bool{},
		ResourceTypes: map[string]bool{},
	}
}

// ErrCapabilityUnavailable describes contributions this build cannot run yet.
type ErrCapabilityUnavailable struct {
	Missing []string
}

func (e *ErrCapabilityUnavailable) Error() string {
	return fmt.Sprintf("this Multica version does not support: %s", strings.Join(e.Missing, ", "))
}

// CheckCapabilities reports every declared contribution the host cannot run.
// It returns all of them at once so an administrator sees the full gap instead
// of discovering it one failed install at a time.
func (m Manifest) CheckCapabilities(host Capabilities) error {
	missing := map[string]bool{}
	for _, surface := range m.Contributes.Surfaces {
		if !host.SurfaceTypes[surface.Type] {
			missing["surface "+surface.Type] = true
		}
	}
	for _, hook := range m.Contributes.Hooks {
		for _, trigger := range hook.Triggers {
			if !host.HookTriggers[trigger] {
				missing["hook trigger "+trigger] = true
			}
		}
		if !host.HookTransport[hook.Transport.Type] {
			missing["hook transport "+hook.Transport.Type] = true
		}
	}
	for _, resource := range m.Contributes.Resources {
		if !host.ResourceTypes[resource.Type] {
			missing["resource "+resource.Type] = true
		}
	}
	if len(missing) == 0 {
		return nil
	}
	names := make([]string, 0, len(missing))
	for name := range missing {
		names = append(names, name)
	}
	sort.Strings(names)
	return &ErrCapabilityUnavailable{Missing: names}
}
