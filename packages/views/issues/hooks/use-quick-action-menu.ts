"use client";

import { useCallback, useMemo } from "react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { Editor } from "@tiptap/core";
import type { Node } from "@tiptap/pm/model";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace } from "@multica/core/paths";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { isDeletedComment } from "@multica/core/issues/comment-deletion";
import { parseMentions } from "@multica/core/issues/comment-trigger-outcomes";
import { issueDetailOptions, issueKeys } from "@multica/core/issues/queries";
import { buildQuickActionBody, quickActionListOptions } from "@multica/core/quick-actions";
import type { Agent, AgentSkillSummary, AgentTask, Issue, MemberWithUser, Squad, TimelineEntry } from "@multica/core/types";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import { useT } from "../../i18n";
import { commentTriggerPreviewSignature, type UseCommentTriggerPreviewResult } from "./use-comment-trigger-preview";

export interface IssueSlashSkillContext {
  issue?: Pick<Issue, "assignee_type" | "assignee_id">;
  agents: Agent[];
  squads: Squad[];
  members: MemberWithUser[];
  userId: string | null;
  parentId?: string;
  targetMissing?: boolean;
  timeline?: TimelineEntry[];
  tasks?: AgentTask[];
}

function threadRoot(
  comment: TimelineEntry,
  comments: Map<string, TimelineEntry>,
): TimelineEntry | undefined {
  const seen = new Set<string>();
  let root = comment;
  while (root.parent_id) {
    if (seen.has(root.id)) return undefined;
    seen.add(root.id);
    const parent = comments.get(root.parent_id);
    if (!parent) return undefined;
    root = parent;
  }
  return root;
}

/** Undefined permits the Issue assignee fallback; an empty list locks routing to no target. */
function resolveThreadSlashTargetIds(
  parentId: string,
  timeline: TimelineEntry[] | undefined,
  tasks: AgentTask[] | undefined,
  squadsById: Map<string, Squad>,
): string[] | undefined {
  if (!timeline) return undefined;
  const comments = new Map(
    timeline.filter((entry) => entry.type === "comment").map((entry) => [entry.id, entry]),
  );
  const parent = comments.get(parentId);
  if (!parent || isDeletedComment(parent)) return [];
  if (parent.actor_type === "agent") return parent.actor_id ? [parent.actor_id] : [];

  const root = threadRoot(parent, comments);
  if (root?.actor_type === "member") {
    const explicit = parseMentions(root.content ?? "").find(
      (mention) => mention.type === "agent" || mention.type === "squad",
    );
    if (explicit) {
      if (explicit.type === "agent") return [explicit.id];
      const squad = squadsById.get(explicit.id);
      return squad && !squad.archived_at ? [squad.leader_id] : [];
    }
    const ownerIds = (tasks ?? [])
      .filter((task) =>
        task.trigger_comment_id === root.id || task.coalesced_comment_ids?.includes(root.id))
      .map((task) => task.agent_id);
    for (const comment of comments.values()) {
      if (comment.actor_type === "agent" && threadRoot(comment, comments)?.id === root.id) {
        ownerIds.push(comment.actor_id);
      }
    }
    if (ownerIds.length > 0) return ownerIds;
  }

  if (parent.actor_type === "member") return [];
  return undefined;
}

/** Reads target mentions from the live document; no Markdown/debounce round-trip. */
export function resolveIssueSlashSkills(
  doc: Node,
  { issue, agents, squads, members, userId, parentId, targetMissing, timeline, tasks }: IssueSlashSkillContext,
): AgentSkillSummary[] {
  if (targetMissing) return [];
  const agentsById = new Map(agents.map((agent) => [agent.id, agent]));
  const squadsById = new Map(squads.map((squad) => [squad.id, squad]));
  const targetIds: string[] = [];
  let hasExplicitTarget = false;
  let hasMemberOrAllMention = false;

  doc.descendants((node) => {
    if (node.type.name !== "mention") return;
    const { type, id } = node.attrs as { type?: string; id?: string };
    if (!id) return;
    if (type === "member" || type === "all") hasMemberOrAllMention = true;
    if (type === "agent") {
      hasExplicitTarget = true;
      targetIds.push(id);
    } else if (type === "squad") {
      hasExplicitTarget = true;
      const squad = squadsById.get(id);
      if (squad && !squad.archived_at) targetIds.push(squad.leader_id);
    }
  });

  // Explicit agent/squad mentions win over member/all; without one, those
  // mentions suppress every implicit route just as comment creation does.
  if (!hasExplicitTarget) {
    if (hasMemberOrAllMention) return [];
    const threadTargets = parentId
      ? resolveThreadSlashTargetIds(parentId, timeline, tasks, squadsById)
      : undefined;
    if (threadTargets) {
      targetIds.push(...threadTargets);
    } else if (issue?.assignee_id) {
      if (issue.assignee_type === "agent") {
        targetIds.push(issue.assignee_id);
      } else if (issue.assignee_type === "squad") {
        const squad = squadsById.get(issue.assignee_id);
        if (squad && !squad.archived_at) targetIds.push(squad.leader_id);
      }
    }
  }

  const memberRole = members.find((member) => member.user_id === userId)?.role ?? null;
  const seenAgentIds = new Set<string>();
  const seenSkillIds = new Set<string>();
  const skills: AgentSkillSummary[] = [];
  for (const agentId of targetIds) {
    if (seenAgentIds.has(agentId)) continue;
    seenAgentIds.add(agentId);
    const agent = agentsById.get(agentId);
    if (
      !agent ||
      agent.archived_at ||
      !agent.runtime_id ||
      !canAssignAgentToIssue(agent, { userId, role: memberRole }).allowed
    ) continue;
    for (const skill of agent.skills) {
      if (skill.enabled === false || seenSkillIds.has(skill.id)) continue;
      seenSkillIds.add(skill.id);
      skills.push(skill);
    }
  }
  return skills;
}

