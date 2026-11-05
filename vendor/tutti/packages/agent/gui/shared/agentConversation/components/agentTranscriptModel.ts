import { useLayoutEffect, useMemo, useRef } from "react";
import type { AgentConversationVM } from "../contracts/agentConversationVM";
import { agentTranscriptRowHasPresentationKind } from "../projection/agentTranscriptPresentation";
import { normalizeAgentTitleText } from "../../utils/agentTitleText.ts";

export interface AgentTranscriptTurnGroup {
  key: string;
  turnId: string | null;
  rows: Array<{
    row: AgentConversationVM["rows"][number];
    rowIndex: number;
  }>;
}

export interface AgentMessageLocatorItem {
  nodeIndex?: number;
  messageId?: string;
  hasAgentResponse: boolean;
  agentResponseKnown?: boolean;
  key: string;
  rowKey: string;
  turnGroupIndex: number;
  rowIndex: number;
  turnId?: string | null;
  summary: string;
}

export interface AgentParticipantTurnProjection {
  dividerRowIndexes: ReadonlySet<number>;
  turnIndexByRowIndex: ReadonlyMap<number, number>;
}

export function useEnteringTranscriptRows(
  rowKeys: string[],
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>
): ReadonlySet<string> {
  const previousKeysRef = useRef<Set<string> | null>(null);
  const previousKeys = previousKeysRef.current;
  const enteringRowKeys = new Set<string>();
  const entryKeys = rowKeys.map((key, index) => transcriptRowEntryKey(rows[index], key));

  if (previousKeys) {
    for (const [index, key] of entryKeys.entries()) {
      if (!previousKeys.has(key)) {
        enteringRowKeys.add(rowKeys[index]!);
      }
    }
  }

  useLayoutEffect(() => {
    previousKeysRef.current = new Set(entryKeys);
  }, [entryKeys]);

  return enteringRowKeys;
}

export function transcriptRowEntryKey(
  row: AgentConversationVM["rows"][number] | undefined,
  fallback: string
): string {
  if (row?.kind === "message" && row.speaker === "user") {
    for (const message of row.messages) {
      for (const item of message.sourceTimelineItems ?? []) {
        const submitId = item.payload?.clientSubmitId;
        // A durable acknowledgement replaces the echo of this same submission.
        if (typeof submitId === "string" && submitId.trim())
          return `client-submit:user:${submitId.trim()}`;
      }
    }
  }
  return fallback;
}

export function transcriptRowKey(
  row: AgentConversationVM["rows"][number]
): string {
  if (row.kind === "tool-group") {
    return row.expansionKey ?? row.id;
  }
  return row.id;
}

export function isAssistantParticipantContentRow(
  row: AgentConversationVM["rows"][number]
): boolean {
  switch (row.kind) {
    case "message":
      return row.speaker === "assistant";
    case "generated-image":
    case "processing":
    case "tool-group":
    case "turn-summary":
      return true;
  }
}

export function findLastMessageRowIndex(
  rows: readonly {
    row: AgentConversationVM["rows"][number];
    rowIndex: number;
  }[]
): number | null {
  for (let index = rows.length - 1; index >= 0; index -= 1) {
    const entry = rows[index];
    if (
      entry?.row.kind === "message" &&
      entry.row.speaker === "assistant" &&
      entry.row.messages.length > 0
    ) {
      return entry.rowIndex;
    }
  }
  return null;
}

export function buildAgentTranscriptTurnGroups(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>,
  rowKeys: ReadonlyArray<string>
): AgentTranscriptTurnGroup[] {
  const groups: AgentTranscriptTurnGroup[] = [];
  let currentGroup: AgentTranscriptTurnGroup | null = null;

  rows.forEach((row, rowIndex) => {
    const turnId = transcriptPresentationTurnId(
      rows,
      rowIndex,
      currentGroup?.turnId ?? null
    );
    if (!currentGroup || currentGroup.turnId !== turnId) {
      currentGroup = {
        key: turnId ?? `orphan:${rowKeys[rowIndex] ?? transcriptRowKey(row)}`,
        turnId,
        rows: []
      };
      groups.push(currentGroup);
    }

    currentGroup.rows.push({ row, rowIndex });
  });

  return groups;
}

