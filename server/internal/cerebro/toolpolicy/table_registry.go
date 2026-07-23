package toolpolicy

// table_registry.go defines the catalog metadata used to render
// firtal_registry data sources in the unified permission table. Access itself
// is authored and resolved exclusively through cerebro_tool_policy.

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
