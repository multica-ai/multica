"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueTasksOptions } from "@multica/core/issues/queries";
import { useCommentComposerStore } from "@multica/core/issues/stores";
import {
  agentRunState,
  recipientActions,
  recipientRouting,
  resolveRecipientAction,
  type AgentRunState,
  type RecipientAction,
  type RecipientRouting,
} from "@multica/core/issues/run-steering";
import type { AgentTask, CommentTriggerPreviewAgent } from "@multica/core/types";

const NO_TASKS: AgentTask[] = [];
const ACTIVE_STATUSES = new Set<AgentTask["status"]>(["queued", "deferred", "dispatched", "waiting_local_directory", "running"]);
const NO_CHOICES: Record<string, RecipientAction> = {};

export interface RecipientEntry {
  agent: CommentTriggerPreviewAgent;
  state: AgentRunState;
  action: RecipientAction;
  actions: RecipientAction[];
}

/**
 * The composer's per-recipient choice. The preview decides WHO receives the
 * message (existing trigger rules); each recipient's live run on this issue
 * decides what can happen to it: steer a running turn, fold into a queued
 * run, or start a new one.
 */
export function useRecipientActions({
  issueId,
  agents,
  allowSteer,
  hasAttachments = false,
  steerByDefault: steerHere,
  resetKey,
}: {
  issueId: string;
  agents: CommentTriggerPreviewAgent[];
  /** False for edits: an edit neither steers nor stops a run. */
  allowSteer: boolean;
  hasAttachments?: boolean;
  /** Whether a running turn takes the message by default from this composer. */
  steerByDefault: (task: AgentTask) => boolean;
  /** Choices reset when the composer's context changes. */
  resetKey: string;
}) {
  // Every comment row mounts this hook for its edit composer, so it reads only
  // the active runs of its own recipients: with none (the usual idle row) it
  // neither fetches nor re-renders on unrelated task events.
  const agentKey = agents.map((agent) => agent.id).join(",");
  const selectActive = useCallback((all: AgentTask[]) => {
    const ids = new Set(agentKey.split(","));
    return all.filter((task) => ids.has(task.agent_id) && ACTIVE_STATUSES.has(task.status));
  }, [agentKey]);
  const { data: tasks = NO_TASKS } = useQuery({
    ...issueTasksOptions(issueId),
    enabled: !!issueId && agents.length > 0,
    select: selectActive,
  });
  // The personal default can turn steering off for every composer; it only
  // decides what happens without a per-message choice.
  const steerWithoutChoice = useCommentComposerStore((s) => s.runningAgentReply !== "after_run");
  const steerByDefault = useCallback(
    (task: AgentTask) => steerWithoutChoice && steerHere(task),
    [steerWithoutChoice, steerHere],
  );
  const [chosen, setChosen] = useState<Record<string, RecipientAction>>(NO_CHOICES);

  useEffect(() => {
    setChosen(NO_CHOICES);
  }, [resetKey]);

  // Forget choices for agents that are no longer recipients (an @mention removed).
  useEffect(() => {
    const visible = new Set(agents.map((agent) => agent.id));
    setChosen((prev) => {
      const next = Object.fromEntries(Object.entries(prev).filter(([id]) => visible.has(id)));
      return Object.keys(next).length === Object.keys(prev).length ? prev : next;
    });
  }, [agents]);

  const recipients = useMemo<RecipientEntry[]>(() => agents.map((agent) => {
    const state = agentRunState(tasks, agent.id);
    const opts = {
      canSteer: allowSteer && !hasAttachments,
      canRestart: allowSteer,
      steerByDefault: state.kind === "running" && steerByDefault(state.task),
    };
    return {
      agent,
      state,
      action: resolveRecipientAction(state, chosen[agent.id], opts),
      actions: recipientActions(state, opts),
    };
  }), [agents, tasks, chosen, allowSteer, hasAttachments, steerByDefault]);

  // A recipient would take this in its running turn, but files cannot go there.
  const attachmentsBlockSteer = allowSteer && hasAttachments && recipients.some((r) =>
    r.state.kind === "running" && r.state.steerable
    && (chosen[r.agent.id] === "steer" || (!chosen[r.agent.id] && steerByDefault(r.state.task))));

  const setAction = useCallback((agentId: string, action: RecipientAction) => {
    setChosen((prev) => (prev[agentId] === action ? prev : { ...prev, [agentId]: action }));
  }, []);

  const reset = useCallback(() => {
    setChosen(NO_CHOICES);
  }, []);

  const routing = useMemo<RecipientRouting>(() => recipientRouting(recipients.map((r) => ({
    agentId: r.agent.id, action: r.action, state: r.state,
  }))), [recipients]);

  return { recipients, attachmentsBlockSteer, routing, setAction, reset };
}
