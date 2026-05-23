// Agent override card (JEH-1710 bid 4, Skærm B).
//
// The agent inherits its runtime's tools as the default. This card surfaces
// that default per tool, and lets a workspace owner/admin force a tool on or
// off for THIS agent only. Override rows are visually distinct from inherit
// rows so the admin can spot exceptions at a glance.
//
// Effective state = override.enabled when an override row exists; otherwise
// runtime_tool.enabled. The "Effektiv"-kolonnen is the source of truth — what
// the agent actually gets.

"use client";

import { useMemo, useState } from "react";
import {
  AlertCircle,
  Check,
  Circle,
  Cloud,
  Info,
  Loader2,
  Search,
  Wrench,
  X,
} from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { Agent } from "@multica/core/types";
import type {
  AgentToolOverride,
  RuntimeTool,
} from "@multica/cerebro-types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";

interface AgentToolsCardProps {
  agent: Agent;
  /** Workspace role of the viewer. Edit is restricted to owner/admin. */
  canEdit: boolean;
  /** Display name of the agent's runtime, shown in the inherit banner. */
  runtimeName?: string;
}

type RowFilter = "all" | "override" | "effective_on";

type OverrideMode = "inherit" | "force_on" | "force_off";

interface ToolRowData {
  tool: RuntimeTool;
  overrideMode: OverrideMode;
  effective: boolean;
}

const runtimeToolsKey = (runtimeId: string) =>
  ["cerebro", "runtime-tools", runtimeId] as const;

const agentOverridesKey = (agentId: string) =>
  ["cerebro", "agent-tool-overrides", agentId] as const;

export function AgentToolsCard({ agent, canEdit, runtimeName }: AgentToolsCardProps) {
  const qc = useQueryClient();
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<RowFilter>("all");

  const runtimeId = agent.runtime_id;

  const runtimeToolsQuery = useQuery({
    queryKey: runtimeToolsKey(runtimeId ?? ""),
    queryFn: () => api.listRuntimeTools(runtimeId!),
    enabled: !!runtimeId,
  });

  const overridesQuery = useQuery({
    queryKey: agentOverridesKey(agent.id),
    queryFn: () => api.listAgentToolOverrides(agent.id),
  });

  const setOverrideMutation = useMutation({
    mutationFn: ({ toolName, enabled }: { toolName: string; enabled: boolean }) =>
      api.setAgentToolOverride(agent.id, toolName, enabled),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: agentOverridesKey(agent.id) });
    },
    onError: (err) => {
      toast.error(
        err instanceof Error ? err.message : "Kunne ikke gemme override",
      );
    },
  });

  const clearOverrideMutation = useMutation({
    mutationFn: ({ toolName }: { toolName: string }) =>
      api.clearAgentToolOverride(agent.id, toolName),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: agentOverridesKey(agent.id) });
    },
    onError: (err) => {
      toast.error(
        err instanceof Error ? err.message : "Kunne ikke fjerne override",
      );
    },
  });

  const rows: ToolRowData[] = useMemo(() => {
    const tools = runtimeToolsQuery.data ?? [];
    const overrides = overridesQuery.data ?? [];
    const overrideByName = new Map<string, AgentToolOverride>();
    for (const o of overrides) overrideByName.set(o.tool_name, o);
    return tools.map((tool) => {
      const override = overrideByName.get(tool.name);
      let overrideMode: OverrideMode = "inherit";
      let effective = tool.enabled;
      if (override) {
        overrideMode = override.enabled ? "force_on" : "force_off";
        effective = override.enabled;
      }
      return { tool, overrideMode, effective };
    });
  }, [runtimeToolsQuery.data, overridesQuery.data]);

  const filteredRows = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return rows.filter((row) => {
      if (filter === "override" && row.overrideMode === "inherit") return false;
      if (filter === "effective_on" && !row.effective) return false;
      if (!needle) return true;
      return (
        row.tool.name.toLowerCase().includes(needle) ||
        row.tool.description.toLowerCase().includes(needle) ||
        (row.tool.mcp_server_name ?? "").toLowerCase().includes(needle)
      );
    });
  }, [rows, search, filter]);

  const counts = useMemo(() => {
    let override = 0;
    let effective_on = 0;
    for (const row of rows) {
      if (row.overrideMode !== "inherit") override += 1;
      if (row.effective) effective_on += 1;
    }
    return { all: rows.length, override, effective_on };
  }, [rows]);

  if (!runtimeId) {
    return (
      <div className="rounded-md border bg-card p-6 text-center text-sm text-muted-foreground">
        Agenten har ikke en runtime tilknyttet endnu — tildel én før du
        konfigurerer override.
      </div>
    );
  }

  return (
    <div className="rounded-md border bg-card">
      <div className="border-b p-4">
        <h3 className="text-sm font-medium">Tools på agenten</h3>
        <p className="mt-0.5 text-xs text-muted-foreground">
          Effektiv liste over hvad <span className="font-medium">{agent.name}</span>{" "}
          kan kalde — arvet fra runtime'en, med agent-specifikke override.
        </p>
      </div>

      <InheritBanner runtimeName={runtimeName ?? ""} />

      <div className="flex flex-wrap items-center gap-2 border-b p-3">
        <div className="relative min-w-[200px] max-w-xs flex-1">
          <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Søg tools…"
            className="h-8 w-full rounded-md border bg-background pl-7 pr-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </div>
        <FilterChips value={filter} onChange={setFilter} counts={counts} />
      </div>

      <AgentToolsBody
        runtimeToolsQuery={runtimeToolsQuery}
        rows={rows}
        filteredRows={filteredRows}
        canEdit={canEdit}
        onSetOverride={(toolName, enabled) =>
          setOverrideMutation.mutate({ toolName, enabled })
        }
        onClearOverride={(toolName) =>
          clearOverrideMutation.mutate({ toolName })
        }
        mutating={setOverrideMutation.isPending || clearOverrideMutation.isPending}
      />

      <Legend />
    </div>
  );
}

