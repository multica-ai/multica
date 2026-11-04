import type { AgentActivityTurn } from "@tutti-os/agent-activity-core";
import type { WorkspaceAgentActivityTimelineItem } from "./workspaceAgentTimelineTypes";
import { isWorkspaceAgentToolCallItem } from "./workspaceAgentToolCallDisplay";
import type {
  BuildWorkspaceAgentSessionDetailInput,
  WorkspaceAgentSessionDetailAgentItem,
  WorkspaceAgentSessionDetailToolCall,
  WorkspaceAgentSessionDetailToolGroupEntry,
  WorkspaceAgentSessionDetailTurn,
  WorkspaceAgentSessionDetailViewModel
} from "./workspaceAgentSessionDetailViewModel";
import {
  isPlaceholderThinkingBody,
  isRecentDuplicateUserMessage,
  messageBody,
  messageRole,
  messageStatusKind,
  normalizedMessageBody,
  stripReviewProcessSummaryTitle,
  thinkingStatusKind,
  userMessageProjectionKey
} from "./workspaceAgentTimelineMessageHelpers";
import { isCollaborationTimelineItem } from "./agentConversation/projection/agentCollaborationProjection";
import {
  compareToolCallsAscending,
  delegatedToolStepFromCall,
  firstPresentString,
  isTaskLikeToolCall,
  mergeSourceTimelineItems,
  normalizedPayload,
  parentToolUseIdFromCall,
  shouldShowProcessingIndicator,
  stringRecordValue,
  summarizeToolCallGroup,
  systemNoticeFromPayload,
  toolCallView,
  visibleErrorFromPayload,
  withSourceTimelineItems
} from "./workspaceAgentTimelineProjectionHelpers";
import { enrichProjectedTurnsWithCanonicalErrors } from "./workspaceAgentTurnErrorProjection";
import {
  normalizeToolName,
  shouldSuppressToolCall,
  suppressedUnavailableAskUserQuestionCallIds
} from "./workspaceAgentTimelineSuppression";

/**
 * A turn as built, kept while its first item's object lives: a commit keeps the
 * objects of items that did not change, and most turns have none that did.
 */
const builtTurnByFirstItem = new WeakMap<
  WorkspaceAgentActivityTimelineItem,
  {
    items: readonly WorkspaceAgentActivityTimelineItem[];
    canonicalKey: string;
    turn: WorkspaceAgentSessionDetailTurn;
  }
>();

/** The items of each turn, or null when an item's turn depends on those before it. */
function itemsByExplicitTurn(
  items: readonly WorkspaceAgentActivityTimelineItem[]
): Map<string, WorkspaceAgentActivityTimelineItem[]> | null {
  const groups = new Map<string, WorkspaceAgentActivityTimelineItem[]>();
  for (const item of items) {
    const turnId = item.turnId?.trim();
    if (!turnId) return null;
    const group = groups.get(turnId);
    if (group) group.push(item);
    else groups.set(turnId, [item]);
  }
  return groups;
}

/** What a turn takes from its canonical record, beyond its own items. */
function canonicalTurnKey(
  turnId: string,
  sessionTurns: readonly AgentActivityTurn[]
): string {
  const turn = sessionTurns.find((candidate) => candidate.turnId === turnId);
  return turn?.outcome === "failed" || turn?.outcome === "interrupted"
    ? JSON.stringify([turn.outcome, turn.error?.message, turn.error?.detail])
    : "";
}

export function buildCanonicalWorkspaceAgentDetailView(
  input: BuildWorkspaceAgentSessionDetailInput
): WorkspaceAgentSessionDetailViewModel {
  return buildDetailView(input, true);
}

