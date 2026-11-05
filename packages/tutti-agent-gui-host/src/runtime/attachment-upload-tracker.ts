import {
agentComposerDraftFiles,
agentComposerDraftImages,
agentComposerDraftLargeTexts
} from "../../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/model/agentComposerDraft";
import type { AgentComposerDraft } from "../../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import type { AgentGuiHostEvent,AgentGuiHostServices } from "../protocol";

type UploadEvent = Extract<AgentGuiHostEvent, { type: "attachment.upload" }>;
interface Upload {
  name?: string;
  outcome?: UploadEvent;
  waiters: { resolve(): void; reject(error: Error): void }[];
}

/** The attachments a draft holds, whichever kind of chip shows them. */
export function composerDraftAttachmentIds(
  draft: AgentComposerDraft
): Set<string> {
  const ids = [
    ...agentComposerDraftImages(draft).map((image) => image.attachmentId),
    ...agentComposerDraftFiles(draft).map((file) => file.assetId),
    ...agentComposerDraftLargeTexts(draft).map((item) => item.assetId)
  ];
  return new Set(ids.filter((id): id is string => Boolean(id)));
}

/**
 * Follows the transfers the parent runs in the background, from adding an
 * attachment until a message owns it. Attachments it does not know were
 * already stored, so there is nothing to wait for.
 */
export function createAttachmentUploadTracker(
  client: AgentGuiHostServices,
  locale?: string
) {
  const uploads = new Map<string, Upload>();
  const listeners = new Set<() => void>();
  let active = 0;
  const refresh = () => {
    const count = [...uploads.values()].filter(
      (upload) => !upload.outcome && upload.name !== undefined
    ).length;
    if (count === active) return;
    active = count;
    for (const listener of listeners) listener();
  };
  const failure = (
    upload: Upload,
    event: Extract<UploadEvent, { state: "failed" }>
  ) =>
    new Error(
      locale === "zh-CN"
        ? `${upload.name ?? "附件"} 上传失败：${event.message}`
        : `Could not upload ${upload.name ?? "the attachment"}: ${event.message}`
    );

  return {
    track(id: string, name: string) {
      const upload = uploads.get(id);
      if (upload) upload.name = name;
      else uploads.set(id, { name, waiters: [] });
      refresh();
    },
    settle(event: UploadEvent) {
      // An outcome can overtake the response that announced the transfer.
      const upload = uploads.get(event.attachmentId) ?? { waiters: [] };
      uploads.set(event.attachmentId, upload);
      upload.outcome = event;
      const waiters = upload.waiters.splice(0);
      for (const waiter of waiters) {
        if (event.state === "uploaded") waiter.resolve();
        else waiter.reject(failure(upload, event));
      }
      refresh();
      if (event.state === "failed" && !waiters.length) {
        void client.hostApi
          .showMessage({
            message: failure(upload, event).message,
            tone: "error"
          })
          .catch(() => undefined);
      }
    },
    /** Waits for these attachments; an earlier failure gets another attempt. */
    async ensure(ids: readonly string[]) {
      await Promise.all(
        ids.map(async (id) => {
          const upload = uploads.get(id);
          if (!upload || upload.outcome?.state === "uploaded") return;
          if (upload.outcome) {
            upload.outcome = undefined;
            refresh();
            await client.attachment.retry({ attachmentId: id });
          }
          await new Promise<void>((resolve, reject) => {
            upload.waiters.push({ resolve, reject });
          });
        })
      );
    },
    /** Messages own these attachments now. */
    forget(ids: readonly string[]) {
      for (const id of ids) {
        if (uploads.delete(id))
          void client.attachment
            .forget({ attachmentId: id })
            .catch(() => undefined);
      }
      refresh();
    },
    /** The draft dropped these: stop their transfers and discard what arrived. */
    remove(ids: readonly string[]) {
      for (const id of ids) {
        const upload = uploads.get(id);
        if (!upload) continue;
        uploads.delete(id);
        for (const waiter of upload.waiters)
          waiter.reject(
            new Error(
              locale === "zh-CN" ? "附件已移除" : "The attachment was removed"
            )
          );
        void client.attachment
          .cancel({ attachmentId: id })
          .catch(() => undefined);
      }
      refresh();
    },
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    activeCount: () => active
  };
}
