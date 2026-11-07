import { createRichTextMentionMarkdown } from "@tutti-os/ui-rich-text/core";
import type { AgentActivityRuntimeStagePastedTextResult } from "../../../conversationServices";
import type { AgentPromptContentBlock } from "../../../shared/contracts/dto";
import { pastedTextDraftDisplayName } from "../../../shared/pastedTextReferenceProjection";
import { agentComposerFileMentionReferences } from "../agentRichText/agentMentionMarkdown";
import {
  agentComposerDraftConnectorBlocks,
  agentComposerDraftConnectors,
  agentPromptContentConnectors,
  mergeAgentComposerDraftConnectorKeys
} from "./agentComposerDraftConnectors";
import {
  agentPromptImageBlockToDraftImage,
  type AgentPromptImageContentBlock
} from "./agentComposerDraftImages";
import {
  agentComposerDraftQuotes,
  agentComposerQuotePromptContent
} from "./agentComposerDraftQuotes";
import {
  agentPreparedPromptFileToDraftFile,
  agentPromptContentToComposerPrompt,
  agentPromptFileBlocks,
  agentPromptPastedTextBlocks,
  materializeAgentComposerFileMentions
} from "./agentExternalPromptFiles";
import type {
  AgentComposerDraft,
  AgentComposerDraftConnector,
  AgentComposerDraftContent,
  AgentComposerDraftFile,
  AgentComposerDraftImage,
  AgentComposerDraftLargeText,
  AgentComposerFileBlock,
  AgentComposerQuoteBlock,
  AgentGUIProviderSkillOption
} from "./agentGuiNodeTypes";
import {
  AGENT_PASTED_TEXT_BLOCK_KIND,
  AGENT_PASTED_TEXT_MENTION_KIND
} from "./agentGuiNodeTypes";
import {
  projectLocalConnectorPrompt,
  promptForProviderSkills,
  skillTriggerForPrefix
} from "./agentSkillOptions";

export {
  agentComposerDraftQuotes,
  appendAgentComposerDraftQuote
} from "./agentComposerDraftQuotes";

export {
  agentComposerDraftConnectors,
  agentComposerDraftPreservingConnectors
} from "./agentComposerDraftConnectors";
export { formatAgentComposerDraftBytes } from "./agentComposerDraftImages";

export {
  extractPastedTextArchivePaths,
  linkifyPastedTextReferences,
  pastedTextDraftDisplayName
} from "../../../shared/pastedTextReferenceProjection";
const PASTED_TEXT_MENTION_PREVIEW_MAX_CHARS = 10;

/**
 * First {@link PASTED_TEXT_MENTION_PREVIEW_MAX_CHARS} characters of the pasted
 * body (collapsed to a single line), used as the chip label everywhere. Markdown
 * link-label metacharacters are stripped so it round-trips through the
 * `[preview](path)` reference the persisted content carries.
 */
export function pastedTextPreview(text: string): string {
  const collapsed = text
    .replace(/\s+/g, " ")
    .trim()
    .replace(/[[\]()]/g, "");
  if (collapsed.length <= PASTED_TEXT_MENTION_PREVIEW_MAX_CHARS) {
    return collapsed;
  }
  return `${collapsed.slice(0, PASTED_TEXT_MENTION_PREVIEW_MAX_CHARS)}…`;
}

/**
 * First non-empty line of the pasted body, trimmed and length-capped, used as
 * the chip's primary label in the conversation flow. Falls back to the display
 * file name when the body is unavailable (e.g. a queue-restored item).
 */
function pastedTextPreviewLabel(
  item: AgentComposerDraftLargeText,
  index: number
): string {
  return (
    pastedTextPreview(item.text) ||
    item.name.trim() ||
    pastedTextDraftDisplayName(index)
  );
}

