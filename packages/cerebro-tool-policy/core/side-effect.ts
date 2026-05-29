// Side-effect taxonomy for the capability catalog (FIR-2284 redesign, Bite A).
//
// The approved UX filters tools by *what they can do to the world* — Read,
// Control, Write · local, Egress, Write · external, Auth flow — on top of the
// free-form `category` (the capability "class"). The backend capability register
// does not yet carry this dimension, so we classify client-side by keyword over
// the tool key + title + category. This is an HONEST heuristic, not authoritative
// metadata: a few agentic tools land one bucket off (e.g. ToolSearch reads as
// Read, TaskCreate as Write). Making side-effect a first-class field on
// cerebro_capability is Bite B; until then this keeps the filter useful without
// blocking the new flade.
//
// Ordering is deliberate and fails toward the MORE sensitive bucket: a tool that
// both reads and sends is classified as the side effect a reviewer most needs to
// see (auth > external write > egress > local write > read/control). That way a
// dangerous capability is never hidden under a benign label.

import type { ToolPolicyRow, ToolEffectiveSetting } from "./tool-policy";

/** The six side-effect classes the catalog filters on, in display order. */
export type SideEffect =
  | "read"
  | "control"
  | "write_local"
  | "egress"
  | "write_external"
  | "auth";

/** Human label for each side effect — whole words, no shouting (per Jesper). */
export const SIDE_EFFECT_LABEL: Record<SideEffect, string> = {
  read: "Read",
  control: "Control",
  write_local: "Write · local",
  egress: "Egress",
  write_external: "Write · external",
  auth: "Auth flow",
};

/** Display order for side-effect filter chips. */
export const SIDE_EFFECTS: SideEffect[] = [
  "read",
  "control",
  "write_local",
  "egress",
  "write_external",
  "auth",
];

// Ordered matchers — first hit wins, most sensitive first. Kept as an array (not
// a map) so the precedence is explicit and testable.
const MATCHERS: { effect: SideEffect; re: RegExp }[] = [
  {
    effect: "auth",
    re: /\b(oauth|o-auth|login|sign[\s_-]?in|authori[sz]e|authenticat|credential|connect[\s_-]?account|grant[\s_-]?access)\b/,
  },
  {
    effect: "write_external",
    re: /\b(send|publish|tweet|upload|gmail|e-?mail|mail|smtp)\b|\bpost\b.*\b(message|comment|issue|mail|email|slack)\b/,
  },
  {
    effect: "egress",
    re: /\b(fetch|download|crawl|scrape|curl|wget|http|web[\s_-]?search|web[\s_-]?fetch|web|url)\b/,
  },
  {
    effect: "write_local",
    re: /\b(write|edit|create|update|delete|remove|rm|drop|truncate|bash|shell|exec|run|build|deploy|restart|kill|mkdir|move|copy|patch|commit|push|notebook|set|put)\b/,
  },
  {
    effect: "read",
    re: /\b(get|list|read|view|show|output|status|describe|inspect|count|search|monitor|query)\b/,
  },
];

/**
 * classifySideEffect buckets one tool by keyword over its key, title and
 * category. Anything that matches nothing is treated as Control — the bucket for
 * agentic, no-IO tools (Agent, Skill, plan/worktree, scheduling) that don't read
 * or write but still steer the run.
 */
export function classifySideEffect(row: ToolPolicyRow): SideEffect {
  const hay = normalize(`${row.tool_key} ${row.title} ${row.category}`);
  for (const m of MATCHERS) {
    if (m.re.test(hay)) return m.effect;
  }
  return "control";
}

// normalize turns a mixed key/title/category into space-separated lowercase
// words so the \b-anchored matchers fire reliably. Tool keys come in two shapes:
// CamelCase built-ins (TaskGet, WebFetch) and snake/dotted MCP actions
// (gmail_send, slack.post_message). Both must tokenise to real words — a raw \b
// never sits inside "TaskGet" and treats "_" as a word char, so "gmail_send"
// would otherwise hide its "send". We split camelCase, then replace every
// non-alphanumeric run with a space.
function normalize(s: string): string {
  return s
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[^a-zA-Z0-9]+/g, " ")
    .toLowerCase()
    .trim();
}

/** A class (category) facet for the filter chips: its count + decision spread. */
export interface ClassFacet {
  /** The raw category label from the capability register (e.g. "MCP · ClickUp"). */
  category: string;
  count: number;
  /** Effective-verdict distribution across the class, for the little spread bar. */
  allow: number;
  ask: number;
  deny: number;
}

/**
 * classFacets groups rows by their `category` and tallies the effective-verdict
 * spread per class, preserving first-seen order so the chips match the table's
 * SQL ordering (already category-sorted server-side). An empty category collapses
 * to "Uncategorised" so every tool is reachable by a chip.
 */
export function classFacets(rows: ToolPolicyRow[]): ClassFacet[] {
  const order: string[] = [];
  const byCat = new Map<string, ClassFacet>();
  for (const row of rows) {
    const category = row.category || "Uncategorised";
    let facet = byCat.get(category);
    if (!facet) {
      facet = { category, count: 0, allow: 0, ask: 0, deny: 0 };
      byCat.set(category, facet);
      order.push(category);
    }
    facet.count += 1;
    bumpVerdict(facet, row.effective.setting);
  }
  return order.map((c) => byCat.get(c)!);
}

function bumpVerdict(facet: ClassFacet, setting: ToolEffectiveSetting): void {
  if (setting === "allow") facet.allow += 1;
  else if (setting === "ask") facet.ask += 1;
  else facet.deny += 1;
}
