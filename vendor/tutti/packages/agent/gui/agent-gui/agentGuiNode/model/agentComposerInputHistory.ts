import type { AgentPromptContentBlock } from "../../../shared/contracts/dto";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { AgentComposerDraft } from "./agentGuiNodeTypes";
import {
  agentComposerDraftHasContent,
  agentPromptContentToComposerDraft,
  buildAgentComposerDraft,
  emptyAgentComposerDraft
} from "./agentComposerDraft";

export interface AgentComposerInputHistoryEntry {
  id: string;
  draft: AgentComposerDraft;
}

export interface AgentComposerInputHistoryState {
  entryId: string | null;
  pendingOlderPage: boolean;
}

export interface AgentComposerInputHistoryNavigation {
  draft?: AgentComposerDraft;
  handled: boolean;
  requestOlderPage: boolean;
  state: AgentComposerInputHistoryState;
}

export const EMPTY_AGENT_COMPOSER_INPUT_HISTORY_STATE: AgentComposerInputHistoryState =
  {
    entryId: null,
    pendingOlderPage: false
  };

const inputHistoryByConversation = new WeakMap<
  AgentConversationVM,
  AgentComposerInputHistoryEntry[]
>();

interface ProjectedHistoryEntry {
  entry: AgentComposerInputHistoryEntry;
  occurredAtUnixMs: number | null;
  sequence: number;
}

export function projectAgentComposerInputHistory(
  conversation: AgentConversationVM | null
): AgentComposerInputHistoryEntry[] {
  if (!conversation) {
    return [];
  }
  const cached = inputHistoryByConversation.get(conversation);
  if (cached) {
    return cached;
  }
  const projectedEntries: ProjectedHistoryEntry[] = [];
  let sequence = 0;
  const append = (
    entry: AgentComposerInputHistoryEntry | null,
    occurredAtUnixMs?: number | null
  ): void => {
    if (!entry) {
      return;
    }
    projectedEntries.push({
      entry,
      occurredAtUnixMs: occurredAtUnixMs ?? null,
      sequence: sequence++
    });
  };

  const entries: AgentComposerInputHistoryEntry[] = [];
  for (const turn of conversation.sourceDetail.turns) {
    for (const message of turn.userMessages) {
      append(projectHistoryEntry(message, turn.id), message.occurredAtUnixMs);
    }
  }

  projectedEntries.sort(compareProjectedHistoryEntries);
  for (const { entry } of projectedEntries) {
    const previous = entries.at(-1);
    if (
      previous &&
      historyEntryDuplicateKey(previous) === historyEntryDuplicateKey(entry)
    ) {
      // Keep the newest duplicate id. Prepending an older page then cannot
      // invalidate a cursor that already points at the loaded copy.
      entries[entries.length - 1] = entry;
    } else {
      entries.push(entry);
    }
  }
  inputHistoryByConversation.set(conversation, entries);
  return entries;
}

export function navigateAgentComposerInputHistory(input: {
  currentDraft: AgentComposerDraft;
  direction: "older" | "newer";
  entries: readonly AgentComposerInputHistoryEntry[];
  hasOlderPage: boolean;
  state: AgentComposerInputHistoryState;
}): AgentComposerInputHistoryNavigation {
  let currentIndex = input.state.entryId
    ? input.entries.findIndex((entry) => entry.id === input.state.entryId)
    : -1;
  if (
    input.state.entryId &&
    (currentIndex < 0 ||
      !areAgentComposerHistoryDraftsEqual(
        input.currentDraft,
        input.entries[currentIndex]!.draft
      ))
  ) {
    if (agentComposerDraftHasContent(input.currentDraft)) {
      return unhandledHistoryNavigation();
    }
    currentIndex = -1;
  }

  if (currentIndex < 0) {
    if (
      input.direction === "newer" ||
      agentComposerDraftHasContent(input.currentDraft)
    ) {
      return unhandledHistoryNavigation();
    }
    const latest = input.entries.at(-1);
    if (latest) {
      return recalledHistoryNavigation(latest);
    }
    if (input.hasOlderPage) {
      return olderPageHistoryNavigation(null);
    }
    return unhandledHistoryNavigation();
  }

  if (input.direction === "older") {
    const older = input.entries[currentIndex - 1];
    if (older) {
      return recalledHistoryNavigation(older);
    }
    if (input.hasOlderPage) {
      return olderPageHistoryNavigation(input.state.entryId);
    }
    return {
      handled: false,
      requestOlderPage: false,
      state: { entryId: input.state.entryId, pendingOlderPage: false }
    };
  }

  const newer = input.entries[currentIndex + 1];
  if (newer) {
    return recalledHistoryNavigation(newer);
  }
  return {
    draft: emptyAgentComposerDraft(),
    handled: true,
    requestOlderPage: false,
    state: EMPTY_AGENT_COMPOSER_INPUT_HISTORY_STATE
  };
}

export function resolvePendingAgentComposerInputHistory(input: {
  entries: readonly AgentComposerInputHistoryEntry[];
  state: AgentComposerInputHistoryState;
}): AgentComposerInputHistoryNavigation | null {
  if (!input.state.pendingOlderPage) {
    return null;
  }
  if (input.state.entryId === null) {
    const latest = input.entries.at(-1);
    return latest
      ? recalledHistoryNavigation(latest)
      : {
          handled: false,
          requestOlderPage: false,
          state: EMPTY_AGENT_COMPOSER_INPUT_HISTORY_STATE
        };
  }
  const currentIndex = input.entries.findIndex(
    (entry) => entry.id === input.state.entryId
  );
  const older = currentIndex > 0 ? input.entries[currentIndex - 1] : null;
  return older
    ? recalledHistoryNavigation(older)
    : {
        handled: false,
        requestOlderPage: false,
        state: { entryId: input.state.entryId, pendingOlderPage: false }
      };
}