/**
 * Encodes a landed pasted-text item as a canonical mention link for the
 * conversation-flow display prompt. A local href carries the archive `path`
 * and byte size so the host can render a chip and open a preview on click.
 * A remote href deliberately carries only the draft identity and preview
 * label: signed object URLs and attachment locators must stay in structured
 * prompt content, never in display-only text that may be projected to a
 * shared caller transcript. Returns "" when the item has not landed.
 */
export function pastedTextMentionMarkdown(
  item: AgentComposerDraftLargeText,
  index: number
): string {
  const path = item.path?.trim();
  const remoteUrl = item.url?.trim();
  if (!path && !remoteUrl) {
    return "";
  }
  return createRichTextMentionMarkdown({
    providerId: AGENT_PASTED_TEXT_MENTION_KIND,
    entityId: item.id,
    label: pastedTextPreviewLabel(item, index),
    ...(path
      ? {
          scope: {
            path,
            ...(typeof item.sizeBytes === "number" &&
            Number.isFinite(item.sizeBytes)
              ? { size: String(item.sizeBytes) }
              : {})
          }
        }
      : { scope: { presentation: "remote" } })
  });
}

/**
 * Applies the provider-readable locator returned by the host staging boundary
 * to one draft item. Keeping this mapping in the model lets the composer hook
 * remain a transport caller rather than a second attachment-state owner.
 */
export function applyPastedTextStagingResult(
  item: AgentComposerDraftLargeText,
  result: AgentActivityRuntimeStagePastedTextResult
): AgentComposerDraftLargeText {
  const path = result.path?.trim();
  const url = result.url?.trim();
  if (!path && !url) {
    throw new Error(
      "Pasted text staging completed without a provider-readable locator."
    );
  }
  return {
    ...item,
    ...(path ? { path } : { url: url! }),
    ...(result.mimeType?.trim() ? { mimeType: result.mimeType.trim() } : {}),
    ...(result.assetId?.trim() ? { assetId: result.assetId.trim() } : {}),
    ...(result.uri?.trim() ? { uri: result.uri.trim() } : {}),
    ...(result.uploadStatus?.trim()
      ? { uploadStatus: result.uploadStatus.trim() }
      : {}),
    name: result.name || item.name,
    sizeBytes: result.sizeBytes,
    uploading: false
  };
}

export const MAX_AGENT_COMPOSER_DRAFT_IMAGES = 8;

export function emptyAgentComposerDraft(): AgentComposerDraft {
  return [{ type: "text", text: "" }];
}

export function snapshotAgentComposerDraft(
  draft: AgentComposerDraft
): AgentComposerDraft {
  const [textBlock, ...attachmentBlocks] = draft;
  return [{ ...textBlock }, ...attachmentBlocks.map((block) => ({ ...block }))];
}

export function agentComposerDraftPrompt(
  draft: AgentComposerDraftContent
): string {
  return draft[0].text;
}

export function agentComposerDraftImages(
  draft: AgentComposerDraftContent
): AgentComposerDraftImage[] {
  return draft
    .filter(
      (
        block
      ): block is Extract<
        AgentComposerDraftContent[number],
        { type: "image" }
      > => block.type === "image"
    )
    .map(({ type: _type, ...image }) => image);
}

export function agentComposerDraftFiles(
  draft: AgentComposerDraftContent
): AgentComposerDraftFile[] {
  return draft
    .filter(
      (
        block
      ): block is Extract<
        AgentComposerDraftContent[number],
        { type: "file" }
      > => block.type === "file" && block.kind === "file"
    )
    .map(({ type: _type, kind: _kind, text: _text, ...file }) => file);
}

export function agentComposerDraftLargeTexts(
  draft: AgentComposerDraftContent
): AgentComposerDraftLargeText[] {
  return draft
    .filter(
      (
        block
      ): block is Extract<
        AgentComposerDraftContent[number],
        { type: "file"; kind: typeof AGENT_PASTED_TEXT_BLOCK_KIND }
      > => block.type === "file" && block.kind === AGENT_PASTED_TEXT_BLOCK_KIND
    )
    .map(({ type: _type, kind: _kind, ...item }) => item);
}

