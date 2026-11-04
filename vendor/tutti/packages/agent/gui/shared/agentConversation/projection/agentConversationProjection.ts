import type {
  WorkspaceAgentSessionDetailTurn,
  WorkspaceAgentSessionDetailViewModel
} from "../../workspaceAgentSessionDetailViewModel";
import { extractImageGenerationPreview } from "../../imageGenerationTool";
import type { AgentConversationVM } from "../contracts/agentConversationVM";
import type { AgentGeneratedImageRowVM } from "../contracts/agentGeneratedImageRowVM";
import type {
  AgentMessageContentVM,
  AgentMessageRowVM
} from "../contracts/agentMessageRowVM";
import type { AgentToolCallVM } from "../contracts/agentToolCallVM";
import type { AgentTranscriptRowVM } from "../contracts/agentTranscriptRowVM";
import { projectAgentMessageFinalText } from "./agentMessageFinalTextProjection";
import { computeAgentToolGroups } from "./agentToolGroupingProjection";
import { buildAgentTurnSequenceItems } from "./agentTurnSequenceProjection";
import { projectTurnRows } from "./agentTurnRowProjection";
import { projectAgentProcessingRow } from "./agentProcessingProjection";
import { projectAgentTurnSummaryRows } from "./agentTurnSummaryProjection";

export interface AgentConversationProjectionOptions {
  avoidGroupingEdits?: boolean;
}

const RENDER_IRRELEVANT_TRANSCRIPT_ROW_FIELDS = new Set(["occurredAtUnixMs"]);
// Codex has emitted both the older "2%" wording and the current wording
// without a percentage; the percentage is diagnostic context, not identity.
const CODEX_SKILLS_CONTEXT_BUDGET_NOTICE_PATTERN =
  /skill descriptions were shortened to fit the(?:\s+\d+(?:\.\d+)?%)?\s+skills context budget/i;
const CODEX_MODEL_METADATA_FALLBACK_NOTICE_FRAGMENT =
  "defaulting to fallback metadata";
const MARKDOWN_IMAGE_PATTERN =
  /!\[[^\]]*]\(\s*(?:<([^>]+)>|([^)\s]+))(?:\s+["'][^"']*["'])?\s*\)/g;

export function projectAgentConversationVM(
  detail: WorkspaceAgentSessionDetailViewModel,
  options: AgentConversationProjectionOptions = {}
): AgentConversationVM {
  const rows: AgentTranscriptRowVM[] = [];
  const turns = detail.turns;
  const summariesByTurnId = new Map(
    projectAgentTurnSummaryRows(detail).map((summary) => [
      summary.turnId,
      summary
    ])
  );

  turns.forEach((turn) => {
    rows.push(
      ...projectTurnConversationRows(turn, detail.session.workspaceId, {
        agentSessionId: detail.session.agentSessionId,
        avoidGroupingEdits: options.avoidGroupingEdits
      })
    );
    const summary = summariesByTurnId.get(turn.id);
    if (summary) rows.push(summary);
  });

  const normalizedRows = dropCodexDiagnosticNotices(
    dropRedundantErrorWarningNotices(
      mergeAdjacentAssistantMessageRows(
        dropRedundantCompactFailureEchoRows(
          mergeAdjacentTransportRetryNoticeRows(rows)
        )
      )
    )
  );
  const processing = projectAgentProcessingRow(detail, normalizedRows);
  const projectedRows = projectAgentMessageFinalText(
    processing ? [...normalizedRows, processing] : normalizedRows,
    detail
  );

  return {
    activity: detail.activity,
    workspaceRoot: detail.workspaceRoot,
    sourceDetail: detail,
    rows: projectedRows
  };
}

function dropRedundantCompactFailureEchoRows(
  rows: readonly AgentTranscriptRowVM[]
): AgentTranscriptRowVM[] {
  const filtered: AgentTranscriptRowVM[] = [];
  for (const row of rows) {
    const previous = filtered.at(-1);
    if (
      isSingleCompactFailureNoticeRow(previous) &&
      isSinglePlainAssistantMessageRow(row) &&
      previous.turnId === row.turnId &&
      previous.messages[0]?.systemNotice?.detail?.trim() ===
        row.messages[0]?.body.trim()
    ) {
      continue;
    }
    filtered.push(row);
  }
  return filtered;
}

