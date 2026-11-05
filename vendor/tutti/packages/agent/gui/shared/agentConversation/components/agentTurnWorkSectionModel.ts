import type { AgentActivityTurn } from "@tutti-os/agent-activity-core";
import type { AgentGUIObservationGap } from "../../../types";
import type {
  AgentMessageContentVM,
  AgentMessageRowVM
} from "../contracts/agentMessageRowVM";
import type { AgentTranscriptTurnGroup } from "./agentTranscriptModel";

export type AgentTurnTiming =
  | {
      kind: "live";
      startedAtUnixMs: number;
      frozenAtUnixMs?: number | null;
      observationGapPresentationState?: NonNullable<
        AgentGUIObservationGap["presentationState"]
      >;
    }
  | { kind: "settled"; elapsedSeconds: number };

export type AgentTurnDuration =
  | { kind: "seconds"; seconds: number }
  | { kind: "minutes"; minutes: number }
  | { kind: "minutes-seconds"; minutes: number; seconds: number };

export type AgentTurnWorkSectionRow =
  AgentTranscriptTurnGroup["rows"][number] & {
    renderKey?: string;
  };

export interface AgentTurnWorkSectionSegment {
  kind: "visible" | "work";
  rows: AgentTurnWorkSectionRow[];
}

export interface AgentTurnWorkSectionModel {
  timing: AgentTurnTiming;
  leadingRows: AgentTurnWorkSectionRow[];
  sections: AgentTurnWorkSectionSegment[];
  collapseEligible: boolean;
}

/**
 * Selects at most one participant header per speaker in each presentation
 * turn. Completed collapsed turns prefer a visible message so the Agent header
 * does not disappear into the hidden work section.
 */
export function findParticipantHeaderRenderKeys(
  groups: readonly AgentTranscriptTurnGroup[],
  rowKeys: readonly string[],
  modelByGroupKey: ReadonlyMap<string, AgentTurnWorkSectionModel | null>,
  participantTurnIndexByRowIndex: ReadonlyMap<number, number>,
  groupRowOffsets?: ReadonlyMap<string, number>
): ReadonlySet<string> {
  const headerCandidateByTurn = new Map<
    number,
    Map<
      AgentMessageRowVM["speaker"],
      { renderKey: string; visibilityPriority: number }
    >
  >();

  for (const group of groups) {
    const model = modelByGroupKey.get(group.key);
    const renderedRows: ReadonlyArray<{
      entry: AgentTurnWorkSectionRow;
      visibilityPriority: number;
    }> = model
      ? [
          ...model.leadingRows.map((entry) => ({
            entry,
            visibilityPriority: 0
          })),
          ...model.sections.flatMap((section) =>
            section.rows.map((entry) => ({
              entry,
              visibilityPriority:
                model.collapseEligible && section.kind === "work" ? 1 : 0
            }))
          )
        ]
      : group.rows.map((entry) => ({ entry, visibilityPriority: 0 }));

    for (const { entry, visibilityPriority } of renderedRows) {
      const row = entry.row;
      if (row.kind !== "message" || row.messages.length === 0) {
        continue;
      }
      const rowIndex = (groupRowOffsets?.get(group.key) ?? 0) + entry.rowIndex;
      const participantTurnIndex =
        participantTurnIndexByRowIndex.get(rowIndex) ?? rowIndex;
      let candidateBySpeaker = headerCandidateByTurn.get(participantTurnIndex);
      if (!candidateBySpeaker) {
        candidateBySpeaker = new Map();
        headerCandidateByTurn.set(participantTurnIndex, candidateBySpeaker);
      }
      const currentCandidate = candidateBySpeaker.get(row.speaker);
      if (
        currentCandidate &&
        currentCandidate.visibilityPriority <= visibilityPriority
      ) {
        continue;
      }
      candidateBySpeaker.set(row.speaker, {
        renderKey: entry.renderKey ?? rowKeys[rowIndex] ?? row.id,
        visibilityPriority
      });
    }
  }

  return new Set(
    [...headerCandidateByTurn.values()].flatMap((candidateBySpeaker) =>
      [...candidateBySpeaker.values()].map((candidate) => candidate.renderKey)
    )
  );
}

