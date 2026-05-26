"use client";

// The FIR-2230 permission table — one row per tool with the mockup columns
// Tool · Source · Runtime · This agent · Effective, and an Allow/Ask/Deny/Inherit
// control per editable layer. The SAME component renders on the agent page and
// the runtime page; `view` decides which layer column is editable (agent → the
// "This agent" column, runtime → the "Runtime" column) and which subject the
// authoring writes target. All resolution happens server-side, so the row already
// carries its Effective verdict and the "Capped by …" reason — the UI only
// renders it.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Lock, ShieldAlert, ShieldCheck, ShieldQuestion } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { cn } from "@multica/ui/lib/utils";
import {
  toolPolicyTableOptions,
  useSetToolPolicy,
  type ToolEffectiveSetting,
  type ToolLayer,
  type ToolPolicyRow,
  type ToolSetting,
} from "../core";

export interface ToolPolicyTableProps {
  wsId: string;
  /** Which page this table is on — decides the editable layer + write subject. */
  view: "agent" | "runtime";
  /** The agent id (view="agent") or runtime id (view="runtime") being edited. */
  subjectId: string;
  /** The runtime the agent runs on — shows the inherited Runtime column (agent view). */
  runtimeId?: string | null;
  /** The viewing user + their groups, so the Effective column reflects the ceiling. */
  userId?: string | null;
  groupIds?: string[];
}

type TabKey = "all" | "overrides" | "approval";

const SETTING_CHOICES: ToolSetting[] = ["allow", "ask", "deny", "inherit"];
const SETTING_LABEL: Record<ToolSetting, string> = {
  allow: "Allow",
  ask: "Ask",
  deny: "Deny",
  inherit: "Inherit",
};