function buildDetailView(
  input: BuildWorkspaceAgentSessionDetailInput,
  reuseTurns: boolean
): WorkspaceAgentSessionDetailViewModel {
  const {
    activity,
    session,
    sessionTurns = [],
    timelineItems,
    workspaceRoot = null
  } = input;
  const turns = new Map<string, WorkspaceAgentSessionDetailTurn>();
  const recentUserMessages = new Map<
    string,
    WorkspaceAgentActivityTimelineItem
  >();
  const seenThinkingMessages = new Set<string>();
  let activeSequenceTurnId: string | null = null;
  const sortedTimelineItems = [...timelineItems].sort(
    compareTimelineItemsAscending
  );
  const suppressedToolCallIds =
    suppressedUnavailableAskUserQuestionCallIds(sortedTimelineItems);
  const canonicalTurns =
    sessionTurns.length > 0
      ? sessionTurns
      : session.latestTurn
        ? [session.latestTurn]
        : [];
  // Goal controls, suppressed calls and items without a turn reach across turns.
  const groups =
    reuseTurns && suppressedToolCallIds.size === 0
      ? itemsByExplicitTurn(sortedTimelineItems)
      : null;
  const reusedTurns = new Map<string, WorkspaceAgentSessionDetailTurn>();
  for (const [turnId, items] of groups ?? []) {
    const built = builtTurnByFirstItem.get(items[0]!);
    if (
      built &&
      built.items.length === items.length &&
      built.items.every((item, index) => item === items[index]) &&
      built.canonicalKey === canonicalTurnKey(turnId, canonicalTurns)
    ) {
      reusedTurns.set(turnId, built.turn);
    }
  }
  const dedupedTurnIds = new Set<string>();

  for (const item of sortedTimelineItems) {
    const role = messageRole(item);
    const body = messageBody(item);
    const explicitTurnId = item.turnId?.trim();
    const reused = explicitTurnId ? reusedTurns.get(explicitTurnId) : undefined;
    if (reused) {
      if (!turns.has(explicitTurnId!)) turns.set(explicitTurnId!, reused);
      if (role === "user") {
        const projectionKey = userMessageProjectionKey(item, body);
        if (projectionKey) {
          // This prompt was kept when the turn was built. If it would be dropped now, build everything.
          if (isRecentDuplicateUserMessage(recentUserMessages.get(projectionKey), item)) {
            return buildDetailView(input, false);
          }
          recentUserMessages.set(projectionKey, item);
        }
      }
      continue;
    }

    if (role === "user") {
      const turnId = explicitTurnId || `seq:${item.seq || item.id}`;
      const projectionKey = userMessageProjectionKey(item, body);
      if (!projectionKey) {
        continue;
      }
      if (
        isRecentDuplicateUserMessage(
          recentUserMessages.get(projectionKey),
          item
        )
      ) {
        dedupedTurnIds.add(turnId);
        continue;
      }
      activeSequenceTurnId = turnId;
      const turn = getTurn(turns, turnId);
      const message = withSourceTimelineItems(
        {
          id: itemId(item),
          body,
          turnId,
          occurredAtUnixMs:
            item.occurredAtUnixMs ?? item.createdAtUnixMs ?? null
        },
        [item]
      );
      turn.userMessages.push(message);
      turn.userMessage ??= message;
      recentUserMessages.set(projectionKey, item);
      continue;
    }

    const turnId =
      explicitTurnId || activeSequenceTurnId || `seq:${item.seq || item.id}`;
    const turn = getTurn(turns, turnId);

    // Collaboration runs render as a dedicated card. Unlike ordinary agent
    // messages they must stay visible while their body (resultText) is still
    // empty — a running consult has no output yet — so this branch does not
    // require a non-empty body.
    if (isCollaborationTimelineItem(item)) {
      const status = firstPresentString(
        item.status,
        stringRecordValue(item.payload, "status")
      );
      const statusKind = messageStatusKind(status);
      const message = withSourceTimelineItems(
        {
          id: itemId(item),
          body,
          ...(status ? { status } : {}),
          ...(statusKind ? { statusKind } : {}),
          turnId,
          occurredAtUnixMs:
            item.occurredAtUnixMs ?? item.createdAtUnixMs ?? null
        },
        [item]
      );
      turn.agentMessages.push(message);
      turn.agentItems.push({ kind: "message", message });
      continue;
    }

    if (isWorkspaceAgentToolCallItem(item)) {
      if (shouldSuppressToolCall(item, suppressedToolCallIds)) {
        continue;
      }
      upsertToolCall(turn, item);
      continue;
    }

    if (role === "thinking" && body) {
      const payload = normalizedPayload(item.payload);
      if (payload?.messageKind === "review-process") {
        const status = firstPresentString(
          item.status,
          stringRecordValue(payload, "status")
        );
        const statusKind = messageStatusKind(status);
        const message = withSourceTimelineItems(
          {
            id: itemId(item),
            body: stripReviewProcessSummaryTitle(body),
            ...(status ? { status } : {}),
            ...(statusKind ? { statusKind } : {}),
            turnId,
            occurredAtUnixMs:
              item.occurredAtUnixMs ?? item.createdAtUnixMs ?? null
          },
          [item]
        );
        turn.agentMessages.push(message);
        turn.agentItems.push({ kind: "message", message });
        continue;
      }
      if (isPlaceholderThinkingBody(body)) {
        continue;
      }
      const duplicateKey = `${turnId}\u0000${normalizedMessageBody(body)}`;
      if (seenThinkingMessages.has(duplicateKey)) {
        continue;
      }
      seenThinkingMessages.add(duplicateKey);
      const statusKind = thinkingStatusKind(item);
      const thinking = withSourceTimelineItems(
        {
          id: itemId(item),
          body,
          ...(statusKind ? { statusKind } : {}),
          turnId,
          occurredAtUnixMs:
            item.occurredAtUnixMs ?? item.createdAtUnixMs ?? null
        },
        [item]
      );
      turn.agentItems.push({ kind: "thinking", thinking });
      continue;
    }

    if (role === "agent" && body) {
      const payload = normalizedPayload(item.payload);
      const visibleError = visibleErrorFromPayload(payload);
      const systemNotice = systemNoticeFromPayload(payload, item);
      const status = firstPresentString(
        item.status,
        stringRecordValue(payload, "status")
      );
      const statusKind = messageStatusKind(status);
      const message = withSourceTimelineItems(
        {
          id: itemId(item),
          body,
          ...(status ? { status } : {}),
          ...(statusKind ? { statusKind } : {}),
          turnId,
          occurredAtUnixMs:
            item.occurredAtUnixMs ?? item.createdAtUnixMs ?? null,
          ...(visibleError ? { visibleError } : {}),
          ...(systemNotice ? { systemNotice } : {})
        },
        [item]
      );
      turn.agentMessages.push(message);
      turn.agentItems.push({ kind: "message", message });
    }
  }

  enrichProjectedTurnsWithCanonicalErrors({
    turns,
    latestTurnId: session.latestTurn?.turnId ?? null,
    // A kept turn already has what its record adds, by its key.
    sessionTurns: canonicalTurns.filter((turn) => !reusedTurns.has(turn.turnId)),
    provider: session.provider,
    agentSessionId: session.agentSessionId
  });

  const visibleTurns = [...turns.values()].filter(
    (turn) => turn.userMessages.length > 0 || turn.agentItems.length > 0
  );
  const delegatedCalls = findDelegatedToolCalls(visibleTurns);
  // Nesting moves calls between turns, which a kept turn must not take part in.
  if (delegatedCalls && reusedTurns.size > 0) {
    return buildDetailView(input, false);
  }
  if (delegatedCalls) nestDelegatedToolCalls(delegatedCalls);
  const builtTurns = visibleTurns.filter((turn) => !reusedTurns.has(turn.id));
  builtTurns.forEach(mergeBackgroundTerminalContinuations);
  builtTurns.forEach((turn) => {
    turn.rawAgentItems = [...turn.agentItems];
    turn.agentItems = regroupAgentItems(turn.agentItems);
  });
  if (groups && !delegatedCalls) {
    for (const turn of builtTurns) {
      const items = groups.get(turn.id);
      if (!items || dedupedTurnIds.has(turn.id)) continue;
      builtTurnByFirstItem.set(items[0]!, {
        items,
        canonicalKey: canonicalTurnKey(turn.id, canonicalTurns),
        turn
      });
    }
  }

  return {
    activity,
    session,
    sessionTurns,
    cwd: session.cwd.trim(),
    workspaceRoot: workspaceRoot?.trim() || null,
    turns: visibleTurns,
    showProcessingIndicator: shouldShowProcessingIndicator(
      session,
      visibleTurns
    )
  };
}

