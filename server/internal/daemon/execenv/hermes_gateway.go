package execenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const hermesGatewayStateMarker = ".multica-gateway-state-v1"

const hermesGatewayStateEnv = "MULTICA_HERMES_GATEWAY_STATE"

// HermesGatewayStateIsolated reports whether a task home must own its own
// Hermes gateway record.
//
// Hermes only runs a gateway for its root home or <root>/profiles/<name> when
// it is itself supervised by s6 (the official container image): any other home
// is refused with "profile_mismatch", and `hermes gateway run` there delegates
// to the supervisor instead of starting an unmanaged daemon. In that
// environment every task must share the container's single gateway, so the
// overlay keeps the mirrored gateway.lock instead of detaching it.
//
// Override with MULTICA_HERMES_GATEWAY_STATE=isolated|shared; default is
// auto-detect.
func HermesGatewayStateIsolated() bool {
	return hermesGatewayStateIsolated(os.Getenv(hermesGatewayStateEnv), "/proc/1/comm", "/run/s6/basedir")
}

// hermesGatewayStateIsolated mirrors the s6 probe Hermes itself uses: PID 1 is
// s6-svscan and the s6 base directory exists. Any probe error means isolated.
func hermesGatewayStateIsolated(mode, initComm, s6Basedir string) bool {
	switch strings.TrimSpace(mode) {
	case "isolated":
		return true
	case "shared":
		return false
	}
	comm, err := os.ReadFile(initComm)
	if err != nil || strings.TrimSpace(string(comm)) != "s6-svscan" {
		return true
	}
	fi, err := os.Stat(s6Basedir)
	return err != nil || !fi.IsDir()
}

// hermesGatewayOwnerHome returns the home of the gateway a shared-mode task
// attaches to, read from the gateway.lock (fallback gateway.pid) record the
// overlay mirrors from the shared home. The record is the same JSON Hermes
// writes for itself ({"pid":…,"kind":"hermes-gateway","hermes_home":…}); the
// overlay entry is read through any link. ok is false when no record names a
// home, so callers keep their own answer rather than inventing one.
func hermesGatewayOwnerHome(home string) (string, bool) {
	for _, name := range []string{"gateway.lock", "gateway.pid"} {
		data, err := os.ReadFile(filepath.Join(home, name))
		if err != nil {
			continue
		}
		var record struct {
			HermesHome string `json:"hermes_home"`
		}
		if json.Unmarshal(data, &record) != nil {
			continue
		}
		if owner := strings.TrimSpace(record.HermesHome); owner != "" {
			return owner, true
		}
	}
	return "", false
}

func isHermesGatewayStateEntry(name string) bool {
	return name == "logs" || name == "sessions" || name == "cron" ||
		strings.HasPrefix(name, "gateway.") || strings.HasPrefix(name, "gateway_")
}

// prepareHermesGatewayState gives an isolated task home its own gateway state.
//
// Older overlays mirrored these paths, including the host's live PID records
// and cron jobs. Detach them once, without following links into the source home.
//
// The marker only records that one-time migration; it does not mean the home
// is still detached. Shared mode (see HermesGatewayStateIsolated) links
// gateway.lock, sessions, logs and cron into the container's gateway home and
// leaves an existing marker in place, so when an operator flips
// MULTICA_HERMES_GATEWAY_STATE back to isolated, or the home moves to a non-s6
// host, those links would survive and the task would keep attaching to a
// gateway it is now supposed to own. Every isolated prepare therefore removes
// gateway-state entries that are links (the link itself, never its target).
// Regular files and real directories past the marker are the task's own
// gateway record and sessions/ and must survive reuse.
func prepareHermesGatewayState(home string) error {
	marker := filepath.Join(home, hermesGatewayStateMarker)
	migrated := false
	if _, err := os.Stat(marker); err == nil {
		migrated = true
	} else if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !isHermesGatewayStateEntry(entry.Name()) {
			continue
		}
		path := filepath.Join(home, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			if err := os.Remove(path); err != nil {
				return err
			}
		} else if !migrated {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	if migrated {
		return nil
	}
	return writeFileAtomic(marker, []byte("isolated Hermes gateway state\n"), 0o600)
}

// ACP needs the local session runtime, not messaging adapters or HTTP ingress.
// Explicit false survives credentials from both dotenv and the process env in
// old and gateway-backed Hermes. Include env-only bundled platforms, as well as
// every platform configured by the user, including third-party plugins.
// When validating a new Hermes release, check gateway.config.Platform and
// plugins/platforms for new env-only adapters and update this list.
var hermesGatewayPlatforms = []string{
	"local", "telegram", "discord", "whatsapp", "whatsapp_cloud", "slack",
	"signal", "mattermost", "matrix", "email", "sms", "dingtalk", "api_server",
	"webhook", "msgraph_webhook", "feishu", "wecom", "wecom_callback", "weixin",
	"bluebubbles", "qqbot", "yuanbao", "relay", "a2a", "buzz", "google_chat",
	"irc", "line", "ntfy", "photon", "raft", "simplex", "teams",
}

func disableHermesGatewayIngress(doc *yaml.Node) error {
	top := yamlDocumentRoot(doc)
	if top == nil {
		return fmt.Errorf("hermes gateway config: expected a mapping")
	}
	platforms := map[string]map[string]any{}
	if existing := yamlMapValue(top, "platforms"); existing != nil {
		if err := existing.Decode(&platforms); err != nil {
			return fmt.Errorf("hermes platforms config: %w", err)
		}
		detachHermesConfigAliases(doc, existing)
	}
	if platforms == nil {
		platforms = map[string]map[string]any{}
	}
	nested := resolveHermesYAMLAlias(yamlMapValue(top, "gateway"))
	if nested != nil && nested.Kind == yaml.MappingNode {
		if configured := yamlMapValue(nested, "platforms"); configured != nil {
			var entries map[string]any
			if err := configured.Decode(&entries); err != nil {
				return fmt.Errorf("hermes gateway platforms config: %w", err)
			}
			for name := range entries {
				if _, exists := platforms[name]; !exists {
					platforms[name] = nil
				}
			}
		}
	}
	for _, name := range hermesGatewayPlatforms {
		if platforms[name] == nil {
			platforms[name] = map[string]any{}
		}
	}
	for name, config := range platforms {
		if config == nil {
			config = map[string]any{}
			platforms[name] = config
		}
		config["enabled"] = false
	}
	var node yaml.Node
	if err := node.Encode(platforms); err != nil {
		return err
	}
	yamlSetMapValue(top, "platforms", &node)
	// gateway.<platform> shorthand overrides the top-level platform blocks.
	if nested != nil && nested.Kind == yaml.MappingNode {
		var settings map[string]any
		if err := nested.Decode(&settings); err != nil {
			return err
		}
		for name := range platforms {
			if config, ok := settings[name].(map[string]any); ok {
				config["enabled"] = false
			}
		}
		detachHermesConfigAliases(doc, nested)
		var derived yaml.Node
		if err := derived.Encode(settings); err != nil {
			return err
		}
		yamlSetMapValue(top, "gateway", &derived)
	}
	yamlSetMapValue(top, "multiplex_profiles", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"})
	return nil
}

// Preserve references elsewhere in the config before replacing an anchored section.
func detachHermesConfigAliases(doc, node *yaml.Node) {
	if node.Anchor != "" {
		expandAliasesOf(doc, node)
	}
	for _, child := range node.Content {
		detachHermesConfigAliases(doc, child)
	}
}
