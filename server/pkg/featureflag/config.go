package featureflag

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnvFlagFile is the environment variable consulted by NewServiceFromEnv to
// locate a YAML rule file. It follows the same convention as the other
// MULTICA_*_CONFIG / MULTICA_*_FILE knobs documented in .env.example.
const EnvFlagFile = "MULTICA_FEATURE_FLAGS_FILE"

// EnvOverridePrefix is the prefix EnvProvider uses when NewServiceFromEnv
// composes the standard provider chain. Individual flags can be overridden
// at runtime with `FF_<FLAG_KEY>=true|false|42%|<variant>` env vars without
// touching the YAML file — the env override beats the file value.
const EnvOverridePrefix = "FF_"

// ruleConfig is the wire format used by the YAML / JSON loader. It mirrors
// Rule but uses snake_case keys (for YAML ergonomics) and pointer types so
// we can tell "unset" from "explicit zero".
//
// Keeping the wire shape separate from runtime Rule means the config format
// can evolve (add fields, deprecate names) without forcing every business
// caller of Rule to recompile against the new shape.
type ruleConfig struct {
	Default *bool          `yaml:"default,omitempty"`
	Variant string         `yaml:"variant,omitempty"`
	Allow   []string       `yaml:"allow,omitempty"`
	AllowBy string         `yaml:"allow_by,omitempty"`
	Deny    []string       `yaml:"deny,omitempty"`
	DenyBy  string         `yaml:"deny_by,omitempty"`
	Percent *percentConfig `yaml:"percent,omitempty"`
}

type percentConfig struct {
	Percent int    `yaml:"percent"`
	By      string `yaml:"by,omitempty"`
}

// toRule converts the wire shape to a runtime Rule, applying defaults for
// fields that the YAML omitted.
func (rc ruleConfig) toRule() Rule {
	r := Rule{
		Variant: rc.Variant,
		Allow:   rc.Allow,
		AllowBy: rc.AllowBy,
		Deny:    rc.Deny,
		DenyBy:  rc.DenyBy,
	}
	if rc.Default != nil {
		r.Default = *rc.Default
	}
	if rc.Percent != nil {
		r.Percent = &PercentRollout{
			Percent: rc.Percent.Percent,
			By:      rc.Percent.By,
		}
	}
	return r
}

// LoadRulesFromYAMLFile reads a YAML file mapping flag keys to rule
// definitions and returns the parsed map ready to be installed on a
// StaticProvider via LoadRules.
//
// Schema (every field except `default` is optional):
//
//	billing_new_invoice_email:
//	  default: true
//
//	checkout_algo:
//	  default: false
//	  variant: experiment-v2
//	  percent:
//	    percent: 25
//	    by: user_id
//
//	ops_disable_recommendations:
//	  default: false
//	  allow: ["user-internal-1", "user-internal-2"]
//
// An empty or whitespace-only file returns an empty map with no error, so
// operators can drop a flags file in place before authoring any flag
// without breaking server startup.
func LoadRulesFromYAMLFile(path string) (map[string]Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("featureflag: read %s: %w", path, err)
	}
	return parseRulesYAML(data)
}

// parseRulesYAML is the file-format-aware core of LoadRulesFromYAMLFile.
// Exposed unexported so tests can exercise the parser without touching the
// filesystem.
func parseRulesYAML(data []byte) (map[string]Rule, error) {
	// An empty body is a valid "no flags defined yet" state. yaml.Unmarshal
	// on `nil` leaves the destination nil, so handle this explicitly to
	// return an empty (non-nil) map for the convenience of callers.
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]Rule{}, nil
	}
	var raw map[string]ruleConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("featureflag: parse: %w", err)
	}
	out := make(map[string]Rule, len(raw))
	for key, rc := range raw {
		out[key] = rc.toRule()
	}
	return out, nil
}

// NewServiceFromEnv constructs a Service wired with the standard multica
// config sources, in order of decreasing precedence:
//
//  1. EnvProvider (FF_<KEY> overrides — Ops kill switches, fastest path).
//  2. StaticProvider loaded from the YAML file at MULTICA_FEATURE_FLAGS_FILE
//     (when the env var is set and the file exists).
//
// When MULTICA_FEATURE_FLAGS_FILE is unset, the Service still works — the
// EnvProvider is the sole layer, and IsEnabled falls through to the
// caller's default for any flag without an FF_<KEY> override. The server
// can therefore boot before any flag config is authored.
//
// When the file path is set but the file is malformed, this returns an
// error rather than silently dropping the configuration — operators
// expect feature-flag misconfig to fail loudly the way every other
// config knob does (DATABASE_URL parse errors, JWT_SECRET missing in
// production, etc.).
func NewServiceFromEnv(opts ...Option) (*Service, error) {
	var providers []Provider
	providers = append(providers, NewEnvProvider(EnvOverridePrefix))

	path := strings.TrimSpace(os.Getenv(EnvFlagFile))
	var loadedCount int
	if path != "" {
		rules, err := LoadRulesFromYAMLFile(path)
		if err != nil {
			return nil, err
		}
		sp := NewStaticProvider()
		sp.LoadRules(rules)
		providers = append(providers, sp)
		loadedCount = len(rules)
	}

	svc := NewService(NewChainProvider(providers...), opts...)
	if svc.logger != nil {
		svc.logger.Info("feature flags initialised",
			slog.String("file", path),
			slog.Int("rules", loadedCount),
			slog.String("env_prefix", EnvOverridePrefix),
		)
	}
	return svc, nil
}