function getTurn(
  turns: Map<string, WorkspaceAgentSessionDetailTurn>,
  id: string
): WorkspaceAgentSessionDetailTurn {
  const existing = turns.get(id);
  if (existing) {
    return existing;
  }
  const turn: WorkspaceAgentSessionDetailTurn = {
    id,
    userMessage: null,
    userMessages: [],
    agentMessages: [],
    toolCalls: [],
    toolCallCount: 0,
    hasFailedToolCall: false,
    agentItems: []
  };
  turns.set(id, turn);
  return turn;
}

function compareTimelineItemsAscending(
  left: WorkspaceAgentActivityTimelineItem,
  right: WorkspaceAgentActivityTimelineItem
): number {
  const leftSeq = left.seq ?? 0;
  const rightSeq = right.seq ?? 0;
  if (leftSeq > 0 && rightSeq > 0 && leftSeq !== rightSeq) {
    return leftSeq - rightSeq;
  }
  return (
    (left.occurredAtUnixMs ?? left.createdAtUnixMs ?? 0) -
      (right.occurredAtUnixMs ?? right.createdAtUnixMs ?? 0) ||
    leftSeq - rightSeq ||
    left.id - right.id
  );
}

function itemId(item: WorkspaceAgentActivityTimelineItem): string {
  const eventId = item.eventId?.trim();
  if (eventId) {
    return eventId;
  }
  const seq = item.seq ?? 0;
  return seq > 0 ? `seq:${seq}` : `id:${item.id}`;
}

