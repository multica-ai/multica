package permissionexplain

// response.go shapes a toolpolicy.TableRow into the explain response, including
// the how_to_fix / who_can_fix annotation derived from the row's Effective
// verdict. The small JSON / setting / uuid helpers mirror the equivalents in
// toolpolicy/handler.go (copied here so this package stays self-contained).

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

func toCapabilityResponse(row toolpolicy.TableRow) capabilityResponse {
	groups := make([]groupAttributionResponse, len(row.CappedByGroups))
	for i, g := range row.CappedByGroups {
		groups[i] = groupAttributionResponse{Name: g.Name, Owner: g.Owner}
	}
	howToFix, whoCanFix := fixGuidance(row)
	return capabilityResponse{
		ToolKey:           row.ToolKey,
		ResourcePattern:   row.ResourcePattern,
		Title:             row.Title,
		Category:          row.Category,
		Source:            row.Source,
		ManagedExternally: row.ManagedExternally,
		Layers: layerSettings{
			Workspace: settingPtr(row.Layers, toolpolicy.LayerWorkspace),
			Runtime:   settingPtr(row.Layers, toolpolicy.LayerRuntime),
			Agent:     settingPtr(row.Layers, toolpolicy.LayerAgent),
			Group:     settingPtr(row.Layers, toolpolicy.LayerGroup),
			User:      settingPtr(row.Layers, toolpolicy.LayerUser),
		},
		Effective: effectiveResponse{
			Setting:   string(row.Effective.Setting),
			DecidedBy: string(row.Effective.DecidedBy),
			CappedBy:  string(row.Effective.CappedBy),
			Reason:    row.Effective.Reason,
		},
		CappedByGroups: groups,
		HowToFix:       howToFix,
		WhoCanFix:      whoCanFix,
	}
}

// fixGuidance derives a plain-language how_to_fix and who_can_fix from the row's
// Effective verdict and external-management flag. Precedence:
//  1. managed externally → the tool-policy chain is not the enforcement point;
//  2. a group decided or capped the row → point at the group;
//  3. a settable layer (workspace/runtime/agent/user) decided it → point at that
//     layer's tool-policy row;
//  4. allowed by default (no explicit policy) → nothing to fix.
func fixGuidance(row toolpolicy.TableRow) (howToFix, whoCanFix string) {
	if row.ManagedExternally {
		return "Enforced outside the tool-policy chain (per-feature gate); a tool-policy row has no effect",
			"workspace owner/admin (code/owner-controlled)"
	}

	decidedBy := row.Effective.DecidedBy
	cappedBy := row.Effective.CappedBy

	if decidedBy == toolpolicy.LayerGroup || cappedBy == toolpolicy.LayerGroup {
		how := "Adjust the capping group's policy or remove the subject from it"
		if names := groupNames(row.CappedByGroups); names != "" {
			how += " (groups: " + names + ")"
		}
		return how, "group owner or workspace admin"
	}

	switch decidedBy {
	case toolpolicy.LayerWorkspace, toolpolicy.LayerRuntime, toolpolicy.LayerAgent, toolpolicy.LayerUser:
		return "Set/clear the " + string(decidedBy) + " layer for tool " + row.ToolKey + " via tool-policy",
			"workspace owner/admin"
	}

	// No layer tightened the row above the base. If the effective setting is
	// allow, it is allowed by default; otherwise the base default itself is the
	// decider, which only an admin changes.
	if row.Effective.Setting == toolpolicy.SettingAllow {
		return "Allowed by default (no explicit policy)", "—"
	}
	return "Adjust the workspace default (base) for tool " + row.ToolKey + " via tool-policy",
		"workspace owner/admin"
}

func groupNames(groups []toolpolicy.GroupAttribution) string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		if g.Name != "" {
			names = append(names, g.Name)
		}
	}
	return strings.Join(names, ", ")
}

func settingPtr(layers map[toolpolicy.Layer]toolpolicy.Setting, l toolpolicy.Layer) *string {
	if s, ok := layers[l]; ok {
		v := string(s)
		return &v
	}
	return nil
}

func validSetting(s toolpolicy.Setting) bool {
	switch s {
	case toolpolicy.SettingInherit, toolpolicy.SettingAllow, toolpolicy.SettingAsk, toolpolicy.SettingDeny:
		return true
	default:
		return false
	}
}

func uuidEqual(a, b pgtype.UUID) bool {
	return a.Valid && b.Valid && a.Bytes == b.Bytes
}

func uuidString(id pgtype.UUID) string {
	return util.UUIDToString(id)
}

func uuidStrings(ids []pgtype.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, util.UUIDToString(id))
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