export function buildAgentComposerDraft(input: {
  prompt: string;
  images?: readonly AgentComposerDraftImage[];
  files?: readonly AgentComposerDraftFile[];
  largeTexts?: readonly AgentComposerDraftLargeText[];
  quotes?: readonly AgentComposerQuoteBlock[];
  connectors?: readonly AgentComposerDraftConnector[];
}): AgentComposerDraft {
  return [
    { type: "text", text: input.prompt },
    ...(input.images ?? []).map((image) => ({
      type: "image" as const,
      ...image
    })),
    ...(input.files ?? []).map((file) => ({
      type: "file" as const,
      kind: "file" as const,
      ...file
    })),
    ...(input.largeTexts ?? []).map(
      (item): AgentComposerFileBlock => ({
        type: "file" as const,
        kind: AGENT_PASTED_TEXT_BLOCK_KIND,
        ...item
      })
    ),
    ...(input.quotes ?? []).map((quote) => ({ ...quote })),
    ...agentComposerDraftConnectorBlocks(input.connectors ?? [])
  ];
}

export function updateAgentComposerDraft(
  draft: AgentComposerDraft,
  update: Partial<{
    prompt: string;
    images: readonly AgentComposerDraftImage[];
    files: readonly AgentComposerDraftFile[];
    largeTexts: readonly AgentComposerDraftLargeText[];
    quotes: readonly AgentComposerQuoteBlock[];
    connectors: readonly AgentComposerDraftConnector[];
  }>
): AgentComposerDraft {
  return buildAgentComposerDraft({
    prompt: update.prompt ?? agentComposerDraftPrompt(draft),
    images: update.images ?? agentComposerDraftImages(draft),
    files: update.files ?? agentComposerDraftFiles(draft),
    largeTexts: update.largeTexts ?? agentComposerDraftLargeTexts(draft),
    quotes: update.quotes ?? agentComposerDraftQuotes(draft),
    connectors: update.connectors ?? agentComposerDraftConnectors(draft)
  });
}

export function agentComposerDraftHasContent(
  draft: AgentComposerDraft
): boolean {
  const prompt = agentComposerDraftPrompt(draft);
  const references = agentComposerFileMentionReferences(prompt);
  let cursor = 0;
  let textWithoutComposerFiles = "";
  for (const reference of references) {
    textWithoutComposerFiles += prompt.slice(cursor, reference.start);
    cursor = reference.end;
  }
  textWithoutComposerFiles += prompt.slice(cursor);
  const referencedFileIds = new Set(
    references.map((reference) => reference.id)
  );
  if (
    textWithoutComposerFiles.trim() ||
    agentComposerDraftConnectors(draft).length > 0 ||
    agentComposerDraftQuotes(draft).length > 0 ||
    agentComposerDraftFiles(draft).some((file) =>
      referencedFileIds.has(file.id)
    )
  ) {
    return true;
  }
  return draft.some((block) => {
    if (block.type === "text") return false;
    if (block.type === "image") return true;
    if (block.type === "connector") return true;
    if (block.type === "quote") return block.text.trim() !== "";
    return block.kind === "file"
      ? false
      : block.text.trim() !== "" || Boolean(block.path || block.url);
  });
}

/** A transcript reference needs a question; attachments alone keep their existing send behavior. */
export function agentComposerDraftCanSubmit(draft: AgentComposerDraft): boolean {
  return (
    (agentComposerDraftQuotes(draft).length === 0 ||
      agentComposerDraftPrompt(draft).trim().length > 0) &&
    agentComposerDraftHasContent(draft)
  );
}

function promptWithDraftQuotes(draft: AgentComposerDraft, prompt: string): string {
  const quote = agentPromptContentDisplayText(
    agentComposerQuotePromptContent(agentComposerDraftQuotes(draft))
  );
  return [quote, prompt].filter(Boolean).join("\n\n");
}

