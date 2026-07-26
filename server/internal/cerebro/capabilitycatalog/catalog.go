// Package capabilitycatalog owns the read-only identity contract for Cerebro
// capabilities. It maps the key forms exposed by runtimes, the gateway, the
// Multica bridge, and workspace connections to one stable capability ID.
//
// The catalog deliberately has no database or policy dependency. It describes
// identity only; it does not grant, deny, expose, or invoke a tool.
package capabilitycatalog

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Family identifies the owner of a canonical capability.
type Family string

const (
	FamilyPlatform      Family = "platform"
	FamilyGateway       Family = "gateway"
	FamilyConnection    Family = "connection"
	FamilyMCP           Family = "mcp"
	FamilyAPIConnection Family = "api-connection"
	FamilyRuntime       Family = "runtime"
	FamilyTool          Family = "tool"
)

// Surface identifies the namespace in which a key is meaningful. The same
// string may legitimately occur on several surfaces, so it is never indexed
// without this context.
type Surface string

const (
	SurfacePolicy      Surface = "policy"
	SurfaceGateway     Surface = "gateway"
	SurfaceBridge      Surface = "bridge"
	SurfaceMCP         Surface = "mcp"
	SurfaceAPIEndpoint Surface = "api-endpoint"
	SurfaceRuntime     Surface = "runtime"
)

// Relation records whether a key is the canonical spelling, a semantically
// identical alias, or a related-but-not-identical runtime variant.
type Relation string

const (
	RelationCanonical Relation = "canonical"
	RelationAlias     Relation = "alias"
	RelationVariant   Relation = "variant"
)

var (
	// ErrAmbiguousKey means one concrete key form was assigned to two IDs (or
	// assigned two different relations). Such a catalog is unsafe to publish.
	ErrAmbiguousKey = errors.New("capability key maps ambiguously")
	// ErrCanonicalForm means a capability did not have exactly one canonical
	// key form after entries with the same ID were merged.
	ErrCanonicalForm = errors.New("capability must have exactly one canonical form")
	// ErrInvalidCapability means an ID, family, key, or relation is malformed.
	ErrInvalidCapability = errors.New("invalid capability")
)

// Key is one existing key form. Provider qualifies runtime-native names;
// Resource carries the existing connection policy tuple's resource_pattern;
// Source distinguishes policy rows such as runtime_report and connection-tool.
type Key struct {
	Surface  Surface
	Provider string
	Value    string
	Resource string
	Source   string
}

// Form maps a key to a canonical capability with an explicit semantic relation.
type Form struct {
	Key      Key
	Relation Relation
}

// Capability is one official ID and all existing forms that refer to it.
type Capability struct {
	ID     string
	Family Family
	Forms  []Form
}

// Resolved is the result of an exact key-form lookup.
type Resolved struct {
	Capability Capability
	Relation   Relation
}

// Catalog is immutable after construction. Read methods return defensive copies.
type Catalog struct {
	capabilities []Capability
	byID         map[string]int
	byKey        map[Key]resolvedIndex
}

// Inventory is the complete current tool universe supplied by the code-owned
// registries and connection discovery. Build turns it into one validated
// catalog without importing those packages or changing their behavior.
type Inventory struct {
	PlatformTools      []string
	GatewayNativeTools []string
	BridgeTools        []string
	Connections        []ConnectionInventory
	Runtimes           []RuntimeInventory
}

// ConnectionInventory is one workspace connection and its discovered tools.
type ConnectionInventory struct {
	Name         string
	Kind         string
	MCPTools     []string
	APIEndpoints []APIEndpointInventory
}

// APIEndpointInventory is one configured method/path and its model-safe name.
type APIEndpointInventory struct {
	Method      string
	Path        string
	ExposedName string
}

// RuntimeInventory is one provider-qualified native tool surface.
type RuntimeInventory struct {
	Provider string
	Tools    []string
}

type resolvedIndex struct {
	capability int
	relation   Relation
}