function upsertToolCall(
  turn: WorkspaceAgentSessionDetailTurn,
  item: WorkspaceAgentActivityTimelineItem
): void {
  const call = toolCallView(item);
  const existingIndex = turn.toolCalls.findIndex(
    (existing) => existing.id === call.id
  );
  if (existingIndex >= 0) {
    const existing = turn.toolCalls[existingIndex];
    if (!existing) {
      return;
    }
    turn.toolCalls[existingIndex] = mergeToolCallDetail(existing, call);
  } else {
    turn.toolCalls.push(call);
  }
  turn.toolCallCount = turn.toolCalls.length;
  turn.hasFailedToolCall = turn.toolCalls.some(
    (existing) => existing.statusKind === "failed"
  );
  upsertToolCallAgentItem(turn, call, itemId(item));
}

function upsertToolCallAgentItem(
  turn: WorkspaceAgentSessionDetailTurn,
  call: WorkspaceAgentSessionDetailToolCall,
  sourceId: string
): void {
  for (const entry of turn.agentItems) {
    if (entry.kind !== "tool-calls") {
      continue;
    }
    const existingIndex = entry.toolCalls.findIndex(
      (existing) => existing.id === call.id
    );
    if (existingIndex >= 0) {
      const existing = entry.toolCalls[existingIndex];
      if (!existing) {
        continue;
      }
      entry.toolCalls[existingIndex] = mergeToolCallDetail(existing, call);
      refreshToolCallAgentItem(entry);
      return;
    }
  }

  const entry: WorkspaceAgentSessionDetailAgentItem = {
    kind: "tool-calls",
    id: `tools:${sourceId}`,
    toolCalls: [call],
    toolCallCount: 1,
    hasFailedToolCall: call.statusKind === "failed"
  };
  turn.agentItems.push(entry);
}

function refreshToolCallAgentItem(
  entry: Extract<WorkspaceAgentSessionDetailAgentItem, { kind: "tool-calls" }>
): void {
  entry.toolCallCount = entry.toolCalls.length;
  entry.hasFailedToolCall = entry.toolCalls.some(
    (existing) => existing.statusKind === "failed"
  );
  const summary = summarizeToolCallGroup(entry.toolCalls);
  if (summary) {
    entry.summary = summary;
  } else {
    delete entry.summary;
  }
}

