import type { WorkspaceAgentSessionDetailTurn } from "../../workspaceAgentSessionDetailViewModel";
import { linkifyPastedTextReferences } from "../../pastedTextReferenceProjection";
import { parseTuttiModeCheckpointWake } from "../tuttiModeCheckpointWakeMarker";
import type {
  AgentMessageContentVM,
  AgentMessageRowVM
} from "../contracts/agentMessageRowVM";

export function conversationUserMessageRowKey(messageId: string): string {
  return `message:user:${messageId}`;
}

export function projectConversationUserRow(
  message: WorkspaceAgentSessionDetailTurn["userMessages"][number],
  fallbackTurnId: string,
  workspaceId: string | null | undefined
): AgentMessageRowVM {
  const turnId = message.turnId ?? fallbackTurnId;
  const rawFirstTextBlock = firstRawUserPromptTextBlock(message);
  return {
    kind: "message",
    id: conversationUserMessageRowKey(message.id),
    turnId,
    speaker: "user",
    rawFirstTextBlock,
    messages: projectUserMessageContentParts(
      message,
      turnId,
      workspaceId,
      rawFirstTextBlock
    ),
    thinking: [],
    occurredAtUnixMs: message.occurredAtUnixMs ?? null
  };
}

// A daemon checkpoint-wake prompt is detected by its agent-inert sentinel line
// (parseTuttiModeCheckpointWake), never by matching the prompt prose. When
// present we render a single compact summary card instead of the wall of text;
// the full prompt stays available behind the card's expand affordance.
function tuttiModeCheckpointWakePart(
  message: WorkspaceAgentSessionDetailTurn["userMessages"][number],
  turnId: string,
  rawFirstTextBlock: string | null
): AgentMessageContentVM | null {
  const parsed = parseTuttiModeCheckpointWake(rawFirstTextBlock);
  if (!parsed) {
    return null;
  }
  return {
    kind: "message-content",
    id: `${message.id}:checkpoint-wake`,
    turnId,
    body: parsed.body,
    presentationKind: "content",
    contentKind: "tutti-checkpoint-wake",
    checkpointWake: parsed.marker,
    copyText: parsed.body,
    occurredAtUnixMs: message.occurredAtUnixMs ?? null,
    sourceTimelineItems: message.sourceTimelineItems
  };
}

function firstRawUserPromptTextBlock(
  message: WorkspaceAgentSessionDetailTurn["userMessages"][number]
): string | null {
  for (const item of message.sourceTimelineItems ?? []) {
    const content = item.payload?.content;
    if (!Array.isArray(content)) {
      continue;
    }
    for (const raw of content) {
      const block = promptContentRecord(raw);
      if (
        block?.type === "text" &&
        typeof block.text === "string" &&
        block.kind !== "selected-text"
      ) {
        return block.text;
      }
    }
  }
  return null;
}