export function normalizeAgentPromptContentBlocks(
  content: readonly AgentPromptContentBlock[]
): AgentPromptContentBlock[] {
  const result: AgentPromptContentBlock[] = [];
  for (const block of content) {
    if (block.type === "text") {
      const text = block.text?.trim() ?? "";
      if (text) {
        result.push({ type: "text", text });
      }
      continue;
    }
    if (block.type === "image") {
      const mimeType = block.mimeType?.trim();
      const attachmentId = block.attachmentId?.trim();
      const data = block.data?.trim();
      const url =
        attachmentId && block.url?.startsWith("data:")
          ? undefined
          : block.url?.trim();
      const imagePath = block.path?.trim();
      if (
        (!attachmentId && !data && !url && !imagePath) ||
        (data && url) ||
        (url && !isSafePromptImageUrl(url)) ||
        (mimeType !== "image/png" &&
          mimeType !== "image/jpeg" &&
          mimeType !== "image/webp")
      ) {
        continue;
      }
      result.push({
        type: "image",
        mimeType,
        ...(attachmentId ? { attachmentId } : {}),
        ...(url
          ? { url }
          : data
            ? { data }
            : imagePath
              ? { path: imagePath }
              : {}),
        ...(block.name?.trim() ? { name: block.name.trim() } : {})
      });
      continue;
    }
    if (block.type === "file") {
      const filePath = block.path?.trim();
      const hostPath = block.hostPath?.trim();
      const url = block.url?.trim();
      const uri = block.uri?.trim();
      const assetId = block.assetId?.trim();
      if (!filePath && !hostPath && !url && !uri && !assetId) {
        continue;
      }
      result.push({
        type: "file",
        ...(block.mimeType?.trim() ? { mimeType: block.mimeType.trim() } : {}),
        ...(filePath ? { path: filePath } : {}),
        ...(hostPath ? { hostPath } : {}),
        ...(url ? { url } : {}),
        ...(block.name?.trim() ? { name: block.name.trim() } : {}),
        ...(uri ? { uri } : {}),
        ...(block.uploadStatus?.trim()
          ? { uploadStatus: block.uploadStatus.trim() }
          : {}),
        ...(assetId ? { assetId } : {}),
        ...(typeof block.sizeBytes === "number"
          ? { sizeBytes: block.sizeBytes }
          : {}),
        kind:
          block.kind === AGENT_PASTED_TEXT_BLOCK_KIND
            ? AGENT_PASTED_TEXT_BLOCK_KIND
            : "file"
      });
      continue;
    }
    if (block.type === "skill" || block.type === "mention") {
      const name = block.name?.trim();
      const path = block.path?.trim();
      if (name && path) {
        result.push({ type: block.type, name, path });
      }
      continue;
    }
    if (block.type === "connector") {
      const connectorKey = block.connectorKey?.trim();
      if (connectorKey && /^[a-z][a-z0-9._-]{0,127}$/.test(connectorKey)) {
        result.push({ type: "connector", connectorKey });
      }
    }
  }
  return result;
}

function isSafePromptImageUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return (
      url.protocol === "https:" &&
      Boolean(url.hostname) &&
      !url.username &&
      !url.password
    );
  } catch {
    return false;
  }
}

export function agentPromptContentDisplayText(
  content: readonly AgentPromptContentBlock[]
): string {
  return content
    .filter((block) => block.type === "text")
    .map((block) => block.text?.trim() ?? "")
    .filter(Boolean)
    .join("\n");
}

export function agentPromptContentHasImage(
  content: readonly AgentPromptContentBlock[]
): boolean {
  return content.some((block) => block.type === "image");
}

export function agentPromptContentHasFile(
  content: readonly AgentPromptContentBlock[]
): boolean {
  return content.some((block) => block.type === "file");
}

