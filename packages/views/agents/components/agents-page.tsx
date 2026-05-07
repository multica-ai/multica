"use client";

import { useState, useEffect, useMemo } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { Bot, Plus, Archive, ChevronDown, Check, Sliders, Settings2 } from "lucide-react";
import type { CreateAgentRequest, UpdateAgentRequest, AgentDefaultsWithUser } from "@multica/core/types";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { toast } from "sonner";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { PageHeader } from "../../layout/page-header";
import { CreateAgentDialog } from "./create-agent-dialog";
import { AgentListItem } from "./agent-list-item";
import { AgentDetail } from "./agent-detail";
import { PersonalDefaultsDetail, SystemDefaultsDetail, OtherDefaultsDetail } from "./defaults-detail";
import { ActorAvatar } from "../../common/actor-avatar";
import type { MemberWithUser } from "@multica/core/types";

type AgentScope = "mine" | "all";

const PERSONAL_DEFAULTS_ID = "personal-defaults";
const SYSTEM_DEFAULTS_ID = "system-defaults";
const defaultsPrefix = "defaults-";

function isDefaultsId(id: string) {
  return id === PERSONAL_DEFAULTS_ID || id === SYSTEM_DEFAULTS_ID || id.startsWith(defaultsPrefix);
}

export function AgentsPage() {
  const currentUser = useAuthStore((s) => s.user);
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [scope, setScope] = useState<AgentScope>("mine");
  const [ownerFilter, setOwnerFilter] = useState<string | null>(null);
  const { data: agents = [], isLoading } = useQuery(
    agentListOptions(wsId, scope === "mine" ? "me" : undefined),
  );
  const [selectedId, setSelectedId] = useState<string>("");
  const [showArchived, setShowArchived] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_agents_layout",
  });

  // All defaults (fetched only in "all" scope)
  const { data: allDefaults = [] } = useQuery({
    queryKey: ["workspaces", wsId, "all-agent-defaults"],
    queryFn: () => api.listAllAgentDefaults(wsId),
    enabled: scope === "all",
  });

  const myMembership = members.find((m) => m.user_id === currentUser?.id) ?? null;
  const isAdminOrOwner =
    myMembership?.role === "owner" || myMembership?.role === "admin";

  const uniqueOwners = useMemo(() => {
    if (scope !== "all") return [] as MemberWithUser[];
    const ids = Array.from(
      new Set(agents.map((a) => a.owner_id).filter(Boolean) as string[]),
    );
    return ids
      .map((id) => members.find((m) => m.user_id === id))
      .filter(Boolean) as MemberWithUser[];
  }, [scope, agents, members]);

  const ownerCounts = useMemo(() => {
    const m = new Map<string, number>();
    for (const a of agents) {
      if (a.owner_id) m.set(a.owner_id, (m.get(a.owner_id) ?? 0) + 1);
    }
    return m;
  }, [agents]);

  const listAfterOwnerFilter = useMemo(() => {
    if (scope === "all" && ownerFilter) {
      return agents.filter((a) => a.owner_id === ownerFilter);
    }
    return agents;
  }, [agents, scope, ownerFilter]);

  const filteredAgents = useMemo(
    () =>
      showArchived
        ? listAfterOwnerFilter.filter((a) => !!a.archived_at)
        : listAfterOwnerFilter.filter((a) => !a.archived_at),
    [listAfterOwnerFilter, showArchived],
  );

  const archivedCount = useMemo(
    () => listAfterOwnerFilter.filter((a) => !!a.archived_at).length,
    [listAfterOwnerFilter],
  );

  const getOwnerMember = (ownerId: string | null) => {
    if (!ownerId) return null;
    return members.find((m) => m.user_id === ownerId) ?? null;
  };

  const selectedOwner = ownerFilter ? getOwnerMember(ownerFilter) : null;

  const invalidateAgentQueries = () => {
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
  };

  // Select first item on initial load or when filter changes
  useEffect(() => {
    if (isDefaultsId(selectedId)) return;
    if (filteredAgents.length > 0 && !filteredAgents.some((a) => a.id === selectedId)) {
      setSelectedId(filteredAgents[0]!.id);
    }
  }, [filteredAgents, selectedId]);

  const handleCreate = async (data: CreateAgentRequest) => {
    const agent = await api.createAgent(data);
    invalidateAgentQueries();
    setSelectedId(agent.id);
  };

  const handleUpdate = async (id: string, data: Record<string, unknown>) => {
    try {
      await api.updateAgent(id, data as UpdateAgentRequest);
      invalidateAgentQueries();
      toast.success("Agent updated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update agent");
      throw e;
    }
  };

  const handleArchive = async (id: string) => {
    try {
      await api.archiveAgent(id);
      invalidateAgentQueries();
      toast.success("Agent archived");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to archive agent");
    }
  };

  const handleRestore = async (id: string) => {
    try {
      await api.restoreAgent(id);
      invalidateAgentQueries();
      toast.success("Agent restored");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to restore agent");
    }
  };

  const handleDuplicate = async (id: string) => {
    try {
      const newAgent = await api.copyAgent(id);
      invalidateAgentQueries();
      setSelectedId(newAgent.id);
      setShowArchived(false);
      toast.success("Agent duplicated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to duplicate agent");
    }
  };

  const handleDuplicateDefaults = async (configId: string) => {
    try {
      await api.duplicateAgentDefaults(wsId, configId);
      qc.invalidateQueries({ queryKey: ["workspaces", wsId, "personal-agent-defaults"] });
      toast.success("Defaults duplicated. Environment variable keys were copied — please fill in the values.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to duplicate defaults");
    }
  };

  const selected = agents.find((a) => a.id === selectedId) ?? null;
  const selectedOtherDefaults: AgentDefaultsWithUser | null =
    selectedId.startsWith(defaultsPrefix)
      ? allDefaults.find((d) => d.id === selectedId.slice(defaultsPrefix.length)) ?? null
      : null;

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0">
        {/* List skeleton */}
        <div className="w-72 border-r">
          <div className="flex h-12 items-center justify-between border-b px-4">
            <Skeleton className="h-4 w-16" />
            <Skeleton className="h-6 w-6 rounded" />
          </div>
          <div className="divide-y">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3">
                <Skeleton className="h-8 w-8 rounded-full" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-4 w-24" />
                  <Skeleton className="h-3 w-16" />
                </div>
              </div>
            ))}
          </div>
        </div>
        {/* Detail skeleton */}
        <div className="flex-1 p-6 space-y-6">
          <div className="flex items-center gap-3">
            <Skeleton className="h-10 w-10 rounded-full" />
            <div className="space-y-1.5">
              <Skeleton className="h-5 w-32" />
              <Skeleton className="h-3 w-20" />
            </div>
          </div>
          <div className="space-y-3">
            <Skeleton className="h-8 w-full rounded-lg" />
            <Skeleton className="h-8 w-full rounded-lg" />
            <Skeleton className="h-8 w-3/4 rounded-lg" />
          </div>
        </div>
      </div>
    );
  }

  // ── Render detail panel ──────────────────────────────────────────────────
  const renderDetail = () => {
    if (selectedId === PERSONAL_DEFAULTS_ID) {
      return <PersonalDefaultsDetail key={PERSONAL_DEFAULTS_ID} />;
    }
    if (selectedId === SYSTEM_DEFAULTS_ID) {
      return <SystemDefaultsDetail key={SYSTEM_DEFAULTS_ID} />;
    }
    if (selectedOtherDefaults) {
      return (
        <OtherDefaultsDetail
          key={selectedOtherDefaults.id}
          defaults={selectedOtherDefaults}
          onDuplicate={() => handleDuplicateDefaults(selectedOtherDefaults.id!)}
        />
      );
    }
    if (selected) {
      return (
        <AgentDetail
          key={selected.id}
          agent={selected}
          runtimes={runtimes}
          members={members}
          currentUserId={currentUser?.id ?? null}
          onUpdate={handleUpdate}
          onArchive={handleArchive}
          onRestore={handleRestore}
          onDuplicate={() => handleDuplicate(selected.id)}
        />
      );
    }
    return (
      <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
        <Bot className="h-10 w-10 text-muted-foreground/30" />
        <p className="mt-3 text-sm">Select an agent to view details</p>
        <Button
          onClick={() => setShowCreate(true)}
          size="xs"
          className="mt-3"
        >
          <Plus className="h-3 w-3" />
          Create Agent
        </Button>
      </div>
    );
  };

  return (
    <ResizablePanelGroup
      orientation="horizontal"
      className="flex-1 min-h-0"
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
    >
      <ResizablePanel id="list" defaultSize={280} minSize={240} maxSize={400} groupResizeBehavior="preserve-pixel-size">
        {/* Left column — agent list */}
        <div className="overflow-y-auto h-full border-r">
          <PageHeader className="justify-between">
            <h1 className="text-sm font-semibold">Agents</h1>
            <div className="flex items-center gap-1">
              {archivedCount > 0 && (
                <Button
                  variant={showArchived ? "secondary" : "ghost"}
                  size="icon-sm"
                  onClick={() => setShowArchived(!showArchived)}
                  title={showArchived ? "Show active agents" : "Show archived agents"}
                >
                  <Archive className="text-muted-foreground" />
                </Button>
              )}
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => setShowCreate(true)}
              >
                <Plus className="text-muted-foreground" />
              </Button>
            </div>
          </PageHeader>

          <div className="flex items-center justify-between border-b px-4 py-2">
            <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
              <button
                type="button"
                onClick={() => {
                  setScope("mine");
                  setOwnerFilter(null);
                }}
                className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
                  scope === "mine"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                Mine
              </button>
              <button
                type="button"
                onClick={() => {
                  setScope("all");
                  setOwnerFilter(null);
                }}
                className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
                  scope === "all"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                All
              </button>
            </div>

            {scope === "all" && uniqueOwners.length > 1 && (
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <button
                      type="button"
                      className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                    />
                  }
                >
                  {selectedOwner ? (
                    <>
                      <ActorAvatar actorType="member" actorId={selectedOwner.user_id} size={16} />
                      <span className="max-w-20 truncate">{selectedOwner.name}</span>
                    </>
                  ) : (
                    <span>Owner</span>
                  )}
                  <ChevronDown className="h-3 w-3 opacity-50" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-48">
                  <DropdownMenuItem
                    onClick={() => setOwnerFilter(null)}
                    className="flex items-center justify-between"
                  >
                    <span className="text-xs">All owners</span>
                    {!ownerFilter && <Check className="h-3.5 w-3.5 text-foreground" />}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  {uniqueOwners.map((m) => (
                    <DropdownMenuItem
                      key={m.user_id}
                      onClick={() => setOwnerFilter(ownerFilter === m.user_id ? null : m.user_id)}
                      className="flex items-center justify-between"
                    >
                      <div className="flex min-w-0 items-center gap-2">
                        <ActorAvatar actorType="member" actorId={m.user_id} size={18} />
                        <span className="truncate text-xs">{m.name}</span>
                        <span className="text-xs text-muted-foreground">
                          {ownerCounts.get(m.user_id) ?? 0}
                        </span>
                      </div>
                      {ownerFilter === m.user_id && (
                        <Check className="h-3.5 w-3.5 shrink-0 text-foreground" />
                      )}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>

          {/* Defaults cards — shown at top of list */}
          {scope === "mine" && !showArchived && (
            <div className="border-b">
              {/* Personal Defaults */}
              <button
                onClick={() => setSelectedId(PERSONAL_DEFAULTS_ID)}
                className={`flex w-full items-center gap-3 px-4 py-3 text-left transition-colors ${
                  selectedId === PERSONAL_DEFAULTS_ID ? "bg-accent" : "hover:bg-accent/50"
                }`}
              >
                <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-blue-500/10">
                  <Sliders className="h-4 w-4 text-blue-500" />
                </div>
                <div className="min-w-0 flex-1">
                  <span className="text-sm font-medium">Personal Defaults</span>
                  <p className="text-xs text-muted-foreground">Your default agent config</p>
                </div>
              </button>
              {/* System Defaults — admin/owner only */}
              {isAdminOrOwner && (
                <button
                  onClick={() => setSelectedId(SYSTEM_DEFAULTS_ID)}
                  className={`flex w-full items-center gap-3 px-4 py-3 text-left transition-colors border-t ${
                    selectedId === SYSTEM_DEFAULTS_ID ? "bg-accent" : "hover:bg-accent/50"
                  }`}
                >
                  <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-amber-500/10">
                    <Settings2 className="h-4 w-4 text-amber-500" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <span className="text-sm font-medium">System Defaults</span>
                    <p className="text-xs text-muted-foreground">Workspace-wide defaults</p>
                  </div>
                </button>
              )}
            </div>
          )}

          {scope === "all" && !showArchived && allDefaults.length > 0 && (
            <div className="border-b">
              <div className="px-4 py-2">
                <span className="text-xs font-medium text-muted-foreground">Agent Defaults</span>
              </div>
              {allDefaults.map((d) => (
                <button
                  key={d.id}
                  onClick={() => setSelectedId(`${defaultsPrefix}${d.id}`)}
                  className={`flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors ${
                    selectedId === `${defaultsPrefix}${d.id}` ? "bg-accent" : "hover:bg-accent/50"
                  }`}
                >
                  <ActorAvatar actorType="member" actorId={d.user_id} size={24} />
                  <div className="min-w-0 flex-1">
                    <span className="text-sm font-medium truncate">{d.user_name}</span>
                    <p className="text-xs text-muted-foreground">Personal Defaults</p>
                  </div>
                </button>
              ))}
            </div>
          )}

          {/* Agent list */}
          {filteredAgents.length === 0 && !isDefaultsId(selectedId) ? (
            <div className="flex flex-col items-center justify-center px-4 py-12">
              <Bot className="h-8 w-8 text-muted-foreground/40" />
              <p className="mt-3 text-sm text-muted-foreground">
                {showArchived
                  ? "No archived agents"
                  : scope === "mine"
                    ? archivedCount > 0
                      ? "No active agents"
                      : "No agents yet"
                    : ownerFilter
                      ? "No agents for this owner"
                      : archivedCount > 0
                        ? "No active agents"
                        : "No agents yet"}
              </p>
              {!showArchived && (
                <Button
                  onClick={() => setShowCreate(true)}
                  size="xs"
                  className="mt-3"
                >
                  <Plus className="h-3 w-3" />
                  Create Agent
                </Button>
              )}
            </div>
          ) : (
            <div className="divide-y">
              {filteredAgents.map((agent) => (
                <AgentListItem
                  key={agent.id}
                  agent={agent}
                  isSelected={agent.id === selectedId}
                  onClick={() => setSelectedId(agent.id)}
                  ownerMember={scope === "all" ? getOwnerMember(agent.owner_id) : undefined}
                />
              ))}
            </div>
          )}
        </div>
      </ResizablePanel>

      <ResizableHandle />

      <ResizablePanel id="detail" minSize="50%">
        {renderDetail()}
      </ResizablePanel>

      {showCreate && (
        <CreateAgentDialog
          runtimes={runtimes}
          runtimesLoading={runtimesLoading}
          members={members}
          currentUserId={currentUser?.id ?? null}
          onClose={() => setShowCreate(false)}
          onCreate={handleCreate}
        />
      )}
    </ResizablePanelGroup>
  );
}
