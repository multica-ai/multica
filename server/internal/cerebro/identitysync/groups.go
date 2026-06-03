// Package identitysync implements the FIR-2596 sync that mirrors a fixed set
// of Google Workspace groups into Multica's cerebro_group / cerebro_group_member
// tables. The source of truth is BigQuery (firtal-data-lake.google_admin.
// group_members, synced daily by Hevo) — NOT the Google Admin SDK, so no
// service account / domain-wide delegation is needed to READ membership.
//
// Reconciliation is one-directional (Google → Multica) and provenance-aware:
// members the sync adds are stamped source='google_workspace' and only those
// are ever removed. Hand-added ('manual') members are never touched.
package identitysync

// SourceGoogleWorkspace is the cerebro_group_member.source value stamped on
// every member this worker creates. Reconciliation removals are scoped to it.
const SourceGoogleWorkspace = "google_workspace"

// SyncedGroup maps one Google Workspace group to its Multica display name.
//
// The match key is the Email, never the Name: Google carries an "(Admin)"
// prefix on these groups and there are overlapping non-prefixed groups, so the
// stable email is the only safe identity. The Name below is authoritative and
// overrides whatever BigQuery reports for the group.
type SyncedGroup struct {
	Email string
	Name  string
}

// SyncedGroups is the fixed FIR-2596 list of 9 groups. Membership comes from
// BigQuery; the display name comes from here.
var SyncedGroups = []SyncedGroup{
	{Email: "customer-service@firtal.com", Name: "Customer Service"},
	{Email: "purchase-admin@firtal.com", Name: "Purchase"},
	{Email: "goodsflow@firtal.com", Name: "Goods Flow"},
	{Email: "operationsspecialist@firtal.com", Name: "Operations Specialist & Control"},
	{Email: "warehouse@firtal.com", Name: "Warehouse"},
	{Email: "sales-admin@firtal.com", Name: "Sales"},
	{Email: "tech-admin@firtal.com", Name: "Tech"},
	{Email: "finance@firtal.com", Name: "Finance"},
	{Email: "mgmt@firtal.com", Name: "MGMT"},
}

// SyncedGroupEmails returns the email list in SyncedGroups order, for the
// BigQuery WHERE clause.
func SyncedGroupEmails() []string {
	out := make([]string, len(SyncedGroups))
	for i, g := range SyncedGroups {
		out[i] = g.Email
	}
	return out
}