function InheritBanner({ runtimeName }: { runtimeName: string }) {
  return (
    <div className="flex items-start gap-2 border-b bg-muted/30 px-4 py-3 text-xs">
      <Info className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
      <div className="space-y-1">
        <p className="font-medium text-foreground">
          Agenten arver tools fra
          {runtimeName ? (
            <>
              {" "}
              <code className="font-mono text-[11px]">{runtimeName}</code>
            </>
          ) : (
            " sin runtime"
          )}
          .
        </p>
        <p className="text-muted-foreground">
          Override bruges kun til at afvige fra runtime'ens default — slå et tool
          fra for denne agent, eller åbn et tool runtime'en har lukket. Lad være
          med at sætte alt op her; gør det på runtime-niveau.
        </p>
      </div>
    </div>
  );
}

function FilterChips({
  value,
  onChange,
  counts,
}: {
  value: RowFilter;
  onChange: (v: RowFilter) => void;
  counts: { all: number; override: number; effective_on: number };
}) {
  const options: { value: RowFilter; label: string; count: number }[] = [
    { value: "all", label: "Alle", count: counts.all },
    { value: "override", label: "Kun override", count: counts.override },
    { value: "effective_on", label: "Effektivt aktive", count: counts.effective_on },
  ];
  return (
    <div role="radiogroup" className="inline-flex items-center rounded-md border p-0.5">
      {options.map((opt) => {
        const active = value === opt.value;
        return (
          <button
            key={opt.value}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(opt.value)}
            className={cn(
              "rounded-sm px-2 py-0.5 text-[11px] font-medium transition-colors",
              active
                ? "bg-muted text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {opt.label} ({opt.count})
          </button>
        );
      })}
    </div>
  );
}

interface AgentToolsBodyProps {
  runtimeToolsQuery: ReturnType<typeof useQuery<RuntimeTool[], Error>>;
  rows: ToolRowData[];
  filteredRows: ToolRowData[];
  canEdit: boolean;
  onSetOverride: (toolName: string, enabled: boolean) => void;
  onClearOverride: (toolName: string) => void;
  mutating: boolean;
}

function AgentToolsBody({
  runtimeToolsQuery,
  rows,
  filteredRows,
  canEdit,
  onSetOverride,
  onClearOverride,
  mutating,
}: AgentToolsBodyProps) {
  if (runtimeToolsQuery.isLoading) {
    return (
      <div className="space-y-2 p-4">
        {[0, 1, 2].map((i) => (
          <div
            key={i}
            className="flex items-center gap-3 rounded-md border p-3 opacity-60"
          >
            <div className="h-4 w-4 animate-pulse rounded bg-muted" />
            <div className="flex-1 space-y-1.5">
              <div className="h-3.5 w-32 animate-pulse rounded bg-muted" />
              <div className="h-3 w-56 animate-pulse rounded bg-muted/70" />
            </div>
            <Loader2 className="h-4 w-4 animate-spin text-muted-foreground/60" />
          </div>
        ))}
      </div>
    );
  }

  if (runtimeToolsQuery.isError) {
    return (
      <div className="flex flex-col items-center justify-center gap-1 p-10 text-center">
        <AlertCircle className="h-7 w-7 text-muted-foreground/50" />
        <p className="mt-2 text-sm text-muted-foreground">
          Kunne ikke hente tools for runtime'en.
        </p>
        <Button
          variant="outline"
          size="sm"
          onClick={() => runtimeToolsQuery.refetch()}
          className="mt-2"
        >
          Prøv igen
        </Button>
      </div>
    );
  }

  if (filteredRows.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-1 p-10 text-center">
        <Wrench className="h-7 w-7 text-muted-foreground/40" />
        <p className="mt-2 text-sm text-muted-foreground">
          {rows.length > 0
            ? "Ingen tools matcher dine filtre."
            : "Runtime'en har ingen tools endnu — daemon scanner ved næste heartbeat."}
        </p>
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="bg-muted/30 text-xs uppercase tracking-wide text-muted-foreground">
          <tr>
            <th className="px-4 py-2.5 text-left font-medium">Tool</th>
            <th className="px-4 py-2.5 text-left font-medium">Kilde</th>
            <th className="px-4 py-2.5 text-left font-medium">Runtime-default</th>
            <th className="px-4 py-2.5 text-left font-medium">Override</th>
            <th className="px-4 py-2.5 text-left font-medium">Effektiv</th>
            <th className="w-8 px-2 py-2.5" />
          </tr>
        </thead>
        <tbody>
          {filteredRows.map((row) => (
            <AgentToolRow
              key={row.tool.name}
              row={row}
              canEdit={canEdit}
              mutating={mutating}
              onSetOverride={onSetOverride}
              onClearOverride={onClearOverride}
            />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function AgentToolRow({
  row,
  canEdit,
  mutating,
  onSetOverride,
  onClearOverride,
}: {
  row: ToolRowData;
  canEdit: boolean;
  mutating: boolean;
  onSetOverride: (toolName: string, enabled: boolean) => void;
  onClearOverride: (toolName: string) => void;
}) {
  const { tool, overrideMode, effective } = row;
  const isForceOn = overrideMode === "force_on";
  const isForceOff = overrideMode === "force_off";
  const isOverride = isForceOn || isForceOff;
  return (
    <tr
      className={cn(
        "border-t border-l-2 border-l-transparent",
        isForceOn && "border-l-emerald-500 bg-emerald-500/[0.04]",
        isForceOff && "border-l-rose-500 bg-rose-500/[0.04]",
      )}
    >
      <td className="px-4 py-3 align-top">
        <div className="font-medium">{tool.name}</div>
        {tool.description && (
          <div className="mt-0.5 max-w-md text-xs text-muted-foreground">
            {tool.description}
          </div>
        )}
      </td>
      <td className="px-4 py-3 align-top">
        <SourceBadge source={tool.source} mcpServerName={tool.mcp_server_name} />
      </td>
      <td className="px-4 py-3 align-top">
        <RuntimeDefaultBadge enabled={tool.enabled} />
      </td>
      <td className="px-4 py-3 align-top">
        <OverrideTag mode={overrideMode} />
      </td>
      <td className="px-4 py-3 align-top">
        <EffectiveState effective={effective} />
      </td>
      <td className="px-2 py-3 align-top">
        {canEdit && (
          <OverrideSelect
            mode={overrideMode}
            disabled={mutating}
            onChange={(next) => {
              if (next === "inherit") onClearOverride(tool.name);
              else onSetOverride(tool.name, next === "force_on");
            }}
            highlight={isOverride}
          />
        )}
      </td>
    </tr>
  );
}

function SourceBadge({
  source,
  mcpServerName,
}: {
  source: string;
  mcpServerName: string;
}) {
  switch (source) {
    case "cloud":
      return (
        <Badge variant="secondary" className="gap-1">
          <Cloud className="h-3 w-3" /> Cloud
        </Badge>
      );
    case "mcp":
      return (
        <Badge variant="outline" className="gap-1 font-mono text-[11px]">
          MCP · {mcpServerName || "ukendt server"}
        </Badge>
      );
    default:
      return <Badge variant="outline">{source || "ukendt"}</Badge>;
  }
}

function RuntimeDefaultBadge({ enabled }: { enabled: boolean }) {
  return enabled ? (
    <Badge variant="secondary" className="gap-1">
      <Check className="h-3 w-3" /> På
    </Badge>
  ) : (
    <Badge variant="outline" className="gap-1 text-muted-foreground">
      <Circle className="h-3 w-3" /> Fra
    </Badge>
  );
}

function OverrideTag({ mode }: { mode: OverrideMode }) {
  if (mode === "force_on") {
    return (
      <span className="inline-flex items-center gap-1 rounded-md border border-emerald-500/40 bg-emerald-500/10 px-1.5 py-0.5 text-[11px] font-medium text-emerald-700 dark:text-emerald-300">
        <Check className="h-3 w-3" /> Tving på
      </span>
    );
  }
  if (mode === "force_off") {
    return (
      <span className="inline-flex items-center gap-1 rounded-md border border-rose-500/40 bg-rose-500/10 px-1.5 py-0.5 text-[11px] font-medium text-rose-700 dark:text-rose-300">
        <X className="h-3 w-3" /> Tving fra
      </span>
    );
  }
  return (
    <span className="text-xs italic text-muted-foreground">— Arver</span>
  );
}

function EffectiveState({ effective }: { effective: boolean }) {
  return effective ? (
    <span className="inline-flex items-center gap-1 text-xs font-medium text-emerald-700 dark:text-emerald-300">
      <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" /> Aktiv
    </span>
  ) : (
    <span className="inline-flex items-center gap-1 text-xs font-medium text-muted-foreground">
      <span className="h-1.5 w-1.5 rounded-full bg-muted-foreground/50" /> Inaktiv
    </span>
  );
}

// One-click override picker. A native <select> beats a two-click menu here:
// only three choices, all named, and the row already shows the current state
// in dedicated "Override" and "Effektiv" columns. The accent border on the
// override rows keeps exceptions visible without needing a menu animation.
function OverrideSelect({
  mode,
  disabled,
  onChange,
  highlight,
}: {
  mode: OverrideMode;
  disabled: boolean;
  onChange: (next: OverrideMode) => void;
  highlight: boolean;
}) {
  return (
    <select
      aria-label="Skift override"
      value={mode}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value as OverrideMode)}
      className={cn(
        "h-7 rounded-md border bg-background px-1.5 text-xs text-muted-foreground hover:text-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50",
        highlight && "text-foreground",
      )}
    >
      <option value="inherit">Arv runtime</option>
      <option value="force_on">Tving på</option>
      <option value="force_off">Tving fra</option>
    </select>
  );
}

function Legend() {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t bg-muted/20 px-4 py-2 text-[11px] text-muted-foreground">
      <span className="inline-flex items-center gap-1">
        <span className="inline-block h-3 w-1 rounded-sm bg-emerald-500" />
        Override: tvunget på
      </span>
      <span className="inline-flex items-center gap-1">
        <span className="inline-block h-3 w-1 rounded-sm bg-rose-500" />
        Override: tvunget fra
      </span>
      <span>
        <span className="italic">Arver</span> = følger runtime-default
      </span>
    </div>
  );
}