export function agentPromptContentImageBlocks(
  content: readonly AgentPromptContentBlock[]
): AgentPromptImageContentBlock[] {
  return normalizeAgentPromptContentBlocks(content).filter(
    (block): block is AgentPromptImageContentBlock =>
      block.type === "image" &&
      (typeof block.attachmentId === "string" ||
        typeof block.data === "string" ||
        typeof block.url === "string" ||
        typeof block.path === "string") &&
      typeof block.mimeType === "string"
  );
}

export function agentPromptContentToComposerDraft(
  content: readonly AgentPromptContentBlock[],
  idPrefix: string
): AgentComposerDraft {
  const normalizedContent = normalizeAgentPromptContentBlocks(content);
  const largeTexts = agentPromptPastedTextBlocks(normalizedContent).map(
    (block, index) =>
      agentPromptPastedTextBlockToDraftLargeText(block, idPrefix, index)
  );
  const files = agentPromptFileBlocks(normalizedContent).map((file, index) =>
    agentPreparedPromptFileToDraftFile(file, idPrefix, index)
  );
  return buildAgentComposerDraft({
    prompt: agentPromptContentToComposerPrompt(normalizedContent, files),
    images: agentPromptContentImageBlocks(normalizedContent)
      .slice(0, MAX_AGENT_COMPOSER_DRAFT_IMAGES)
      .map((image, index) =>
        agentPromptImageBlockToDraftImage(image, idPrefix, index)
      ),
    files,
    largeTexts,
    connectors: agentPromptContentConnectors(normalizedContent)
  });
}

function agentPromptPastedTextBlockToDraftLargeText(
  block: AgentPromptContentBlock & { type: "file" },
  idPrefix: string,
  index: number
): AgentComposerDraftLargeText {
  return {
    id: `${idPrefix}:pasted-text:${index}`,
    name: block.name?.trim() || "pasted-text.txt",
    text: "",
    ...(block.path ? { path: block.path } : {}),
    ...(block.url ? { url: block.url } : {}),
    ...(block.mimeType ? { mimeType: block.mimeType } : {}),
    ...(block.uri ? { uri: block.uri } : {}),
    ...(block.assetId ? { assetId: block.assetId } : {}),
    ...(block.uploadStatus ? { uploadStatus: block.uploadStatus } : {}),
    ...(typeof block.sizeBytes === "number"
      ? { sizeBytes: block.sizeBytes }
      : {})
  };
}

export function agentComposerDraftToPromptContent(input: {
  draft: AgentComposerDraft;
  skills: readonly AgentGUIProviderSkillOption[];
}): AgentPromptContentBlock[] {
  const providerPrompt = materializeAgentComposerFileMentions(
    agentComposerDraftPrompt(input.draft),
    agentComposerDraftFiles(input.draft)
  );
  const prompt = promptForProviderSkills({
    prompt: providerPrompt,
    skills: input.skills
  });
  const connectorProjection = projectLocalConnectorPrompt({
    prompt,
    skills: input.skills
  });
  const connectorKeys = mergeAgentComposerDraftConnectorKeys(
    input.draft,
    connectorProjection.connectorKeys
  );
  return normalizeAgentPromptContentBlocks([
    ...textPromptContent(promptWithDraftQuotes(input.draft, connectorProjection.prompt)),
    ...promptItemBlocksForProviderSkills({
      prompt: connectorProjection.prompt,
      skills: input.skills
    }),
    ...connectorKeys.map((connectorKey) => ({
      type: "connector" as const,
      connectorKey
    })),
    ...agentComposerDraftImages(input.draft)
      .slice(0, MAX_AGENT_COMPOSER_DRAFT_IMAGES)
      .filter((image) => !image.uploading && !image.uploadError)
      .map((image) => ({
        type: "image" as const,
        mimeType: image.mimeType,
        ...(image.attachmentId ? { attachmentId: image.attachmentId } : {}),
        ...(image.url
          ? { url: image.url }
          : image.path
            ? { path: image.path }
            : { data: image.data }),
        name: image.name
      })),
    ...largeTextPromptContent(agentComposerDraftLargeTexts(input.draft))
  ]);
}