interface AgentTurnWorkSectionOptions {
  collapseIntermediateAssistantReplies?: boolean;
  liveFrozenAtUnixMs?: number | null;
  liveObservationGapPresentationState?:
    | AgentGUIObservationGap["presentationState"]
    | null;
}

export function resolveAgentTurnTiming(
  turn: AgentActivityTurn | null | undefined,
  isActiveTurn: boolean,
  submittedAtUnixMs?: number | null
): AgentTurnTiming | null {
  if (!turn || !Number.isFinite(turn.startedAtUnixMs)) {
    return null;
  }
  const timingStartedAtUnixMs =
    Number.isFinite(submittedAtUnixMs) &&
    (submittedAtUnixMs as number) <= turn.startedAtUnixMs
      ? (submittedAtUnixMs as number)
      : turn.startedAtUnixMs;

  if (turn.phase !== "settled") {
    return isActiveTurn
      ? { kind: "live", startedAtUnixMs: timingStartedAtUnixMs }
      : null;
  }

  const endUnixMs = turn.settledAtUnixMs;
  if (
    !Number.isFinite(endUnixMs) ||
    (endUnixMs as number) < timingStartedAtUnixMs
  ) {
    return null;
  }

  return {
    kind: "settled",
    elapsedSeconds: Math.max(
      0,
      Math.floor(((endUnixMs as number) - timingStartedAtUnixMs) / 1_000)
    )
  };
}

export function formatAgentTurnDuration(
  elapsedSeconds: number
): AgentTurnDuration {
  const safeSeconds = Math.max(0, Math.floor(elapsedSeconds));
  if (safeSeconds < 60) {
    return { kind: "seconds", seconds: safeSeconds };
  }

  const minutes = Math.floor(safeSeconds / 60);
  const seconds = safeSeconds % 60;
  if (seconds === 0) {
    return { kind: "minutes", minutes };
  }
  return { kind: "minutes-seconds", minutes, seconds };
}

export function buildAgentTurnWorkSectionModel(
  group: AgentTranscriptTurnGroup,
  turn: AgentActivityTurn | null | undefined,
  isActiveTurn = false,
  options: AgentTurnWorkSectionOptions = {}
): AgentTurnWorkSectionModel | null {
  const leadingRowCount = countLeadingUserRows(group.rows);
  const leadingRows = group.rows.slice(0, leadingRowCount);
  const submittedAtUnixMs = resolveTurnSubmittedAtUnixMs(
    leadingRows,
    turn?.startedAtUnixMs
  );
  const timing = resolveAgentTurnTiming(turn, isActiveTurn, submittedAtUnixMs);
  if (!timing) {
    return null;
  }

  const finalTarget = findFinalAssistantTextTarget(group.rows);
  const sections = buildOrderedSections(
    group.rows,
    leadingRowCount,
    finalTarget,
    options.collapseIntermediateAssistantReplies === true
  );
  const hasHiddenWork = sections.some(
    (section) => section.kind === "work" && section.rows.length > 0
  );
  const collapseEligible =
    finalTarget !== null &&
    turn?.phase === "settled" &&
    turn.outcome === "completed" &&
    hasHiddenWork &&
    !groupContainsBlockingMessage(group) &&
    !group.rows.some(({ row }) => row.kind === "generated-image");

  return {
    timing:
      timing.kind === "live"
        ? {
            ...timing,
            ...(Number.isFinite(options.liveFrozenAtUnixMs) &&
            (options.liveFrozenAtUnixMs as number) >= timing.startedAtUnixMs
              ? { frozenAtUnixMs: options.liveFrozenAtUnixMs as number }
              : {}),
            ...(options.liveObservationGapPresentationState
              ? {
                  observationGapPresentationState:
                    options.liveObservationGapPresentationState
                }
              : {})
          }
        : timing,
    leadingRows,
    sections,
    collapseEligible
  };
}

