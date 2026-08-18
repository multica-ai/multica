// Package plugincontract defines the versioned public contract between plugin
// authors and the Multica host. It must stay independent from private handlers,
// services, and database implementations.
//
// A plugin relates to Multica in exactly three ways:
//
//   - Action   (plugin -> Multica): host capabilities the plugin calls.
//   - Hook     (Multica -> plugin): plugin capabilities the host calls.
//   - Resource (no call at all):    static contributions such as skill text.
//
// "Who triggers" and "what capability is called" are orthogonal: a hook is
// declared once and may be invoked by any of the triggers it opts into.
package plugincontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

const (
	// ManifestVersion1 is the only manifest version this host understands.
	ManifestVersion1 = 1

	// ManifestFilename is the conventional file name inside a plugin package.
	ManifestFilename = "multica.plugin.json"

	// MaxManifestSize bounds a fetched manifest before it is parsed.
	MaxManifestSize = 1 << 20

	// MaxVersionLength mirrors the plugin_installation.version column bound.
	MaxVersionLength = 64
)

// Surface types. A surface is an iframe the host mounts at a fixed location.
const (
	SurfaceIssuePanel   = "issue_panel"
	SurfaceSidebarPanel = "sidebar_panel"
	SurfaceModal        = "modal"
)

// Hook triggers. Declaring a trigger says who may invoke the hook, never what
// the hook does. Only `event` is asynchronous, and it never blocks the host.
const (
	TriggerUI     = "ui"
	TriggerManual = "manual"
	TriggerAgent  = "agent"
	TriggerEvent  = "event"
)

// Hook transports.
const (
	TransportHTTP = "http"
	TransportMCP  = "mcp"
)

// Resource types. Resources involve no call in either direction.
const (
	ResourceSkill = "skill"
)

// Config field types. This is a deliberately small subset of JSON Schema: the
// host renders the configuration form from it, so plugins never ship form UI.
const (
	ConfigString = "string"
	ConfigNumber = "number"
	ConfigBool   = "bool"
	ConfigEnum   = "enum"
	ConfigSecret = "secret"
)

// Scopes. This list is complete and closed; anything else is rejected at parse
// time. `net:<domain>` is the only parameterized form.
const (
	ScopeIssuesRead       = "issues:read"
	ScopeIssuesWrite      = "issues:write"
	ScopeCommentsRead     = "comments:read"
	ScopeCommentsWrite    = "comments:write"
	ScopeTasksRead        = "tasks:read"
	ScopeTasksWrite       = "tasks:write"
	ScopeAgentsRead       = "agents:read"
	ScopeMembersRead      = "members:read"
	ScopeStorageUser      = "storage:user"
	ScopeStorageWorkspace = "storage:workspace"

	// ScopeNetPrefix guards outbound network access: both the iframe CSP
	// connect-src allowlist and the hook transport host check derive from it.
	ScopeNetPrefix = "net:"
)

// Product events an `event`-triggered hook may subscribe to.
const (
	EventIssueCreated       = "issue.created"
	EventIssueUpdated       = "issue.updated"
	EventIssueStatusChanged = "issue.status_changed"
	EventCommentCreated     = "comment.created"
	EventTaskStarted        = "task.started"
	EventTaskCompleted      = "task.completed"
	EventTaskFailed         = "task.failed"
)

var fixedScopes = map[string]bool{
	ScopeIssuesRead:       true,
	ScopeIssuesWrite:      true,
	ScopeCommentsRead:     true,
	ScopeCommentsWrite:    true,
	ScopeTasksRead:        true,
	ScopeTasksWrite:       true,
	ScopeAgentsRead:       true,
	ScopeMembersRead:      true,
	ScopeStorageUser:      true,
	ScopeStorageWorkspace: true,
}

var knownEvents = map[string]bool{
	EventIssueCreated:       true,
	EventIssueUpdated:       true,
	EventIssueStatusChanged: true,
	EventCommentCreated:     true,
	EventTaskStarted:        true,
	EventTaskCompleted:      true,
	EventTaskFailed:         true,
}

