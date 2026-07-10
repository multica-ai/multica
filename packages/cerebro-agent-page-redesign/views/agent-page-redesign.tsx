"use client";

import { useState } from "react";
import {
  Activity,
  AlertCircle,
  ArrowLeft,
  ListTodo,
  Lock,
  MessageSquare,
  MoreHorizontal,
  Pause,
  Plus,
  Square,
  Trash2,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
  UpdateAgentRequest,
} from "@multica/core/types";
import {
  agentTaskSnapshotKeys,
  agentTasksKeys,
  providerSupportsMcpConfig,
  useWorkspacePresenceMap,
} from "@multica/core/agents";
import { api, ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { useWorkspaceId } from "@multica/core/hooks";
import { larkInstallationsOptions } from "@multica/core/lark";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { useAgentPermissions } from "@multica/core/permissions";
import { runtimeListOptions } from "@multica/core/runtimes";
import {
  agentListOptions,
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { CerebroCapabilitiesTab } from "@multica/cerebro-agent-capabilities/views";
import { CerebroAgentContextTab } from "@multica/cerebro-agent-context/views";
import { CerebroAgentMemoryTab } from "@multica/cerebro-agent-memory/views";
import { CerebroToolsTab } from "@multica/cerebro-agent-tools/views";
import { PauseBanner, PauseRuntimeButton } from "@multica/cerebro-runtime/views";
import { Button } from "@multica/ui/components/ui/button";
import { CapabilityBanner } from "@multica/ui/components/common/capability-banner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ActorIssuesPanel } from "@multica/views/common/actor-issues-panel";
import { ActivityTab } from "@multica/views/agents/components/tabs/activity-tab";
import { CustomArgsTab } from "@multica/views/agents/components/tabs/custom-args-tab";
import { EnvTab } from "@multica/views/agents/components/tabs/env-tab";
import { InfisicalFoldersTab } from "@multica/views/agents/components/tabs/infisical-folders-tab";
import { IntegrationsTab } from "@multica/views/agents/components/tabs/integrations-tab";
import { McpConfigTab } from "@multica/views/agents/components/tabs/mcp-config-tab";
import { SandboxTab } from "@multica/views/agents/components/tabs/sandbox-tab";
import { SkillsTab } from "@multica/views/agents/components/tabs/skills-tab";
import { PageHeader } from "@multica/views/layout/page-header";
import { AppLink, useNavigation } from "@multica/views/navigation";
import { agentPageTabs, type RedesignTab } from "./agent-page-tabs";
import { IdentityRail } from "./identity-rail";

export function CerebroAgentDetailPage({ agentId }: { agentId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const qc = useQueryClient();
  const currentUser = useAuthStore((s) => s.user);
  const [tab, setTab] = useState<RedesignTab>("tasks");
  const [taskView, setTaskView] = useState<"activity" | "tasks">("tasks");
  const [activeDirty, setActiveDirty] = useState(false);
  const [pendingTab, setPendingTab] = useState<RedesignTab | null>(null);
  const [confirmArchive, setConfirmArchive] = useState(false);
  const [confirmCancelTasks, setConfirmCancelTasks] = useState(false);

  const { data: agents = [], isLoading } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: larkListing } = useQuery({
    ...larkInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);

  const setChatOpen = useChatStore((s) => s.setOpen);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const openModal = useModalStore((s) => s.open);

  const agent: Agent | null = agents.find((a) => a.id === agentId) ?? null;
  const { error: detailError } = useQuery({
    queryKey: ["agent-detail-probe", wsId, agentId],
    queryFn: () => api.getAgent(agentId),
    enabled: !isLoading && !agent && !!agentId,
    retry: false,
  });
  const isForbidden =
    detailError instanceof ApiError && detailError.status === 403;
  const perms = useAgentPermissions(agent, wsId);
  const canEdit = perms.canEdit.allowed;

  const handleUpdate = async (data: Record<string, unknown>) => {
    if (!agent) return;
    const id = agent.id;
    const queryKey = workspaceKeys.agents(wsId);
    const prevAgents = qc.getQueryData<Agent[]>(queryKey);
    const prevAgent = prevAgents?.find((a) => a.id === id);
    const prevFields: Record<string, unknown> = {};
    if (prevAgent) {
      for (const key of Object.keys(data)) {
        prevFields[key] = (prevAgent as unknown as Record<string, unknown>)[key];
      }
    }
    qc.setQueryData<Agent[]>(queryKey, (old) =>
      old?.map((a) => (a.id === id ? ({ ...a, ...data } as Agent) : a)),
    );
    try {
      await api.updateAgent(id, data as UpdateAgentRequest);
      qc.invalidateQueries({ queryKey });
      toast.success("Agent updated");
    } catch (e) {
      if (prevAgent) {
        qc.setQueryData<Agent[]>(queryKey, (old) =>
          old?.map((a) =>
            a.id === id ? ({ ...a, ...prevFields } as Agent) : a,
          ),
        );
      }
      qc.invalidateQueries({ queryKey });
      toast.error(e instanceof Error ? e.message : "Update failed");
      throw e;
    }
  };

  const handleArchive = async (id: string) => {
    try {
      await api.archiveAgent(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success("Agent archived");
      navigation.push(paths.agents());
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Archive failed");
    }
  };

  const handleRestore = async (id: string) => {
    try {
      await api.restoreAgent(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success("Agent restored");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Restore failed");
    }
  };

  const handleCancelTasks = async (id: string) => {
    try {
      const { cancelled } = await api.cancelAgentTasks(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      qc.invalidateQueries({ queryKey: agentTaskSnapshotKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: agentTasksKeys.detail(wsId, id) });
      toast.success(
        cancelled === 0
          ? "No active tasks to cancel"
          : `Cancelled ${cancelled} active ${cancelled === 1 ? "task" : "tasks"}`,
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Cancel failed");
    }
  };

  const messageAgent = () => {
    if (!agent) return;
    setActiveSession(null);
    setSelectedAgentId(agent.id);
    setChatOpen(true);
  };

  const newIssueForAgent = () => {
    if (!agent) return;
    openModal("create-issue", {
      assignee_type: "agent",
      assignee_id: agent.id,
    });
  };

  if (!agent) {
    return (
      <div className="flex h-full min-h-0 flex-col">
        <PageHeader className="px-5">
          <AppLink
            href={paths.agents()}
            className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Agents
          </AppLink>
        </PageHeader>
        <div className="flex-1 overflow-y-auto bg-muted/40 p-4 md:p-8">
          <div className="mx-auto max-w-[1400px]">
            {isLoading ? (
              <Skeleton className="h-[600px] w-full rounded-2xl" />
            ) : isForbidden ? (
              <div className="flex min-h-[420px] flex-col items-center justify-center gap-3 rounded-2xl border bg-background p-10 text-center">
                <Lock className="h-8 w-8 text-muted-foreground" />
                <div>
                  <p className="text-sm font-medium">
                    You don't have access to this private agent.
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    Ask the owner to add you, or go back to Agents.
                  </p>
                </div>
                <Button size="sm" onClick={() => navigation.push(paths.agents())}>
                  Back to Agents
                </Button>
              </div>
            ) : (
              <div className="rounded-2xl border bg-background p-10 text-center text-sm text-muted-foreground">
                Agent not found, or you don't have access to it.
              </div>
            )}
          </div>
        </div>
      </div>
    );
  }

  const runtime: AgentRuntime | null = agent.runtime_id
    ? runtimes.find((r) => r.id === agent.runtime_id) ?? null
    : null;
  const owner: MemberWithUser | undefined = agent.owner_id
    ? members.find((m) => m.user_id === agent.owner_id)
    : undefined;
  const presence = presenceMap.get(agent.id) ?? null;
  const isArchived = !!agent.archived_at;
  const runningCount = presence?.runningCount ?? 0;
  const queuedCount = presence?.queuedCount ?? 0;
  const hasActiveWork = runningCount + queuedCount > 0;
  const myRole = currentUser
    ? members.find((m) => m.user_id === currentUser.id)?.role ?? null
    : null;
  const canManageRuntime =
    !!runtime &&
    (runtime.owner_id === currentUser?.id || myRole === "owner" || myRole === "admin");
  const visibleTabs = agentPageTabs({
    mcpConfig: runtime ? providerSupportsMcpConfig(runtime.provider) : true,
    integrations: larkListing?.configured === true,
  });
  const effectiveTab = visibleTabs.some((item) => item.id === tab)
    ? tab
    : "tasks";

  const requestTabChange = (next: RedesignTab) => {
    if (next === tab) return;
    if (activeDirty) {
      setPendingTab(next);
      return;
    }
    setTab(next);
  };

  const commitTabChange = () => {
    if (!pendingTab) return;
    setTab(pendingTab);
    setActiveDirty(false);
    setPendingTab(null);
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader className="justify-between gap-3 px-5">
        <div className="flex min-w-0 items-center gap-2 text-sm">
          <AppLink
            href={paths.agents()}
            className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Agents
          </AppLink>
          <span className="text-muted-foreground/40">/</span>
          <span className="truncate font-medium">{agent.name}</span>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {runtime?.paused_at && canManageRuntime && (
            <PauseRuntimeButton runtime={runtime} workspaceId={wsId} compact />
          )}
          {runtime?.paused_at && !canManageRuntime && (
            <span
              className="inline-flex h-8 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground"
              title="Runtime is paused"
            >
              <Pause className="h-3.5 w-3.5" />
              Paused
            </span>
          )}
          {!isArchived && (
            <Button variant="outline" size="sm" onClick={messageAgent}>
              <MessageSquare className="h-3.5 w-3.5" />
              Message
            </Button>
          )}
          {!isArchived && (
            <Button variant="outline" size="sm" onClick={newIssueForAgent}>
              <Plus className="h-3.5 w-3.5" />
              New issue
            </Button>
          )}
          {canEdit && !isArchived && (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<Button variant="ghost" size="icon-sm" />}
              >
                <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-auto">
                {hasActiveWork && (
                  <DropdownMenuItem onClick={() => setConfirmCancelTasks(true)}>
                    <Square className="h-3.5 w-3.5" />
                    Cancel active tasks
                  </DropdownMenuItem>
                )}
                {hasActiveWork && <DropdownMenuSeparator />}
                <DropdownMenuItem
                  className="text-destructive"
                  onClick={() => setConfirmArchive(true)}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  Archive agent
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          {isArchived && canEdit && (
            <Button variant="outline" size="sm" onClick={() => handleRestore(agent.id)}>
              Restore
            </Button>
          )}
        </div>
      </PageHeader>

      {!canEdit && (
        <div className="bg-muted/40 px-4 pt-3 md:px-8">
          <div className="mx-auto max-w-[1400px]">
            <CapabilityBanner
              reason={perms.canEdit.reason}
              resource="agent"
              ownerName={owner?.name}
            />
          </div>
        </div>
      )}

      {runtime?.paused_at && (
        <div className="bg-muted/40 px-4 pt-3 md:px-8">
          <div className="mx-auto max-w-[1400px]">
            <PauseBanner runtime={runtime} />
          </div>
        </div>
      )}

      {isArchived && (
        <div className="flex shrink-0 items-center gap-2 border-b bg-muted/50 px-6 py-2 text-xs text-muted-foreground">
          <AlertCircle className="h-3.5 w-3.5 shrink-0" />
          <span className="flex-1">
            This agent is archived. Restore it before assigning new work.
          </span>
          {canEdit && (
            <Button
              variant="outline"
              size="sm"
              className="h-6 text-xs"
              onClick={() => handleRestore(agent.id)}
            >
              Restore
            </Button>
          )}
        </div>
      )}

      <div className="flex-1 overflow-y-auto bg-muted/40 p-4 md:p-8">
        <div className="mx-auto max-w-[1400px] overflow-hidden rounded-2xl border bg-background shadow-sm">
          <div className="flex flex-col md:flex-row md:items-stretch">
            <IdentityRail
              agent={agent}
              runtime={runtime}
              owner={owner}
              presence={presence}
              runtimes={runtimes}
              members={members}
              currentUserId={currentUser?.id ?? null}
              canEdit={canEdit}
              onUpdate={handleUpdate}
            />

            <div className="flex min-w-0 flex-1 flex-col">
              <div className="flex items-center gap-6 overflow-x-auto border-b px-6">
                {visibleTabs.map(({ id, label, icon: Icon }) => {
                  const active = effectiveTab === id;
                  return (
                    <button
                      key={id}
                      type="button"
                      onClick={() => requestTabChange(id)}
                      className={`flex items-center gap-1.5 whitespace-nowrap border-b-2 px-0.5 py-3.5 text-sm font-medium ${
                        active
                          ? "border-foreground text-foreground"
                          : "border-transparent text-muted-foreground hover:text-foreground"
                      }`}
                    >
                      <Icon className="h-[15px] w-[15px]" />
                      {label}
                    </button>
                  );
                })}
              </div>

              <div className="flex-1 bg-muted/20">
                {effectiveTab === "tasks" && (
                  <div className="flex min-h-[70vh] flex-col gap-3 p-2 md:p-4">
                    <div className="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-[3px]">
                      {(["activity", "tasks"] as const).map((v) => {
                        const active = taskView === v;
                        return (
                          <button
                            key={v}
                            type="button"
                            onClick={() => setTaskView(v)}
                            className={`flex items-center gap-1.5 rounded-md px-3.5 py-1.5 text-[13px] font-medium transition-colors ${
                              active
                                ? "bg-background text-foreground shadow-sm"
                                : "text-muted-foreground hover:text-foreground"
                            }`}
                          >
                            {v === "activity" ? (
                              <Activity className="h-[15px] w-[15px]" />
                            ) : (
                              <ListTodo className="h-[15px] w-[15px]" />
                            )}
                            {v === "activity" ? "Activity" : "Tasks"}
                          </button>
                        );
                      })}
                    </div>
                    {taskView === "activity" ? (
                      <div className="rounded-xl border bg-background p-4 md:p-5">
                        <ActivityTab agent={agent} />
                      </div>
                    ) : (
                      <ActorIssuesPanel actorType="agent" actorId={agent.id} />
                    )}
                  </div>
                )}
                {effectiveTab === "instructions" && (
                  <CerebroAgentContextTab agent={agent} canEdit={canEdit} />
                )}
                {effectiveTab === "skills" && (
                  <div className="p-5 md:p-6">
                    <SkillsTab agent={agent} canEdit={canEdit} />
                  </div>
                )}
                {effectiveTab === "env" && (
                  <TabContent>
                    <EnvTab
                      agent={agent}
                      readOnly={!canEdit || agent.custom_env_redacted}
                      onDirtyChange={setActiveDirty}
                      onSaved={() =>
                        qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) })
                      }
                    />
                  </TabContent>
                )}
                {effectiveTab === "infisical" && (
                  <TabContent>
                    <InfisicalFoldersTab
                      agent={agent}
                      readOnly={!canEdit}
                      onDirtyChange={setActiveDirty}
                    />
                  </TabContent>
                )}
                {effectiveTab === "custom_args" && (
                  <TabContent>
                    <CustomArgsTab
                      agent={agent}
                      runtimeDevice={runtime ?? undefined}
                      readOnly={!canEdit}
                      onSave={(updates) => handleUpdate(updates)}
                      onDirtyChange={setActiveDirty}
                    />
                  </TabContent>
                )}
                {effectiveTab === "sandbox" && (
                  <TabContent>
                    <SandboxTab
                      agent={agent}
                      canEdit={canEdit}
                      onSave={(updates) => handleUpdate(updates)}
                    />
                  </TabContent>
                )}
                {effectiveTab === "mcp_config" && (
                  <TabContent>
                    <McpConfigTab
                      agent={agent}
                      canEdit={canEdit}
                      onDirtyChange={setActiveDirty}
                    />
                  </TabContent>
                )}
                {effectiveTab === "integrations" && (
                  <TabContent>
                    <IntegrationsTab agent={agent} />
                  </TabContent>
                )}
                {effectiveTab === "tools" && (
                  <div className="p-5 md:p-6">
                    <CerebroToolsTab agent={agent} canEdit={canEdit} />
                  </div>
                )}
                {effectiveTab === "capabilities" && (
                  <div className="p-5 md:p-6">
                    <CerebroCapabilitiesTab agent={agent} />
                  </div>
                )}
                {effectiveTab === "memory" && (
                  <TabContent>
                    <CerebroAgentMemoryTab agent={agent} canEdit={canEdit} />
                  </TabContent>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>

      {pendingTab !== null && (
        <AlertDialog
          open
          onOpenChange={(open) => {
            if (!open) setPendingTab(null);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Discard unsaved changes?</AlertDialogTitle>
              <AlertDialogDescription>
                This tab has unsaved changes. Switching tabs now will discard them.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Keep editing</AlertDialogCancel>
              <AlertDialogAction variant="destructive" onClick={commitTabChange}>
                Discard changes
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}

      {confirmArchive && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open) setConfirmArchive(false);
          }}
        >
          <DialogContent className="max-w-sm" showCloseButton={false}>
            <DialogHeader className="gap-1">
              <DialogTitle className="text-sm font-semibold">
                Archive {agent.name}?
              </DialogTitle>
              <DialogDescription className="text-xs">
                The agent leaves active lists. You can restore it later.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setConfirmArchive(false)}>
                Keep agent
              </Button>
              <Button
                variant="destructive"
                onClick={() => {
                  setConfirmArchive(false);
                  void handleArchive(agent.id);
                }}
              >
                Archive agent
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}

      {confirmCancelTasks && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open) setConfirmCancelTasks(false);
          }}
        >
          <DialogContent className="max-w-sm" showCloseButton={false}>
            <DialogHeader className="gap-1">
              <DialogTitle className="text-sm font-semibold">
                Cancel active tasks for {agent.name}?
              </DialogTitle>
              <DialogDescription className="text-xs">
                This will stop {runningCount + queuedCount} active{" "}
                {runningCount + queuedCount === 1 ? "task" : "tasks"}. Running
                work may leave a partial result.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setConfirmCancelTasks(false)}>
                Keep tasks
              </Button>
              <Button
                variant="destructive"
                onClick={() => {
                  setConfirmCancelTasks(false);
                  void handleCancelTasks(agent.id);
                }}
              >
                Cancel tasks
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function TabContent({ children }: { children: React.ReactNode }) {
  return <div className="flex h-full flex-col p-4 md:p-6">{children}</div>;
}
