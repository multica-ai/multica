package toolpolicy

// table.go is the read model behind the FIR-2230 permission table — the data
// the admin screen renders (phase 2: "the data layer the screen reads from").
//
// chain.go folds one tool's chain into one verdict; store.go round-trips a
// single (tool, context) through the DB. The table view needs the other axis:
// every tool known in the workspace, each with its explicit per-layer settings
// for one (runtime, agent, user, groups) context AND the combined Effective
// verdict already computed — so the UI renders one row per tool with the
// Tool · Source · Runtime · This agent · Effective columns from the mockup
// without resolving anything client-side.
//
// The tool universe comes from the capability register (cerebro_capability):
// every CLI tool and every MCP action a runtime has reported. We LEFT JOIN the
// per-layer settings onto it so a tool with no explicit setting still appears
// (resolving to the Base default), and a tool with settings at several layers
// collapses into one row whose Layers map carries each layer's choice.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// TableRow is one tool's full picture for the admin table: its identity, the
// explicit setting held at each layer for the queried context, and the resolved
// Effective verdict.
type TableRow struct {
	// ToolKey is the stable identifier the resolver keys on — a CLI tool name
	// ("add_comment") or a namespaced MCP action ("bigquery.query").
	ToolKey string
	// ResourcePattern is the per-resource scope this row authors (FIR-2505
	// slice 1). Empty for the capability-wide row pre-FIR-2505 callers wrote;
	// non-empty when the row targets a specific resource (e.g. a repo URL). The
	// resolver matches the pattern verbatim.
	ResourcePattern string
	// Title and Category are human-readable labels from the capability register,
	// so the UI never shows a raw key or id.
	Title    string
	Category string
	// Source records how the capability was discovered (e.g. "scan", "report").
	Source string
	// ManagedExternally is true for platform capabilities whose enforcement point
	// is not the tool-policy gate (membership ACL, daemon token, etc.). Always
	// false for reported runtime tools and repo rows. Shown so the admin sees the
	// row is informational, not gated (FIR-2594).
	ManagedExternally bool
	// Layers holds the explicit setting at each layer for this context. A layer
	// absent from the map carries no explicit choice (Inherit). LayerGroup, when
	// present, is the combined value across the context's groups (most permissive
	// group wins, per CombineGroups) — the single value that enters the chain.
	Layers map[Layer]Setting
	// Effective is the combined verdict across the whole chain for this tool.
	Effective Effective
	// CappedByGroups names the group(s) whose policy drives the group-layer
	// restriction on this row, each with its owner (the group's creator). Only
	// populated when the Group layer is the decider or capper, so the UI can say
	// "Capped by group <name> (owner: <person>)" instead of an anonymous "group"
	// (TECH-3287 hul 5). Empty when no group caps this row.
	CappedByGroups []GroupAttribution
}

// GroupAttribution names one group that drives a group-layer restriction and the
// person who owns it (the group's creator), so the admin sees exactly which group
// to change and who to ask. Owner is empty for groups with no recorded creator
// (e.g. synced groups).
type GroupAttribution struct {
	Name  string
	Owner string
}

// TableQuery selects the tool universe (a workspace) and the chain context the
// effective column is computed for. Any of RuntimeID/AgentID/UserID may be the
// zero value (Valid=false) when that layer is not part of the view — an agent
// page passes agent+runtime+user+groups, a runtime page passes only the runtime.
type TableQuery struct {
	WorkspaceID pgtype.UUID
	RuntimeID   pgtype.UUID
	AgentID     pgtype.UUID
	UserID      pgtype.UUID
	GroupIDs    []pgtype.UUID
	// Base is the workspace/system default applied when every layer inherits.
	// Empty defaults to Allow (see Resolve).
	Base Setting
	// IncludePlatform appends the code-owned platform-capability catalog
	// (platformcatalog) to the listing — the Multica platform actions an agent or
	// user can take, which no runtime reports. Gated by the caller (the handler
	// checks the cerebro_platform_capabilities flag) so prod sees nothing new
	// until an admin turns the flag on (FIR-2594).
	IncludePlatform bool
}

