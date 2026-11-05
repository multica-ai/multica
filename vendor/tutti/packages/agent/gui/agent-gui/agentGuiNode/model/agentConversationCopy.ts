import type {
  AgentActivityMessage,
  AgentPromptContentBlock
} from "@tutti-os/agent-activity-core";
import type { ConversationServices } from "../../../conversationServices";
import { isLocalImagePath } from "../../../shared/imageGenerationTool";

// Per-image cap (binary bytes) for embedding into the hydrated text/html
// variant. Larger images keep the lean reference instead and are surfaced to
// the user through the omittedImages count.
export const AGENT_CONVERSATION_COPY_MAX_EMBEDDED_IMAGE_BYTES = 2 * 1024 * 1024;

export type AgentGUIConversationCopyAction = "copy-markdown" | "copy-reference";

export interface AgentGUIConversationCopyLabels {
  file: string;
  image: string;
  mentionPrefix: string;
  /** Template for the collapsed-interim summary; contains `{{count}}`. */
  previousMessages: string;
}

export interface AgentGUIConversationAttachment {
  data: string;
  mimeType: string;
  name?: string;
}

/** Hydrates a local workspace image path into base64 bytes at copy time. */
export type AgentGUIConversationLocalImageReader = (input: {
  path: string;
  mimeType: string | null;
}) => Promise<AgentGUIConversationAttachment>;

export interface AgentGUIConversationSerializedTranscript {
  /** Hydrated variant for the text/html flavor: images embed as data URIs. */
  hydratedMarkdown: string;
  /** Lean variant for text/plain: short image references, never base64. */
  markdown: string;
  /** Hydratable images left unembedded (oversized or failed reads). */
  omittedImages: number;
}

export async function loadCompleteAgentConversationMessages(input: {
  agentSessionId: string;
  runtime: Pick<ConversationServices, "conversations">;
  workspaceId: string;
}): Promise<AgentActivityMessage[]> {
  const messages = await input.runtime.conversations.loadAll({
    workspaceId: input.workspaceId,
    agentSessionId: input.agentSessionId
  });
  return [...messages].sort(compareMessagesAscending);
}

// Lean Codex-export-style transcript: user inputs and per-turn final agent
// replies are kept in full; interim agent narration collapses into one
// <details> block per assistant run; tool payloads are dropped except image
// outputs (session deliverables such as generated images), which are emitted
// plain; thinking, runtime system notices, and JSON fallbacks are dropped.
// One message walk accumulates two parallel variants that differ only on
// image targets: `markdown` (text/plain) keeps short references — source
// link, attachment:ID, or a bold label — and never carries base64, while
// `hydratedMarkdown` (rendered into the text/html flavor) embeds image bytes
// as data URIs from inline data, the runtime attachment store, and local
// workspace paths via the host reader, because rich-paste targets consume
// data URIs but cannot fetch local paths.
export async function serializeAgentConversationForClipboard(input: {
  labels: AgentGUIConversationCopyLabels;
  messages: readonly AgentActivityMessage[];
  readAttachment?: (
    attachmentId: string
  ) => Promise<AgentGUIConversationAttachment>;
  readLocalImage?: AgentGUIConversationLocalImageReader;
  title: string;
}): Promise<AgentGUIConversationSerializedTranscript> {
  const context: SerializeContext = {
    labels: input.labels,
    readAttachment: input.readAttachment,
    readLocalImage: input.readLocalImage,
    stats: { omittedImages: 0 }
  };
  const kept: {
    body: SerializedBody;
    isToolImage?: boolean;
    role: "assistant" | "user";
  }[] = [];
  for (const message of [...input.messages].sort(compareMessagesAscending)) {
    if (isSystemNoticeMessage(message)) {
      continue;
    }
    if (isToolMessage(message)) {
      const body = await serializeImagesOnly(message.payload ?? {}, context);
      if (body.plain) {
        kept.push({ body, isToolImage: true, role: "assistant" });
      }
      continue;
    }
    const role = keptMessageRole(message);
    if (!role) {
      continue;
    }
    const body = await serializeMessageContent(message.payload ?? {}, context);
    if (body.plain) {
      kept.push({ body, role });
    }
  }

  // Both variants assemble in lockstep from the same kept entries so the
  // grouping (blockquotes, details collapse, ordering) stays identical and
  // only image targets differ.
  const markdownBlocks: string[] = [`# ${singleLine(input.title)}`];
  const hydratedBlocks: string[] = [`# ${singleLine(input.title)}`];
  const pushBlock = (plain: string, hydrated: string): void => {
    markdownBlocks.push(plain);
    hydratedBlocks.push(hydrated);
  };
  let index = 0;
  while (index < kept.length) {
    const entry = kept[index]!;
    if (entry.role === "user") {
      pushBlock(
        blockquoteMarkdown(entry.body.plain),
        blockquoteMarkdown(entry.body.hydrated)
      );
      index += 1;
      continue;
    }
    let last = index;
    while (last + 1 < kept.length && kept[last + 1]!.role === "assistant") {
      last += 1;
    }
    const run = kept.slice(index, last + 1);
    const textEntries = run.filter((item) => !item.isToolImage);
    const interim = textEntries.slice(0, -1);
    const finalText = textEntries.at(-1);
    if (interim.length > 0) {
      const summary = input.labels.previousMessages.replace(
        "{{count}}",
        `${interim.length}`
      );
      const detailsBlock = (side: keyof SerializedBody): string =>
        [
          `<details><summary>${summary}</summary>`,
          ...interim.map((item) => blockquoteMarkdown(item.body[side])),
          "</details>"
        ].join("\n\n");
      pushBlock(detailsBlock("plain"), detailsBlock("hydrated"));
    }
    for (const item of run) {
      if (item.isToolImage || item === finalText) {
        pushBlock(item.body.plain, item.body.hydrated);
      }
    }
    index = last + 1;
  }

  return {
    hydratedMarkdown: hydratedBlocks.join("\n\n").trim(),
    markdown: markdownBlocks.join("\n\n").trim(),
    omittedImages: context.stats.omittedImages
  };
}