export function ToolPolicyTable({
  wsId,
  view,
  subjectId,
  runtimeId,
  userId,
  groupIds,
}: ToolPolicyTableProps) {
  const ctx = useMemo(
    () =>
      view === "agent"
        ? { agentId: subjectId, runtimeId, userId, groupIds }
        : { runtimeId: subjectId },
    [view, subjectId, runtimeId, userId, groupIds],
  );

  const query = useQuery(toolPolicyTableOptions(wsId, ctx));
  const setPolicy = useSetToolPolicy();

  const [search, setSearch] = useState("");
  const [tab, setTab] = useState<TabKey>("all");

  const rows = query.data ?? [];
  const filtered = useMemo(() => filterRows(rows, search, tab), [rows, search, tab]);

  // The layer this page authors, and the subject those writes target.
  const editLayer: ToolLayer = view === "agent" ? "agent" : "runtime";

  function onSetLayer(toolKey: string, setting: ToolSetting) {
    setPolicy.mutate({
      tool_key: toolKey,
      layer: editLayer,
      subject_id: subjectId,
      setting,
    });
  }

  return (
    <div className="flex flex-col gap-3" data-testid="tool-policy-table">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Tabs value={tab} onValueChange={(v) => setTab(v as TabKey)}>
          <TabsList>
            <TabsTrigger value="all">All</TabsTrigger>
            <TabsTrigger value="overrides">Overrides only</TabsTrigger>
            <TabsTrigger value="approval">Needs approval</TabsTrigger>
          </TabsList>
        </Tabs>
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search tools…"
          className="h-9 w-full max-w-xs"
          aria-label="Search tools"
        />
      </div>

      {query.isLoading ? (
        <p className="text-sm text-muted-foreground">Loading tools…</p>
      ) : filtered.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {rows.length === 0 ? "No tools reported yet." : "No tools match this filter."}
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Tool</TableHead>
              <TableHead>Source</TableHead>
              <TableHead>Runtime</TableHead>
              {view === "agent" && <TableHead>This agent</TableHead>}
              <TableHead>Effective</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.map((row) => (
              <TableRow key={row.tool_key}>
                <TableCell>
                  <div className="flex flex-col">
                    <span className="font-medium">{row.title || row.tool_key}</span>
                    <span className="text-xs text-muted-foreground">{row.tool_key}</span>
                  </div>
                </TableCell>
                <TableCell className="text-sm text-muted-foreground">
                  {row.source || "—"}
                </TableCell>

                {/* Runtime column: editable on the runtime page, read-only on the agent page. */}
                <TableCell>
                  {view === "runtime" ? (
                    <LayerControl
                      value={row.layers.runtime}
                      disabled={setPolicy.isPending}
                      onChange={(s) => onSetLayer(row.tool_key, s)}
                    />
                  ) : (
                    <ReadonlySetting value={row.layers.runtime} />
                  )}
                </TableCell>

                {/* This agent column only exists on the agent page, and is editable there. */}
                {view === "agent" && (
                  <TableCell>
                    <LayerControl
                      value={row.layers.agent}
                      disabled={setPolicy.isPending}
                      onChange={(s) => onSetLayer(row.tool_key, s)}
                    />
                  </TableCell>
                )}

                <TableCell>
                  <EffectiveBadge
                    setting={row.effective.setting}
                    reason={row.effective.reason}
                    capped={row.effective.capped_by !== ""}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

// filterRows applies the active tab and the search box. "overrides" keeps rows
// with any explicit per-layer setting; "approval" keeps rows whose effective
// verdict pauses for a human (ask).
function filterRows(rows: ToolPolicyRow[], search: string, tab: TabKey): ToolPolicyRow[] {
  const q = search.trim().toLowerCase();
  return rows.filter((r) => {
    if (q) {
      const hay = `${r.title} ${r.tool_key} ${r.category}`.toLowerCase();
      if (!hay.includes(q)) return false;
    }
    if (tab === "overrides") {
      const l = r.layers;
      if (!l.runtime && !l.agent && !l.group && !l.user) return false;
    }
    if (tab === "approval" && r.effective.setting !== "ask") return false;
    return true;
  });
}

// LayerControl is the Allow/Ask/Deny/Inherit segmented control for one editable
// layer cell. The active choice is highlighted; clicking another writes it.
function LayerControl({
  value,
  disabled,
  onChange,
}: {
  value: ToolSetting | null;
  disabled?: boolean;
  onChange: (setting: ToolSetting) => void;
}) {
  const active = value ?? "inherit";
  return (
    <div className="inline-flex overflow-hidden rounded-md border" role="group">
      {SETTING_CHOICES.map((choice) => {
        const isActive = choice === active;
        return (
          <Button
            key={choice}
            type="button"
            size="sm"
            variant={isActive ? "default" : "ghost"}
            disabled={disabled}
            aria-pressed={isActive}
            className={cn("h-7 rounded-none px-2 text-xs", !isActive && "text-muted-foreground")}
            onClick={() => onChange(choice)}
          >
            {SETTING_LABEL[choice]}
          </Button>
        );
      })}
    </div>
  );
}

// ReadonlySetting renders a layer's value when this page cannot edit it (e.g. the
// Runtime column on the agent page).
function ReadonlySetting({ value }: { value: ToolSetting | null }) {
  if (!value || value === "inherit") {
    return <span className="text-xs text-muted-foreground">Inherit</span>;
  }
  return (
    <Badge variant="outline" className="text-xs">
      {SETTING_LABEL[value]}
    </Badge>
  );
}

// EffectiveBadge renders the resolved verdict with an icon + the reason, so a
// "Capped by user" row reads as denied-because-of-the-ceiling at a glance.
function EffectiveBadge({
  setting,
  reason,
  capped,
}: {
  setting: ToolEffectiveSetting;
  reason: string;
  capped: boolean;
}) {
  const variant = setting === "allow" ? "secondary" : setting === "ask" ? "outline" : "destructive";
  const Icon = setting === "allow" ? ShieldCheck : setting === "ask" ? ShieldQuestion : capped ? Lock : ShieldAlert;
  return (
    <div className="flex flex-col gap-0.5">
      <Badge variant={variant} className="w-fit gap-1">
        <Icon className="size-3" />
        {SETTING_LABEL[setting]}
      </Badge>
      {reason && <span className="text-xs text-muted-foreground">{reason}</span>}
    </div>
  );
}
