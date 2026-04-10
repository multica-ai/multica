"use client";

import { useState, useEffect, useMemo } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { Bot, Plus, Archive } from "lucide-react";
import type { CreateAgentRequest, UpdateAgentRequest } from "@multica/core/types";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { CreateAgentDialog } from "./create-agent-dialog";
import { AgentListItem } from "./agent-list-item";
import { AgentDetail } from "./agent-detail";

export function AgentsPage() {
  const isLoading = useAuthStore((s) => s.isLoading);
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const [selectedId, setSelectedId] = useState<string>("");
  const [showArchived, setShowArchived] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(runtimeListOptions(wsId));
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_agents_layout",
  });

  const filteredAgents = useMemo(
    () => showArchived ? agents.filter((a) => !!a.archived_at) : agents.filter((a) => !a.archived_at),
    [agents, showArchived],
  );

  const archivedCount = useMemo(() => agents.filter((a) => !!a.archived_at).length, [agents]);

  // Select first agent on initial load or when filter changes
  useEffect(() => {
    if (filteredAgents.length > 0 && !filteredAgents.some((a) => a.id === selectedId)) {
      setSelectedId(filteredAgents[0]!.id);
    }
  }, [filteredAgents, selectedId]);

  const handleCreate = async (data: CreateAgentRequest) => {
    const agent = await api.createAgent(data);
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    setSelectedId(agent.id);
  };

  const handleUpdate = async (id: string, data: Record<string, unknown>) => {
    try {
      await api.updateAgent(id, data as UpdateAgentRequest);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success("Agent updated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update agent");
      throw e;
    }
  };

  const handleArchive = async (id: string) => {
    try {
      await api.archiveAgent(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success("Agent archived");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to archive agent");
    }
  };

  const handleRestore = async (id: string) => {
    try {
      await api.restoreAgent(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success("Agent restored");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to restore agent");
    }
  };

  const selected = agents.find((a) => a.id === selectedId) ?? null;

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
          <div className="flex h-12 items-center justify-between border-b px-4">
            <h1 className="text-sm font-semibold">Agents</h1>
            <div className="flex items-center gap-1">
              {archivedCount > 0 && (
                <Button
                  variant={showArchived ? "secondary" : "ghost"}
                  size="icon-xs"
                  onClick={() => setShowArchived(!showArchived)}
                  title={showArchived ? "Show active agents" : "Show archived agents"}
                >
                  <Archive className="h-4 w-4 text-muted-foreground" />
                </Button>
              )}
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={() => setShowCreate(true)}
              >
                <Plus className="h-4 w-4 text-muted-foreground" />
              </Button>
            </div>
          </div>
          {filteredAgents.length === 0 ? (
            <div className="flex flex-col items-center justify-center px-4 py-12">
              <Bot className="h-8 w-8 text-muted-foreground/40" />
              <p className="mt-3 text-sm text-muted-foreground">
                {showArchived ? "No archived agents" : archivedCount > 0 ? "No active agents" : "No agents yet"}
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
                />
              ))}
            </div>
          )}
        </div>
      </ResizablePanel>

      <ResizableHandle />

      <ResizablePanel id="detail" minSize="50%">
        {/* Right column — agent detail */}
        {selected ? (
          <AgentDetail
            key={selected.id}
            agent={selected}
            runtimes={runtimes}
            onUpdate={handleUpdate}
            onArchive={handleArchive}
            onRestore={handleRestore}
          />
        ) : (
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
        )}
      </ResizablePanel>

      {showCreate && (
        <CreateAgentDialog
          runtimes={runtimes}
          runtimesLoading={runtimesLoading}
          onClose={() => setShowCreate(false)}
          onCreate={handleCreate}
        />
      )}
    </ResizablePanelGroup>
  );
}
