"use client";

import { useMemo, useState } from "react";
import { GitBranch, Lock, UserMinus, Zap } from "lucide-react";
import { toast } from "sonner";
import type { Agent, IssueAssigneeType, UpdateIssueRequest } from "@multica/core/types";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { useActorName } from "@multica/core/workspace/hooks";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions, squadListOptions, assigneeFrequencyOptions } from "@multica/core/workspace/queries";
import { workflowActiveListOptions, workflowTemplateListOptions, workflowNodesOptions } from "@multica/core/workflows/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { ActorAvatar } from "../../../common/actor-avatar";
import {
  PropertyPicker,
  PickerItem,
  PickerSection,
  PickerEmpty,
} from "./property-picker";
import { useT } from "../../../i18n";
import { matchesPinyin } from "../../../editor/extensions/pinyin-match";
import { RuntimeSelectDialog } from "../../../agents/components/runtime-select-dialog";

/**
 * Legacy boolean shape kept around for callers (e.g. `use-issue-actions.ts`)
 * that haven't migrated to the new `canAssignAgentToIssue` Decision API yet.
 * Internally redirects to the canonical rule so behaviour stays in sync.
 */
export function canAssignAgent(
  agent: Agent,
  userId: string | undefined,
  memberRole: string | undefined,
): boolean {
  return canAssignAgentToIssue(agent, {
    userId: userId ?? null,
    role: memberRole === "owner" || memberRole === "admin" || memberRole === "member"
      ? memberRole
      : null,
  }).allowed;
}

