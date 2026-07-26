"use client";

// The FIR-2358 simple, user-facing tool permission table — the Decision-A flat
// surface that replaces the rich Effective-chain table on the agent Tools tab.
// One Allow/Ask/Block toggle per tool, rows grouped into four sections
// (Læs · Udfør · Hent · Destruktivt). It reuses the cerebro_tool_policy data
// layer untouched: writes target the AGENT layer only, and "no override" simply
// means the row inherits from the layer below (the Inherit affordance is dropped
// — not setting a rule already means inherit, so no extra button is needed).
//
// The four sections are a frontend grouping: the backend `category` is a
// free-form domain label (e.g. "Slack", "Deploy", "Web"), so classifyTool maps
// each tool into one of the four action buckets by keyword. Grouping is purely
// visual — it never changes a verdict. The toggle highlight falls back to the
// server-resolved Effective verdict when the agent has no explicit override, so
// the surface fails CLOSED: an unknown/ drifted verdict shows Bloker, never
// Tillad (see ../core, schemas downgrade unknown effective → deny).

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import {
  toolPolicyTableOptions,
  useSetToolPolicy,
  type ToolEffectiveSetting,
  type ToolPolicyRow,
} from "../core";
import { FirtalRegistryRowConfigure } from "./firtal-registry-row-configure";

export interface SimpleToolPolicyTableProps {
  wsId: string;
  /** The agent whose per-tool rules this table edits (writes the agent layer). */
  agentId: string;
  /** The runtime below the agent, so inherited rows resolve their Effective verdict. */
  runtimeId?: string | null;
  /** The agent owner, so the inherited Effective verdict reflects the real ceiling. */
  userId?: string | null;
}

// The three authorable verdicts in display order, with their Danish labels.
const SETTINGS: ToolEffectiveSetting[] = ["allow", "ask", "deny"];
const SETTING_LABEL: Record<ToolEffectiveSetting, string> = {
  allow: "Allow",
  ask: "Ask",
  deny: "Block",
};
// Active-state colour per verdict, matching the cerebro fork's emerald/amber/
// destructive palette (see cerebro-agent-tools, cerebro-access).
const SETTING_ACTIVE_CLASS: Record<ToolEffectiveSetting, string> = {
  allow: "text-emerald-700 dark:text-emerald-300",
  ask: "text-amber-700 dark:text-amber-400",
  deny: "text-destructive",
};

type GroupKey = "read" | "execute" | "fetch" | "destructive";

interface GroupDef {
  key: GroupKey;
  label: string;
  danger: boolean;
}

// Display order of the four sections. Destruktivt renders last and red.
const GROUPS: GroupDef[] = [
  { key: "read", label: "Read", danger: false },
  { key: "execute", label: "Execute", danger: false },
  { key: "fetch", label: "Fetch", danger: false },
  { key: "destructive", label: "Destructive", danger: true },
];

// classifyTool buckets one tool into a section. Priority order is destructive →
// fetch → execute → read so a dangerous tool is never hidden under a benign
// header; read is the default. Matching is keyword-based over the tool key,
// title, and category because the backend category is free-form.
export function classifyTool(row: ToolPolicyRow): GroupKey {
  const hay = `${row.tool_key} ${row.title} ${row.category}`.toLowerCase();
  if (
    /delete|destroy|\bdrop\b|truncate|purge|wipe|remove|\brm\b|revoke|terminate|\bkill\b|force.?push|reset.?hard/.test(
      hay,
    ) ||
    /\bsend\b.*\b(mail|email|e-?mail)\b/.test(hay)
  ) {
    return "destructive";
  }
  if (/fetch|https?|\bweb\b|download|crawl|scrape|\burl\b|curl|\bmcp\b/.test(hay)) {
    return "fetch";
  }
  if (
    /\brun\b|exec|bash|shell|command|\bcmd\b|write|create|edit|update|deploy|build|restart|post|publish|trigger|mutation/.test(
      hay,
    )
  ) {
    return "execute";
  }
  return "read";
}

interface Section {
  def: GroupDef;
  rows: ToolPolicyRow[];
}

// groupRows splits the (already filtered) rows into the four sections in display
// order, dropping empty sections. Within a section the SQL order is preserved.
export function groupRows(rows: ToolPolicyRow[]): Section[] {
  const buckets = new Map<GroupKey, ToolPolicyRow[]>();
  for (const row of rows) {
    const key = classifyTool(row);
    const list = buckets.get(key);
    if (list) list.push(row);
    else buckets.set(key, [row]);
  }
  return GROUPS.map((def) => ({ def, rows: buckets.get(def.key) ?? [] })).filter(
    (s) => s.rows.length > 0,
  );
}

