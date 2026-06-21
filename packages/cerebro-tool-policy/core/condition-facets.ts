// Contextual facets for the WHEN-condition editor (FIR-1609 follow-up).
//
// The CEO rejected the generic condition editor: a host allow-list and a
// free-text "actions" box were shown on EVERY tool, even where they make no
// sense (a host allow-list on a notification tool is nonsense). This helper
// decides, per row, WHICH condition facets are meaningful so the editor can show
// only those — host only where the tool calls out to a host, and a preset list
// of verbs only where the tool has a verbed resource model (a registry data
// source, a repo capability, a credential). Everything else gets no facet, so
// the editor hides the structured sections entirely (the CEL escape hatch stays
// available under an Advanced disclosure on any row that still shows the
// control).
//
// This is a pure, client-side heuristic over the same row fields the rest of the
// catalog already classifies on (tool_key / source / category / side effect) —
// it never widens access on its own (the Decision still gates), so failing soft
// (returning no facet) is safe.

import type { ToolPolicyRow } from "./tool-policy";
import { classifySideEffect } from "./side-effect";

/**
 * Which structured WHEN facets are meaningful for one tool. `host` is true when
 * the tool calls out to a host (so the host allow-list is worth showing).
 * `actions` is the preset list of verbs the tool's resource model supports
 * (empty means "no actions facet" — the tool has no verb dimension to narrow).
 */
export interface ConditionFacets {
  host: boolean;
  actions: string[];
  /**
   * Whether the CEL escape hatch is meaningful here. CEL is evaluable at every
   * chain gate, so it is true on any chain-gated row — but FIR-1708 C lets the
   * server withhold it on rows the chain does not gate (managed-externally).
   */
  cel: boolean;
}

/**
 * conditionFacets decides the meaningful WHEN facets for a row.
 *
 * FIR-1708 C: when the server sends `enforced_conditions` (the kinds whose
 * request attribute its gate actually threads), that is authoritative — the
 * editor shows exactly those. When it is absent (an older backend), we fall back
 * to the pure client-side heuristic below so there is no regression: host keyed
 * off egress, action presets off the tool's resource model, and CEL always on
 * (its historic behaviour). The action *labels* always come from the resource
 * model, since the server reports only that the action kind bites, not its verbs.
 */
export function conditionFacets(row: ToolPolicyRow): ConditionFacets {
  const enforced = row.enforced_conditions;
  if (enforced) {
    return {
      host: enforced.includes("host"),
      actions: enforced.includes("action") ? actionPreset(row) : [],
      cel: enforced.includes("cel"),
    };
  }
  return { host: hasHostFacet(row), actions: actionPreset(row), cel: true };
}

// actionPreset returns the literal verb list for a tool whose resource model has
// a verb dimension worth narrowing — the registry data sources (get_schema /
// execute), repo capabilities (read / checkout / push), and credential tools
// (reveal / rotate). Anything else has no verb dimension, so it returns [].
function actionPreset(row: ToolPolicyRow): string[] {
  const key = row.tool_key.toLowerCase();
  if (row.tool_key === "firtal_registry" || row.source === "registry-data-source") {
    return ["get_schema", "execute"];
  }
  if (key.startsWith("repo.") || row.category === "repo" || row.source === "repo") {
    return ["read", "checkout", "push"];
  }
  if (key.includes("credential")) {
    return ["reveal", "rotate"];
  }
  return [];
}

// hasHostFacet is true when the tool reaches out to a host, so a host allow-list
// is a meaningful way to narrow when the rule applies. We treat an egress side
// effect, the web fetch/search built-ins, and every connection-sourced row as
// host-bound; everything else is not.
function hasHostFacet(row: ToolPolicyRow): boolean {
  if (classifySideEffect(row) === "egress") return true;
  const key = row.tool_key.toLowerCase();
  if (key.includes("web_fetch") || key.includes("web_search") || key.includes("webfetch")) {
    return true;
  }
  if (
    row.source === "connection" ||
    row.source === "connection-tool" ||
    row.source === "connection-endpoint" ||
    key.startsWith("connection:")
  ) {
    return true;
  }
  return false;
}