export function agentComposerDraftSubmittedText(
  draft: AgentComposerDraft
): string {
  return agentPromptContentDisplayText(
    normalizeAgentPromptContentBlocks([
      ...textPromptContent(promptWithDraftQuotes(draft, agentComposerDraftPrompt(draft))),
      ...largeTextPromptContent(agentComposerDraftLargeTexts(draft))
    ])
  );
}

export function agentComposerDraftDisplayPrompt(
  draft: AgentComposerDraft
): string | undefined {
  const files = agentComposerDraftFiles(draft);
  // Composer-file hrefs are draft-only. Persist the same path/URL locators the
  // provider prompt already materializes so transcript chips stay openable.
  const prompt = materializeAgentComposerFileMentions(
    agentComposerDraftPrompt(draft).trim(),
    files
  );
  const largeTexts = agentComposerDraftLargeTexts(draft).filter(
    (item) =>
      Boolean(item.path || item.url) && !item.uploading && !item.uploadError
  );
  const quoteText = agentPromptContentDisplayText(
    agentComposerQuotePromptContent(agentComposerDraftQuotes(draft))
  );
  if (!largeTexts.length && !quoteText) {
    return prompt.includes("](mention://") ? prompt : undefined;
  }
  const parts = [quoteText ? [quoteText, prompt].filter(Boolean).join("\n\n") : prompt].filter(Boolean);
  parts.push(
    ...largeTexts
      .map((item, index) => pastedTextMentionMarkdown(item, index))
      .filter(Boolean)
  );
  return parts.join("\n");
}

export function projectAgentComposerDraftSubmission(input: {
  draft: AgentComposerDraft;
  skills: readonly AgentGUIProviderSkillOption[];
}): {
  content: AgentPromptContentBlock[];
  displayPrompt?: string;
} {
  const content = agentComposerDraftToPromptContent(input);
  const files = agentComposerDraftFiles(input.draft);
  const explicitDisplayPrompt = agentComposerDraftDisplayPrompt(input.draft);
  const visibleText = materializeAgentComposerFileMentions(
    agentComposerDraftSubmittedText(input.draft),
    files
  );
  const runtimeText = agentPromptContentDisplayText(content);
  const displayPrompt =
    explicitDisplayPrompt ??
    (visibleText !== runtimeText ? visibleText : undefined);

  return {
    content,
    ...(displayPrompt ? { displayPrompt } : {})
  };
}

function promptItemBlocksForProviderSkills(input: {
  prompt: string;
  skills: readonly AgentGUIProviderSkillOption[];
}): AgentPromptContentBlock[] {
  const result: AgentPromptContentBlock[] = [];
  for (const skill of input.skills) {
    if (skill.invocation !== "promptItem") {
      continue;
    }
    const path = skill.path?.trim();
    if (!path) {
      continue;
    }
    const trigger = skillTriggerForPrefix(skill, "$");
    if (!trigger || !promptHasTrigger(input.prompt, trigger)) {
      continue;
    }
    result.push({
      type: skill.kind === "connector" ? "mention" : "skill",
      name: skill.name,
      path
    });
  }
  return result;
}

