"use client";

import { useMemo, useState } from "react";
import { AlertCircle, Check, Circle, Cloud, Info, Loader2, Search, Wrench } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { Agent } from "@multica/core/types";
import type { RuntimeTool, RuntimeToolEffectiveAccess } from "@multica/cerebro-types";
import { FirtalRegistryRowConfigure } from "@multica/cerebro-tool-policy/views";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";

interface AgentToolsCardProps {
  agent: Agent;
  canEdit: boolean;
  runtimeName?: string;
}

type RowFilter = "all" | "effective_on";

interface ToolRowData {
  tool: RuntimeTool;
  effective: boolean;
  effectiveReason: string;
}

const runtimeToolsKey = (runtimeId: string) => ["cerebro", "runtime-tools", runtimeId] as const;
const effectiveAccessKey = (runtimeId: string, agentId: string) =>
  ["cerebro", "runtime-tool-effective-access", runtimeId, "agent", agentId] as const;

export function AgentToolsCard({ agent, canEdit, runtimeName }: AgentToolsCardProps) {
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<RowFilter>("all");
  const runtimeId = agent.runtime_id;
  const runtimeToolsQuery = useQuery({
    queryKey: runtimeToolsKey(runtimeId ?? ""),
    queryFn: () => api.listRuntimeTools(runtimeId!),
    enabled: !!runtimeId,
  });
  const effectiveAccessQuery = useQuery({
    queryKey: effectiveAccessKey(runtimeId ?? "", agent.id),
    queryFn: () => api.listRuntimeToolEffectiveAccess(runtimeId!, { agent_id: agent.id }),
    enabled: !!runtimeId,
  });

  const rows = useMemo<ToolRowData[]>(() => {
    const accessByName = new Map<string, RuntimeToolEffectiveAccess>();
    for (const row of effectiveAccessQuery.data ?? []) {
      accessByName.set(row.inventory.tool_name || row.descriptor.tool_key, row);
    }
    return (runtimeToolsQuery.data ?? []).map((tool) => {
      const access = accessByName.get(tool.name);
      return {
        tool,
        effective: access?.exposure_effective.effective ?? tool.enabled,
        effectiveReason:
          access?.exposure_effective.reason ??
          (tool.enabled ? "Runtime default is active" : "Runtime default is disabled"),
      };
    });
  }, [runtimeToolsQuery.data, effectiveAccessQuery.data]);

  const filteredRows = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return rows.filter((row) => {
      if (filter === "effective_on" && !row.effective) return false;
      return !needle || row.tool.name.toLowerCase().includes(needle) ||
        row.tool.description.toLowerCase().includes(needle) ||
        (row.tool.mcp_server_name ?? "").toLowerCase().includes(needle);
    });
  }, [rows, search, filter]);

  if (!runtimeId) {
    return <div className="rounded-md border bg-card p-6 text-center text-sm text-muted-foreground">The agent has no runtime assigned yet.</div>;
  }

  const activeCount = rows.filter((row) => row.effective).length;
  return (
    <div className="rounded-md border bg-card">
      <div className="border-b p-4">
        <h3 className="text-sm font-medium">Agent tools</h3>
        <p className="mt-0.5 text-xs text-muted-foreground">Effective list of what <span className="font-medium">{agent.name}</span> can call.</p>
      </div>
      <div className="flex items-start gap-2 border-b bg-muted/30 px-4 py-3 text-xs">
        <Info className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
        <div>
          <p className="font-medium">Policy decisions for {runtimeName || "this runtime"}</p>
          <p className="mt-1 text-muted-foreground">Access is managed centrally in Settings → Permissions. This page shows the resulting decision.</p>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2 border-b p-3">
        <div className="relative min-w-[200px] max-w-xs flex-1">
          <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <input type="search" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search tools…" className="h-8 w-full rounded-md border bg-background pl-7 pr-2 text-xs" />
        </div>
        <div role="radiogroup" className="inline-flex items-center rounded-md border p-0.5">
          {([{ value: "all", label: "All", count: rows.length }, { value: "effective_on", label: "Effectively active", count: activeCount }] as const).map((option) => (
            <button key={option.value} type="button" role="radio" aria-checked={filter === option.value} onClick={() => setFilter(option.value)} className={cn("rounded-sm px-2 py-0.5 text-[11px] font-medium", filter === option.value ? "bg-muted text-foreground" : "text-muted-foreground")}>{option.label} ({option.count})</button>
          ))}
        </div>
      </div>
      <AgentToolsBody query={runtimeToolsQuery} rows={rows} filteredRows={filteredRows} canEdit={canEdit} agentId={agent.id} />
    </div>
  );
}