interface SerializeContext {
  labels: AgentGUIConversationCopyLabels;
  readAttachment?: (
    attachmentId: string
  ) => Promise<AgentGUIConversationAttachment>;
  readLocalImage?: AgentGUIConversationLocalImageReader;
  stats: { omittedImages: number };
}

// The plain/hydrated variants of one serialized fragment. Only image blocks
// diverge; every other block serializes identically on both sides.
interface SerializedBody {
  hydrated: string;
  plain: string;
}

function sameBody(value: string): SerializedBody {
  return { hydrated: value, plain: value };
}

function joinBodies(bodies: readonly SerializedBody[]): SerializedBody {
  return {
    hydrated: bodies
      .map((body) => body.hydrated)
      .filter(Boolean)
      .join("\n\n")
      .trim(),
    plain: bodies
      .map((body) => body.plain)
      .filter(Boolean)
      .join("\n\n")
      .trim()
  };
}

function keptMessageRole(
  message: AgentActivityMessage
): "assistant" | "user" | null {
  const role = normalizedRole(message.role);
  if (role === "user") {
    return "user";
  }
  if (role === "assistant" || role === "agent") {
    return "assistant";
  }
  return null;
}

// Single-pass .replace over newlines (not split/map/join) keeps peak memory
// low: bodies can carry large inline data-URI images.
function blockquoteMarkdown(value: string): string {
  const quoted = value.replace(/\n(.?)/g, (_match, next: string) =>
    next ? `\n> ${next}` : "\n>"
  );
  return `> ${quoted}`;
}

async function serializeMessageContent(
  payload: Record<string, unknown>,
  context: SerializeContext
): Promise<SerializedBody> {
  const content = payload.content;
  if (Array.isArray(content)) {
    const blocks = await Promise.all(
      content.map((block) => serializeContentValue(block, context))
    );
    const serialized = joinBodies(blocks);
    if (serialized.plain) {
      return serialized;
    }
  } else if (typeof content === "string" && content.trim()) {
    return sameBody(content.trim());
  }

  return sameBody(
    firstString(
      payload.displayPrompt,
      payload.text,
      payload.detail,
      payload.title,
      payload.summary
    ) ?? ""
  );
}

async function serializeContentValue(
  value: unknown,
  context: SerializeContext
): Promise<SerializedBody> {
  if (typeof value === "string") {
    return sameBody(value.trim());
  }
  if (!isRecord(value)) {
    return sameBody("");
  }

  if (value.type === "content" && value.content !== undefined) {
    return serializeContentValue(value.content, context);
  }

  const block = value as AgentPromptContentBlock & Record<string, unknown>;
  switch (block.type) {
    case "text":
      return sameBody(typeof block.text === "string" ? block.text.trim() : "");
    case "image":
      return serializeImageBlock(block, context);
    case "file":
      return sameBody(serializeFileBlock(block, context.labels));
    case "mention":
      return sameBody(serializeMentionBlock(block, context.labels));
    case "skill":
      return sameBody(serializeSkillBlock(block));
    default: {
      const nested = await Promise.all(
        [value.content, value.output, value.result]
          .filter((candidate) => candidate !== undefined)
          .map((candidate) => serializeContentValue(candidate, context))
      );
      return joinBodies(nested);
    }
  }
}

