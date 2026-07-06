"use client";

// FIR-2670 review point #8 — the redesigned "Tools & permissions" presentation
// for the shared tool-policy surfaces. This is PRESENTATION ONLY: it takes the
// already-filtered, already-resolved catalog rows and the caller's own
// per-row Decision cell (DecisionControl + ConditionControl + configure
// buttons, unchanged) and lays them out as grouped capability cards to match
// the shipped agent-page redesign design language. It never resolves a verdict,
// never writes a policy, and never re-groups repos/credentials/connections that
// already render as their own sections — so no logic is forked from the classic
// table. The whole surface is gated behind `cerebro_agent_page_redesign` at the
// call site, so production keeps the flat table until the flag flips.

import { useState, type ReactNode } from "react";
import { ChevronRight, FolderGit2, KeyRound, LayoutGrid, Plug, Terminal } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { ToolEffectiveSetting, ToolPolicyRow } from "../core";
import { permissionType, type PermissionType } from "./tool-policy-table";

// One capability card: a titled group of tool rows with a verdict-dot summary.
export interface CapabilityGroup {
  /** Stable key — the display label lower-cased. */
  key: string;
  /** Human label shown in the card header. */
  label: string;
  /** The permission type that picks the header icon and orders the groups. */
  type: PermissionType;
  rows: ToolPolicyRow[];
  /** Count of rows per resolved Effective verdict, for the header dot summary. */
  counts: Record<ToolEffectiveSetting, number>;
}

// Header icon per permission type — a light nod to the design's per-group icon
// without inventing a bespoke icon for every free-form backend category.
const TYPE_ICON: Record<PermissionType, typeof LayoutGrid> = {
  Multica: LayoutGrid,
  Runtime: Terminal,
  Connections: Plug,
  Repos: FolderGit2,
  Credentials: KeyRound,
};

// Order the cards by permission type first (Multica · Runtime · Connections ·
// Repos · Credentials), then by first appearance within a type, so the layout is
// stable across renders and never reshuffles as verdicts change.
const TYPE_ORDER: PermissionType[] = [
  "Multica",
  "Runtime",
  "Connections",
  "Repos",
  "Credentials",
];

// A tidy display label for a card: the backend category when it is a real domain
// label ("Slack", "Deploy", "Web"), otherwise the coarse permission type. The
// generic buckets the backend emits for uncategorised tools ("tools",
// "Runtimes") are not useful headers, so they fall back to the type.
function groupLabel(row: ToolPolicyRow): string {
  const cat = row.category?.trim() ?? "";
  const generic = cat === "" || cat.toLowerCase() === "tools" || cat === "Runtimes";
  if (generic) return permissionType(row);
  return cat;
}

// groupByCapability splits the flat catalog into capability cards. Grouping is by
// the display label; within a card the caller's row order is preserved. Groups
// are ordered by permission type then first appearance — never by verdict, so a
// toggle never makes a card jump.
export function groupByCapability(rows: ToolPolicyRow[]): CapabilityGroup[] {
  const byKey = new Map<string, CapabilityGroup>();
  for (const row of rows) {
    const label = groupLabel(row);
    const key = label.toLowerCase();
    let group = byKey.get(key);
    if (!group) {
      group = {
        key,
        label,
        type: permissionType(row),
        rows: [],
        counts: { allow: 0, ask: 0, deny: 0 },
      };
      byKey.set(key, group);
    }
    group.rows.push(row);
    group.counts[row.effective.setting] += 1;
  }
  return [...byKey.values()].sort((a, b) => {
    const ta = TYPE_ORDER.indexOf(a.type);
    const tb = TYPE_ORDER.indexOf(b.type);
    if (ta !== tb) return ta - tb;
    return 0; // insertion order is preserved for a Map, so ties stay stable
  });
}

// A colored count dot in the card header. Hidden when the count is zero so a card
// only advertises the verdicts it actually contains.
function VerdictDot({
  setting,
  count,
}: {
  setting: ToolEffectiveSetting;
  count: number;
}) {
  if (count === 0) return null;
  const dot: Record<ToolEffectiveSetting, string> = {
    allow: "bg-emerald-500",
    ask: "bg-amber-500",
    deny: "bg-destructive",
  };
  return (
    <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground">
      <span className={cn("size-[7px] rounded-full", dot[setting])} />
      {count}
    </span>
  );
}

