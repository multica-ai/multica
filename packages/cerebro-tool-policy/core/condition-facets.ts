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
}

/**
 * conditionFacets decides the meaningful WHEN facets for a row. The action
 * presets are literal verb lists keyed off the tool's resource model; host is a
 * boolean keyed off whether the tool egresses to a host. Both are deliberately
 * conservative — when nothing matches, the editor shows no structured section.
 */
export function conditionFacets(row: ToolPolicyRow): ConditionFacets {
  return { host: hasHostFacet(row), actions: actionPreset(row) };
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