function regroupAgentItems(
  items: readonly WorkspaceAgentSessionDetailAgentItem[]
): WorkspaceAgentSessionDetailAgentItem[] {
  const regrouped: WorkspaceAgentSessionDetailAgentItem[] = [];
  let pending: Array<
    Extract<WorkspaceAgentSessionDetailAgentItem, { kind: "tool-calls" }>
  > = [];

  const flushPending = () => {
    if (pending.length === 0) {
      return;
    }
    const groupedCalls = pending.flatMap((item) => item.toolCalls);
    if (groupedCalls.length >= 2) {
      regrouped.push({
        kind: "tool-calls",
        id: pending.map((item) => item.id).join("+"),
        toolCalls: groupedCalls,
        toolCallCount: groupedCalls.length,
        hasFailedToolCall: groupedCalls.some(
          (call) => call.statusKind === "failed"
        ),
        summary: summarizeToolCallGroup(groupedCalls),
        groupEntries:
          pending.flatMap<WorkspaceAgentSessionDetailToolGroupEntry>((item) =>
            item.toolCalls.map((call) => ({ kind: "tool-call", call }))
          )
      });
    } else {
      regrouped.push(...pending);
    }
    pending = [];
  };

  for (const item of items) {
    if (item.kind === "tool-calls" && isGroupableToolCallItem(item)) {
      pending.push(item);
      continue;
    }
    flushPending();
    regrouped.push(item);
  }

  flushPending();
  return regrouped;
}

function isGroupableToolCallItem(
  item: Extract<WorkspaceAgentSessionDetailAgentItem, { kind: "tool-calls" }>
): boolean {
  return (
    item.toolCalls.length === 1 &&
    item.toolCalls.every((call) => isGroupableToolCall(call))
  );
}

function mergeToolCallDetail(
  previous: WorkspaceAgentSessionDetailToolCall,
  next: WorkspaceAgentSessionDetailToolCall
): WorkspaceAgentSessionDetailToolCall {
  return withSourceTimelineItems(
    {
      ...next,
      name: next.name || previous.name,
      toolName: next.toolName || previous.toolName,
      callType: next.callType || previous.callType,
      summary: next.summary || previous.summary,
      payload: next.payload ?? previous.payload
    },
    mergeSourceTimelineItems(
      previous.sourceTimelineItems,
      next.sourceTimelineItems
    )
  );
}

function mergeBackgroundTerminalContinuations(
  turn: WorkspaceAgentSessionDetailTurn
): void {
  if (turn.agentItems.length === 0 || turn.toolCalls.length === 0) {
    return;
  }

  const removedCallIDs = new Set<string>();
  const singleCallItems = turn.agentItems.filter(
    (
      item
    ): item is Extract<
      WorkspaceAgentSessionDetailAgentItem,
      { kind: "tool-calls" }
    > => item.kind === "tool-calls" && item.toolCalls.length === 1
  );
  const turnToolIndexByID = new Map(
    turn.toolCalls.map((call, index) => [call.id, index])
  );

  for (let index = 0; index < singleCallItems.length - 1; index += 1) {
    const item = singleCallItems[index];
    const primaryCall = item?.toolCalls[0];
    if (!item || !primaryCall) {
      continue;
    }
    const terminalSessionID = backgroundTerminalSessionID(primaryCall);
    if (!terminalSessionID) {
      continue;
    }

    const continuationCall = singleCallItems[index + 1]?.toolCalls[0];
    if (
      !continuationCall ||
      !isBackgroundTerminalContinuation(continuationCall, terminalSessionID)
    ) {
      continue;
    }

    const mergedCall = mergeBackgroundTerminalCall(
      primaryCall,
      continuationCall
    );
    item.toolCalls[0] = mergedCall;
    const turnToolIndex = turnToolIndexByID.get(primaryCall.id);
    if (turnToolIndex !== undefined) {
      turn.toolCalls[turnToolIndex] = mergedCall;
    }
    refreshToolCallAgentItem(item);
    removedCallIDs.add(continuationCall.id);
    index += 1;
  }

  if (removedCallIDs.size === 0) {
    return;
  }

  turn.agentItems = turn.agentItems.filter(
    (item) =>
      item.kind !== "tool-calls" ||
      item.toolCalls.every((call) => !removedCallIDs.has(call.id))
  );
  turn.toolCalls = turn.toolCalls.filter(
    (call) => !removedCallIDs.has(call.id)
  );
  turn.toolCallCount = turn.toolCalls.length;
  turn.hasFailedToolCall = turn.toolCalls.some(
    (existing) => existing.statusKind === "failed"
  );
}

