"use client";

import { useState } from "react";
import {
  Activity,
  ArrowLeft,
  BookOpenText,
  FileText,
  ListTodo,
  MessageSquare,
  Plus,
  ShieldCheck,
  Wrench,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
  UpdateAgentRequest,
} from "@multica/core/types";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useAgentPermissions } from "@multica/core/permissions";
import { runtimeListOptions } from "@multica/core/runtimes";
import {
  agentListOptions,
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { CerebroCapabilitiesTab } from "@multica/cerebro-agent-capabilities/views";
import { CerebroAgentContextTab } from "@multica/cerebro-agent-context/views";
import { CerebroToolsTab } from "@multica/cerebro-agent-tools/views";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ActorIssuesPanel } from "@multica/views/common/actor-issues-panel";
// FIR-2670: reuse the shipped Activity feed + Skills add/remove tab — see
// CEREBRO-PATCH(views-agent-redesign-reuse).
import { ActivityTab } from "@multica/views/agents/components/tabs/activity-tab";
import { SkillsTab } from "@multica/views/agents/components/tabs/skills-tab";
import { PageHeader } from "@multica/views/layout/page-header";
import { AppLink } from "@multica/views/navigation";
import { IdentityRail } from "./identity-rail";

type RedesignTab =
  | "tasks"
  | "instructions"
  | "skills"
  | "tools"
  | "capabilities";

const TABS: {
  id: RedesignTab;
  label: string;
  icon: typeof ListTodo;
}[] = [
  { id: "tasks", label: "Tasks", icon: ListTodo },
  { id: "instructions", label: "Instructions", icon: FileText },
  { id: "skills", label: "Skills", icon: BookOpenText },
  { id: "tools", label: "Tools", icon: Wrench },
  { id: "capabilities", label: "Capabilities", icon: ShieldCheck },
];

/**
 * FIR-2670 — redesigned agent detail page, behind the
 * `cerebro_agent_page_redesign` flag. The redesigned identity rail + flat tab
 * strip live inside the standard Multica page shell (`PageHeader` for the
 * mobile sidebar trigger + breadcrumb, and a single scroll container) so the
 * page follows the same layout contract as every other page — a header on
 * mobile and a body that scrolls on every viewport. Every tab reuses the
 * existing, shipped surfaces (Tasks = ActorIssuesPanel, Instructions =
 * versioned Agent Office, Tools/Capabilities = the tool-policy + capability
 * engine views) so the redesign is presentation-only — no new data path.
 */
export function CerebroAgentDetailPage({ agentId }: { agentId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const currentUser = useAuthStore((s) => s.user);
  const [tab, setTab] = useState<RedesignTab>("tasks");
  // Activity | Tasks toggle inside the Tasks tab (FIR-2670 #6). Board is the
  // default; Activity is the shipped live-status feed.
  const [taskView, setTaskView] = useState<"activity" | "tasks">("tasks");

  const { data: agents = [], isLoading } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);

  // Header actions: Message opens the chat dock with a fresh DM to this agent;
  // New issue opens the create-issue modal pre-assigned to this agent. Both are
  // existing, shipped flows — the buttons are just new entry points.
  const setChatOpen = useChatStore((s) => s.setOpen);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const openModal = useModalStore((s) => s.open);

  const agent: Agent | null = agents.find((a) => a.id === agentId) ?? null;
  const perms = useAgentPermissions(agent, wsId);
  const canEdit = perms.canEdit.allowed;

  // Optimistic inline-edit write for the identity rail pickers — patch the
  // cached agent first so the picker chip flips immediately, then reconcile
  // with the server. Mirrors the shipped agent page's handleUpdate.
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

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* Standard page chrome: mobile sidebar trigger (the "header" that was
          missing on mobile), breadcrumb, and the primary actions. */}
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
        <div className="flex shrink-0 items-center gap-2">
          <Button variant="outline" size="sm" onClick={messageAgent}>
            <MessageSquare className="h-3.5 w-3.5" />
            Message
          </Button>
          <Button variant="outline" size="sm" onClick={newIssueForAgent}>
            <Plus className="h-3.5 w-3.5" />
            New issue
          </Button>
        </div>
      </PageHeader>

      {/* Single scroll container — the SidebarInset is overflow-hidden, so the
          page itself must own the scroll or nothing scrolls (mobile + desktop). */}
      <div className="flex-1 overflow-y-auto bg-muted/40 p-4 md:p-8">
        <div className="mx-auto max-w-[1400px] overflow-hidden rounded-2xl border bg-background shadow-sm">
          <div className="flex flex-col md:flex-row md:items-stretch">
            <IdentityRail
              agent={agent}
              runtime={runtime}
              owner={owner}
              presence={presence}
              accountName={workspace?.name ?? null}
              runtimes={runtimes}
              members={members}
              currentUserId={currentUser?.id ?? null}
              canEdit={canEdit}
              onUpdate={handleUpdate}
            />

            <div className="flex min-w-0 flex-1 flex-col">
              {/* tab strip */}
              <div className="flex items-center gap-6 overflow-x-auto border-b px-6">
                {TABS.map(({ id, label, icon: Icon }) => {
                  const active = tab === id;
                  return (
                    <button
                      key={id}
                      type="button"
                      onClick={() => setTab(id)}
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

              {/* tab content — no fixed height cap; the board fills the width
                  and grows with its content instead of being clipped. */}
              <div className="flex-1 bg-muted/20">
                {tab === "tasks" && (
                  <div className="flex min-h-[70vh] flex-col gap-3 p-2 md:p-4">
                    {/* Activity | Tasks segmented toggle (FIR-2670 #6): the
                        shipped live-status Activity feed sits alongside the
                        board, matching the design. */}
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
                {tab === "instructions" && (
                  <CerebroAgentContextTab agent={agent} canEdit={canEdit} />
                )}
                {tab === "skills" && (
                  <div className="p-5 md:p-6">
                    <SkillsTab agent={agent} canEdit={canEdit} />
                  </div>
                )}
                {tab === "tools" && (
                  <div className="p-5 md:p-6">
                    <CerebroToolsTab agent={agent} canEdit={canEdit} />
                  </div>
                )}
                {tab === "capabilities" && (
                  <div className="p-5 md:p-6">
                    <CerebroCapabilitiesTab agent={agent} />
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