function isSingleCompactFailureNoticeRow(
  row: AgentTranscriptRowVM | undefined
): row is AgentMessageRowVM {
  if (
    !row ||
    row.kind !== "message" ||
    row.speaker !== "assistant" ||
    row.thinking.length > 0 ||
    row.messages.length !== 1
  ) {
    return false;
  }
  const notice = row.messages[0]?.systemNotice;
  return (
    notice?.command === "compact" &&
    notice.commandStatus === "failed" &&
    Boolean(notice.detail?.trim())
  );
}

function isSinglePlainAssistantMessageRow(
  row: AgentTranscriptRowVM
): row is AgentMessageRowVM {
  return (
    row.kind === "message" &&
    row.speaker === "assistant" &&
    row.thinking.length === 0 &&
    row.messages.length === 1 &&
    !row.messages[0]?.visibleError &&
    !row.messages[0]?.systemNotice
  );
}

/**
 * Latest plain assistant reply in the projected transcript (newest first).
 * Used as the optional consult context; error cards, system notices, plan and
 * collaboration cards are skipped so only ordinary reply text qualifies.
 */
export function latestAssistantMessageText(
  conversation: Pick<AgentConversationVM, "rows"> | null | undefined
): string | null {
  if (!conversation) {
    return null;
  }
  for (let rowIndex = conversation.rows.length - 1; rowIndex >= 0; rowIndex--) {
    const row = conversation.rows[rowIndex];
    if (row?.kind !== "message" || row.speaker !== "assistant") {
      continue;
    }
    for (
      let messageIndex = row.messages.length - 1;
      messageIndex >= 0;
      messageIndex--
    ) {
      const message = row.messages[messageIndex];
      if (!message || message.visibleError || message.systemNotice) {
        continue;
      }
      if (message.contentKind && message.contentKind !== "text") {
        continue;
      }
      const body = message.body.trim();
      if (body) {
        return body;
      }
    }
  }
  return null;
}

export function reconcileProjectedAgentConversationVM(
  previous: AgentConversationVM | null | undefined,
  next: AgentConversationVM
): AgentConversationVM {
  if (!previous) {
    return next;
  }

  const previousRowsById = new Map(previous.rows.map((row) => [row.id, row]));
  let reusedRowCount = 0;
  const rows = next.rows.map((row) => {
    const previousRow = previousRowsById.get(row.id);
    if (previousRow && equivalentTranscriptRowForRender(previousRow, row)) {
      reusedRowCount += 1;
      return previousRow;
    }
    return row;
  });
  const rowsArrayReused =
    reusedRowCount === next.rows.length &&
    next.rows.length === previous.rows.length &&
    next.rows.every((_row, index) => rows[index] === previous.rows[index]);

  return {
    ...next,
    rows: rowsArrayReused ? previous.rows : rows
  };
}

function equivalentTranscriptRowForRender(
  previous: AgentTranscriptRowVM,
  next: AgentTranscriptRowVM
): boolean {
  return equivalentValueIgnoringKeys(
    previous,
    next,
    RENDER_IRRELEVANT_TRANSCRIPT_ROW_FIELDS
  );
}

function equivalentValueIgnoringKeys(
  left: unknown,
  right: unknown,
  ignoredKeys: ReadonlySet<string>
): boolean {
  if (Object.is(left, right)) {
    return true;
  }
  if (typeof left !== typeof right || left === null || right === null) {
    return false;
  }
  if (Array.isArray(left) || Array.isArray(right)) {
    return (
      Array.isArray(left) &&
      Array.isArray(right) &&
      left.length === right.length &&
      left.every((value, index) =>
        equivalentValueIgnoringKeys(value, right[index], ignoredKeys)
      )
    );
  }
  if (typeof left !== "object" || typeof right !== "object") {
    return false;
  }
  const leftRecord = left as Record<string, unknown>;
  const rightRecord = right as Record<string, unknown>;
  const leftKeys = Object.keys(leftRecord).filter(
    (key) => !ignoredKeys.has(key)
  );
  const rightKeys = Object.keys(rightRecord).filter(
    (key) => !ignoredKeys.has(key)
  );
  return (
    leftKeys.length === rightKeys.length &&
    leftKeys.every(
      (key) =>
        Object.prototype.hasOwnProperty.call(rightRecord, key) &&
        equivalentValueIgnoringKeys(
          leftRecord[key],
          rightRecord[key],
          ignoredKeys
        )
    )
  );
}