// Table returns one row per capability (tool) in the workspace, each with the
// explicit per-layer settings for the query's context and the resolved
// Effective verdict. Tools with no stored settings resolve to Base (Allow by
// default), so an unconfigured workspace still lists every tool as allowed.
func (s *Store) Table(ctx context.Context, in TableQuery) ([]TableRow, error) {
	// Expand to the user's real groups when a user is in scope but no explicit
	// groups were passed, so the Effective column reflects the full chain
	// including the user's group layer (see resolveGroupIDs).
	groupIDs, err := s.resolveGroupIDs(ctx, in.WorkspaceID, in.UserID, in.GroupIDs)
	if err != nil {
		return nil, err
	}

	// When a runtime is in scope (a runtime page, or an agent page where the
	// agent's runtime is known) the table must show what THAT runtime can do —
	// not every tool in the workspace. The capability register records which
	// runtime reported/owns each tool in cerebro_capability_subject, so we keep
	// only capabilities tied to the queried runtime. Without a runtime in scope
	// (Valid=false → $2 is NULL, which the EXISTS never matches) we fall back to
	// the full workspace universe rather than returning nothing.
	//
	// Connection capabilities (source 'connection') are the exception: they are
	// workspace-level resources, never runtime-reported, so they have no
	// cerebro_capability_subject row and the EXISTS would hide them on every
	// runtime/agent view. But connection access is authored at all five layers
	// (the per-tool rows in table_connection.go already bypass this filter for
	// the same reason), so the admin must see the connection-wide row — the one
	// the Connections tab groups its tools under — everywhere too. Keep it.
	runtimeFilter := ""
	if in.RuntimeID.Valid {
		runtimeFilter = `
		 AND (
		   c.source = 'connection'
		   OR EXISTS (
		     SELECT 1 FROM cerebro_capability_subject sub
		     WHERE sub.capability_id = c.id
		       AND sub.subject_type = 'runtime'
		       AND sub.subject_id = $2
		   )
		 )`
	}

	// One row per (tool, matching policy layer) for the capability-wide
	// (resource_pattern = '') view. The LEFT JOIN keeps tools with no settings
	// (NULL layer), the subject predicates mirror ListCerebroToolPolicyForContext
	// so an absent (Valid=false) subject id — which marshals to NULL — never
	// matches and that layer stays Inherit. The workspace root layer is always
	// keyed on the workspace itself ($1), so it enters every view's Effective
	// column even when no other subject is in scope. The resource_pattern filter
	// keeps slice 1 user-visible behaviour identical to pre-FIR-2505: only the
	// capability-wide rows are collapsed here; per-resource rows are emitted in
	// slice 2 by a separate read path so the UI can group them under each repo.
	rows, err := s.pool.Query(ctx, `
		SELECT c.capability_key, c.title, c.category, c.source,
		       p.layer, p.subject_id, p.setting
		FROM cerebro_capability c
		LEFT JOIN cerebro_tool_policy p
		  ON p.workspace_id = c.workspace_id
		 AND p.tool_key = c.capability_key
		 AND p.resource_pattern = ''
		 AND (
		   (p.layer = 'workspace' AND p.subject_id = $1) OR
		   (p.layer = 'runtime'   AND p.subject_id = $2) OR
		   (p.layer = 'agent'     AND p.subject_id = $3) OR
		   (p.layer = 'user'      AND p.subject_id = $4) OR
		   (p.layer = 'group'     AND p.subject_id = ANY($5::uuid[]))
		 )
		WHERE c.workspace_id = $1`+runtimeFilter+`
		ORDER BY c.category, lower(c.title), c.capability_key
	`, in.WorkspaceID, in.RuntimeID, in.AgentID, in.UserID, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load table: %w", err)
	}
	defer rows.Close()

	// Accumulate per tool, preserving the SQL order (category, title) via order[].
	// groupSubjects keeps each group row's (id, setting) so we can later name the
	// group(s) that drive a group-layer cap (TECH-3287 hul 5); groups holds the
	// same settings flattened for CombineGroups.
	type acc struct {
		row           TableRow
		groups        []Setting
		groupSubjects []groupSubjectSetting
	}
	byTool := map[string]*acc{}
	var order []string

	for rows.Next() {
		var toolKey, title, category, source string
		var layer, setting pgtype.Text
		var subjectID pgtype.UUID
		if err := rows.Scan(&toolKey, &title, &category, &source, &layer, &subjectID, &setting); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan table row: %w", err)
		}

		a, ok := byTool[toolKey]
		if !ok {
			a = &acc{row: TableRow{
				ToolKey:  toolKey,
				Title:    title,
				Category: category,
				Source:   source,
				Layers:   map[Layer]Setting{},
			}}
			byTool[toolKey] = a
			order = append(order, toolKey)
		}

		if !layer.Valid || !setting.Valid {
			continue // tool with no explicit setting at any queried layer
		}
		l := Layer(layer.String)
		set := Setting(setting.String)
		if l == LayerGroup {
			a.groups = append(a.groups, set)
			a.groupSubjects = append(a.groupSubjects, groupSubjectSetting{id: subjectID, setting: set})
		} else {
			a.row.Layers[l] = set
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate table rows: %w", err)
	}

	out := make([]TableRow, 0, len(order))
	// drivingByIndex[i] holds the group ids that drive row i's group-layer cap,
	// resolved to names in one batch query after the loop so attribution costs a
	// single round-trip regardless of table size (TECH-3287 hul 5).
	drivingByIndex := make([][]pgtype.UUID, 0, len(order))
	allGroupIDs := map[string]pgtype.UUID{}
	for _, key := range order {
		a := byTool[key]
		if len(a.groups) > 0 {
			a.row.Layers[LayerGroup] = CombineGroups(a.groups...)
		}
		a.row.Effective = Resolve(Input{Settings: a.row.Layers, Base: in.Base})
		drivingByIndex = append(drivingByIndex, drivingGroupIDs(a.row.Effective, a.row.Layers[LayerGroup], a.groupSubjects, allGroupIDs))
		out = append(out, a.row)
	}
	if err := s.attachGroupAttribution(ctx, out, drivingByIndex, allGroupIDs); err != nil {
		return nil, err
	}

	// Append the per-repo rows (one per repo capability per workspace repo). These
	// are not in the capability register, so the query above never emits them; they
	// carry a non-empty ResourcePattern and the synthetic "repo" category the admin
	// screen groups into a collapsible block (FIR-2505 slice 2).
	out, err = s.appendRepoRows(ctx, in, groupIDs, out)
	if err != nil {
		return nil, err
	}

	// Append the per-connection-tool rows (one per tool per enabled MCP
	// connection). Like repo rows these are not in the capability register; they
	// carry a non-empty ResourcePattern (the tool name) and the connection's
	// display name as Category, so the UI groups a connection's tools under it
	// and gates each one individually (TECH-3156).
	out, err = s.appendConnectionToolRows(ctx, in, groupIDs, out)
	if err != nil {
		return nil, err
	}

	// Append the code-owned platform-capability rows (FIR-2594). Like repo rows
	// these are not in the capability register, so the query above never emits
	// them; they are capability-wide (empty ResourcePattern) and carry the
	// "platform" source. Gated by IncludePlatform so an unflagged workspace lists
	// exactly what it listed before.
	if in.IncludePlatform {
		out, err = s.appendPlatformRows(ctx, in, groupIDs, out)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// uuidParam is a small helper for callers assembling a TableQuery from request
// strings; an empty string yields the zero (absent) UUID so the layer is simply
// omitted from the view rather than erroring.
func uuidParam(raw string) (pgtype.UUID, error) {
	if raw == "" {
		return pgtype.UUID{}, nil
	}
	return util.ParseUUID(raw)
}

// groupSubjectSetting pairs one group's id with the setting it holds for a tool,
// so we can name the group behind a group-layer cap (TECH-3287 hul 5).
type groupSubjectSetting struct {
	id      pgtype.UUID
	setting Setting
}

// drivingGroupIDs returns the group ids responsible for a group-layer
// restriction on one row. It only attributes when the Group layer is the decider
// or capper (otherwise the group did not shape the verdict). The driving groups
// are those whose setting matches the combined group value (CombineGroups keeps
// the least-restrictive opinion, so for a Deny cap every opinionated group is a
// Deny and all are named). Discovered ids are recorded in seen for the batch
// name lookup.
func drivingGroupIDs(eff Effective, combined Setting, subjects []groupSubjectSetting, seen map[string]pgtype.UUID) []pgtype.UUID {
	if eff.CappedBy != LayerGroup && eff.DecidedBy != LayerGroup {
		return nil
	}
	combinedRank := rank(combined)
	var driving []pgtype.UUID
	for _, gs := range subjects {
		if gs.id.Valid && rank(gs.setting) == combinedRank {
			driving = append(driving, gs.id)
			seen[uuidKey(gs.id)] = gs.id
		}
	}
	return driving
}

// attachGroupAttribution resolves every driving group id to its name + owner in
// one query and writes the result onto each row's CappedByGroups, preserving the
// driving order. A row with no driving groups is left untouched.
func (s *Store) attachGroupAttribution(ctx context.Context, out []TableRow, drivingByIndex [][]pgtype.UUID, allGroupIDs map[string]pgtype.UUID) error {
	if len(allGroupIDs) == 0 {
		return nil
	}
	ids := make([]pgtype.UUID, 0, len(allGroupIDs))
	for _, id := range allGroupIDs {
		ids = append(ids, id)
	}
	attr, err := s.loadGroupAttribution(ctx, ids)
	if err != nil {
		return err
	}
	for i := range out {
		for _, id := range drivingByIndex[i] {
			if ga, ok := attr[uuidKey(id)]; ok {
				out[i].CappedByGroups = append(out[i].CappedByGroups, ga)
			}
		}
	}
	return nil
}

// loadGroupAttribution batch-loads the name + owner (creator) for the given group
// ids. It uses the raw pool like the rest of this read model, so no sqlc query
// has to land for a read that only the admin table needs.
func (s *Store) loadGroupAttribution(ctx context.Context, ids []pgtype.UUID) (map[string]GroupAttribution, error) {
	out := map[string]GroupAttribution{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT g.id, g.name, COALESCE(u.name, '')
		FROM cerebro_group g
		LEFT JOIN "user" u ON u.id = g.created_by
		WHERE g.id = ANY($1::uuid[])
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load group attribution: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id pgtype.UUID
		var name, owner string
		if err := rows.Scan(&id, &name, &owner); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan group attribution: %w", err)
		}
		out[uuidKey(id)] = GroupAttribution{Name: name, Owner: owner}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate group attribution: %w", err)
	}
	return out, nil
}

// uuidKey is a stable map key for a pgtype.UUID (its raw 16 bytes).
func uuidKey(id pgtype.UUID) string {
	return string(id.Bytes[:])
}
