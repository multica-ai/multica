// Package tasktoken issues short-lived identity tokens for agent tasks.
//
// A deployment configures a catalog of templates; each agent enables the
// subset it needs. At claim time the server resolves the run's accountable
// human, interpolates that identity into the template's claims, signs a JWT,
// and the daemon injects it under the template's env name. The private key
// and the catalog live only in server configuration — the web UI can pick
// from the catalog but never define what may be signed.
package tasktoken

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// DefaultTTL applies when a template omits "ttl".
	DefaultTTL = time.Hour
	// MaxTTL caps how long an issued token may live. A token sits in the
	// agent process environment for the whole run, so its lifetime is the
	// exposure window.
	MaxTTL = 24 * time.Hour
)

// envNamePattern is the shell-safe environment variable name form.
var envNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// reservedEnvNames are names the daemon owns. The daemon blocks these again
// at injection time (isBlockedEnvKey); rejecting them here turns a silently
// dropped variable into a startup error the operator can act on.
var reservedEnvNames = map[string]struct{}{
	"HOME": {}, "PATH": {}, "USER": {}, "SHELL": {}, "TERM": {},
	"TMPDIR": {}, "TMP": {}, "TEMP": {},
	"CODEX_HOME": {}, "REASONIX_STATE_HOME": {}, "CURSOR_DATA_DIR": {},
	"OPENCLAW_CONFIG_PATH": {}, "OPENCLAW_INCLUDE_ROOTS": {}, "HERMES_HOME": {},
}

// reservedClaims are written by the signer itself and must not be templated.
var reservedClaims = map[string]struct{}{"iat": {}, "exp": {}, "jti": {}}

// supportedAlgorithms is deliberately asymmetric-only: the verifier is a
// separate system that must not need the signing secret.
var supportedAlgorithms = map[string]struct{}{
	"ES256": {}, "ES384": {}, "RS256": {}, "RS384": {},
}

// knownVariables is the closed set of interpolation variables. Anything else
// in a template is a configuration error, caught at startup rather than
// silently emitting an empty claim at issue time.
var knownVariables = map[string]struct{}{
	"identity.email":       {},
	"identity.email_local": {},
	"identity.name":        {},
	"identity.id":          {},
	"identity.source":      {},
	"workspace.id":         {},
	"workspace.slug":       {},
	"agent.id":             {},
	"agent.name":           {},
	"task.id":              {},
}

// variablePattern matches "{{ name }}" with optional inner whitespace.
var variablePattern = regexp.MustCompile(`\{\{\s*([a-z_.]+)\s*\}\}`)

// Template is one entry in the configured catalog.
type Template struct {
	ID          string
	Label       string
	Description string
	Env         string
	Algorithm   string
	KeyID       string
	TTL         time.Duration
	Claims      map[string]string
	// AllowedDomains, when non-empty, restricts issuing to identities whose
	// email domain is in the list (compared case-insensitively). The identity's
	// email is the root of the whole trust chain, so a template for a sensitive
	// system can refuse to speak for guests and external collaborators whose
	// addresses live outside the corporate domain.
	AllowedDomains []string
	// Manifest is opaque descriptive metadata about the system this token
	// opens — a base URL, a display name, whatever the consuming tool needs to
	// know to actually use it. The issuer collects the manifests of the
	// templates it successfully signed into one JSON array and injects it
	// under ManifestEnv, so a tool's view of "systems I can reach" is derived
	// from the tokens it actually holds and cannot drift out of sync.
	//
	// Nothing here is interpreted: it is copied through verbatim.
	Manifest json.RawMessage
}

// Catalog is a parsed, fully validated set of templates. A nil *Catalog means
// the feature is not configured.
type Catalog struct {
	ordered []Template
	byID    map[string]Template
}

// templateJSON mirrors the wire form; TTL arrives as a Go duration string.
type templateJSON struct {
	ID             string            `json:"id"`
	Label          string            `json:"label"`
	Description    string            `json:"description"`
	Env            string            `json:"env"`
	Algorithm      string            `json:"algorithm"`
	KeyID          string            `json:"key_id"`
	TTL            string            `json:"ttl"`
	Claims         map[string]string `json:"claims"`
	AllowedDomains []string          `json:"allowed_domains"`
	Manifest       json.RawMessage   `json:"manifest"`
}

// ParseCatalog parses and validates the catalog JSON. Blank input returns
// (nil, nil): the feature is off. Any invalid entry is an error — the caller
// is expected to refuse to start rather than run on half a configuration.
func ParseCatalog(raw string) (*Catalog, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var entries []templateJSON
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}

	c := &Catalog{byID: make(map[string]Template, len(entries))}
	for i, e := range entries {
		tpl, err := validateEntry(e)
		if err != nil {
			return nil, fmt.Errorf("catalog entry %d (id=%q): %w", i, e.ID, err)
		}
		if _, dup := c.byID[tpl.ID]; dup {
			return nil, fmt.Errorf("catalog entry %d: duplicate id %q", i, tpl.ID)
		}
		c.byID[tpl.ID] = tpl
		c.ordered = append(c.ordered, tpl)
	}
	return c, nil
}