function transcriptPresentationTurnId(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>,
  rowIndex: number,
  currentTurnId: string | null
): string | null {
  const rowTurnId = rows[rowIndex]?.turnId ?? null;
  if (rowTurnId || !currentTurnId) {
    return rowTurnId;
  }
  const nextTurnId =
    rows.slice(rowIndex + 1).find((candidate) => candidate.turnId !== null)
      ?.turnId ?? null;
  // A session-level row can occur chronologically inside a live Turn. Keep it
  // in that Turn's presentation group only when the next lifecycle-owned row
  // proves the surrounding Turn is unchanged; the row itself stays turnless.
  return nextTurnId === currentTurnId ? currentTurnId : null;
}

export function buildTurnGroupIndexByRowIndex(
  turnGroups: readonly AgentTranscriptTurnGroup[]
): ReadonlyMap<number, number> {
  const rowIndexToTurnGroupIndex = new Map<number, number>();
  turnGroups.forEach((group, groupIndex) => {
    group.rows.forEach(({ rowIndex }) => {
      rowIndexToTurnGroupIndex.set(rowIndex, groupIndex);
    });
  });
  return rowIndexToTurnGroupIndex;
}

export function buildUserMessageLocatorItems(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>,
  rowKeys: ReadonlyArray<string>,
  turnGroupIndexByRowIndex: ReadonlyMap<number, number>
): AgentMessageLocatorItem[] {
  const items: AgentMessageLocatorItem[] = [];
  rows.forEach((row, rowIndex) => {
    if (row.kind !== "message" || row.speaker !== "user") {
      return;
    }
    const summary = summarizeUserMessageRow(row);
    if (!summary) {
      return;
    }
    const rowKey = rowKeys[rowIndex] ?? transcriptRowKey(row);
    items.push({
      hasAgentResponse: hasAgentResponseForTurn(rows, row, rowIndex),
      key: `user-message:${rowKey}`,
      rowKey,
      turnGroupIndex: turnGroupIndexByRowIndex.get(rowIndex) ?? rowIndex,
      rowIndex,
      turnId: row.turnId ?? null,
      messageId: row.messages[0]?.sourceTimelineItems?.[0]?.eventId ?? row.messages[0]?.id,
      summary
    });
  });
  return items;
}

export function hasAgentResponseForTurn(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>,
  userRow: AgentConversationVM["rows"][number],
  userRowIndex: number
): boolean {
  const turnId = userRow.turnId ?? null;
  for (let index = userRowIndex + 1; index < rows.length; index += 1) {
    const row = rows[index];
    if (!row) {
      continue;
    }
    if (row.kind === "generated-image") {
      return !turnId || row.turnId === turnId;
    }
    if (row.kind !== "message") {
      continue;
    }
    if (row.speaker === "user") {
      return false;
    }
    if (turnId && row.turnId !== turnId) {
      return false;
    }
    if (row.speaker === "assistant") {
      return true;
    }
  }
  return false;
}

export function summarizeUserMessageRow(
  row: Extract<AgentConversationVM["rows"][number], { kind: "message" }>
): string {
  return normalizeLocatorSummary(
    row.messages
      .map((message) => {
        if (message.contentKind === "selected-text") {
          return message.selectedText?.texts.join(" ") ?? "";
        }
        return message.copyText ?? message.body;
      })
      .join(" ")
  );
}

export function normalizeLocatorSummary(value: string): string {
  return normalizeAgentTitleText(value);
}

