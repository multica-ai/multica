package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const hermesGatewayStateMarker = ".multica-gateway-state-v1"

func isHermesGatewayStateEntry(name string) bool {
	return name == "logs" || name == "sessions" || name == "cron" ||
		strings.HasPrefix(name, "gateway.") || strings.HasPrefix(name, "gateway_")
}

// Older overlays mirrored these paths, including the host's live PID records
// and cron jobs. Detach them once, without following links into the source home.
func prepareHermesGatewayState(home string) error {
	marker := filepath.Join(home, hermesGatewayStateMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if isHermesGatewayStateEntry(entry.Name()) {
			if err := os.RemoveAll(filepath.Join(home, entry.Name())); err != nil {
				return err
			}
		}
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