// agentVerdict reads the toggle's highlighted verdict: the agent's own override
// when set, otherwise the inherited Effective verdict (always concrete, deny on
// drift) so the surface never shows a blank or an unsafe "allow" by accident.
function agentVerdict(row: ToolPolicyRow): ToolEffectiveSetting {
  const o = row.layers.agent;
  if (o === "allow" || o === "ask" || o === "deny") return o;
  return row.effective.setting;
}

export function SimpleToolPolicyTable({
  wsId,
  agentId,
  runtimeId,
  userId,
}: SimpleToolPolicyTableProps) {
  const ctx = useMemo(
    () => ({ agentId, runtimeId, userId }),
    [agentId, runtimeId, userId],
  );
  const query = useQuery(toolPolicyTableOptions(wsId, ctx));
  const setPolicy = useSetToolPolicy();

  const [search, setSearch] = useState("");
  const rows = query.data ?? [];

  const sections = useMemo(() => {
    const q = search.trim().toLowerCase();
    const filtered = q
      ? rows.filter((r) =>
          `${r.title} ${r.tool_key} ${r.category}`.toLowerCase().includes(q),
        )
      : rows;
    return groupRows(filtered);
  }, [rows, search]);

  function onSet(toolKey: string, setting: ToolEffectiveSetting) {
    setPolicy.mutate({
      tool_key: toolKey,
      layer: "agent",
      subject_id: agentId,
      setting,
    });
  }

  return (
    <div className="flex flex-col gap-4" data-testid="simple-tool-policy-table">
      <div className="flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">
          Set each capability to Allow, Ask, or Block. Applies to this agent.
        </p>
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search capabilities…"
            className="h-9 pl-9"
            aria-label="Search capabilities"
          />
        </div>
      </div>

      {query.isLoading ? (
        <p className="text-sm text-muted-foreground">Loading capabilities…</p>
      ) : sections.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {rows.length === 0
            ? "No capabilities reported yet."
            : "No capabilities match the search."}
        </p>
      ) : (
        <div className="overflow-hidden rounded-lg border">
          <div className="grid grid-cols-[1fr_auto] items-center gap-4 border-b px-5 py-2.5 text-xs font-medium text-muted-foreground">
            <span>Capability</span>
            <span className="justify-self-center">Rule</span>
          </div>
          {sections.map((section) => (
            <div key={section.def.key}>
              <div
                className={cn(
                  "border-b bg-muted px-5 py-2 text-[11px] font-semibold uppercase tracking-wide",
                  section.def.danger
                    ? "text-destructive"
                    : "text-muted-foreground",
                )}
              >
                {section.def.label}
              </div>
              {section.rows.map((row) => (
                <div
                  key={row.tool_key}
                  data-testid={`tool-row-${row.tool_key}`}
                  className="grid grid-cols-[1fr_auto] items-center gap-4 border-b px-5 py-3 last:border-b-0"
                >
                  <div className="min-w-0">
                    <div className="truncate text-sm font-medium">
                      {row.title || row.tool_key}
                    </div>
                    <div className="truncate text-xs text-muted-foreground">
                      {row.tool_key}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <FirtalRegistryRowConfigure
                      toolKey={row.tool_key}
                      agentId={agentId}
                      variant="outline"
                    />
                    <VerdictToggle
                      value={agentVerdict(row)}
                      disabled={setPolicy.isPending}
                      onChange={(s) => onSet(row.tool_key, s)}
                    />
                  </div>
                </div>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// VerdictToggle is the 3-state Tillad/Spørg/Bloker segmented control for one
// tool. Exactly one verdict is highlighted; clicking another writes it to the
// agent layer.
function VerdictToggle({
  value,
  disabled,
  onChange,
}: {
  value: ToolEffectiveSetting;
  disabled?: boolean;
  onChange: (setting: ToolEffectiveSetting) => void;
}) {
  return (
    <div
      className="inline-flex gap-0.5 rounded-md bg-muted p-0.5"
      role="group"
      aria-label="Rule"
    >
      {SETTINGS.map((setting) => {
        const isActive = setting === value;
        return (
          <Button
            key={setting}
            type="button"
            size="sm"
            variant="ghost"
            disabled={disabled}
            aria-pressed={isActive}
            className={cn(
              "h-7 rounded px-3 text-xs font-medium hover:bg-transparent",
              isActive
                ? cn(
                    "bg-background font-semibold shadow-sm",
                    SETTING_ACTIVE_CLASS[setting],
                  )
                : "text-muted-foreground",
            )}
            onClick={() => onChange(setting)}
          >
            {SETTING_LABEL[setting]}
          </Button>
        );
      })}
    </div>
  );
}