function mergeBackgroundTerminalCall(
  primary: WorkspaceAgentSessionDetailToolCall,
  continuation: WorkspaceAgentSessionDetailToolCall
): WorkspaceAgentSessionDetailToolCall {
  const continuationOutput = normalizedPayload(
    continuation.payload
      ?.output as WorkspaceAgentActivityTimelineItem["payload"]
  );
  const mergedPayload = primary.payload ? { ...primary.payload } : {};
  if (continuationOutput) {
    mergedPayload.output = continuationOutput;
  }
  return {
    ...primary,
    status: continuation.status || primary.status,
    statusKind: continuation.statusKind || primary.statusKind,
    payload:
      Object.keys(mergedPayload).length > 0 ? mergedPayload : primary.payload,
    occurredAtUnixMs: continuation.occurredAtUnixMs ?? primary.occurredAtUnixMs
  };
}

interface DelegatedToolCalls {
  parentCalls: Map<string, WorkspaceAgentSessionDetailToolCall>;
  callTurnByID: Map<string, WorkspaceAgentSessionDetailTurn>;
  childCallsByParent: Map<string, WorkspaceAgentSessionDetailToolCall[]>;
}

/** Calls that belong under a task's call, possibly in another turn; null when there are none. */
function findDelegatedToolCalls(
  turns: readonly WorkspaceAgentSessionDetailTurn[]
): DelegatedToolCalls | null {
  if (turns.length === 0) {
    return null;
  }
  const parentCalls = new Map<string, WorkspaceAgentSessionDetailToolCall>();
  const callTurnByID = new Map<string, WorkspaceAgentSessionDetailTurn>();
  for (const turn of turns) {
    for (const call of turn.toolCalls) {
      parentCalls.set(call.id, call);
      parentCalls.set(call.id.replace(/^call:/, ""), call);
      callTurnByID.set(call.id, turn);
      callTurnByID.set(call.id.replace(/^call:/, ""), turn);
    }
  }
  const childCallsByParent = new Map<
    string,
    WorkspaceAgentSessionDetailToolCall[]
  >();
  for (const turn of turns) {
    for (const call of turn.toolCalls) {
      const parentToolUseId = parentToolUseIdFromCall(call);
      if (!parentToolUseId) {
        continue;
      }
      const parentCall = parentCalls.get(parentToolUseId);
      if (!parentCall || !isTaskLikeToolCall(parentCall)) {
        continue;
      }
      const children = childCallsByParent.get(parentToolUseId) ?? [];
      children.push(call);
      childCallsByParent.set(parentToolUseId, children);
    }
  }
  return childCallsByParent.size > 0
    ? { parentCalls, callTurnByID, childCallsByParent }
    : null;
}

function nestDelegatedToolCalls({
  parentCalls,
  callTurnByID,
  childCallsByParent
}: DelegatedToolCalls): void {
  const removeIDsByTurn = new Map<
    WorkspaceAgentSessionDetailTurn,
    Set<string>
  >();
  for (const [parentID, childCalls] of childCallsByParent.entries()) {
    const parentCall = parentCalls.get(parentID);
    if (!parentCall) {
      continue;
    }
    appendDelegatedToolSteps(parentCall, childCalls);
    for (const childCall of childCalls) {
      const childTurn = callTurnByID.get(childCall.id);
      if (!childTurn) {
        continue;
      }
      const removeIDs = removeIDsByTurn.get(childTurn) ?? new Set<string>();
      removeIDs.add(childCall.id);
      removeIDsByTurn.set(childTurn, removeIDs);
    }
  }
  for (const [turn, removeIDs] of removeIDsByTurn.entries()) {
    pruneTurnToolCalls(turn, removeIDs);
  }
}