export interface CapabilityCatalogProps {
  /** The already-filtered flat capability rows (no repo/credential sub-rows). */
  rows: ToolPolicyRow[];
  /** The caller's per-row decision cell — unchanged DecisionControl et al. */
  renderDecision: (row: ToolPolicyRow) => ReactNode;
  /** Optional per-card bulk action rendered on the right of the header. */
  renderGroupAction?: (group: CapabilityGroup) => ReactNode;
  /**
   * FIR-2706 — optional inline detail for a row that is itself a group (a
   * connection with per-tool rows, a tool with data sources). When it returns a
   * node, the row gets an expand chevron and the node renders in a collapsible
   * panel below the row. Returning null keeps the row a plain leaf.
   */
  renderDetail?: (row: ToolPolicyRow) => ReactNode | null;
}

// CapabilityCatalog renders the flat catalog as grouped capability cards. It is
// responsive by construction: the header wraps and the decision cell keeps its
// own layout, so the same markup serves desktop and mobile (no separate table +
// card-list fork like the classic view).
export function CapabilityCatalog({
  rows,
  renderDecision,
  renderGroupAction,
  renderDetail,
}: CapabilityCatalogProps) {
  const groups = groupByCapability(rows);
  // FIR-2706 — which rows are expanded to show their inline group. Keyed by the
  // same tool_key:resource_pattern string used for the row's React key.
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const toggle = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  return (
    <div className="flex flex-col gap-3.5" data-testid="capability-catalog">
      {groups.map((group) => {
        const Icon = TYPE_ICON[group.type];
        return (
          <div
            key={group.key}
            data-testid={`capability-group-${group.key}`}
            className="overflow-hidden rounded-xl border bg-background shadow-sm"
          >
            <div className="flex flex-wrap items-center justify-between gap-3 border-b bg-muted/40 px-4 py-3">
              <div className="flex items-center gap-2 text-sm font-semibold">
                <Icon className="size-[15px] text-muted-foreground" />
                {group.label}
                <span className="font-mono text-[11px] font-normal text-muted-foreground">
                  {group.rows.length}{" "}
                  {group.rows.length === 1 ? "tool" : "tools"}
                </span>
              </div>
              <div className="flex items-center gap-3">
                <div className="flex items-center gap-2.5">
                  <VerdictDot setting="allow" count={group.counts.allow} />
                  <VerdictDot setting="ask" count={group.counts.ask} />
                  <VerdictDot setting="deny" count={group.counts.deny} />
                </div>
                {renderGroupAction?.(group)}
              </div>
            </div>
            {group.rows.map((row) => {
              const rowKey = `${row.tool_key}:${row.resource_pattern ?? ""}`;
              const detail = renderDetail?.(row) ?? null;
              const isOpen = detail != null && expanded.has(rowKey);
              return (
                <div
                  key={rowKey}
                  data-testid={`tool-card-${row.tool_key}${
                    row.resource_pattern ? `:${row.resource_pattern}` : ""
                  }`}
                  className="border-t first:border-t-0"
                >
                  <div className="flex items-center justify-between gap-4 px-4 py-3">
                    {/* The name area doubles as the expand target when the row is
                        itself a group, so a big touch target opens it on mobile
                        without stealing the row from the Decision toggle. */}
                    {detail != null ? (
                      <button
                        type="button"
                        onClick={() => toggle(rowKey)}
                        aria-expanded={isOpen}
                        data-testid={`tool-expand-${row.tool_key}`}
                        className="flex min-w-0 flex-1 items-center gap-2 text-left"
                      >
                        <ChevronRight
                          className={cn(
                            "size-4 shrink-0 text-muted-foreground transition-transform",
                            isOpen && "rotate-90",
                          )}
                        />
                        <span className="min-w-0">
                          <span className="block truncate text-sm font-medium">
                            {row.title || row.tool_key}
                          </span>
                          <span className="block truncate font-mono text-xs text-muted-foreground">
                            {row.tool_key}
                          </span>
                        </span>
                      </button>
                    ) : (
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium">
                          {row.title || row.tool_key}
                        </div>
                        <div className="truncate font-mono text-xs text-muted-foreground">
                          {row.tool_key}
                        </div>
                      </div>
                    )}
                    <div className="flex shrink-0 items-center gap-2">
                      {renderDecision(row)}
                    </div>
                  </div>
                  {isOpen ? (
                    <div
                      data-testid={`tool-detail-${row.tool_key}`}
                      className="border-t bg-muted/30 px-4 py-3"
                    >
                      {detail}
                    </div>
                  ) : null}
                </div>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}