function projectUserMessageContentParts(
  message: WorkspaceAgentSessionDetailTurn["userMessages"][number],
  turnId: string,
  workspaceId: string | null | undefined,
  rawFirstTextBlock: string | null
): AgentMessageContentVM[] {
  const checkpointWake = tuttiModeCheckpointWakePart(
    message,
    turnId,
    rawFirstTextBlock
  );
  if (checkpointWake) {
    return [checkpointWake];
  }
  const blocks = userPromptContentBlocks(message, workspaceId);
  if (blocks.length === 0) {
    return [textPart(message, turnId)];
  }
  const parts: AgentMessageContentVM[] = [];
  const selectedTextBlocks = blocks.filter(
    (block): block is UserPromptTextBlock =>
      block.type === "text" && block.kind === "selected-text"
  );
  if (selectedTextBlocks.length > 0) {
    // Reference context is a presentation-only part. The ordinary text part
    // remains the sole editable/copyable user message, matching the composer
    // question bubble while keeping the selected boundary visible.
    parts.push({
      kind: "message-content",
      id: `${message.id}:selected-text`,
      turnId,
      body: "",
      presentationKind: "content",
      contentKind: "selected-text",
      selectedText: {
        count: selectedTextBlocks.length,
        texts: selectedTextBlocks.map((block) =>
          stripSelectedTextPrefix(block.text)
        )
      },
      occurredAtUnixMs: message.occurredAtUnixMs ?? null,
      sourceTimelineItems: message.sourceTimelineItems
    });
  }
  const imageBlocks = blocks.filter(
    (block): block is UserPromptImageBlock => block.type === "image"
  );
  if (imageBlocks.length > 0) {
    parts.push({
      kind: "message-content",
      id: `${message.id}:images:0`,
      turnId,
      body: "",
      presentationKind: "content",
      contentKind: "image-grid",
      images: imageBlocks.map((image, index) => ({
        id: `${message.id}:image:${index}`,
        workspaceId: image.workspaceId,
        agentSessionId: image.agentSessionId,
        attachmentId: image.attachmentId,
        mimeType: image.mimeType,
        name: image.name,
        data: image.data,
        url: image.url,
        path: image.path
      })),
      occurredAtUnixMs: message.occurredAtUnixMs ?? null,
      sourceTimelineItems: message.sourceTimelineItems
    });
  }
  blocks.forEach((block, index) => {
    if (
      block.type === "image" ||
      block.text.trim() === "" ||
      block.kind === "selected-text"
    ) {
      return;
    }
    parts.push({
      kind: "message-content",
      id: `${message.id}:text:${index}`,
      turnId,
      body: block.text,
      presentationKind: "content",
      contentKind: "text",
      occurredAtUnixMs: message.occurredAtUnixMs ?? null,
      sourceTimelineItems: message.sourceTimelineItems
    });
  });
  return parts.length > 0 ? parts : [textPart(message, turnId)];
}

function textPart(
  message: WorkspaceAgentSessionDetailTurn["userMessages"][number],
  turnId: string
): AgentMessageContentVM {
  return {
    kind: "message-content",
    id: message.id,
    turnId,
    body: message.body,
    presentationKind: "content",
    contentKind: "text",
    occurredAtUnixMs: message.occurredAtUnixMs ?? null,
    sourceTimelineItems: message.sourceTimelineItems
  };
}

type UserPromptContentBlock = UserPromptTextBlock | UserPromptImageBlock;

interface UserPromptTextBlock {
  type: "text";
  text: string;
  kind?: string;
}

interface UserPromptImageBlock {
  type: "image";
  workspaceId?: string | null;
  agentSessionId: string;
  attachmentId?: string | null;
  mimeType: string;
  name?: string | null;
  data?: string | null;
  url?: string | null;
  path?: string | null;
}