function mergeAdjacentAssistantMessageRows(
  rows: readonly AgentTranscriptRowVM[]
): AgentTranscriptRowVM[] {
  const merged: AgentTranscriptRowVM[] = [];
  for (const row of rows) {
    const previous = merged.at(-1);
    if (
      isMergeableAssistantMessageRow(previous) &&
      canMergeAdjacentAssistantMessageRows(previous, row)
    ) {
      // Rows are kept between projections, so a merge makes a new one.
      const messages = [...previous.messages];
      const lastMessage = messages.at(-1);
      const nextMessage = row.messages[0];
      if (lastMessage && nextMessage) {
        messages[messages.length - 1] = {
          ...lastMessage,
          body: joinAdjacentAssistantMessageBodies(
            lastMessage.body,
            nextMessage.body,
            nextMessage.statusKind
          ),
          statusKind:
            nextMessage.statusKind ??
            (lastMessage.statusKind === "working" ||
            lastMessage.statusKind === "waiting"
              ? null
              : lastMessage.statusKind) ??
            null,
          occurredAtUnixMs:
            nextMessage.occurredAtUnixMs ?? lastMessage.occurredAtUnixMs
        };
      }
      if (row.messages.length > 1) {
        messages.push(...row.messages.slice(1));
      }
      merged[merged.length - 1] = {
        ...previous,
        messages,
        occurredAtUnixMs: row.occurredAtUnixMs ?? previous.occurredAtUnixMs
      };
      continue;
    }
    merged.push(row);
  }
  return merged;
}

/**
 * Join adjacent assistant bodies without fusing independent Markdown blocks.
 * Active streaming fragments remain byte-for-byte continuations, while a
 * settled fragment gains a line boundary when neither side provides one.
 */
function joinAdjacentAssistantMessageBodies(
  previousBody: string,
  nextBody: string,
  nextStatusKind: AgentMessageContentVM["statusKind"]
): string {
  if (nextStatusKind === "working" || nextStatusKind === "waiting") {
    return previousBody + nextBody;
  }
  if (
    previousBody.endsWith("\n") ||
    nextBody.startsWith("\n") ||
    nextBody.startsWith("\r")
  ) {
    return previousBody + nextBody;
  }
  return `${previousBody}\n${nextBody}`;
}

function dropCodexDiagnosticNotices(
  rows: readonly AgentTranscriptRowVM[]
): AgentTranscriptRowVM[] {
  const filteredRows: AgentTranscriptRowVM[] = [];
  for (const row of rows) {
    if (row.kind !== "message" || row.speaker !== "assistant") {
      filteredRows.push(row);
      continue;
    }
    const messages = row.messages.filter(
      (message) => !isCodexDiagnosticNotice(message)
    );
    if (messages.length === 0 && row.thinking.length === 0) {
      continue;
    }
    if (messages.length === row.messages.length) {
      filteredRows.push(row);
      continue;
    }
    filteredRows.push({ ...row, messages });
  }
  return filteredRows;
}

function isCodexDiagnosticNotice(message: AgentMessageContentVM): boolean {
  const notice = message.systemNotice;
  if (!notice || notice.noticeKind !== "warning") {
    return false;
  }
  const text = [notice.title, notice.detail, message.body]
    .filter(Boolean)
    .join("\n")
    .toLowerCase();
  if (CODEX_SKILLS_CONTEXT_BUDGET_NOTICE_PATTERN.test(text)) {
    // Persisted skill notices may omit source, but an explicitly non-runtime
    // notice must remain visible even when its text matches this diagnostic.
    return !notice.source || notice.source === "runtime";
  }
  return (
    notice.source === "runtime" &&
    text.includes(CODEX_MODEL_METADATA_FALLBACK_NOTICE_FRAGMENT)
  );
}

