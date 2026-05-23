// Unified runtime-tools inventory + access-control admin (JEH-1710, Skærm A).
//
// Replaces the rå JSON paste UX. Renders every tool a runtime serves
// (cloud + MCP blended), lets workspace owners/admins toggle each tool, and
// granulates access via group + user grants. Empty states are explicit —
// blanks would suggest "no data yet" when the real meaning is "no specific
// access granted".

"use client";

import { useMemo, useState } from "react";
import {
  AlertCircle,
  Loader2,
  RefreshCw,
  Search,
  ChevronDown,
  X,
  Users,
  User,
  Cloud,
  Wrench,
} from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { AgentRuntime, MemberWithUser } from "@multica/core/types";
import type {
  RuntimeTool,
  RuntimeToolGrants,
} from "@multica/cerebro-types";
import { useWorkspaceId } from "@multica/core/hooks";
import { groupListOptions } from "@multica/cerebro-groups";
import type { CerebroGroup } from "@multica/cerebro-groups";
import { Switch } from "@multica/ui/components/ui/switch";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";

interface RuntimeToolsCardProps {
  runtime: AgentRuntime;
  workspaceId: string;
  /** Workspace role of the viewer. Edit is restricted to owner/admin. */
  canEdit: boolean;
}

type SourceFilter = "all" | "cloud" | "mcp";

const runtimeToolsKey = (runtimeId: string) =>
  ["cerebro", "runtime-tools", runtimeId] as const;

const runtimeToolGrantsKey = (runtimeId: string) =>
  ["cerebro", "runtime-tool-grants", runtimeId] as const;

const workspaceMembersKey = (wsId: string) =>
  ["workspace", "members", wsId] as const;