function backgroundTerminalSessionID(
  call: WorkspaceAgentSessionDetailToolCall
): string | null {
  const outputText = stringRecordValue(call.payload?.output, "output");
  if (!outputText) {
    return null;
  }
  const match = outputText.match(/Process running with session ID (\d+)/i);
  return match?.[1] ?? null;
}

function isBackgroundTerminalContinuation(
  call: WorkspaceAgentSessionDetailToolCall,
  sessionID: string
): boolean {
  const input = normalizedPayload(
    call.payload?.input as WorkspaceAgentActivityTimelineItem["payload"]
  );
  const inputSessionID = firstPresentString(
    stringRecordValue(input, "session_id"),
    stringRecordValue(input, "sessionId")
  );
  if (inputSessionID !== sessionID) {
    return false;
  }
  const chars = firstPresentString(stringRecordValue(input, "chars")) ?? "";
  if (chars !== "") {
    return false;
  }
  return stringRecordValue(call.payload?.output, "output") !== null;
}

function isGroupableToolCall(
  call: WorkspaceAgentSessionDetailToolCall
): boolean {
  if (
    call.callType === "approval" ||
    call.callType === "interactive" ||
    call.callType === "subagent"
  ) {
    return false;
  }
  switch (normalizeToolName(call.toolName)) {
    case "askuserquestion":
    case "enterplanmode":
    case "exitplanmode":
    case "task":
    case "subagent":
    case "delegatetask":
      return false;
    default:
      return true;
  }
}

function pruneTurnToolCalls(
  turn: WorkspaceAgentSessionDetailTurn,
  removeIDs: ReadonlySet<string>
): void {
  if (removeIDs.size === 0) {
    return;
  }
  turn.toolCalls = turn.toolCalls.filter((call) => !removeIDs.has(call.id));
  turn.toolCallCount = turn.toolCalls.length;
  turn.hasFailedToolCall = turn.toolCalls.some(
    (existing) => existing.statusKind === "failed"
  );
  turn.agentItems =
    turn.agentItems.flatMap<WorkspaceAgentSessionDetailAgentItem>((item) => {
      if (item.kind !== "tool-calls") {
        return [item];
      }
      const toolCalls = item.toolCalls.filter(
        (call) => !removeIDs.has(call.id)
      );
      if (toolCalls.length === 0) {
        return [];
      }
      const nextItem = {
        ...item,
        toolCalls
      };
      refreshToolCallAgentItem(nextItem);
      return [nextItem];
    });
}

function appendDelegatedToolSteps(
  parentCall: WorkspaceAgentSessionDetailToolCall,
  childCalls: readonly WorkspaceAgentSessionDetailToolCall[]
): void {
  const nextPayload = parentCall.payload ? { ...parentCall.payload } : {};
  const nextMetadata =
    normalizedPayload(
      nextPayload.metadata as WorkspaceAgentActivityTimelineItem["payload"]
    ) ?? {};
  const existingSteps = Array.isArray(nextMetadata.steps)
    ? [...nextMetadata.steps]
    : [];
  const existingStepIDs = new Set(
    existingSteps
      .map((step) =>
        normalizedPayload(step as WorkspaceAgentActivityTimelineItem["payload"])
      )
      .map(
        (step) =>
          stringRecordValue(step, "toolUseId") ?? stringRecordValue(step, "id")
      )
      .filter((value): value is string => Boolean(value))
  );
  for (const childCall of [...childCalls].sort(compareToolCallsAscending)) {
    const step = delegatedToolStepFromCall(childCall);
    const stepID =
      stringRecordValue(step, "toolUseId") ?? stringRecordValue(step, "id");
    if (stepID && existingStepIDs.has(stepID)) {
      continue;
    }
    if (stepID) {
      existingStepIDs.add(stepID);
    }
    existingSteps.push(step);
  }
  nextMetadata.steps = existingSteps;
  nextPayload.metadata = nextMetadata;
  parentCall.payload = nextPayload;
}
