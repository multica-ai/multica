import type { Attachment } from "@multica/core/types";
import { contentReferencesAttachment } from "@multica/core/types/attachment-url";
import type {
AgentPromptContentBlock,
AgentQueuedPrompt,
PromptQueueState
} from "@tutti-os/agent-activity-core";
import { extractRichTextMentionsFromContent } from "@tutti-os/ui-rich-text/core";

export interface SavedPromptQueue {
  agentSessionId: string;
  prompts: AgentQueuedPrompt[];
}

function record(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function blocks(value: unknown): value is AgentPromptContentBlock[] {
  const types = ["text", "image", "file", "skill", "mention", "connector"];
  const strings = [
    "text",
    "mimeType",
    "data",
    "url",
    "attachmentId",
    "name",
    "path",
    "connectorKey",
    "uri",
    "hostPath",
    "uploadStatus",
    "assetId",
    "kind"
  ];
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.every(
      (block) =>
        record(block) &&
        typeof block.type === "string" &&
        types.includes(block.type) &&
        (block.type !== "text" || typeof block.text === "string") &&
        strings.every(
          (key) => block[key] === undefined || typeof block[key] === "string"
        ) &&
        (block.sizeBytes === undefined ||
          (typeof block.sizeBytes === "number" &&
            Number.isFinite(block.sizeBytes) &&
            block.sizeBytes >= 0))
    )
  );
}

function contentSnapshot(
  content: readonly AgentPromptContentBlock[]
): AgentPromptContentBlock[] {
  return content.map((block) => {
    const next = { ...block };
    // Prepared attachments already live in the host. Preview URLs and inline
    // image copies are neither durable references nor needed in localStorage.
    if (next.assetId || next.attachmentId) {
      next.attachmentId ??= next.assetId;
      delete next.data;
      if (next.url?.startsWith("blob:") || next.url?.startsWith("data:"))
        delete next.url;
    }
    return next;
  });
}

function promptSnapshot(prompt: AgentQueuedPrompt): AgentQueuedPrompt {
  return {
    id: prompt.id,
    clientSubmitId: prompt.clientSubmitId || prompt.id,
    content: contentSnapshot(prompt.content),
    createdAtUnixMs: prompt.createdAtUnixMs,
    ...(prompt.displayPrompt ? { displayPrompt: prompt.displayPrompt } : {}),
    ...(prompt.runtimeContent?.length
      ? { runtimeContent: contentSnapshot(prompt.runtimeContent) }
      : {}),
    ...(prompt.requiredSettingsPatch
      ? { requiredSettingsPatch: { ...prompt.requiredSettingsPatch } }
      : {})
  };
}

export function readSavedPromptQueues(value: unknown): SavedPromptQueue[] {
  if (!Array.isArray(value)) return [];
  const queues: SavedPromptQueue[] = [];
  for (const entry of value) {
    if (
      !record(entry) ||
      typeof entry.agentSessionId !== "string" ||
      !entry.agentSessionId.trim() ||
      !Array.isArray(entry.prompts)
    )
      continue;
    const prompts: AgentQueuedPrompt[] = [];
    for (const item of entry.prompts) {
      if (
        !record(item) ||
        typeof item.id !== "string" ||
        !item.id.trim() ||
        !blocks(item.content) ||
        typeof item.createdAtUnixMs !== "number" ||
        !Number.isFinite(item.createdAtUnixMs) ||
        (item.clientSubmitId !== undefined &&
          (typeof item.clientSubmitId !== "string" ||
            !item.clientSubmitId.trim())) ||
        (item.displayPrompt !== undefined &&
          typeof item.displayPrompt !== "string") ||
        (item.runtimeContent !== undefined && !blocks(item.runtimeContent))
      )
        continue;
      const patch = item.requiredSettingsPatch;
      if (
        patch !== undefined &&
        (!record(patch) ||
          Object.entries(patch).some(
            ([key, flag]) =>
              !["browserUse", "computerUse"].includes(key) ||
              typeof flag !== "boolean"
          ))
      )
        continue;
      const prompt: AgentQueuedPrompt = {
        id: item.id,
        content: item.content,
        createdAtUnixMs: item.createdAtUnixMs,
        ...(typeof item.clientSubmitId === "string"
          ? { clientSubmitId: item.clientSubmitId }
          : {}),
        ...(typeof item.displayPrompt === "string"
          ? { displayPrompt: item.displayPrompt }
          : {}),
        ...(blocks(item.runtimeContent)
          ? { runtimeContent: item.runtimeContent }
          : {}),
        ...(record(patch)
          ? {
              requiredSettingsPatch: {
                ...(typeof patch.browserUse === "boolean"
                  ? { browserUse: patch.browserUse }
                  : {}),
                ...(typeof patch.computerUse === "boolean"
                  ? { computerUse: patch.computerUse }
                  : {})
              }
            }
          : {})
      };
      if (!prompts.some((existing) => existing.id === prompt.id))
        prompts.push(promptSnapshot(prompt));
    }
    if (
      prompts.length &&
      !queues.some(
        (existing) => existing.agentSessionId === entry.agentSessionId
      )
    )
      queues.push({ agentSessionId: entry.agentSessionId, prompts });
  }
  return queues;
}

export function snapshotPromptQueues(
  state: PromptQueueState,
  attachments: readonly Attachment[]
): SavedPromptQueue[] {
  return Object.values(state.recordsBySessionId).flatMap((queue) => {
    const prompts = queue.prompts
      .filter((prompt) => prompt.visibleInQueue !== false)
      .map((prompt) => {
        const runtimeContent = [...(prompt.runtimeContent ?? prompt.content)];
        const references = runtimeContent.flatMap((block) =>
          block.type === "text" && block.text
            ? extractRichTextMentionsFromContent(block.text)
                .filter((mention) => mention.providerId === "file")
                .map((mention) => mention.entityId)
            : []
        );
        for (const attachment of attachments) {
          if (
            runtimeContent.some(
              (block) =>
                block.attachmentId === attachment.id ||
                block.assetId === attachment.id
            )
          )
            continue;
          const mentioned =
            [
              attachment.url,
              attachment.markdown_url,
              attachment.download_url,
              attachment.attachment_download_url
            ].some((url) => url && references.includes(url)) ||
            runtimeContent.some(
              (block) =>
                block.type === "text" &&
                block.text &&
                contentReferencesAttachment(block.text, attachment)
            );
          // Markdown-only file mentions rely on an in-memory catalog. Keep their
          // IDs as runtime blocks so restored sends still attach the real files.
          if (mentioned)
            runtimeContent.push({
              type: "file",
              attachmentId: attachment.id,
              name: attachment.filename
            });
        }
        return promptSnapshot({ ...prompt, runtimeContent });
      });
    return prompts.length
      ? [{ agentSessionId: queue.agentSessionId, prompts }]
      : [];
  });
}