export function escapeCssString(value: string): string {
  return value.replace(/["\\]/g, "\\$&");
}

export function findTurnDividerRowIndexes(
  turnIndexById: ReadonlyMap<string, number>,
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>
): ReadonlySet<number> {
  const dividerRowIndexes = new Set<number>();
  const previousTurnIds = new Set<string>();

  rows.forEach((row, rowIndex) => {
    const currentTurnId = row.turnId ?? null;
    if (!currentTurnId) {
      return;
    }

    const turnIndex = turnIndexById.get(currentTurnId) ?? -1;
    const previousTurnId = rows[rowIndex - 1]?.turnId ?? null;
    if (
      rowIndex > 0 &&
      turnIndex > 0 &&
      previousTurnId &&
      previousTurnId !== currentTurnId &&
      !agentTranscriptRowHasPresentationKind(
        rows[rowIndex - 1],
        "turn-boundary"
      ) &&
      !previousTurnIds.has(currentTurnId)
    ) {
      dividerRowIndexes.add(rowIndex);
    }

    previousTurnIds.add(currentTurnId);
  });

  return dividerRowIndexes;
}

/**
 * Participant-header presentation (Agent board session detail): a presentation
 * turn starts at each user message and continues until the next user message.
 * Canonical Turn ids deliberately do not participate because recovery or
 * provider continuation may split one visible reply across multiple Turns.
 */
export function buildAgentParticipantTurnProjection(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>
): AgentParticipantTurnProjection {
  const dividerRowIndexes = new Set<number>();
  const turnIndexByRowIndex = new Map<number, number>();
  let turnIndex = 0;

  rows.forEach((row, rowIndex) => {
    if (rowIndex > 0 && row.kind === "message" && row.speaker === "user") {
      turnIndex += 1;
      dividerRowIndexes.add(rowIndex);
    }
    turnIndexByRowIndex.set(rowIndex, turnIndex);
  });

  return {
    dividerRowIndexes,
    turnIndexByRowIndex
  };
}

/**
 * Participant-header presentation: standalone tool-group rows belong to the
 * work that produced the NEXT assistant message, so they attach to that
 * message (rendered inside its block) instead of sitting after the previous
 * one. Trailing tool rows with no following assistant message stay standalone.
 */
export function attachLeadingToolRowsToFollowingMessages(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>
): AgentConversationVM["rows"] {
  const result: AgentConversationVM["rows"] = [];
  let pendingToolRows: Extract<
    AgentConversationVM["rows"][number],
    { kind: "tool-group" }
  >[] = [];
  for (const row of rows) {
    if (row.kind === "tool-group") {
      pendingToolRows.push(row);
      continue;
    }
    if (row.kind === "message" && row.speaker === "assistant") {
      if (pendingToolRows.length > 0) {
        result.push({
          ...row,
          leadingToolRows: [...(row.leadingToolRows ?? []), ...pendingToolRows]
        });
        pendingToolRows = [];
        continue;
      }
      result.push(row);
      continue;
    }
    if (pendingToolRows.length > 0) {
      result.push(...pendingToolRows);
      pendingToolRows = [];
    }
    result.push(row);
  }
  result.push(...pendingToolRows);
  return result;
}

/**
 * Read hook owning the display-row projection for the transcript view: in
 * participant-header mode tool-group rows attach to the following assistant
 * message, presentation turns start at user messages, and row keys derive from
 * the same pass. Keeping the memoization in this model module (next to
 * `useEnteringTranscriptRows`) keeps the view component within the
 * degradation-check memoization budget.
 */
export function useAgentTranscriptDisplayRows(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>,
  participantHeadersEnabled: boolean
): {
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>;
  rowKeys: string[];
  participantTurnProjection: AgentParticipantTurnProjection | null;
} {
  return useMemo(() => {
    const displayRows = participantHeadersEnabled
      ? attachLeadingToolRowsToFollowingMessages(rows)
      : rows;
    return {
      rows: displayRows,
      rowKeys: displayRows.map(transcriptRowKey),
      participantTurnProjection: participantHeadersEnabled
        ? buildAgentParticipantTurnProjection(displayRows)
        : null
    };
  }, [rows, participantHeadersEnabled]);
}