// Images-only projection of serializeContentValue: recurses through the same
// wrappers (arrays, {type:"content"} envelopes, and content/output/result of
// unknown records) but emits image blocks alone. Used for tool-call payloads,
// whose only copy-worthy content is a generated-image deliverable.
async function serializeImagesOnly(
  value: unknown,
  context: SerializeContext
): Promise<SerializedBody> {
  if (Array.isArray(value)) {
    const nested = await Promise.all(
      value.map((item) => serializeImagesOnly(item, context))
    );
    return joinBodies(nested);
  }
  if (!isRecord(value)) {
    return sameBody("");
  }

  if (value.type === "content" && value.content !== undefined) {
    return serializeImagesOnly(value.content, context);
  }

  const block = value as AgentPromptContentBlock & Record<string, unknown>;
  switch (block.type) {
    case "image":
      return serializeImageBlock(block, context);
    case "text":
    case "file":
    case "mention":
    case "skill":
      return sameBody("");
    default: {
      const nested = await Promise.all(
        [value.content, value.output, value.result]
          .filter((candidate) => candidate !== undefined)
          .map((candidate) => serializeImagesOnly(candidate, context))
      );
      return joinBodies(nested);
    }
  }
}

async function serializeImageBlock(
  block: AgentPromptContentBlock & Record<string, unknown>,
  context: SerializeContext
): Promise<SerializedBody> {
  const attachmentId = firstString(block.attachmentId);
  const blockMimeType = firstString(block.mimeType);
  const name = firstString(block.name) ?? context.labels.image;
  const source = firstString(block.url, block.uri, block.path, block.hostPath);
  const inlineData =
    typeof block.data === "string" && block.data.trim()
      ? block.data.trim()
      : null;

  // Plain (text/plain markdown) side: synchronous short references only,
  // never base64 bytes. Inline-data images and data:-URI sources (providers
  // do put data URIs into url/uri fields) have no short reference, so they
  // degrade to a bold label.
  const isDataSource = source?.startsWith("data:") ?? false;
  const plain =
    source && !isDataSource
      ? imageLinkMarkdown(name, source)
      : attachmentId
        ? imageLinkMarkdown(name, `attachment:${attachmentId}`)
        : `**${name}**`;

  // Hydrated (text/html) side: embed real bytes as data URIs — rich-paste
  // targets (Word, Feishu docs, Notion) consume and re-upload data URIs but
  // never fetch local paths or localhost URLs. An oversized image and a
  // failed attachment/local-path read both count as omitted (either way the
  // recipient did not get the bytes) and keep the lean form. A missing
  // reader is not counted: hydration was never available on that surface.
  // http(s), data: and other scheme-qualified sources pass through as-is.
  let hydrated = plain;
  if (source && isDataSource) {
    const payload = source.slice(source.indexOf(",") + 1);
    if (
      base64BinaryByteSize(payload) <=
      AGENT_CONVERSATION_COPY_MAX_EMBEDDED_IMAGE_BYTES
    ) {
      hydrated = imageLinkMarkdown(name, source);
    } else {
      context.stats.omittedImages += 1;
    }
  } else if (source) {
    const localPath = localImagePathFromSource(source);
    if (localPath && context.readLocalImage) {
      try {
        const attachment = await context.readLocalImage({
          mimeType: blockMimeType,
          path: localPath
        });
        hydrated = embedWithinLimit(name, attachment, plain, context);
      } catch {
        context.stats.omittedImages += 1;
      }
    }
  } else if (inlineData) {
    const dataUri = inlineData.startsWith("data:")
      ? inlineData
      : `data:${blockMimeType ?? "image/png"};base64,${inlineData}`;
    const payload = dataUri.slice(dataUri.indexOf(",") + 1);
    if (
      base64BinaryByteSize(payload) <=
      AGENT_CONVERSATION_COPY_MAX_EMBEDDED_IMAGE_BYTES
    ) {
      hydrated = imageLinkMarkdown(name, dataUri);
    } else {
      context.stats.omittedImages += 1;
    }
  } else if (attachmentId && context.readAttachment) {
    try {
      const attachment = await context.readAttachment(attachmentId);
      hydrated = embedWithinLimit(name, attachment, plain, context);
    } catch {
      context.stats.omittedImages += 1;
    }
  }
  return { hydrated, plain };
}