function promptHasTrigger(prompt: string, trigger: string): boolean {
  return new RegExp(`(^|\\s)${escapeRegExp(trigger)}(?=$|\\s)`).test(prompt);
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function textPromptContent(prompt: string): AgentPromptContentBlock[] {
  const text = prompt.trim();
  return text ? [{ type: "text", text }] : [];
}

/**
 * Pasted long text submits as a structured `file` block tagged with
 * {@link AGENT_PASTED_TEXT_BLOCK_KIND}. A landed item has either a local
 * archive path or a remote prepared-file URL. Still-uploading or errored items
 * are dropped from submit (a visible error chip remains for the user to retry
 * or remove). The local codex-style "read this file" instruction, and the
 * remote conversion to an ordinary prepared file, are materialized in the
 * controller at send time via {@link materializePastedTextInstructions} so
 * translations never enter the model layer or the persisted/queued draft.
 */
function largeTextPromptContent(
  largeTexts: readonly AgentComposerDraftLargeText[]
): AgentPromptContentBlock[] {
  return largeTexts
    .filter((item) => {
      const path = item.path?.trim();
      const url = item.url?.trim();
      return Boolean(path || url) && !item.uploading && !item.uploadError;
    })
    .map((item, index) => ({
      type: "file" as const,
      kind: AGENT_PASTED_TEXT_BLOCK_KIND,
      ...(item.path?.trim()
        ? { path: item.path.trim() }
        : { url: item.url!.trim() }),
      ...(item.mimeType?.trim() ? { mimeType: item.mimeType.trim() } : {}),
      // The preview (first chars of the pasted body) is the chip label; carry it
      // as the block name so the send-time instruction persists it in content.
      name: pastedTextPreviewLabel(item, index),
      ...(item.uri?.trim() ? { uri: item.uri.trim() } : {}),
      ...(item.assetId?.trim() ? { assetId: item.assetId.trim() } : {}),
      ...(item.uploadStatus?.trim()
        ? { uploadStatus: item.uploadStatus.trim() }
        : {}),
      ...(typeof item.sizeBytes === "number"
        ? { sizeBytes: item.sizeBytes }
        : {})
    }));
}

/**
 * True when a prompt `file` block is a pasted-text attachment rather than a
 * user-attached file.
 */
export function isPastedTextPromptBlock(
  block: AgentPromptContentBlock
): boolean {
  return block.type === "file" && block.kind === AGENT_PASTED_TEXT_BLOCK_KIND;
}

/**
 * Rewrites `content` for send. Local pasted-text blocks are replaced by one
 * codex-style instruction text block at the tail that references each local
 * archive path. Remote pasted-text blocks become ordinary prepared `file`
 * blocks, preserving their object-store locator and metadata so the shared host
 * can use its normal attachment pipeline. The instruction copy is passed in
 * already-translated so the model layer stays free of any i18n dependency.
 */
export function materializePastedTextInstructions(
  content: readonly AgentPromptContentBlock[],
  format: {
    header: () => string;
    line: (preview: string, path: string) => string;
  }
): AgentPromptContentBlock[] {
  const pastedRefs = content
    .filter(isPastedTextPromptBlock)
    .map((block) => ({
      preview: sanitizePastedTextPreviewForContent(block.name),
      path: block.path?.trim() ?? ""
    }))
    .filter((ref) => ref.path !== "");
  const withoutLocalPastedText = content.flatMap((block) => {
    if (!isPastedTextPromptBlock(block)) {
      return [block];
    }
    if (block.path?.trim()) {
      return [];
    }
    const url = block.url?.trim();
    if (!url) {
      return [];
    }
    return [{ ...block, kind: "file", url }];
  });
  if (pastedRefs.length === 0) {
    return withoutLocalPastedText;
  }
  const instruction = [
    format.header(),
    ...pastedRefs.map((ref) => format.line(ref.preview, ref.path))
  ].join("\n");
  return [...withoutLocalPastedText, { type: "text", text: instruction }];
}

// The preview is embedded quoted in the persisted instruction line
// (`… "<preview>": <path> …`), so strip the quote/newline delimiters that would
// break the parse-back in {@link linkifyPastedTextReferences}.
function sanitizePastedTextPreviewForContent(name: string | undefined): string {
  return (name ?? "").replace(/["\n\r]/g, " ").trim();
}