function userPromptContentBlocks(
  message: WorkspaceAgentSessionDetailTurn["userMessages"][number],
  fallbackWorkspaceId: string | null | undefined
): UserPromptContentBlock[] {
  const item = message.sourceTimelineItems?.find((candidate) =>
    Array.isArray(candidate.payload?.content)
  );
  const content = Array.isArray(item?.payload?.content)
    ? item.payload.content
    : null;
  if (!content) return [];
  const displayPrompt = firstString(
    message.sourceTimelineItems?.map((candidate) =>
      typeof candidate.payload?.displayPrompt === "string"
        ? candidate.payload.displayPrompt
        : ""
    ) ?? []
  );
  const selectedTextBlockIndexes = selectedTextContentBlockIndexes(
    content,
    displayPrompt
  );
  const visibleDisplayPrompt = isSyntheticImageOnlyDisplayPrompt(
    displayPrompt,
    content
  )
    ? ""
    : displayPrompt;
  // Older sessions kept draft-only `mention://composer-file/...` chips in
  // displayPrompt. Prefer the already-materialized provider text so transcript
  // clicks resolve through ordinary file locators.
  const preferMaterializedContentText =
    Boolean(visibleDisplayPrompt) &&
    visibleDisplayPrompt.includes("mention://composer-file/") &&
    content.some((raw) => {
      const block =
        raw && typeof raw === "object" && !Array.isArray(raw)
          ? (raw as Record<string, unknown>)
          : null;
      return (
        block?.type === "text" &&
        typeof block.text === "string" &&
        block.text.trim().length > 0
      );
    });
  const effectiveDisplayPrompt = preferMaterializedContentText
    ? ""
    : selectedTextBlockIndexes.size > 0
      ? ""
      : visibleDisplayPrompt;
  const blocks = content.flatMap(
    (raw, contentIndex): UserPromptContentBlock[] => {
      const block = promptContentRecord(raw);
      if (!block) return [];
      if (block.type === "text" && typeof block.text === "string") {
        const kind = selectedTextBlockIndexes.has(contentIndex)
          ? "selected-text"
          : typeof block.kind === "string"
            ? block.kind
            : undefined;
        return effectiveDisplayPrompt
          ? []
          : [
              {
                type: "text",
                text: linkifyPastedTextReferences(block.text),
                ...(kind ? { kind } : {})
              }
            ];
      }
      if (block.type !== "image") return [];
      const mimeType =
        typeof block.mimeType === "string" ? block.mimeType.trim() : "";
      return [
        {
          type: "image",
          workspaceId: item?.workspaceId ?? fallbackWorkspaceId ?? null,
          agentSessionId: item?.agentSessionId ?? message.id,
          attachmentId: optionalString(block.attachmentId),
          mimeType,
          name: optionalString(block.name),
          data: optionalString(block.data),
          url: optionalString(block.url),
          path: optionalString(block.path)
        }
      ];
    }
  );
  return effectiveDisplayPrompt
    ? [{ type: "text", text: effectiveDisplayPrompt }, ...blocks]
    : blocks;
}

function selectedTextContentBlockIndexes(
  content: readonly unknown[],
  displayPrompt: string
): ReadonlySet<number> {
  const result = new Set<number>();
  const textIndexes: number[] = [];
  for (const [index, raw] of content.entries()) {
    const block = promptContentRecord(raw);
    if (block?.type === "text" && typeof block.text === "string") {
      textIndexes.push(index);
    }
  }
  const firstNonQuoteTextOrdinal = textIndexes.findIndex((contentIndex) => {
    const block = promptContentRecord(content[contentIndex]);
    return (
      typeof block?.text === "string" &&
      block.text.trim() !== "" &&
      !isSelectedTextBlockquote(block.text)
    );
  });
  const normalizedDisplayPrompt = displayPrompt.replace(/\r\n?/gu, "\n");
  for (const [textOrdinal, contentIndex] of textIndexes.entries()) {
    const block = promptContentRecord(content[contentIndex]);
    const text = typeof block?.text === "string" ? block.text : "";
    if (block?.kind === "selected-text") {
      result.add(contentIndex);
      continue;
    }
    if (
      firstNonQuoteTextOrdinal < 0 ||
      textOrdinal <= firstNonQuoteTextOrdinal ||
      !isSelectedTextBlockquote(text) ||
      !normalizedDisplayPrompt.includes(text.replace(/\r\n?/gu, "\n").trim())
    ) {
      continue;
    }
    // The composer currently puts a non-quote typed prompt first and each
    // selected transcript quote after it. Ambiguous quote-only legacy payloads
    // stay ordinary Markdown; only an explicit kind can identify those.
    result.add(contentIndex);
  }
  return result;
}

function isSelectedTextBlockquote(text: string): boolean {
  const lines = text.trim().split(/\r?\n/u);
  return (
    lines.length > 0 && lines.every((line) => /^>\s?/u.test(line.trimStart()))
  );
}

function stripSelectedTextPrefix(text: string): string {
  return text
    .split(/\r?\n/u)
    .map((line) => line.replace(/^\s*>\s?/u, ""))
    .join("\n")
    .trim();
}

function promptContentRecord(raw: unknown): Record<string, unknown> | null {
  return raw && typeof raw === "object" && !Array.isArray(raw)
    ? (raw as Record<string, unknown>)
    : null;
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
    if (!block) continue;
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

function optionalString(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function firstString(values: readonly string[]): string {
  for (const value of values) {
    const trimmed = value.trim();
    if (trimmed) return trimmed;
  }
  return "";
}