export function areAgentComposerHistoryDraftsEqual(
  left: AgentComposerDraft,
  right: AgentComposerDraft
): boolean {
  return historyDraftValue(left) === historyDraftValue(right);
}

function projectHistoryEntry(
  message: AgentConversationVM["sourceDetail"]["turns"][number]["userMessages"][number],
  fallbackTurnId: string
): AgentComposerInputHistoryEntry | null {
  const sourceItem = message.sourceTimelineItems?.find((item) =>
    Array.isArray(item.payload?.content)
  );
  const rawContent = Array.isArray(sourceItem?.payload?.content)
    ? sourceItem.payload.content
    : [];
  const content = rawContent.filter((block): block is AgentPromptContentBlock =>
    Boolean(block && typeof block === "object" && !Array.isArray(block))
  );
  const displayPrompt =
    message.sourceTimelineItems
      ?.map((item) =>
        typeof item.payload?.displayPrompt === "string"
          ? item.payload.displayPrompt.trim()
          : ""
      )
      .find(Boolean) ?? "";
  const visibleDisplayPrompt = isSyntheticImageOnlyDisplayPrompt(
    displayPrompt,
    rawContent
  )
    ? ""
    : displayPrompt;
  const draft =
    content.length > 0
      ? agentPromptContentToComposerDraft(
          content,
          `history-${message.turnId ?? fallbackTurnId}-${message.id}`
        )
      : buildAgentComposerDraft({
          prompt: visibleDisplayPrompt || message.body
        });
  if (!agentComposerDraftHasContent(draft)) {
    return null;
  }
  const entry = {
    id: `${message.turnId ?? fallbackTurnId}:${message.id}`,
    draft
  };
  historyDuplicateKeyByEntry.set(
    entry,
    JSON.stringify({
      content,
      prompt: visibleDisplayPrompt || (content.length === 0 ? message.body : "")
    })
  );
  return entry;
}

function compareProjectedHistoryEntries(
  left: ProjectedHistoryEntry,
  right: ProjectedHistoryEntry
): number {
  if (
    left.occurredAtUnixMs !== null &&
    right.occurredAtUnixMs !== null &&
    left.occurredAtUnixMs !== right.occurredAtUnixMs
  ) {
    return left.occurredAtUnixMs - right.occurredAtUnixMs;
  }
  return left.sequence - right.sequence;
}

const historyDuplicateKeyByEntry = new WeakMap<
  AgentComposerInputHistoryEntry,
  string
>();

function historyEntryDuplicateKey(
  entry: AgentComposerInputHistoryEntry
): string {
  const cached = historyDuplicateKeyByEntry.get(entry);
  if (cached) {
    return cached;
  }
  const key = historyDraftValueWithOptions(entry.draft, true);
  historyDuplicateKeyByEntry.set(entry, key);
  return key;
}

function isSyntheticImageOnlyDisplayPrompt(
  displayPrompt: string,
  content: readonly unknown[]
): boolean {
  if (displayPrompt !== "[Image]" && displayPrompt !== "[Images]") {
    return false;
  }
  let imageCount = 0;
  for (const raw of content) {
    const block =
      raw && typeof raw === "object" && !Array.isArray(raw)
        ? (raw as Record<string, unknown>)
        : null;
    if (!block) {
      continue;
    }
    if (
      block.type === "text" &&
      typeof block.text === "string" &&
      block.text.trim()
    ) {
      return false;
    }
    if (block.type === "image") {
      imageCount += 1;
    }
  }
  return displayPrompt === "[Image]" ? imageCount === 1 : imageCount > 1;
}

function recalledHistoryNavigation(
  entry: AgentComposerInputHistoryEntry
): AgentComposerInputHistoryNavigation {
  return {
    draft: entry.draft,
    handled: true,
    requestOlderPage: false,
    state: { entryId: entry.id, pendingOlderPage: false }
  };
}

function olderPageHistoryNavigation(
  entryId: string | null
): AgentComposerInputHistoryNavigation {
  return {
    handled: true,
    requestOlderPage: true,
    state: { entryId, pendingOlderPage: true }
  };
}

function unhandledHistoryNavigation(): AgentComposerInputHistoryNavigation {
  return {
    handled: false,
    requestOlderPage: false,
    state: EMPTY_AGENT_COMPOSER_INPUT_HISTORY_STATE
  };
}

function historyDraftValue(draft: AgentComposerDraft): string {
  return historyDraftValueWithOptions(draft, false);
}

function historyDraftValueWithOptions(
  draft: AgentComposerDraft,
  ignoreGeneratedIds: boolean
): string {
  return JSON.stringify(
    draft.map((block) =>
      block.type === "image"
        ? {
            ...block,
            ...(ignoreGeneratedIds ? { id: "" } : {}),
            // Preview data is presentation-only and may arrive asynchronously.
            previewUrl: ""
          }
        : block.type === "file" && ignoreGeneratedIds
          ? { ...block, id: "" }
          : block
    )
  );
}