var (
	pluginKeySegmentPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	contributionKeyPattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[_-][a-z0-9]+)*$`)
	semverPattern           = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	netDomainPattern        = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	relativePathPattern     = regexp.MustCompile(`^[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*$`)
)

type Manifest struct {
	ManifestVersion int          `json:"manifest_version"`
	Key             string       `json:"key"`
	Name            string       `json:"name"`
	Description     string       `json:"description,omitempty"`
	Version         string       `json:"version"`
	Author          Author       `json:"author"`
	Icon            string       `json:"icon,omitempty"`
	Scopes          []string     `json:"scopes"`
	Config          ConfigSchema `json:"config,omitempty"`
	Contributes     Contributes  `json:"contributes"`
}

type Author struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type Contributes struct {
	Surfaces  []Surface  `json:"surfaces,omitempty"`
	Hooks     []Hook     `json:"hooks,omitempty"`
	Resources []Resource `json:"resources,omitempty"`
}

// Surface is a host-mounted iframe. The host owns where it appears; the plugin
// owns what renders inside it.
type Surface struct {
	Key       string   `json:"key"`
	Type      string   `json:"type"`
	Name      string   `json:"name"`
	Entry     string   `json:"entry"`
	Platforms []string `json:"platforms,omitempty"`
}

// Hook is one plugin-side capability. Triggers say who may invoke it; the host
// never invents a call site that is not listed here.
type Hook struct {
	Key         string          `json:"key"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	Triggers    []string        `json:"triggers"`
	Events      []string        `json:"events,omitempty"`
	Transport   HookTransport   `json:"transport"`
	TimeoutMs   int             `json:"timeout_ms,omitempty"`
}

type HookTransport struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// Resource is a static contribution. No call happens in either direction, so a
// resource is not a hook.
type Resource struct {
	Type  string `json:"type"`
	Key   string `json:"key"`
	Entry string `json:"entry"`
}