function resolveTurnSubmittedAtUnixMs(
  leadingRows: readonly AgentTurnWorkSectionRow[],
  fallbackUnixMs: number | null | undefined
): number | null {
  const exactTimestamps = leadingRows.flatMap(({ row }) => {
    if (row.kind !== "message" || row.speaker !== "user") {
      return [];
    }
    return row.messages.flatMap((message) =>
      (message.sourceTimelineItems ?? []).flatMap((item) => {
        const value = item.payload?.clientSubmittedAtUnixMs;
        return validUnixMs(value) ? [value] : [];
      })
    );
  });
  const exact = minimumTimestamp(exactTimestamps);
  if (exact !== null) {
    return exact;
  }

  const projected = minimumTimestamp(
    leadingRows.flatMap(({ row }) =>
      row.kind === "message" && row.speaker === "user"
        ? [
            row.occurredAtUnixMs,
            ...row.messages.map((message) => message.occurredAtUnixMs)
          ]
        : []
    )
  );
  return projected ?? (validUnixMs(fallbackUnixMs) ? fallbackUnixMs : null);
}

function minimumTimestamp(
  values: readonly (number | null | undefined)[]
): number | null {
  const valid = values.filter(validUnixMs);
  return valid.length > 0 ? Math.min(...valid) : null;
}

function validUnixMs(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0;
}

function countLeadingUserRows(
  rows: readonly AgentTurnWorkSectionRow[]
): number {
  let count = 0;
  while (isUserMessageRow(rows[count]?.row)) {
    count += 1;
  }
  return count;
}

function buildOrderedSections(
  rows: readonly AgentTurnWorkSectionRow[],
  startIndex: number,
  finalTarget: { rowIndex: number; messageIndex: number } | null,
  collapseIntermediateAssistantReplies: boolean
): AgentTurnWorkSectionSegment[] {
  const sections: AgentTurnWorkSectionSegment[] = [];
  for (let rowIndex = startIndex; rowIndex < rows.length; rowIndex += 1) {
    const entry = rows[rowIndex]!;
    if (entry.row.kind === "message" && entry.row.speaker === "assistant") {
      if (!collapseIntermediateAssistantReplies) {
        appendAssistantMessageSections(sections, entry);
      } else if (finalTarget?.rowIndex === rowIndex) {
        appendFinalAssistantSections(sections, entry, finalTarget.messageIndex);
      } else {
        appendSectionRow(sections, "work", entry);
      }
      continue;
    }
    appendSectionRow(
      sections,
      isExplicitWorkRow(entry.row) ? "work" : "visible",
      entry
    );
  }
  return sections;
}

function appendFinalAssistantSections(
  sections: AgentTurnWorkSectionSegment[],
  sourceEntry: AgentTurnWorkSectionRow,
  finalMessageIndex: number
): void {
  const sourceRow = sourceEntry.row as AgentMessageRowVM;
  const messagesBeforeFinal = sourceRow.messages.slice(0, finalMessageIndex);
  const messagesAfterFinal = sourceRow.messages.slice(finalMessageIndex + 1);

  if (sourceRow.thinking.length > 0 || messagesBeforeFinal.length > 0) {
    appendSectionRow(sections, "work", {
      ...sourceEntry,
      renderKey: `${sourceRow.id}:turn-work-before`,
      row: cloneAssistantRow(sourceRow, messagesBeforeFinal, sourceRow.thinking)
    });
  }

  appendSectionRow(sections, "visible", {
    ...sourceEntry,
    renderKey: `${sourceRow.id}:turn-final`,
    row: cloneAssistantRow(
      sourceRow,
      [sourceRow.messages[finalMessageIndex]!],
      []
    )
  });

  if (messagesAfterFinal.length > 0) {
    appendSectionRow(sections, "work", {
      ...sourceEntry,
      renderKey: `${sourceRow.id}:turn-work-after`,
      row: cloneAssistantRow(sourceRow, messagesAfterFinal, [])
    });
  }
}