export function RuntimeToolsCard({
  runtime,
  workspaceId,
  canEdit,
}: RuntimeToolsCardProps) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId() || workspaceId;
  const [search, setSearch] = useState("");
  const [sourceFilter, setSourceFilter] = useState<SourceFilter>("all");

  const toolsQuery = useQuery({
    queryKey: runtimeToolsKey(runtime.id),
    queryFn: () => api.listRuntimeTools(runtime.id),
  });

  const grantsQuery = useQuery({
    queryKey: runtimeToolGrantsKey(runtime.id),
    queryFn: () => api.listRuntimeToolGrants(runtime.id),
  });

  const groupsQuery = useQuery(groupListOptions(wsId));

  const membersQuery = useQuery({
    queryKey: workspaceMembersKey(wsId),
    queryFn: () => api.listMembers(wsId),
    enabled: !!wsId,
  });

  const tools = toolsQuery.data ?? [];
  const grants = grantsQuery.data ?? { group_grants: [], user_grants: [] };
  const groups = groupsQuery.data ?? [];
  const members = membersQuery.data ?? [];

  const filteredTools = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return tools.filter((tool) => {
      if (sourceFilter === "cloud" && tool.source !== "cloud") return false;
      if (sourceFilter === "mcp" && tool.source !== "mcp") return false;
      if (!needle) return true;
      return (
        tool.name.toLowerCase().includes(needle) ||
        tool.description.toLowerCase().includes(needle) ||
        (tool.mcp_server_name ?? "").toLowerCase().includes(needle)
      );
    });
  }, [tools, search, sourceFilter]);

  const counts = useMemo(() => {
    let cloud = 0;
    let mcp = 0;
    for (const tool of tools) {
      if (tool.source === "cloud") cloud += 1;
      else if (tool.source === "mcp") mcp += 1;
    }
    return { all: tools.length, cloud, mcp };
  }, [tools]);

  const lastScannedAt = useMemo(() => {
    let newest: string | null = null;
    for (const tool of tools) {
      const ts = tool.last_scanned_at;
      if (!ts) continue;
      if (!newest || ts > newest) newest = ts;
    }
    return newest;
  }, [tools]);

  const grantsByTool = useMemo(() => {
    const map = new Map<
      string,
      { groups: typeof grants.group_grants; users: typeof grants.user_grants }
    >();
    for (const g of grants.group_grants) {
      const entry = map.get(g.tool_name) ?? { groups: [], users: [] };
      entry.groups = [...entry.groups, g];
      map.set(g.tool_name, entry);
    }
    for (const u of grants.user_grants) {
      const entry = map.get(u.tool_name) ?? { groups: [], users: [] };
      entry.users = [...entry.users, u];
      map.set(u.tool_name, entry);
    }
    return map;
  }, [grants]);

  const toggleMutation = useMutation({
    mutationFn: ({ toolName, enabled }: { toolName: string; enabled: boolean }) =>
      api.setRuntimeToolEnabled(runtime.id, toolName, enabled),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeToolsKey(runtime.id) });
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Kunne ikke ændre tool-status");
    },
  });

  const groupGrantMutation = useMutation({
    mutationFn: ({
      toolName,
      groupId,
      grant,
    }: {
      toolName: string;
      groupId: string;
      grant: boolean;
    }) =>
      grant
        ? api.addRuntimeToolGroupGrant(runtime.id, toolName, groupId)
        : api.removeRuntimeToolGroupGrant(runtime.id, toolName, groupId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeToolGrantsKey(runtime.id) });
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Kunne ikke opdatere gruppeadgang");
    },
  });

  const userGrantMutation = useMutation({
    mutationFn: ({
      toolName,
      userId,
      grant,
    }: {
      toolName: string;
      userId: string;
      grant: boolean;
    }) =>
      grant
        ? api.addRuntimeToolUserGrant(runtime.id, toolName, userId)
        : api.removeRuntimeToolUserGrant(runtime.id, toolName, userId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeToolGrantsKey(runtime.id) });
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Kunne ikke opdatere brugeradgang");
    },
  });

  function handleScanClick() {
    // Daemon scans on its heartbeat (~15 min debounce). An admin-triggered
    // immediate-scan endpoint is a separate slice; until then, we tell the
    // admin the system will pick the scan up automatically.
    toast.message("Scan kø'et", {
      description: "Daemon scanner runtime'en ved næste heartbeat (cirka 15 min).",
    });
  }

  return (
    <div className="rounded-md border bg-card">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b p-4">
        <div>
          <h3 className="text-sm font-medium">Tools på runtime</h3>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Alle tools på <code className="font-mono text-[11px]">{runtime.name}</code>{" "}
            — cloud og MCP blandet. Per række: aktivér og styr adgang.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <LastScannedLabel scannedAt={lastScannedAt} />
          {canEdit && (
            <Button
              size="sm"
              variant="outline"
              onClick={handleScanClick}
              className="gap-1.5"
            >
              <RefreshCw className="h-3.5 w-3.5" />
              Scan runtime
            </Button>
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2 border-b p-3">
        <div className="relative min-w-[200px] flex-1 max-w-xs">
          <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Søg tools, kilder…"
            className="h-8 w-full rounded-md border bg-background pl-7 pr-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </div>
        <FilterChips value={sourceFilter} onChange={setSourceFilter} counts={counts} />
      </div>

      <RuntimeToolsBody
        toolsQuery={toolsQuery}
        filteredTools={filteredTools}
        grantsByTool={grantsByTool}
        groups={groups}
        members={members}
        canEdit={canEdit}
        onToggle={(toolName, enabled) =>
          toggleMutation.mutate({ toolName, enabled })
        }
        onGroupGrant={(toolName, groupId, grant) =>
          groupGrantMutation.mutate({ toolName, groupId, grant })
        }
        onUserGrant={(toolName, userId, grant) =>
          userGrantMutation.mutate({ toolName, userId, grant })
        }
        togglePending={toggleMutation.isPending}
      />
    </div>
  );
}

function LastScannedLabel({ scannedAt }: { scannedAt: string | null }) {
  if (!scannedAt) {
    return (
      <span className="text-[11px] text-muted-foreground">
        Aldrig scannet
      </span>
    );
  }
  return (
    <span className="text-[11px] text-muted-foreground">
      Scannet {formatRelativeTime(scannedAt)}
    </span>
  );
}

function FilterChips({
  value,
  onChange,
  counts,
}: {
  value: SourceFilter;
  onChange: (v: SourceFilter) => void;
  counts: { all: number; cloud: number; mcp: number };
}) {
  const options: { value: SourceFilter; label: string; count: number }[] = [
    { value: "all", label: "Alle", count: counts.all },
    { value: "cloud", label: "Cloud", count: counts.cloud },
    { value: "mcp", label: "MCP", count: counts.mcp },
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

interface RuntimeToolsBodyProps {
  toolsQuery: ReturnType<typeof useQuery<RuntimeTool[], Error>>;
  filteredTools: RuntimeTool[];
  grantsByTool: Map<
    string,
    {
      groups: RuntimeToolGrants["group_grants"];
      users: RuntimeToolGrants["user_grants"];
    }
  >;
  groups: CerebroGroup[];
  members: MemberWithUser[];
  canEdit: boolean;
  onToggle: (toolName: string, enabled: boolean) => void;
  onGroupGrant: (toolName: string, groupId: string, grant: boolean) => void;
  onUserGrant: (toolName: string, userId: string, grant: boolean) => void;
  togglePending: boolean;
}

function RuntimeToolsBody(props: RuntimeToolsBodyProps) {
  const { toolsQuery, filteredTools } = props;

  if (toolsQuery.isLoading) {
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

  if (toolsQuery.isError) {
    return (
      <div className="flex flex-col items-center justify-center gap-1 p-10 text-center">
        <AlertCircle className="h-7 w-7 text-muted-foreground/50" />
        <p className="mt-2 text-sm text-muted-foreground">
          Kunne ikke hente tools for denne runtime.
        </p>
        <Button
          variant="outline"
          size="sm"
          onClick={() => toolsQuery.refetch()}
          className="mt-2"
        >
          Prøv igen
        </Button>
      </div>
    );
  }

  if (filteredTools.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-1 p-10 text-center">
        <Wrench className="h-7 w-7 text-muted-foreground/40" />
        <p className="mt-2 text-sm text-muted-foreground">
          {toolsQuery.data && toolsQuery.data.length > 0
            ? "Ingen tools matcher dine filtre."
            : "Ingen tools registreret endnu — daemon scanner ved næste heartbeat."}
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
            <th className="px-4 py-2.5 text-left font-medium">Aktiv</th>
            <th className="px-4 py-2.5 text-left font-medium">Grupper</th>
            <th className="px-4 py-2.5 text-left font-medium">Brugere</th>
          </tr>
        </thead>
        <tbody>
          {filteredTools.map((tool) => (
            <RuntimeToolRow key={tool.name} tool={tool} {...props} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

interface RuntimeToolRowProps extends RuntimeToolsBodyProps {
  tool: RuntimeTool;
}

function RuntimeToolRow({
  tool,
  grantsByTool,
  groups,
  members,
  canEdit,
  onToggle,
  onGroupGrant,
  onUserGrant,
  togglePending,
}: RuntimeToolRowProps) {
  const grants = grantsByTool.get(tool.name) ?? { groups: [], users: [] };
  const selectedGroupIds = new Set(grants.groups.map((g) => g.group_id));
  const selectedUserIds = new Set(grants.users.map((u) => u.user_id));

  return (
    <tr className="border-t">
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
        <Switch
          checked={tool.enabled}
          onCheckedChange={(v: boolean) => onToggle(tool.name, v)}
          disabled={!canEdit || togglePending}
          aria-label={`Slå ${tool.name} ${tool.enabled ? "fra" : "til"}`}
        />
      </td>
      <td className="px-4 py-3 align-top">
        <GroupPicker
          allGroups={groups}
          selectedGroupIds={selectedGroupIds}
          grants={grants.groups}
          canEdit={canEdit}
          onToggle={(groupId, grant) => onGroupGrant(tool.name, groupId, grant)}
        />
      </td>
      <td className="px-4 py-3 align-top">
        <UserPicker
          allMembers={members}
          selectedUserIds={selectedUserIds}
          grants={grants.users}
          canEdit={canEdit}
          onToggle={(userId, grant) => onUserGrant(tool.name, userId, grant)}
        />
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
  // Unknown enum values fall through to the generic badge, per API Response
  // Compatibility rules: never crash on a new server-side source value.
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

function GroupPicker({
  allGroups,
  selectedGroupIds,
  grants,
  canEdit,
  onToggle,
}: {
  allGroups: CerebroGroup[];
  selectedGroupIds: Set<string>;
  grants: RuntimeToolGrants["group_grants"];
  canEdit: boolean;
  onToggle: (groupId: string, grant: boolean) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");

  const filtered = query.trim()
    ? allGroups.filter((g) =>
        g.name.toLowerCase().includes(query.trim().toLowerCase()),
      )
    : allGroups;

  return (
    <div className="flex flex-wrap items-center gap-1">
      {grants.length === 0 && (
        <span className="text-xs text-muted-foreground">— ingen specifikke</span>
      )}
      {grants.map((g) => (
        <Badge key={g.group_id} variant="secondary" className="gap-1 pr-1">
          <Users className="h-3 w-3" />
          {g.group_name}
          {canEdit && (
            <button
              type="button"
              onClick={() => onToggle(g.group_id, false)}
              className="ml-0.5 rounded-sm opacity-50 hover:opacity-100"
              aria-label={`Fjern gruppe ${g.group_name}`}
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </Badge>
      ))}
      {canEdit && (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger
            render={
              <button
                type="button"
                className="inline-flex h-6 items-center gap-1 rounded-md border border-dashed px-1.5 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                + Tilføj
                <ChevronDown className="h-3 w-3" />
              </button>
            }
          />
          <PopoverContent align="start" className="w-64 p-0">
            <div className="border-b p-2">
              <input
                type="search"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Søg grupper…"
                className="h-7 w-full rounded-md border bg-background px-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
              />
            </div>
            <div className="max-h-56 overflow-y-auto p-1">
              {filtered.length === 0 ? (
                <p className="px-2 py-3 text-center text-xs text-muted-foreground">
                  Ingen grupper matcher.
                </p>
              ) : (
                filtered.map((g) => {
                  const checked = selectedGroupIds.has(g.id);
                  return (
                    <label
                      key={g.id}
                      className="flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1 text-xs hover:bg-accent"
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={(e) => onToggle(g.id, e.target.checked)}
                        className="h-3 w-3 accent-primary"
                      />
                      <Users className="h-3 w-3 text-muted-foreground" />
                      {g.name}
                    </label>
                  );
                })
              )}
            </div>
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}

function UserPicker({
  allMembers,
  selectedUserIds,
  grants,
  canEdit,
  onToggle,
}: {
  allMembers: MemberWithUser[];
  selectedUserIds: Set<string>;
  grants: RuntimeToolGrants["user_grants"];
  canEdit: boolean;
  onToggle: (userId: string, grant: boolean) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");

  const filtered = query.trim()
    ? allMembers.filter((m) => {
        const q = query.trim().toLowerCase();
        return (
          m.name?.toLowerCase().includes(q) ||
          m.email?.toLowerCase().includes(q)
        );
      })
    : allMembers;

  return (
    <div className="flex flex-wrap items-center gap-1">
      {grants.length === 0 && (
        <span className="text-xs text-muted-foreground">— ingen specifikke</span>
      )}
      {grants.map((u) => (
        <Badge key={u.user_id} variant="secondary" className="gap-1 pr-1">
          <User className="h-3 w-3" />
          {u.user_name || u.user_email}
          {canEdit && (
            <button
              type="button"
              onClick={() => onToggle(u.user_id, false)}
              className="ml-0.5 rounded-sm opacity-50 hover:opacity-100"
              aria-label={`Fjern bruger ${u.user_name || u.user_email}`}
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </Badge>
      ))}
      {canEdit && (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger
            render={
              <button
                type="button"
                className="inline-flex h-6 items-center gap-1 rounded-md border border-dashed px-1.5 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                + Tilføj
                <ChevronDown className="h-3 w-3" />
              </button>
            }
          />
          <PopoverContent align="start" className="w-64 p-0">
            <div className="border-b p-2">
              <input
                type="search"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Søg brugere…"
                className="h-7 w-full rounded-md border bg-background px-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
              />
            </div>
            <div className="max-h-56 overflow-y-auto p-1">
              {filtered.length === 0 ? (
                <p className="px-2 py-3 text-center text-xs text-muted-foreground">
                  Ingen brugere matcher.
                </p>
              ) : (
                filtered.map((m) => {
                  const checked = selectedUserIds.has(m.user_id);
                  return (
                    <label
                      key={m.user_id}
                      className="flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1 text-xs hover:bg-accent"
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={(e) => onToggle(m.user_id, e.target.checked)}
                        className="h-3 w-3 accent-primary"
                      />
                      <User className="h-3 w-3 text-muted-foreground" />
                      <span className="truncate">{m.name || m.email}</span>
                    </label>
                  );
                })
              )}
            </div>
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}

function formatRelativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "for længe siden";
  const diffSec = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (diffSec < 60) return "for et øjeblik siden";
  if (diffSec < 3600) {
    const m = Math.round(diffSec / 60);
    return `for ${m} min. siden`;
  }
  if (diffSec < 86400) {
    const h = Math.round(diffSec / 3600);
    return `for ${h} t. siden`;
  }
  const d = Math.round(diffSec / 86400);
  return `for ${d} d. siden`;
}