export function AssigneePicker({
  assigneeType,
  assigneeId,
  onUpdate,
  isWorkflowRunning = false,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  skipBuiltinRuntimeSelection = false,
}: {
  assigneeType: IssueAssigneeType | null;
  assigneeId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  /** When true, a workflow run is in progress. Changing the assignee will be blocked. */
  isWorkflowRunning?: boolean;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  /** When true, selecting a built-in agent will NOT show the runtime selection dialog.
   *  Use this in contexts like workflow editor where runtime is chosen at execution time. */
  skipBuiltinRuntimeSelection?: boolean;
}) {
  const { t } = useT("issues");
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const [filter, setFilter] = useState("");
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: activeWorkflows = [] } = useQuery(workflowActiveListOptions(wsId));
  const { data: templatesResponse } = useQuery(workflowTemplateListOptions(wsId));

  // Merge workspace workflows with cross-workspace active templates.
  // Deduplicate by ID so templates already present locally don't appear twice.
  const mergedActiveWorkflows = useMemo(() => {
    const templateWorkflows = templatesResponse?.workflows ?? [];
    if (templateWorkflows.length === 0) return activeWorkflows;
    const seen = new Set(activeWorkflows.map((w) => w.id));
    const externalActiveTemplates = templateWorkflows.filter(
      (w) => w.status === "active" && !seen.has(w.id),
    );
    return [...activeWorkflows, ...externalActiveTemplates];
  }, [activeWorkflows, templatesResponse]);
  const { data: frequency = [] } = useQuery(assigneeFrequencyOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { getActorName } = useActorName();
  const queryClient = useQueryClient();

  // Guard: prevent changing assignee while a workflow run is in progress.
  const guardedUpdate = (updates: Partial<UpdateIssueRequest>) => {
    if (
      isWorkflowRunning &&
      assigneeType === "workflow" &&
      updates.assignee_type !== undefined &&
      !(updates.assignee_type === "workflow" && updates.assignee_id === assigneeId)
    ) {
      toast.error(t(($) => $.pickers.assignee.workflow_running));
      return;
    }
    onUpdate(updates);
  };

  // Built-in agent runtime selection dialog state
  const [pendingBuiltinAgent, setPendingBuiltinAgent] = useState<Agent | null>(null);

  // Workflow runtime selection dialog state
  const [pendingWorkflowRuntime, setPendingWorkflowRuntime] = useState<{
    workflowId: string;
    workflowTitle: string;
  } | null>(null);
  const [checkingWorkflow, setCheckingWorkflow] = useState(false);

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;

  // Build a lookup map from frequency data for sorting.
  const freqMap = useMemo(() => {
    const map = new Map<string, number>();
    for (const entry of frequency) {
      map.set(`${entry.assignee_type}:${entry.assignee_id}`, entry.frequency);
    }
    return map;
  }, [frequency]);

  const getFreq = (type: string, id: string) => freqMap.get(`${type}:${id}`) ?? 0;

  const query = filter.trim().toLowerCase();
  const filteredMembers = members
    .filter((m) => m.name.toLowerCase().includes(query) || matchesPinyin(m.name, query))
    .sort((a, b) => getFreq("member", b.user_id) - getFreq("member", a.user_id));
  const filteredAgents = agents
    .filter((a) => !a.archived_at && (a.name.toLowerCase().includes(query) || matchesPinyin(a.name, query)))
    .sort((a, b) => getFreq("agent", b.id) - getFreq("agent", a.id));
  const filteredSquads = squads
    .filter((s) => !s.archived_at && (s.name.toLowerCase().includes(query) || matchesPinyin(s.name, query)))
    .sort((a, b) => getFreq("squad", b.id) - getFreq("squad", a.id));
  const filteredWorkflows = mergedActiveWorkflows
    .filter((w) => w.title.toLowerCase().includes(query) || matchesPinyin(w.title, query))
    .sort((a, b) => getFreq("workflow", b.id) - getFreq("workflow", a.id));

  const isSelected = (type: string, id: string) =>
    assigneeType === type && assigneeId === id;

  const triggerLabel =
    assigneeType && assigneeId
      ? getActorName(assigneeType, assigneeId)
      : t(($) => $.pickers.assignee.trigger_unassigned);

  // Handle clicking a built-in agent: show runtime dialog if >1 runtimes,
  // auto-select if exactly 1, fall through without runtime if 0.
  // When skipBuiltinRuntimeSelection is true, just assign the agent directly.
  const handleBuiltinAgentClick = (agent: Agent) => {
    if (skipBuiltinRuntimeSelection) {
      guardedUpdate({
        assignee_type: "agent",
        assignee_id: agent.id,
      });
      setOpen(false);
      return;
    }
    const onlineRuntimes = runtimes.filter((r) => r.status === "online");
    if (onlineRuntimes.length === 1) {
      // Single runtime: auto-select and close picker
      guardedUpdate({
        assignee_type: "agent",
        assignee_id: agent.id,
        runtime_id: onlineRuntimes[0]!.id,
      });
      setOpen(false);
    } else if (onlineRuntimes.length > 1) {
      // Multiple runtimes: show selection dialog
      setPendingBuiltinAgent(agent);
    } else {
      // No runtimes: proceed without runtime (backend will try auto-select)
      guardedUpdate({
        assignee_type: "agent",
        assignee_id: agent.id,
      });
      setOpen(false);
    }
  };

  const handleRuntimeConfirm = (runtimeId: string) => {
    if (!pendingBuiltinAgent) return;
    guardedUpdate({
      assignee_type: "agent",
      assignee_id: pendingBuiltinAgent.id,
      runtime_id: runtimeId,
    });
    setPendingBuiltinAgent(null);
    setOpen(false);
  };

  // Handle clicking a workflow: check if any node has a built-in agent,
  // and if so, show the runtime selection dialog so all built-in agents
  // in this workflow share the same runtime.
  //
  // For cross-workspace templates: lazily clone into the current workspace
  // on first use. Subsequent uses reuse the existing clone (matched by
  // source_template_id) so each workspace gets at most one clone per template.
  const handleWorkflowClick = async (workflow: { id: string; title: string; is_template?: boolean; source_template_id?: string | null }) => {
    let targetId = workflow.id;
    let targetTitle = workflow.title;

    // Lazy-clone: only cross-workspace templates need a local clone.
    // Templates already in the current workspace can be used directly.
    if (workflow.is_template) {
      const isLocal = activeWorkflows.some((w) => w.id === workflow.id);
      if (!isLocal) {
        const existingClone = activeWorkflows.find(
          (w) => w.source_template_id === workflow.id,
        );
        if (existingClone) {
          targetId = existingClone.id;
          targetTitle = existingClone.title;
        } else {
          setCheckingWorkflow(true);
          try {
            const cloned = await api.createWorkflowFromTemplate(workflow.id, workflow.title);
            await api.updateWorkflow(cloned.id, { status: "active" });
            queryClient.invalidateQueries({ queryKey: ["workflows", wsId] });
            targetId = cloned.id;
            targetTitle = cloned.title;
          } catch {
            // Clone or activation failed — abort, don't assign.
            setCheckingWorkflow(false);
            return;
          }
        }
      }
    }

    setCheckingWorkflow(true);
    try {
      const nodes = await queryClient.fetchQuery(workflowNodesOptions(wsId, targetId));
      const agentMap = new Map(agents.map((a) => [a.id, a]));

      const hasBuiltinAgent = nodes.some((node) => {
        if ((node.worker_type === "agent" || node.worker_type === "squad") && node.worker_id) {
          const agentId = node.worker_type === "squad"
            ? squads.find((s) => s.id === node.worker_id)?.leader_id
            : node.worker_id;
          if (agentId) {
            const agent = agentMap.get(agentId);
            if (agent?.is_builtin) return true;
          }
        }
        if ((node.critic_type === "agent" || node.critic_type === "squad") && node.critic_id) {
          const agentId = node.critic_type === "squad"
            ? squads.find((s) => s.id === node.critic_id)?.leader_id
            : node.critic_id;
          if (agentId) {
            const agent = agentMap.get(agentId);
            if (agent?.is_builtin) return true;
          }
        }
        return false;
      });

      if (hasBuiltinAgent) {
        const onlineRuntimes = runtimes.filter((r) => r.status === "online");
        if (onlineRuntimes.length === 1) {
          guardedUpdate({
            assignee_type: "workflow",
            assignee_id: targetId,
            runtime_id: onlineRuntimes[0]!.id,
          });
          setOpen(false);
        } else if (onlineRuntimes.length > 1) {
          setPendingWorkflowRuntime({
            workflowId: targetId,
            workflowTitle: targetTitle,
          });
        } else {
          // No online runtimes — can't execute built-in agents.
          toast.error(t(($) => $.pickers.assignee.no_runtime_available));
          setCheckingWorkflow(false);
          return;
        }
      } else {
        // No built-in agents in workflow — assign normally
        guardedUpdate({
          assignee_type: "workflow",
          assignee_id: targetId,
        });
        setOpen(false);
      }
    } catch {
      // On error, still assign the workflow (nodes may not have loaded)
      guardedUpdate({
        assignee_type: "workflow",
        assignee_id: targetId,
      });
      setOpen(false);
    } finally {
      setCheckingWorkflow(false);
    }
  };

  const handleWorkflowRuntimeConfirm = (runtimeId: string) => {
    if (!pendingWorkflowRuntime) return;
    guardedUpdate({
      assignee_type: "workflow",
      assignee_id: pendingWorkflowRuntime.workflowId,
      runtime_id: runtimeId,
    });
    setPendingWorkflowRuntime(null);
    setOpen(false);
  };

  return (
    <>
      <PropertyPicker
      open={open}
      onOpenChange={(v: boolean) => {
        setOpen(v);
        if (!v) setFilter("");
      }}
      width="w-64"
      align={align}
      searchable
      searchPlaceholder={t(($) => $.pickers.assignee.search_placeholder)}
      onSearchChange={setFilter}
      triggerRender={triggerRender}
      trigger={
        customTrigger ? customTrigger : assigneeType && assigneeId ? (
          <>
            <ActorAvatar actorType={assigneeType} actorId={assigneeId} size={18} enableHoverCard showStatusDot />
            <span className="truncate">{triggerLabel}</span>
          </>
        ) : (
          <span className="text-muted-foreground">{t(($) => $.pickers.assignee.trigger_unassigned)}</span>
        )
      }
    >
      {/* Unassigned option — hidden when search is active */}
      {!query && (
        <PickerItem
          selected={!assigneeType && !assigneeId}
          onClick={() => {
            guardedUpdate({ assignee_type: null, assignee_id: null });
            setOpen(false);
          }}
        >
          <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="text-muted-foreground">{t(($) => $.pickers.assignee.trigger_unassigned)}</span>
        </PickerItem>
      )}

      {/* Workflows */}
      {filteredWorkflows.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.workflows_group)}>
          {filteredWorkflows.map((w) => (
            <PickerItem
              key={w.id}
              selected={isSelected("workflow", w.id)}
              onClick={() => {
                handleWorkflowClick(w);
              }}
              disabled={checkingWorkflow}
            >
              <GitBranch className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="truncate" title={w.title}>{w.title}</span>
              {w.is_template && (
                <span className="ml-auto shrink-0 inline-flex items-center gap-0.5 rounded bg-amber-100 px-1 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
                  <Zap className="h-2.5 w-2.5" />
                  {t(($) => $.pickers.assignee.template_label)}
                </span>
              )}
            </PickerItem>
          ))}
        </PickerSection>
      )}

      {/* Agents */}
      {filteredAgents.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.agents_group)}>
          {filteredAgents.map((a) => {
            const decision = canAssignAgentToIssue(a, {
              userId: user?.id ?? null,
              role:
                memberRole === "owner" ||
                memberRole === "admin" ||
                memberRole === "member"
                  ? memberRole
                  : null,
            });
            const allowed = decision.allowed;
            return (
              <PickerItem
                key={a.id}
                selected={isSelected("agent", a.id)}
                disabled={!allowed}
                tooltip={!allowed ? decision.message : undefined}
                onClick={() => {
                  if (!allowed) return;
                  if (a.is_builtin) {
                    handleBuiltinAgentClick(a);
                  } else {
                    guardedUpdate({
                      assignee_type: "agent",
                      assignee_id: a.id,
                    });
                    setOpen(false);
                  }
                }}
              >
                <ActorAvatar actorType="agent" actorId={a.id} size={18} showStatusDot />
                <span className={`truncate ${allowed ? "" : "text-muted-foreground"}`}>{a.name}</span>
                {a.is_builtin && (
                  <span className="ml-auto shrink-0 inline-flex items-center gap-0.5 rounded bg-amber-100 px-1 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
                    <Zap className="h-2.5 w-2.5" />
                    {t(($) => $.pickers.assignee.builtin_label)}
                  </span>
                )}
                {a.visibility === "private" && !a.is_builtin && (
                  <Lock className="ml-auto h-3 w-3 text-muted-foreground" />
                )}
                {a.visibility === "private" && a.is_builtin && (
                  <Lock className="h-3 w-3 text-muted-foreground shrink-0" />
                )}
              </PickerItem>
            );
          })}
        </PickerSection>
      )}

      {/* Members */}
      {filteredMembers.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.members_group)}>
          {filteredMembers.map((m) => (
            <PickerItem
              key={m.user_id}
              selected={isSelected("member", m.user_id)}
              onClick={() => {
                guardedUpdate({
                  assignee_type: "member",
                  assignee_id: m.user_id,
                });
                setOpen(false);
              }}
            >
              <ActorAvatar actorType="member" actorId={m.user_id} size={18} />
              <span className="truncate">{m.name}</span>
            </PickerItem>
          ))}
        </PickerSection>
      )}

      {/* Squads — group ownership; assigning to a squad routes the issue to
          its leader agent on the backend. */}
      {filteredSquads.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.squads_group)}>
          {filteredSquads.map((s) => (
            <PickerItem
              key={s.id}
              selected={isSelected("squad", s.id)}
              onClick={() => {
                guardedUpdate({
                  assignee_type: "squad",
                  assignee_id: s.id,
                });
                setOpen(false);
              }}
            >
              <ActorAvatar actorType="squad" actorId={s.id} size={18} />
              <span className="truncate">{s.name}</span>
            </PickerItem>
          ))}
        </PickerSection>
      )}

      {filteredMembers.length === 0 &&
        filteredAgents.length === 0 &&
        filteredSquads.length === 0 &&
        filteredWorkflows.length === 0 &&
        filter && <PickerEmpty />}
    </PropertyPicker>
    {pendingBuiltinAgent && (
      <RuntimeSelectDialog
        agentName={pendingBuiltinAgent.name}
        runtimes={runtimes}
        loading={false}
        onConfirm={handleRuntimeConfirm}
        onClose={() => {
          setPendingBuiltinAgent(null);
        }}
      />
    )}
    {pendingWorkflowRuntime && (
      <RuntimeSelectDialog
        agentName={pendingWorkflowRuntime.workflowTitle}
        runtimes={runtimes}
        loading={false}
        onConfirm={handleWorkflowRuntimeConfirm}
        onClose={() => {
          setPendingWorkflowRuntime(null);
        }}
      />
    )}
    </>
  );
}