/**
 * Supplies the comment composer's `/` menu with this workspace's quick actions
 * and the enabled skills bound to its current Issue target agents (MUL-5465).
 *
 * Picking one INSERTS the cached body rather than running it — the composer
 * path exists precisely for the "this time is different" case, so the user
 * gets to edit and then send with the normal shortcut. Running without review
 * is what the sidebar button is for.
 *
 * The menu offers the same catalog the sidebar shows. Inserting draft text
 * needs no invocation preflight; the comment send path checks permission just
 * as it does for a hand-typed mention.
 *
 * Reads workspace identity through `useCurrentWorkspace` (nullable) rather
 * than `useWorkspaceId` (throws): the comment composer also mounts outside a
 * workspace route, and an enhancement like this must degrade to "no quick
 * actions in the menu", never take the composer down with it.
 */
export function useQuickActionMenu(
  issueId?: string,
  { enabled = true, parentId, targetMissing = false, triggerPreview, previewContent = "" }: {
    enabled?: boolean;
    parentId?: string;
    targetMissing?: boolean;
    triggerPreview?: Pick<UseCommentTriggerPreviewResult, "agents" | "isCurrent">;
    previewContent?: string;
  } = {},
) {
  const { t } = useT("issues");
  const queryClient = useQueryClient();
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const hasIssueContext = enabled && wsId !== "" && !!issueId;
  const { data } = useQuery({
    ...quickActionListOptions(wsId),
    enabled: enabled && wsId !== "",
  });
  const { data: issue } = useQuery({
    ...issueDetailOptions(wsId, issueId ?? ""),
    enabled: hasIssueContext,
    refetchOnMount: false,
  });
  const { data: agents } = useQuery({
    ...agentListOptions(wsId),
    enabled: hasIssueContext,
    refetchOnMount: false,
  });
  const { data: squads } = useQuery({
    ...squadListOptions(wsId),
    enabled: hasIssueContext,
    refetchOnMount: false,
  });
  const { data: members } = useQuery({
    ...memberListOptions(wsId),
    enabled: hasIssueContext,
    refetchOnMount: false,
  });

  const actions = useMemo(
    () => (data ?? []).filter((a) => a.status === "active"),
    [data],
  );

  const getQuickActions = useCallback(
    () => actions.map((a) => ({ id: a.id, name: a.name, description: a.description || undefined })),
    [actions],
  );
  const getQuickActionContent = useCallback((quickActionId: string) => {
    const action = actions.find((a) => a.id === quickActionId);
    if (!action) return "";
    const content = buildQuickActionBody(action);
    if (content === null) {
      toast.error(t(($) => $.comment.trigger_blocked_target_unavailable));
      return "";
    }
    return content;
  }, [actions, t]);

  const previewAgents = triggerPreview?.agents;
  // Empty composers have a synthetic current preview with no targets. Keep
  // synchronous resolution there so the first nonempty query need not wait
  // for a network round-trip before offering skills.
  const hasCurrentPreview = triggerPreview?.isCurrent && commentTriggerPreviewSignature(previewContent) !== "empty";
  const getSkills = useCallback((editor: Editor): AgentSkillSummary[] => {
    if (!hasIssueContext || targetMissing) return [];
    if (hasCurrentPreview) {
      const agentsById = new Map((agents ?? []).map((agent) => [agent.id, agent]));
      const seenSkillIds = new Set<string>();
      return (previewAgents ?? []).flatMap(({ id }) =>
        (agentsById.get(id)?.skills ?? []).filter((skill) => {
          if (skill.enabled === false || seenSkillIds.has(skill.id)) return false;
          seenSkillIds.add(skill.id);
          return true;
        }),
      );
    }
    const timeline = parentId && issueId
      ? queryClient.getQueryData<TimelineEntry[]>(issueKeys.timeline(issueId))
      : undefined;
    const tasks = parentId && issueId
      ? queryClient.getQueryData<AgentTask[]>(issueKeys.tasks(issueId))
      : undefined;
    return resolveIssueSlashSkills(editor.state.doc, {
      issue,
      agents: agents ?? [],
      squads: squads ?? [],
      members: members ?? [],
      userId: useAuthStore.getState().user?.id ?? null,
      parentId,
      targetMissing,
      timeline,
      tasks,
    });
  }, [hasIssueContext, issue, agents, squads, members, queryClient, issueId, parentId, targetMissing, hasCurrentPreview, previewAgents]);

  return useMemo(
    () => ({ getQuickActions, getQuickActionContent, getSkills }),
    [getQuickActions, getQuickActionContent, getSkills],
  );
}
