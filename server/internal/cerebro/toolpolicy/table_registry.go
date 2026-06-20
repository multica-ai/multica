package toolpolicy

// table_registry.go folds the firtal_registry per-data-source grant into the
// unified permission chain (FIR-1609 Phase 5). The standalone registry dialog
// stores, per agent, which data sources the firtal_registry tool may list/run
// (agent_tool_grant.config_json: allowed_data_sources / allowed_data_sources_all).
// Phase 5 surfaces each data source as a per-resource row UNDER the firtal_registry
// tool — the same per-resource shape repos and connection endpoints already use —
// so registry access is governed in one model with per-source granularity.
//
// This file holds only the PURE projection (grant + data-source list -> rows). It
// has no DB or registry-API dependency, so it is fully unit-testable; the handler
// that owns the registry data-source lister maps its richer types onto these and
// appends the rows to the table response (wired in the next slice). Like the
// web_fetch fold-in it is a READ-SIDE projection: agent_tool_grant stays the
// enforced source until the gate reads the chain (Phase 6).

import "strings"

// RegistryToolKey is the capability key of the firtal_registry tool whose
// per-data-source access this projection governs.
const RegistryToolKey = "firtal_registry"

// registryDataSourceCategory groups the projected rows in the admin table, the
// way repos render under "Repositories".
const registryDataSourceCategory = "Data sources"

// registryDataSourceSource marks a row as a projected registry data source, so
// the UI can group/treat it like the other per-resource rows.
const registryDataSourceSource = "registry-data-source"

// RegistryDataSource is the minimal shape of one registry data source the
// projection needs. The handler maps the richer external type onto this so this
// package carries no registry-API dependency.
type RegistryDataSource struct {
	ID   string
	Name string
}

// RegistryGrant is the per-agent firtal_registry grant, reduced to what
// per-data-source access depends on. AllowedAll short-circuits to "every source".
type RegistryGrant struct {
	AllowedAll         bool
	AllowedDataSources []string
}

// grants reports whether the grant permits the given data source id. Matching is
// case-insensitive and whitespace-tolerant, mirroring the gateway enforcement
// (firtalRegistryGrantConfig.allows) so the surface never disagrees with the
// runtime about what is allowed.
func (g RegistryGrant) grants(id string) bool {
	if g.AllowedAll {
		return true
	}
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return false
	}
	for _, a := range g.AllowedDataSources {
		if strings.ToLower(strings.TrimSpace(a)) == id {
			return true
		}
	}
	return false
}

// ProjectRegistryDataSourceRows folds the firtal_registry per-data-source grant
// into one per-resource row per data source (resource_pattern = data source id)
// under the firtal_registry tool. A granted source projects as Allow on `layer`;
// the rest project as an explicit Deny, because the registry is deny-by-default
// (only allowlisted sources are reachable) — so the surface shows precisely which
// sources the agent may use. Rows resolve their Effective verdict against `base`.
//
// It is a read-side projection (agent_tool_grant remains the enforced source
// until Phase 6); an explicit chain override is layered on at the call site the
// same way the web_fetch fold-in defers to an admin's own rule.
func ProjectRegistryDataSourceRows(dataSources []RegistryDataSource, grant RegistryGrant, layer Layer, base Setting) []TableRow {
	rows := make([]TableRow, 0, len(dataSources))
	for _, ds := range dataSources {
		id := strings.TrimSpace(ds.ID)
		if id == "" {
			continue // a source with no id cannot key a resource_pattern
		}
		setting := SettingDeny
		if grant.grants(id) {
			setting = SettingAllow
		}
		row := TableRow{
			ToolKey:         RegistryToolKey,
			ResourcePattern: id,
			Title:           ds.Name,
			Category:        registryDataSourceCategory,
			Source:          registryDataSourceSource,
			Layers:          map[Layer]Setting{layer: setting},
			Conditions:      map[Layer]*Condition{},
		}
		row.Effective = Resolve(Input{Settings: row.Layers, Base: base})
		rows = append(rows, row)
	}
	return rows
}

// AppendRegistryProjection appends the firtal_registry per-data-source rows to an
// existing table, skipping any data source for which an explicit firtal_registry
// row already exists (an admin's authored rule always wins, never clobbered —
// the same deferral the web_fetch fold-in makes). Pure: the handler calls this
// after Store.Table returns, so toolpolicy.Table stays free of the registry list.
func AppendRegistryProjection(rows []TableRow, proj RegistryProjection, layer Layer, base Setting) []TableRow {
	authored := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.ToolKey == RegistryToolKey && row.ResourcePattern != "" {
			authored[row.ResourcePattern] = true
		}
	}
	for _, pr := range ProjectRegistryDataSourceRows(proj.DataSources, proj.Grant, layer, base) {
		if !authored[pr.ResourcePattern] {
			rows = append(rows, pr)
		}
	}
	return rows
}