function appendAssistantMessageSections(
  sections: AgentTurnWorkSectionSegment[],
  sourceEntry: AgentTurnWorkSectionRow
): void {
  const sourceRow = sourceEntry.row as AgentMessageRowVM;
  const parts: Array<{
    kind: AgentTurnWorkSectionSegment["kind"];
    messages: AgentMessageContentVM[];
    thinking: AgentMessageRowVM["thinking"];
  }> = [];

  if (sourceRow.thinking.length > 0) {
    parts.push({ kind: "work", messages: [], thinking: sourceRow.thinking });
  }

  for (const message of sourceRow.messages) {
    const kind = isExplicitWorkMessage(message) ? "work" : "visible";
    const previous = parts.at(-1);
    if (previous?.kind === kind && previous.thinking.length === 0) {
      previous.messages.push(message);
      continue;
    }
    parts.push({ kind, messages: [message], thinking: [] });
  }

  if (parts.length === 0) {
    appendSectionRow(sections, "visible", sourceEntry);
    return;
  }

  if (parts.length === 1 && sourceRow.thinking.length === 0) {
    appendSectionRow(sections, parts[0]!.kind, sourceEntry);
    return;
  }

  parts.forEach((part, partIndex) => {
    appendSectionRow(sections, part.kind, {
      ...sourceEntry,
      renderKey: `${sourceRow.id}:turn-${part.kind}-${partIndex}`,
      row: cloneAssistantRow(sourceRow, part.messages, part.thinking)
    });
  });
}

function appendSectionRow(
  sections: AgentTurnWorkSectionSegment[],
  kind: AgentTurnWorkSectionSegment["kind"],
  row: AgentTurnWorkSectionRow
): void {
  const previous = sections.at(-1);
  if (previous?.kind === kind) {
    previous.rows.push(row);
    return;
  }
  sections.push({ kind, rows: [row] });
}

function findFinalAssistantTextTarget(
  rows: readonly AgentTurnWorkSectionRow[]
): { rowIndex: number; messageIndex: number } | null {
  for (let rowIndex = rows.length - 1; rowIndex >= 0; rowIndex -= 1) {
    const row = rows[rowIndex]?.row;
    if (row?.kind !== "message" || row.speaker !== "assistant") {
      continue;
    }
    for (
      let messageIndex = row.messages.length - 1;
      messageIndex >= 0;
      messageIndex -= 1
    ) {
      const message = row.messages[messageIndex];
      if (
        message?.isTurnFinalText &&
        message.body.trim() &&
        message.contentKind !== "image-grid" &&
        !message.visibleError &&
        !message.systemNotice
      ) {
        return { rowIndex, messageIndex };
      }
    }
  }
  return null;
}

function groupContainsBlockingMessage(
  group: AgentTranscriptTurnGroup
): boolean {
  return group.rows.some(
    ({ row }) =>
      row.kind === "message" &&
      row.messages.some((message) =>
        Boolean(
          message.visibleError ||
          (message.systemNotice && message.presentationKind === "content")
        )
      )
  );
}

function isExplicitWorkRow(row: AgentTurnWorkSectionRow["row"]): boolean {
  return row.kind === "tool-group" || row.kind === "processing";
}

function isExplicitWorkMessage(message: AgentMessageContentVM): boolean {
  return (
    message.presentationKind === "specific-progress" ||
    message.presentationKind === "turn-boundary"
  );
}

function isUserMessageRow(
  row: AgentTurnWorkSectionRow["row"] | undefined
): row is AgentMessageRowVM {
  return row?.kind === "message" && row.speaker === "user";
}

function cloneAssistantRow(
  source: AgentMessageRowVM,
  messages: AgentMessageContentVM[],
  thinking: AgentMessageRowVM["thinking"]
): AgentMessageRowVM {
  return {
    ...source,
    messages,
    thinking
  };
}