function dropRedundantErrorWarningNotices(
  rows: readonly AgentTranscriptRowVM[]
): AgentTranscriptRowVM[] {
  const turnsWithVisibleError = new Set<string>();
  for (const row of rows) {
    if (row.kind !== "message" || row.speaker !== "assistant") {
      continue;
    }
    for (const message of row.messages) {
      if (message.visibleError) {
        turnsWithVisibleError.add(row.turnId);
      }
    }
  }
  if (turnsWithVisibleError.size === 0) {
    return [...rows];
  }

  const filteredRows: AgentTranscriptRowVM[] = [];
  for (const row of rows) {
    if (row.kind !== "message" || row.speaker !== "assistant") {
      filteredRows.push(row);
      continue;
    }
    if (!turnsWithVisibleError.has(row.turnId)) {
      filteredRows.push(row);
      continue;
    }
    const messages = row.messages.filter(
      (message) => !isRedundantErrorWarningNotice(message)
    );
    if (messages.length === 0) {
      continue;
    }
    if (messages.length === row.messages.length) {
      filteredRows.push(row);
      continue;
    }
    filteredRows.push({ ...row, messages });
  }
  return filteredRows;
}

function isRedundantErrorWarningNotice(
  message: AgentMessageContentVM
): boolean {
  const notice = message.systemNotice;
  if (!notice || notice.noticeKind !== "warning") {
    return false;
  }
  const title = notice.title?.trim() ?? "";
  return title === "Codex reported an error.";
}

function mergeAdjacentTransportRetryNoticeRows(
  rows: readonly AgentTranscriptRowVM[]
): AgentTranscriptRowVM[] {
  const merged: AgentTranscriptRowVM[] = [];
  for (const row of rows) {
    const previous = merged.at(-1);
    if (
      isSingleTransportRetryNoticeRow(previous) &&
      isSingleTransportRetryNoticeRow(row) &&
      previous.turnId === row.turnId
    ) {
      const previousMessage = previous.messages[0];
      const nextMessage = row.messages[0];
      if (!previousMessage || !nextMessage) {
        merged.push(row);
        continue;
      }
      const sourceTimelineItems = mergeSourceTimelineItems(
        previousMessage.sourceTimelineItems,
        nextMessage.sourceTimelineItems
      );
      merged[merged.length - 1] = {
        ...previous,
        messages: [
          {
            ...previousMessage,
            body: nextMessage.body || previousMessage.body,
            systemNotice:
              nextMessage.systemNotice ?? previousMessage.systemNotice,
            statusKind:
              nextMessage.statusKind ?? previousMessage.statusKind ?? null,
            occurredAtUnixMs:
              nextMessage.occurredAtUnixMs ?? previousMessage.occurredAtUnixMs,
            ...(sourceTimelineItems ? { sourceTimelineItems } : {})
          },
          ...previous.messages.slice(1)
        ],
        occurredAtUnixMs: row.occurredAtUnixMs ?? previous.occurredAtUnixMs
      };
      continue;
    }
    merged.push(row);
  }
  return merged;
}

function isSingleTransportRetryNoticeRow(
  row: AgentTranscriptRowVM | undefined
): row is AgentMessageRowVM {
  if (
    !row ||
    row.kind !== "message" ||
    row.speaker !== "assistant" ||
    row.thinking.length > 0 ||
    row.messages.length !== 1
  ) {
    return false;
  }
  return row.messages[0]?.systemNotice?.noticeKind === "transport_retry";
}

function mergeSourceTimelineItems(
  previous: AgentMessageContentVM["sourceTimelineItems"],
  next: AgentMessageContentVM["sourceTimelineItems"]
): AgentMessageContentVM["sourceTimelineItems"] {
  if (!previous || previous.length === 0) {
    return next;
  }
  if (!next || next.length === 0) {
    return previous;
  }
  return [...previous, ...next];
}

