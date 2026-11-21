package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

// ConfigureHermesTurnEnvironment declares per-prompt environment names for the
// ACP viewer. Values stay in the process environment, never in this config.
// Requires a Hermes build with turn_env support.
func ConfigureHermesTurnEnvironment(home string, names []string) error {
	path := filepath.Join(home, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	top := yamlDocumentRoot(&doc)
	terminal := yamlMapValue(top, "terminal")
	if terminal == nil || terminal.Kind != yaml.MappingNode {
		return fmt.Errorf("hermes config: missing derived terminal section")
	}
	// Replace rather than merge: a reused overlay must not retain prior names.
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	if existing := yamlMapValue(terminal, "client_env_turn_passthrough"); existing != nil && existing.Anchor != "" {
		expandAliasesOf(&doc, existing)
	}
	yamlSetMapValue(terminal, "client_env_turn_passthrough", yamlStringSeq(names))
	return marshalYAMLToFile(&doc, path)
}