function embedWithinLimit(
  name: string,
  attachment: AgentGUIConversationAttachment,
  fallback: string,
  context: SerializeContext
): string {
  if (
    base64BinaryByteSize(attachment.data) >
    AGENT_CONVERSATION_COPY_MAX_EMBEDDED_IMAGE_BYTES
  ) {
    context.stats.omittedImages += 1;
    return fallback;
  }
  return imageLinkMarkdown(
    name,
    `data:${attachment.mimeType};base64,${attachment.data}`
  );
}

function imageLinkMarkdown(name: string, target: string): string {
  return `![${escapeMarkdownLabel(name)}](<${escapeMarkdownTarget(target)}>)`;
}

// Binary size of a base64 payload without decoding it: 3 bytes per 4 chars,
// minus padding.
function base64BinaryByteSize(value: string): number {
  const trimmed = value.trim();
  const padding = trimmed.endsWith("==") ? 2 : trimmed.endsWith("=") ? 1 : 0;
  return Math.max(0, Math.floor((trimmed.length * 3) / 4) - padding);
}

// file:// URLs address the same local files as bare paths, so they hydrate
// through the same reader after URL decoding (a Windows drive pathname keeps
// no leading slash).
function localImagePathFromSource(source: string): string | null {
  const trimmed = source.trim();
  if (isLocalImagePath(trimmed)) {
    return trimmed;
  }
  if (!/^file:\/\//i.test(trimmed)) {
    return null;
  }
  try {
    const pathname = decodeURIComponent(new URL(trimmed).pathname);
    return /^\/[a-zA-Z]:[\\/]/.test(pathname) ? pathname.slice(1) : pathname;
  } catch {
    return null;
  }
}

function serializeFileBlock(
  block: AgentPromptContentBlock & Record<string, unknown>,
  labels: AgentGUIConversationCopyLabels
): string {
  const source = firstString(block.uri, block.path, block.hostPath, block.url);
  const name = firstString(block.name) ?? source ?? labels.file;
  return source
    ? `[${escapeMarkdownLabel(name)}](<${escapeMarkdownTarget(source)}>)`
    : `**${labels.file}:** ${escapeMarkdownLabel(name)}`;
}

function serializeMentionBlock(
  block: AgentPromptContentBlock & Record<string, unknown>,
  labels: AgentGUIConversationCopyLabels
): string {
  const text = firstString(block.text, block.name);
  if (text) {
    return text.startsWith(labels.mentionPrefix)
      ? text
      : `${labels.mentionPrefix}${text}`;
  }
  return firstString(block.uri, block.path) ?? "";
}

function serializeSkillBlock(
  block: AgentPromptContentBlock & Record<string, unknown>
): string {
  const name = firstString(block.name, block.text);
  return name ? (name.startsWith("/") ? name : `/${name}`) : "";
}

// Runtime notices (context-budget warnings, provider advisories) ride as
// assistant-role messages; they are operational chrome, never transcript.
function isSystemNoticeMessage(message: AgentActivityMessage): boolean {
  return firstString(message.payload.kind) === "agent_system_notice";
}

function isToolMessage(message: AgentActivityMessage): boolean {
  const kind = message.kind.trim().toLowerCase();
  return (
    kind.includes("tool") ||
    kind.includes("call") ||
    firstString(message.payload.callType, message.payload.toolName) !== null
  );
}

function compareMessagesAscending(
  left: AgentActivityMessage,
  right: AgentActivityMessage
): number {
  const leftSequence = left.sequence ?? 0;
  const rightSequence = right.sequence ?? 0;
  if (leftSequence > 0 && rightSequence > 0 && leftSequence !== rightSequence) {
    return leftSequence - rightSequence;
  }
  return (
    left.occurredAtUnixMs - right.occurredAtUnixMs ||
    left.version - right.version ||
    left.messageId.localeCompare(right.messageId)
  );
}

function normalizedRole(role: string): string {
  return role.trim().toLowerCase();
}

function firstString(...values: unknown[]): string | null {
  for (const value of values) {
    if (typeof value === "string" && value.trim()) {
      return value.trim();
    }
  }
  return null;
}

function singleLine(value: string): string {
  return value.trim().replace(/\s+/g, " ");
}

function escapeMarkdownLabel(value: string): string {
  return value
    .replaceAll("\\", "\\\\")
    .replaceAll("[", "\\[")
    .replaceAll("]", "\\]");
}

function escapeMarkdownTarget(value: string): string {
  return value.replace(/>/g, "%3E");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}
