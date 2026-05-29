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
	// Title and Category are human-readable labels from the capability register,
	// so the UI never shows a raw key or id.
	Title    string
	Category string
	// Source records how the capability was discovered (e.g. "scan", "report").
	Source string
	// Layers holds the explicit setting at each layer for this context. A layer
	// absent from the map carries no explicit choice (Inherit). LayerGroup, when
	// present, is the combined value across the context's groups (most permissive
	// group wins, per CombineGroups) — the single value that enters the chain.
	Layers map[Layer]Setting
	// Effective is the combined verdict across the whole chain for this tool.
	Effective Effective
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
	runtimeFilter := ""
	if in.RuntimeID.Valid {
		runtimeFilter = `
		 AND EXISTS (
		   SELECT 1 FROM cerebro_capability_subject sub
		   WHERE sub.capability_id = c.id
		     AND sub.subject_type = 'runtime'
		     AND sub.subject_id = $2
		 )`
	}

	// One row per (tool, matching policy layer). The LEFT JOIN keeps tools with
	// no settings (NULL layer), and the subject predicates mirror
	// ListCerebroToolPolicyForContext so an absent (Valid=false) subject id —
	// which marshals to NULL — never matches and that layer stays Inherit.
	rows, err := s.pool.Query(ctx, `
		SELECT c.capability_key, c.title, c.category, c.source,
		       p.layer, p.subject_id, p.setting
		FROM cerebro_capability c
		LEFT JOIN cerebro_tool_policy p
		  ON p.workspace_id = c.workspace_id
		 AND p.tool_key = c.capability_key
		 AND (
		   (p.layer = 'runtime' AND p.subject_id = $2) OR
		   (p.layer = 'agent'   AND p.subject_id = $3) OR
		   (p.layer = 'user'    AND p.subject_id = $4) OR
		   (p.layer = 'group'   AND p.subject_id = ANY($5::uuid[]))
		 )
		WHERE c.workspace_id = $1`+runtimeFilter+`
		ORDER BY c.category, lower(c.title), c.capability_key
	`, in.WorkspaceID, in.RuntimeID, in.AgentID, in.UserID, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load table: %w", err)
	}
	defer rows.Close()

	// Accumulate per tool, preserving the SQL order (category, title) via order[].
	type acc struct {
		row    TableRow
		groups []Setting
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
		} else {
			a.row.Layers[l] = set
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate table rows: %w", err)
	}

	out := make([]TableRow, 0, len(order))
	for _, key := range order {
		a := byTool[key]
		if len(a.groups) > 0 {
			a.row.Layers[LayerGroup] = CombineGroups(a.groups...)
		}
		a.row.Effective = Resolve(Input{Settings: a.row.Layers, Base: in.Base})
		out = append(out, a.row)
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