function isMergeableAssistantMessageRow(
  row: AgentTranscriptRowVM | undefined
): row is AgentMessageRowVM {
  return Boolean(
    row &&
    row.kind === "message" &&
    row.speaker === "assistant" &&
    row.thinking.length === 0 &&
    !row.messages.some(isSpecialAssistantMessage)
  );
}

function canMergeAdjacentAssistantMessageRows(
  previous: AgentMessageRowVM,
  next: AgentTranscriptRowVM
): next is AgentMessageRowVM {
  return (
    next.kind === "message" &&
    next.speaker === "assistant" &&
    next.thinking.length === 0 &&
    previous.turnId === next.turnId &&
    !next.messages.some(isSpecialAssistantMessage)
  );
}

function isSpecialAssistantMessage(message: {
  visibleError?: unknown;
  systemNotice?: unknown;
  contentKind?: string;
}): boolean {
  return Boolean(
    message.visibleError ||
    message.systemNotice ||
    message.contentKind === "plan" ||
    message.contentKind === "collaboration" ||
    message.contentKind === "tutti-plan-issue-link"
  );
}

/** A turn's rows, kept while the detail keeps the turn's object. */
const rowsByTurn = new WeakMap<
  WorkspaceAgentSessionDetailTurn,
  { key: string; rows: AgentTranscriptRowVM[] }
>();

function projectTurnConversationRows(
  turn: WorkspaceAgentSessionDetailTurn,
  workspaceId: string | null | undefined,
  options: {
    agentSessionId: string;
    avoidGroupingEdits?: boolean;
  }
): AgentTranscriptRowVM[] {
  const key = JSON.stringify([workspaceId, options.agentSessionId, options.avoidGroupingEdits]);
  const cached = rowsByTurn.get(turn);
  if (cached?.key === key) return cached.rows;
  const rows = buildTurnConversationRows(turn, workspaceId, options);
  rowsByTurn.set(turn, { key, rows });
  return rows;
}

function buildTurnConversationRows(
  turn: WorkspaceAgentSessionDetailTurn,
  workspaceId: string | null | undefined,
  options: {
    agentSessionId: string;
    avoidGroupingEdits?: boolean;
  }
): AgentTranscriptRowVM[] {
  const sequence = buildAgentTurnSequenceItems(turn, workspaceId);
  const { groups, groupedIndices, suppressedIndices } = computeAgentToolGroups(
    sequence,
    options
  );
  const skippedIndices = new Set([...groupedIndices, ...suppressedIndices]);
  return promoteGeneratedImageRows(
    projectTurnRows(
      sequence,
      groups,
      skippedIndices,
      turn.id,
      options.agentSessionId
    )
  );
}

function promoteGeneratedImageRows(
  rows: readonly AgentTranscriptRowVM[]
): AgentTranscriptRowVM[] {
  const promoted: AgentTranscriptRowVM[] = [];
  for (const row of rows) {
    promoted.push(row);
    if (row.kind !== "tool-group") {
      continue;
    }
    for (const call of row.calls) {
      const artifact = projectGeneratedImageRow(call, row.turnId);
      if (artifact) {
        promoted.push(artifact);
      }
    }
  }
  return dropDuplicateGeneratedImageMarkdown(promoted);
}