func validateEntry(e templateJSON) (Template, error) {
	tpl := Template{
		ID:          strings.TrimSpace(e.ID),
		Label:       strings.TrimSpace(e.Label),
		Description: strings.TrimSpace(e.Description),
		Env:         strings.TrimSpace(e.Env),
		Algorithm:   strings.TrimSpace(e.Algorithm),
		KeyID:       strings.TrimSpace(e.KeyID),
		Claims:      e.Claims,
		Manifest:    e.Manifest,
	}

	if tpl.ID == "" {
		return Template{}, fmt.Errorf("id is required")
	}
	if tpl.Label == "" {
		return Template{}, fmt.Errorf("label is required")
	}
	if tpl.Env == "" {
		return Template{}, fmt.Errorf("env is required")
	}
	if !envNamePattern.MatchString(tpl.Env) {
		return Template{}, fmt.Errorf("env must match %s, got %q", envNamePattern, tpl.Env)
	}
	if strings.HasPrefix(tpl.Env, "MULTICA_") {
		return Template{}, fmt.Errorf("env %q is reserved: the MULTICA_ prefix belongs to the daemon", tpl.Env)
	}
	if _, bad := reservedEnvNames[tpl.Env]; bad {
		return Template{}, fmt.Errorf("env %q is reserved by the daemon", tpl.Env)
	}

	if tpl.Algorithm == "" {
		tpl.Algorithm = "ES256"
	}
	if _, ok := supportedAlgorithms[tpl.Algorithm]; !ok {
		return Template{}, fmt.Errorf("unsupported algorithm %q", tpl.Algorithm)
	}

	if e.TTL == "" {
		tpl.TTL = DefaultTTL
	} else {
		d, err := time.ParseDuration(e.TTL)
		if err != nil {
			return Template{}, fmt.Errorf("invalid ttl %q: %w", e.TTL, err)
		}
		if d <= 0 {
			return Template{}, fmt.Errorf("ttl must be positive, got %q", e.TTL)
		}
		if d > MaxTTL {
			return Template{}, fmt.Errorf("ttl %q exceeds max %v", e.TTL, MaxTTL)
		}
		tpl.TTL = d
	}

	if len(tpl.Claims) == 0 {
		return Template{}, fmt.Errorf("claims is required and must be non-empty")
	}
	for name, value := range tpl.Claims {
		if _, bad := reservedClaims[name]; bad {
			return Template{}, fmt.Errorf("reserved claim %q is written by the signer", name)
		}
		for _, m := range variablePattern.FindAllStringSubmatch(value, -1) {
			if _, ok := knownVariables[m[1]]; !ok {
				return Template{}, fmt.Errorf("unknown variable %q in claim %q", m[1], name)
			}
		}
	}

	for _, raw := range e.AllowedDomains {
		domain := strings.ToLower(strings.TrimSpace(raw))
		if domain == "" || strings.ContainsAny(domain, "@ \t") {
			return Template{}, fmt.Errorf("allowed_domains entry %q is not a domain", raw)
		}
		tpl.AllowedDomains = append(tpl.AllowedDomains, domain)
	}

	if len(tpl.Manifest) > 0 && !json.Valid(tpl.Manifest) {
		return Template{}, fmt.Errorf("manifest is not valid JSON")
	}

	return tpl, nil
}

// ValidateEnvName reports whether name is usable as an injected environment
// variable: shell-safe in form, and not one the daemon owns.
func ValidateEnvName(name string) error {
	if !envNamePattern.MatchString(name) {
		return fmt.Errorf("env name must match %s, got %q", envNamePattern, name)
	}
	if strings.HasPrefix(name, "MULTICA_") {
		return fmt.Errorf("env name %q is reserved: the MULTICA_ prefix belongs to the daemon", name)
	}
	if _, bad := reservedEnvNames[name]; bad {
		return fmt.Errorf("env name %q is reserved by the daemon", name)
	}
	return nil
}

// Get returns the template with the given id.
func (c *Catalog) Get(id string) (Template, bool) {
	if c == nil {
		return Template{}, false
	}
	tpl, ok := c.byID[id]
	return tpl, ok
}

// List returns every template in configuration order.
func (c *Catalog) List() []Template {
	if c == nil {
		return nil
	}
	out := make([]Template, len(c.ordered))
	copy(out, c.ordered)
	return out
}