function AgentToolsBody({ query, rows, filteredRows, canEdit, agentId }: { query: ReturnType<typeof useQuery<RuntimeTool[], Error>>; rows: ToolRowData[]; filteredRows: ToolRowData[]; canEdit: boolean; agentId: string }) {
  if (query.isLoading) return <div className="flex justify-center p-10"><Loader2 className="h-5 w-5 animate-spin text-muted-foreground" /></div>;
  if (query.isError) return <div className="flex flex-col items-center gap-2 p-10"><AlertCircle className="h-7 w-7 text-muted-foreground/50" /><p className="text-sm text-muted-foreground">Failed to fetch tools for the runtime.</p><Button variant="outline" size="sm" onClick={() => query.refetch()}>Retry</Button></div>;
  if (filteredRows.length === 0) return <div className="flex flex-col items-center p-10"><Wrench className="h-7 w-7 text-muted-foreground/40" /><p className="mt-2 text-sm text-muted-foreground">{rows.length ? "No tools match your filters." : "The runtime has no tools yet — daemon will scan on next heartbeat."}</p></div>;
  return <div className="overflow-x-auto"><table className="w-full text-sm"><thead className="bg-muted/30 text-xs uppercase tracking-wide text-muted-foreground"><tr><th className="px-4 py-2.5 text-left font-medium">Tool</th><th className="px-4 py-2.5 text-left font-medium">Source</th><th className="px-4 py-2.5 text-left font-medium">Runtime inventory</th><th className="px-4 py-2.5 text-left font-medium">Effective</th><th className="w-8 px-2 py-2.5" /></tr></thead><tbody>{filteredRows.map((row) => <AgentToolRow key={row.tool.name} row={row} canEdit={canEdit} agentId={agentId} />)}</tbody></table></div>;
}

function AgentToolRow({ row, canEdit, agentId }: { row: ToolRowData; canEdit: boolean; agentId: string }) {
  const { tool, effective, effectiveReason } = row;
  return <tr className="border-t"><td className="px-4 py-3 align-top"><div className="font-medium">{tool.name}</div>{tool.description && <div className="mt-0.5 max-w-md text-xs text-muted-foreground">{tool.description}</div>}</td><td className="px-4 py-3 align-top"><SourceBadge source={tool.source} mcpServerName={tool.mcp_server_name} /></td><td className="px-4 py-3 align-top">{tool.enabled ? <Badge variant="secondary" className="gap-1"><Check className="h-3 w-3" /> Available</Badge> : <Badge variant="outline" className="gap-1 text-muted-foreground"><Circle className="h-3 w-3" /> Unavailable</Badge>}</td><td className="px-4 py-3 align-top"><div className="max-w-[260px] space-y-1"><span className={cn("inline-flex items-center gap-1 text-xs font-medium", effective ? "text-emerald-700 dark:text-emerald-300" : "text-muted-foreground")}><span className={cn("h-1.5 w-1.5 rounded-full", effective ? "bg-emerald-500" : "bg-muted-foreground/50")} />{effective ? "Active" : "Inactive"}</span>{effectiveReason && <div className="text-[11px] leading-snug text-muted-foreground">{effectiveReason}</div>}</div></td><td className="px-2 py-3 align-top">{canEdit && <FirtalRegistryRowConfigure toolKey={tool.name} agentId={agentId} variant="outline" />}</td></tr>;
}

function SourceBadge({ source, mcpServerName }: { source: string; mcpServerName: string }) {
  if (source === "cloud") return <Badge variant="secondary" className="gap-1"><Cloud className="h-3 w-3" /> Cloud</Badge>;
  if (source === "mcp") return <Badge variant="outline" className="gap-1 font-mono text-[11px]">MCP · {mcpServerName || "unknown server"}</Badge>;
  return <Badge variant="outline">{source || "unknown"}</Badge>;
}