// ConfigField is one host-rendered configuration input. Secrets are stored
// write-only and never echoed back by any read endpoint.
type ConfigField struct {
	Key         string   `json:"-"`
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Options     []string `json:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
}

// ConfigSchema keeps declaration order so the generated form is stable across
// installs. The wire format stays a JSON object keyed by field name.
type ConfigSchema struct {
	Fields []ConfigField
}

func (c ConfigSchema) Len() int { return len(c.Fields) }

func (c ConfigSchema) Field(key string) (ConfigField, bool) {
	for _, field := range c.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return ConfigField{}, false
}

func (c ConfigSchema) MarshalJSON() ([]byte, error) {
	if len(c.Fields) == 0 {
		return []byte("{}"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, field := range c.Fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(field.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		value, err := json.Marshal(struct {
			Type        string   `json:"type"`
			Label       string   `json:"label"`
			Description string   `json:"description,omitempty"`
			Required    bool     `json:"required,omitempty"`
			Options     []string `json:"options,omitempty"`
			Placeholder string   `json:"placeholder,omitempty"`
		}{field.Type, field.Label, field.Description, field.Required, field.Options, field.Placeholder})
		if err != nil {
			return nil, err
		}
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (c *ConfigSchema) UnmarshalJSON(raw []byte) error {
	c.Fields = nil
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("config must be a JSON object: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("config must be a JSON object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("config field name: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("config field name must be a string")
		}
		if seen[key] {
			return fmt.Errorf("config contains duplicate field %q", key)
		}
		seen[key] = true

		var body json.RawMessage
		if err := decoder.Decode(&body); err != nil {
			return fmt.Errorf("config.%s: %w", key, err)
		}
		fieldDecoder := json.NewDecoder(bytes.NewReader(body))
		fieldDecoder.DisallowUnknownFields()
		var field ConfigField
		if err := fieldDecoder.Decode(&field); err != nil {
			return fmt.Errorf("config.%s: %w", key, err)
		}
		field.Key = key
		c.Fields = append(c.Fields, field)
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("config object: %w", err)
	}
	return nil
}

// ParseManifest decodes a strict v1 manifest and returns the canonical bytes an
// installation stores as its consented snapshot. Unknown fields fail instead of
// being ignored so a typo cannot silently weaken what the administrator
// approved.
func ParseManifest(raw []byte) (Manifest, []byte, error) {
	if len(raw) == 0 {
		return Manifest{}, nil, fmt.Errorf("plugin manifest is empty")
	}
	if len(raw) > MaxManifestSize {
		return Manifest{}, nil, fmt.Errorf("plugin manifest exceeds %d bytes", MaxManifestSize)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, nil, fmt.Errorf("decode plugin manifest: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Manifest{}, nil, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, nil, err
	}

	canonical, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("canonicalize plugin manifest: %w", err)
	}
	return manifest, canonical, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("plugin manifest contains trailing JSON")
		}
		return fmt.Errorf("decode trailing plugin manifest data: %w", err)
	}
	return nil
}

func (m Manifest) Validate() error {
	if m.ManifestVersion != ManifestVersion1 {
		return fmt.Errorf("manifest_version must be %d", ManifestVersion1)
	}
	if err := validatePluginKey(m.Key); err != nil {
		return err
	}
	if err := validateDisplayText("name", m.Name, 160); err != nil {
		return err
	}
	if len(m.Description) > 2000 {
		return fmt.Errorf("description exceeds 2000 bytes")
	}
	if strings.ContainsAny(m.Description, "\r") {
		return fmt.Errorf("description must not contain carriage returns")
	}
	// Bounded before the regex result is trusted: semver permits unbounded
	// build/prerelease segments, and plugin_installation.version is capped at
	// 64. Rejecting here turns a constraint violation at INSERT time into a
	// parse error that names the field.
	if len(m.Version) > MaxVersionLength {
		return fmt.Errorf("version exceeds %d bytes", MaxVersionLength)
	}
	if !semverPattern.MatchString(m.Version) {
		return fmt.Errorf("version must be semantic versioning, got %q", m.Version)
	}
	if err := validateDisplayText("author.name", m.Author.Name, 160); err != nil {
		return err
	}
	if m.Author.URL != "" {
		if err := validateHTTPSURL("author.url", m.Author.URL); err != nil {
			return err
		}
	}
	if m.Icon != "" {
		if err := validateRelativePath("icon", m.Icon); err != nil {
			return err
		}
	}
	if err := m.validateScopes(); err != nil {
		return err
	}
	if err := m.validateConfig(); err != nil {
		return err
	}
	return m.validateContributions()
}

func (m Manifest) validateScopes() error {
	if len(m.Scopes) == 0 {
		return fmt.Errorf("scopes must not be empty")
	}
	if len(m.Scopes) > 64 {
		return fmt.Errorf("scopes must not exceed 64 entries")
	}
	seen := map[string]bool{}
	for index, scope := range m.Scopes {
		if seen[scope] {
			return fmt.Errorf("scopes contains duplicate value %q", scope)
		}
		seen[scope] = true
		if err := ValidateScope(scope); err != nil {
			return fmt.Errorf("scopes[%d]: %w", index, err)
		}
	}
	return nil
}

// ValidateScope reports whether a single scope string is one this host defines.
func ValidateScope(scope string) error {
	if fixedScopes[scope] {
		return nil
	}
	if domain, ok := strings.CutPrefix(scope, ScopeNetPrefix); ok {
		if len(domain) > 253 || !netDomainPattern.MatchString(domain) {
			return fmt.Errorf("net scope has an invalid domain %q", domain)
		}
		return nil
	}
	return fmt.Errorf("unsupported scope %q", scope)
}

// NetDomains returns the domains an installation may reach, taken only from the
// scopes it was granted.
func NetDomains(scopes []string) []string {
	domains := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if domain, ok := strings.CutPrefix(scope, ScopeNetPrefix); ok {
			domains = append(domains, domain)
		}
	}
	return domains
}

func (m Manifest) validateConfig() error {
	if m.Config.Len() > 32 {
		return fmt.Errorf("config must not exceed 32 fields")
	}
	for _, field := range m.Config.Fields {
		if !contributionKeyPattern.MatchString(field.Key) {
			return fmt.Errorf("config contains invalid field name %q", field.Key)
		}
		label := "config." + field.Key
		switch field.Type {
		case ConfigString, ConfigNumber, ConfigBool, ConfigSecret:
			if len(field.Options) > 0 {
				return fmt.Errorf("%s.options is only valid for enum fields", label)
			}
		case ConfigEnum:
			if len(field.Options) == 0 {
				return fmt.Errorf("%s.options must not be empty for enum fields", label)
			}
			if len(field.Options) > 64 {
				return fmt.Errorf("%s.options must not exceed 64 entries", label)
			}
			optionSeen := map[string]bool{}
			for _, option := range field.Options {
				if err := validateDisplayText(label+".options", option, 160); err != nil {
					return err
				}
				if optionSeen[option] {
					return fmt.Errorf("%s.options contains duplicate value %q", label, option)
				}
				optionSeen[option] = true
			}
		default:
			return fmt.Errorf("%s.type is unsupported: %q", label, field.Type)
		}
		if err := validateDisplayText(label+".label", field.Label, 160); err != nil {
			return err
		}
		if len(field.Description) > 500 {
			return fmt.Errorf("%s.description exceeds 500 bytes", label)
		}
		if len(field.Placeholder) > 160 {
			return fmt.Errorf("%s.placeholder exceeds 160 bytes", label)
		}
	}
	return nil
}

func (m Manifest) validateContributions() error {
	total := len(m.Contributes.Surfaces) + len(m.Contributes.Hooks) + len(m.Contributes.Resources)
	if total == 0 {
		return fmt.Errorf("contributes must declare at least one surface, hook, or resource")
	}
	if total > 64 {
		return fmt.Errorf("contributes must not exceed 64 entries")
	}

	surfaceKeys := map[string]bool{}
	for index, surface := range m.Contributes.Surfaces {
		field := fmt.Sprintf("contributes.surfaces[%d]", index)
		if !contributionKeyPattern.MatchString(surface.Key) {
			return fmt.Errorf("%s.key is invalid", field)
		}
		if surfaceKeys[surface.Key] {
			return fmt.Errorf("duplicate surface key %q", surface.Key)
		}
		surfaceKeys[surface.Key] = true
		switch surface.Type {
		case SurfaceIssuePanel, SurfaceSidebarPanel, SurfaceModal:
		default:
			return fmt.Errorf("%s.type is unsupported: %q", field, surface.Type)
		}
		if err := validateDisplayText(field+".name", surface.Name, 160); err != nil {
			return err
		}
		if err := validateRelativePath(field+".entry", surface.Entry); err != nil {
			return err
		}
		// The host generates the surface's HTML document and loads this script
		// into it. That is what lets the host attach the CSP derived from the
		// manifest's net: scopes — a plugin-authored HTML document would carry
		// whatever policy its own server sent, so net: would be a claim rather
		// than a control.
		if !strings.HasSuffix(surface.Entry, ".js") && !strings.HasSuffix(surface.Entry, ".mjs") {
			return fmt.Errorf("%s.entry must be a .js or .mjs script; the host renders the surface document itself", field)
		}
		platformSeen := map[string]bool{}
		for _, platform := range surface.Platforms {
			if platform != "web" && platform != "desktop" {
				return fmt.Errorf("%s.platforms contains unsupported platform %q", field, platform)
			}
			if platformSeen[platform] {
				return fmt.Errorf("%s.platforms contains duplicate platform %q", field, platform)
			}
			platformSeen[platform] = true
		}
	}

	hookKeys := map[string]bool{}
	for index, hook := range m.Contributes.Hooks {
		field := fmt.Sprintf("contributes.hooks[%d]", index)
		if !contributionKeyPattern.MatchString(hook.Key) {
			return fmt.Errorf("%s.key is invalid", field)
		}
		if hookKeys[hook.Key] {
			return fmt.Errorf("duplicate hook key %q", hook.Key)
		}
		hookKeys[hook.Key] = true
		if err := validateDisplayText(field+".name", hook.Name, 160); err != nil {
			return err
		}
		// The description is what an agent reads as the MCP tool description,
		// so it is mandatory and may be multi-line.
		if strings.TrimSpace(hook.Description) == "" || len(hook.Description) > 2000 {
			return fmt.Errorf("%s.description must be non-empty and at most 2000 bytes", field)
		}
		if len(hook.InputSchema) > 0 {
			var schema map[string]any
			if err := json.Unmarshal(hook.InputSchema, &schema); err != nil {
				return fmt.Errorf("%s.input_schema must be a JSON object: %w", field, err)
			}
			if schemaType, _ := schema["type"].(string); schemaType != "object" {
				return fmt.Errorf("%s.input_schema.type must be object", field)
			}
		}
		if len(hook.Triggers) == 0 {
			return fmt.Errorf("%s.triggers must not be empty", field)
		}
		triggerSeen := map[string]bool{}
		for _, trigger := range hook.Triggers {
			switch trigger {
			case TriggerUI, TriggerManual, TriggerAgent, TriggerEvent:
			default:
				return fmt.Errorf("%s.triggers contains unsupported trigger %q", field, trigger)
			}
			if triggerSeen[trigger] {
				return fmt.Errorf("%s.triggers contains duplicate trigger %q", field, trigger)
			}
			triggerSeen[trigger] = true
		}
		if triggerSeen[TriggerEvent] {
			if len(hook.Events) == 0 {
				return fmt.Errorf("%s.events must not be empty when the event trigger is declared", field)
			}
			eventSeen := map[string]bool{}
			for _, event := range hook.Events {
				if !knownEvents[event] {
					return fmt.Errorf("%s.events contains unsupported event %q", field, event)
				}
				if eventSeen[event] {
					return fmt.Errorf("%s.events contains duplicate event %q", field, event)
				}
				eventSeen[event] = true
			}
		} else if len(hook.Events) > 0 {
			return fmt.Errorf("%s.events requires the event trigger", field)
		}
		if err := m.validateHookTransport(field, hook.Transport); err != nil {
			return err
		}
		if hook.TimeoutMs != 0 && (hook.TimeoutMs < 100 || hook.TimeoutMs > 30000) {
			return fmt.Errorf("%s.timeout_ms must be between 100 and 30000", field)
		}
	}

	resourceKeys := map[string]bool{}
	for index, resource := range m.Contributes.Resources {
		field := fmt.Sprintf("contributes.resources[%d]", index)
		if resource.Type != ResourceSkill {
			return fmt.Errorf("%s.type is unsupported: %q", field, resource.Type)
		}
		if !contributionKeyPattern.MatchString(resource.Key) {
			return fmt.Errorf("%s.key is invalid", field)
		}
		if resourceKeys[resource.Key] {
			return fmt.Errorf("duplicate resource key %q", resource.Key)
		}
		resourceKeys[resource.Key] = true
		if err := validateRelativePath(field+".entry", resource.Entry); err != nil {
			return err
		}
		if want := "skills/" + resource.Key + "/SKILL.md"; resource.Entry != want {
			return fmt.Errorf("%s.entry must be %q", field, want)
		}
	}
	return nil
}

// validateHookTransport enforces that a hook can only reach a host the manifest
// declared through a `net:` scope. Without this the consent screen would not
// describe where plugin data actually goes.
func (m Manifest) validateHookTransport(field string, transport HookTransport) error {
	switch transport.Type {
	case TransportHTTP, TransportMCP:
	default:
		return fmt.Errorf("%s.transport.type is unsupported: %q", field, transport.Type)
	}
	if err := validateHTTPSURL(field+".transport.url", transport.URL); err != nil {
		return err
	}
	endpoint, err := url.Parse(transport.URL)
	if err != nil {
		return fmt.Errorf("%s.transport.url is invalid", field)
	}
	// Exact host, never a suffix match. The consent screen renders one line per
	// scope ("send data to example.com"), and the same scope list becomes the
	// iframe's CSP connect-src, which is exact-host. A suffix match here would
	// make one scope string mean two different things in two places. A plugin
	// that needs a subdomain declares it: net:api.example.com.
	host := strings.ToLower(strings.TrimSuffix(endpoint.Hostname(), "."))
	for _, domain := range NetDomains(m.Scopes) {
		if host == domain {
			return nil
		}
	}
	return fmt.Errorf("%s.transport.url host %q is not covered by a net: scope", field, host)
}

func validatePluginKey(key string) error {
	if len(key) > 255 {
		return fmt.Errorf("key exceeds 255 bytes")
	}
	segments := strings.Split(key, ".")
	if len(segments) < 2 {
		return fmt.Errorf("key must use a reverse-DNS namespace")
	}
	for _, segment := range segments {
		if !pluginKeySegmentPattern.MatchString(segment) {
			return fmt.Errorf("key contains invalid segment %q", segment)
		}
	}
	return nil
}

func validateDisplayText(field, value string, maxBytes int) error {
	if value == "" || value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must be non-empty without surrounding whitespace", field)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be single-line", field)
	}
	if len(value) > maxBytes {
		return fmt.Errorf("%s exceeds %d bytes", field, maxBytes)
	}
	return nil
}

func validateRelativePath(field, value string) error {
	if value == "" || len(value) > 1024 {
		return fmt.Errorf("%s must be a relative path of at most 1024 bytes", field)
	}
	if !relativePathPattern.MatchString(value) {
		return fmt.Errorf("%s must be a relative path without protocol, leading slash, or traversal", field)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("%s must not contain path traversal", field)
		}
	}
	return nil
}

func validateHTTPSURL(field, value string) error {
	if len(value) > 2048 {
		return fmt.Errorf("%s exceeds 2048 bytes", field)
	}
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%s must be a plain HTTPS URL", field)
	}
	return nil
}