// Build creates a full catalog from the current tool inventories. Input order
// and duplicates do not affect the result.
func Build(in Inventory) (*Catalog, error) {
	definitions := make([]Capability, 0)
	seen := make(map[string]struct{})
	add := func(identity string, capability Capability) {
		if _, exists := seen[identity]; exists {
			return
		}
		seen[identity] = struct{}{}
		definitions = append(definitions, capability)
	}

	for _, name := range in.PlatformTools {
		add("platform:"+name, PlatformTool(name))
	}
	for _, name := range in.BridgeTools {
		// A bridge tool is a transport for a platform capability. Ensure the
		// canonical platform form exists even when the caller supplied only the
		// bridge inventory.
		add("platform:"+name, PlatformTool(name))
		add("bridge:"+name, BridgeTool(name))
	}
	for _, name := range in.GatewayNativeTools {
		if name == "web_fetch" {
			add("known:web_fetch", WebFetch())
			continue
		}
		add("gateway:"+name, GatewayNativeTool(name))
	}
	for _, connection := range in.Connections {
		add("connection:"+connection.Name, Connection(connection.Name, connection.Kind))
		for _, tool := range connection.MCPTools {
			add("mcp:"+connection.Name+"\x00"+tool, MCPConnectionTool(connection.Name, tool))
		}
		for _, endpoint := range connection.APIEndpoints {
			identity := "api:" + connection.Name + "\x00" + endpoint.Method + "\x00" + endpoint.Path + "\x00" + endpoint.ExposedName
			add(identity, APIConnectionEndpoint(connection.Name, endpoint.Method, endpoint.Path, endpoint.ExposedName))
		}
	}
	for _, runtime := range in.Runtimes {
		for _, tool := range runtime.Tools {
			if (runtime.Provider == "claude" && tool == "WebFetch") ||
				(runtime.Provider == "gemini" && tool == "web_fetch") {
				add("known:web_fetch", WebFetch())
				continue
			}
			identity := "runtime:" + runtime.Provider + "\x00" + tool
			add(identity, RuntimeNativeTool(runtime.Provider, tool, runtimePolicyToolKey(tool)))
		}
	}
	return New(definitions...)
}

// New validates, merges, and freezes capability definitions. Multiple inputs
// may contribute forms to the same ID (for example PlatformTool + BridgeTool),
// but the merged capability must still have exactly one canonical form.
func New(definitions ...Capability) (*Catalog, error) {
	merged := make(map[string]Capability, len(definitions))
	for _, definition := range definitions {
		if strings.TrimSpace(definition.ID) == "" || definition.Family == "" {
			return nil, fmt.Errorf("%w: id and family are required", ErrInvalidCapability)
		}
		capability, exists := merged[definition.ID]
		if !exists {
			capability = Capability{ID: definition.ID, Family: definition.Family}
		} else if capability.Family != definition.Family {
			return nil, fmt.Errorf("%w: %q has families %q and %q", ErrInvalidCapability, definition.ID, capability.Family, definition.Family)
		}
		capability.Forms = append(capability.Forms, definition.Forms...)
		merged[definition.ID] = capability
	}

	capabilities := make([]Capability, 0, len(merged))
	for _, capability := range merged {
		canonicalCount := 0
		for _, form := range capability.Forms {
			if err := validateForm(form); err != nil {
				return nil, fmt.Errorf("%w: %s: %v", ErrInvalidCapability, capability.ID, err)
			}
			if form.Relation == RelationCanonical {
				canonicalCount++
			}
		}
		if canonicalCount != 1 {
			return nil, fmt.Errorf("%w: %q has %d canonical forms", ErrCanonicalForm, capability.ID, canonicalCount)
		}
		capabilities = append(capabilities, cloneCapability(capability))
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].ID < capabilities[j].ID })

	catalog := &Catalog{
		capabilities: capabilities,
		byID:         make(map[string]int, len(capabilities)),
		byKey:        make(map[Key]resolvedIndex),
	}
	for i := range catalog.capabilities {
		capability := &catalog.capabilities[i]
		catalog.byID[capability.ID] = i
		sort.Slice(capability.Forms, func(a, b int) bool {
			return formSortKey(capability.Forms[a]) < formSortKey(capability.Forms[b])
		})
		unique := capability.Forms[:0]
		for _, form := range capability.Forms {
			if prior, exists := catalog.byKey[form.Key]; exists {
				if prior.capability != i || prior.relation != form.Relation {
					return nil, fmt.Errorf("%w: %+v", ErrAmbiguousKey, form.Key)
				}
				continue
			}
			catalog.byKey[form.Key] = resolvedIndex{capability: i, relation: form.Relation}
			unique = append(unique, form)
		}
		capability.Forms = unique
	}
	return catalog, nil
}