function dropDuplicateGeneratedImageMarkdown(
  rows: readonly AgentTranscriptRowVM[]
): AgentTranscriptRowVM[] {
  const generatedImagesByTurn = new Map<string, Set<string>>();
  for (const row of rows) {
    if (row.kind !== "generated-image") {
      continue;
    }
    const reference = normalizeGeneratedImageReference(row.uri);
    if (!reference) {
      continue;
    }
    const references = generatedImagesByTurn.get(row.turnId) ?? new Set();
    references.add(reference);
    generatedImagesByTurn.set(row.turnId, references);
  }
  if (generatedImagesByTurn.size === 0) {
    return [...rows];
  }

  const filtered: AgentTranscriptRowVM[] = [];
  for (const row of rows) {
    if (row.kind !== "message" || row.speaker !== "assistant") {
      filtered.push(row);
      continue;
    }
    const generatedImages = generatedImagesByTurn.get(row.turnId);
    if (!generatedImages) {
      filtered.push(row);
      continue;
    }
    let changed = false;
    const messages = row.messages.flatMap((message) => {
      const body = removeGeneratedImageMarkdown(message.body, generatedImages);
      if (body === message.body) {
        return [message];
      }
      changed = true;
      return body ? [{ ...message, body }] : [];
    });
    if (messages.length === 0 && row.thinking.length === 0) {
      continue;
    }
    filtered.push(changed ? { ...row, messages } : row);
  }
  return filtered;
}

function removeGeneratedImageMarkdown(
  body: string,
  generatedImages: ReadonlySet<string>
): string {
  let removed = false;
  const next = body.replace(
    MARKDOWN_IMAGE_PATTERN,
    (
      match,
      angleReference: string | undefined,
      plainReference: string | undefined
    ) => {
      const reference = normalizeGeneratedImageReference(
        angleReference ?? plainReference ?? ""
      );
      if (!reference || !generatedImages.has(reference)) {
        return match;
      }
      removed = true;
      return "";
    }
  );
  return removed
    ? next
        .replace(/[ \t]+\n/g, "\n")
        .replace(/\n{3,}/g, "\n\n")
        .trim()
    : body;
}

function normalizeGeneratedImageReference(reference: string): string | null {
  let normalized = reference.trim();
  if (!normalized) {
    return null;
  }
  if (/^file:\/\//i.test(normalized)) {
    if (!URL.canParse(normalized)) {
      return null;
    }
    normalized = new URL(normalized).pathname;
  } else if (/^[a-z][a-z\d+.-]*:\/\//i.test(normalized)) {
    return URL.canParse(normalized) ? new URL(normalized).href : normalized;
  }
  normalized = decodePercentEncodedReference(normalized);
  normalized = normalized.replaceAll("\\", "/");
  const prefix = normalized.startsWith("/") ? "/" : "";
  const parts: string[] = [];
  for (const part of normalized.split("/")) {
    if (!part || part === ".") {
      continue;
    }
    if (part === ".." && parts.length > 0) {
      parts.pop();
      continue;
    }
    parts.push(part);
  }
  const collapsed = `${prefix}${parts.join("/")}`;
  return /^[a-z]:\//i.test(collapsed) ? collapsed.toLowerCase() : collapsed;
}

function decodePercentEncodedReference(reference: string): string {
  return reference.replace(/(?:%[a-f\d]{2})+/gi, (encodedRun) => {
    const bytes = encodedRun
      .slice(1)
      .split("%")
      .map((hex) => Number.parseInt(hex, 16));
    return new TextDecoder().decode(Uint8Array.from(bytes));
  });
}

function projectGeneratedImageRow(
  call: AgentToolCallVM,
  turnId: string
): AgentGeneratedImageRowVM | null {
  if (
    call.rendererKind !== "image-generation" ||
    call.statusKind !== "completed"
  ) {
    return null;
  }
  const image = extractImageGenerationPreview({
    toolName: call.toolName,
    displayName: call.name,
    outputSavedPath: call.output?.savedPath,
    outputSavedPaths: call.output?.savedPaths,
    outputMimeType: call.output?.imageMimeType,
    outputText: call.output?.text,
    inputPrompt: call.input?.prompt,
    payloadInputPrompt:
      call.payload?.input &&
      typeof call.payload.input === "object" &&
      !Array.isArray(call.payload.input)
        ? (call.payload.input as Record<string, unknown>).prompt
        : null
  });
  if (!image.imageUri) {
    return null;
  }
  return {
    kind: "generated-image",
    id: `generated-image:${call.id}`,
    turnId,
    sourceCallId: call.id,
    uri: image.imageUri,
    mimeType: image.mimeType,
    prompt: image.prompt,
    occurredAtUnixMs: call.occurredAtUnixMs
  };
}
