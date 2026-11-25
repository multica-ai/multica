import type { AgentTask, CommentSupplementReceipt, TimelineEntry } from "../types";

/** The run-scoped capability that lets a running turn take additional input. */
export const TASK_SUPPLEMENT_CAPABILITY = "task-supplement-v1";

type ReceiptFields = Pick<
  TimelineEntry,
  "supplements" | "supplement_task_id" | "supplement_status" | "supplement_failure_reason" | "supplement_delivered_at"
>;

/**
 * Every running turn a comment steered. Servers that predate multi-run
 * steering send only the single supplement_* fields, without the agent.
 */
export function commentSupplementReceipts(entry: ReceiptFields): CommentSupplementReceipt[] {
  if (entry.supplements?.length) return entry.supplements;
  if (!entry.supplement_task_id || !entry.supplement_status) return [];
  return [{
    task_id: entry.supplement_task_id,
    status: entry.supplement_status,
    failure_reason: entry.supplement_failure_reason,
    delivered_at: entry.supplement_delivered_at,
  }];
}

/** A receipt whose turn has not received it yet. */
export function isSupplementInFlight(receipt: CommentSupplementReceipt): boolean {
  return receipt.status === "pending" || receipt.status === "delivering";
}

/**
 * What one recipient is doing on this issue right now, which decides how a
 * message to it can be handled: steer a running turn, fold into a queued run,
 * or start a new one.
 */
export type AgentRunState =
  | { kind: "idle" }
  | { kind: "queued"; task: AgentTask }
  | { kind: "starting"; task: AgentTask }
  | { kind: "running"; task: AgentTask; steerable: boolean };

/** Whether this member can add a message to the task's running turn. */
export function isSteerableTask(task: AgentTask): boolean {
  return task.status === "running"
    && task.supplement_capability === TASK_SUPPLEMENT_CAPABILITY
    && task.can_supplement === true;
}

export function agentRunState(tasks: readonly AgentTask[], agentId: string): AgentRunState {
  // A running turn wins over a queued follow-up: it is what a message reaches now.
  let queued: AgentTask | undefined;
  let starting: AgentTask | undefined;
  for (const task of tasks) {
    if (task.agent_id !== agentId) continue;
    if (task.status === "running") return { kind: "running", task, steerable: isSteerableTask(task) };
    if (task.status === "dispatched" || task.status === "waiting_local_directory") starting ??= task;
    else if (task.status === "queued" || task.status === "deferred") queued ??= task;
  }
  if (starting) return { kind: "starting", task: starting };
  if (queued) return { kind: "queued", task: queued };
  return { kind: "idle" };
}

/** How a message reaches one recipient. */
export type RecipientAction =
  /** Into the running turn, read at its next step. */
  | "steer"
  /** Normal trigger while the recipient is busy: handled once this turn ends. */
  | "after_run"
  /** Stop the current turn, then start over with this message. */
  | "restart"
  /** Normal trigger for an idle recipient, or one whose run is still queued. */
  | "start"
  /** Post without triggering this recipient. */
  | "skip";

export interface RecipientActionOptions {
  /** False while editing, or when the message carries files: a running turn takes text only. */
  canSteer: boolean;
  /** False while editing: an edit never stops a run. */
  canRestart: boolean;
  /** Steer by default. The composer sets this for the running turn's own thread. */
  steerByDefault: boolean;
}

export function recipientActions(state: AgentRunState, opts: RecipientActionOptions): RecipientAction[] {
  if (state.kind === "idle" || state.kind === "queued") return ["start", "skip"];
  const actions: RecipientAction[] = [];
  if (state.kind === "running" && state.steerable && opts.canSteer) actions.push("steer");
  actions.push("after_run");
  if (opts.canRestart) actions.push("restart");
  actions.push("skip");
  return actions;
}

/** The chosen action while it is still available, otherwise the default. */
export function resolveRecipientAction(
  state: AgentRunState,
  chosen: RecipientAction | undefined,
  opts: RecipientActionOptions,
): RecipientAction {
  const actions = recipientActions(state, opts);
  if (chosen && actions.includes(chosen)) return chosen;
  if (opts.steerByDefault && actions.includes("steer")) return "steer";
  return actions.find((action) => action !== "steer")!;
}

export interface RecipientRouting {
  suppressAgentIds: string[];
  /** The exact running turns chosen: a turn that ends first is never swapped for another. */
  steerTaskIds: string[];
  /** Turns to stop before the comment is posted, so it starts a fresh run. */
  restartTaskIds: string[];
}

export function recipientRouting(
  recipients: readonly { agentId: string; action: RecipientAction; state: AgentRunState }[],
): RecipientRouting {
  const routing: RecipientRouting = { suppressAgentIds: [], steerTaskIds: [], restartTaskIds: [] };
  for (const { agentId, action, state } of recipients) {
    if (action === "skip") routing.suppressAgentIds.push(agentId);
    else if (action === "steer" && state.kind === "running") routing.steerTaskIds.push(state.task.id);
    else if (action === "restart" && state.kind !== "idle") routing.restartTaskIds.push(state.task.id);
  }
  return routing;
}