// All returns every capability ordered by canonical ID.
func (c *Catalog) All() []Capability {
	if c == nil {
		return nil
	}
	out := make([]Capability, len(c.capabilities))
	for i, capability := range c.capabilities {
		out[i] = cloneCapability(capability)
	}
	return out
}

// Resolve returns the canonical capability and the key's semantic relation.
func (c *Catalog) Resolve(key Key) (Resolved, bool) {
	if c == nil {
		return Resolved{}, false
	}
	index, ok := c.byKey[key]
	if !ok {
		return Resolved{}, false
	}
	return Resolved{
		Capability: cloneCapability(c.capabilities[index.capability]),
		Relation:   index.relation,
	}, true
}

// Forms returns all known forms for one canonical ID in stable order.
func (c *Catalog) Forms(id string) ([]Form, bool) {
	if c == nil {
		return nil, false
	}
	index, ok := c.byID[id]
	if !ok {
		return nil, false
	}
	forms := c.capabilities[index].Forms
	out := make([]Form, len(forms))
	copy(out, forms)
	return out, true
}

// UnmappedPolicyKeys returns unique keys with no exact catalog mapping. The
// result is deterministic so it can be used directly in reports and tests.
func (c *Catalog) UnmappedPolicyKeys(keys []Key) []Key {
	seen := make(map[Key]struct{}, len(keys))
	var out []Key
	for _, key := range keys {
		if _, mapped := c.Resolve(key); mapped {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool { return keySortKey(out[i]) < keySortKey(out[j]) })
	return out
}

// Canonical, Alias, and Variant make the relation explicit at every call site.
func Canonical(key Key) Form { return Form{Key: key, Relation: RelationCanonical} }
func Alias(key Key) Form     { return Form{Key: key, Relation: RelationAlias} }
func Variant(key Key) Form   { return Form{Key: key, Relation: RelationVariant} }

// PolicyKey identifies the actual tool_key/resource_pattern/source tuple read
// by the current tool-policy table.
func PolicyKey(toolKey, resourcePattern, source string) Key {
	return Key{Surface: SurfacePolicy, Value: toolKey, Resource: resourcePattern, Source: source}
}

// RuntimePolicyKey adds the provider context that the live resolver obtains
// from the selected runtime. This prevents equal tools:<Name> strings on two
// providers from being falsely declared identical.
func RuntimePolicyKey(provider, toolKey, source string) Key {
	return Key{Surface: SurfacePolicy, Provider: provider, Value: toolKey, Source: source}
}

func GatewayKey(name string) Key { return Key{Surface: SurfaceGateway, Value: name} }
func BridgeKey(name string) Key  { return Key{Surface: SurfaceBridge, Value: name} }

// MCPKey mirrors toolpolicy.MCPToolToken: mcp__<connection>__<tool>.
func MCPKey(connection, tool string) Key {
	return Key{Surface: SurfaceMCP, Value: "mcp__" + connection + "__" + tool}
}

func APIEndpointKey(exposedName string) Key {
	return Key{Surface: SurfaceAPIEndpoint, Value: exposedName}
}

func RuntimeKey(provider, name string) Key {
	return Key{Surface: SurfaceRuntime, Provider: provider, Value: name}
}

// PlatformTool registers a platform/CLI policy capability.
func PlatformTool(name string) Capability {
	return Capability{
		ID:     "platform:" + idSegment(name),
		Family: FamilyPlatform,
		Forms:  []Form{Canonical(PolicyKey(name, "", "platform"))},
	}
}

// BridgeTool maps an in-process Multica MCP bridge name to the same platform
// capability. The bridge is another transport, not another permission.
func BridgeTool(name string) Capability {
	return Capability{
		ID:     "platform:" + idSegment(name),
		Family: FamilyPlatform,
		Forms:  []Form{Alias(BridgeKey(name))},
	}
}

// GatewayNativeTool registers a gateway-only built-in. web_fetch is handled by
// WebFetch because it has verified aliases on local runtimes.
func GatewayNativeTool(name string) Capability {
	return Capability{
		ID:     "gateway:" + idSegment(name),
		Family: FamilyGateway,
		Forms:  []Form{Canonical(GatewayKey(name))},
	}
}

// WebFetch is the known cross-runtime identity required by FIR-3389. Claude's
// WebFetch is semantically identical to the gateway tool, while Gemini's
// similarly named built-in is kept as a variant until parity is proven.
func WebFetch() Capability {
	return Capability{
		ID:     "tool:web_fetch",
		Family: FamilyTool,
		Forms: []Form{
			Canonical(GatewayKey("web_fetch")),
			Alias(PolicyKey("web_fetch", "", "gateway")),
			Alias(RuntimeKey("claude", "WebFetch")),
			Alias(PolicyKey("tools:WebFetch", "", "runtime_report")),
			Alias(RuntimePolicyKey("claude", "tools:WebFetch", "runtime_report")),
			Variant(RuntimeKey("gemini", "web_fetch")),
			Variant(PolicyKey("tools:web_fetch", "", "runtime_report")),
			Variant(RuntimePolicyKey("gemini", "tools:web_fetch", "runtime_report")),
		},
	}
}

// Connection registers the connection-wide policy capability. kind is
// retained in the family selection contract for callers even though the
// existing wide key has the same shape for api and mcp_http connections.
func Connection(name, kind string) Capability {
	_ = kind
	return Capability{
		ID:     "connection:" + idSegment(name),
		Family: FamilyConnection,
		Forms:  []Form{Canonical(PolicyKey("connection:"+name, "", "connection"))},
	}
}

// MCPConnectionTool maps the callable mcp__ token and the current
// connection:<name> + resource_pattern policy tuple to one capability.
func MCPConnectionTool(connection, tool string) Capability {
	return Capability{
		ID:     "connection:" + idSegment(connection) + ":mcp:" + idSegment(tool),
		Family: FamilyMCP,
		Forms: []Form{
			Canonical(MCPKey(connection, tool)),
			Alias(PolicyKey("connection:"+connection, tool, "connection-tool")),
		},
	}
}

// APIConnectionEndpoint maps the model-safe callable name and the existing
// connection endpoint policy tuple to one capability.
func APIConnectionEndpoint(connection, method, path, exposedName string) Capability {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = normalizePath(path)
	return Capability{
		ID:     "connection:" + idSegment(connection) + ":api:" + method + ":" + path,
		Family: FamilyAPIConnection,
		Forms: []Form{
			Canonical(APIEndpointKey(exposedName)),
			Alias(PolicyKey("connection:"+connection, method+" "+path, "connection-endpoint")),
		},
	}
}

// RuntimeNativeTool registers one provider-qualified built-in and its
// tools:<Name> policy key. Provider qualification prevents equal raw names on
// different runtimes from being declared identical accidentally.
func RuntimeNativeTool(provider, name, policyToolKey string) Capability {
	return Capability{
		ID:     "runtime:" + idSegment(provider) + ":" + idSegment(name),
		Family: FamilyRuntime,
		Forms: []Form{
			Canonical(RuntimeKey(provider, name)),
			Alias(RuntimePolicyKey(provider, policyToolKey, "runtime_report")),
		},
	}
}

func runtimePolicyToolKey(toolName string) string {
	if strings.HasPrefix(toolName, "mcp__") || strings.Contains(toolName, ":") {
		return toolName
	}
	return "tools:" + toolName
}

func validateForm(form Form) error {
	if form.Relation != RelationCanonical && form.Relation != RelationAlias && form.Relation != RelationVariant {
		return fmt.Errorf("unknown relation %q", form.Relation)
	}
	if form.Key.Surface == "" || strings.TrimSpace(form.Key.Value) == "" {
		return errors.New("key surface and value are required")
	}
	if form.Key.Surface == SurfaceRuntime && strings.TrimSpace(form.Key.Provider) == "" {
		return errors.New("runtime key provider is required")
	}
	return nil
}

func cloneCapability(capability Capability) Capability {
	out := capability
	out.Forms = make([]Form, len(capability.Forms))
	copy(out.Forms, capability.Forms)
	return out
}

func idSegment(value string) string {
	return url.PathEscape(strings.ToLower(strings.TrimSpace(value)))
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func formSortKey(form Form) string {
	return string(form.Relation) + "\x00" + keySortKey(form.Key)
}

func keySortKey(key Key) string {
	return strings.Join([]string{string(key.Surface), key.Provider, key.Value, key.Resource, key.Source}, "\x00")
}
