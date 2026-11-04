import type { ConversationService } from "@multica/conversation-client/services";

/** The daemon can name attachments for clients, link stored content and discard uploads. */
export const ATTACHMENT_RESERVE_CAPABILITY = "attachment-reserve-v1";
const MAX_CONCURRENT_UPLOADS = 4;

export interface StartedUpload { id: string; name: string; mime_type: string; size: number; uploading: boolean }
export type UploadOutcome = { attachmentId: string; state: "uploaded" } | { attachmentId: string; state: "failed"; message: string };

interface Job {
  id: string;
  file: File;
  abort: AbortController;
  /** Settles when the transfer has stopped, however it ended. */
  stopped: Promise<void>;
  state: "active" | "uploaded" | "failed";
}

export async function contentHash(file: File): Promise<string | null> {
  try {
    const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
    return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
  } catch { return null; }
}

/**
 * Transfers attachments while the draft that holds them carries on. An
 * attachment has its final id the moment it is added, so a message can name it
 * before its bytes arrive; the sender waits for the outcome instead of the
 * composer. Content the daemon already has is never transferred again.
 */
export function createLocalV2Uploads(client: Pick<ConversationService, "upload" | "linkUpload" | "discardUpload">, capabilities: () => Promise<string[]>, onOutcome: (outcome: UploadOutcome) => void, hash: (file: File) => Promise<string | null> = contentHash) {
  const jobs = new Map<string, Job>();
  const waiting: (() => void)[] = [];
  let active = 0;
  let disposed = false;

  const slot = async () => {
    if (active >= MAX_CONCURRENT_UPLOADS) await new Promise<void>((resolve) => { waiting.push(resolve); });
    active++;
  };
  const release = () => { active--; waiting.shift()?.(); };

  const transfer = async (job: Job) => {
    await slot();
    try {
      if (job.abort.signal.aborted) throw new DOMException("Aborted", "AbortError");
      const sha256 = await hash(job.file);
      if (sha256) {
        try {
          await client.linkUpload({ id: job.id, sha256, name: job.file.name, mime_type: job.file.type || "application/octet-stream" });
          return;
        } catch (error) {
          // not_found is the common case. Any other failure may also be the
          // link itself, which the upload below reports with its own outcome.
          if (job.abort.signal.aborted) throw error;
        }
      }
      await client.upload(job.file, { id: job.id, signal: job.abort.signal });
    } finally { release(); }
  };

  const run = (job: Job) => {
    job.state = "active";
    job.abort = new AbortController();
    const attempt = transfer(job).then(
      () => {
        job.state = "uploaded";
        if (!disposed && jobs.get(job.id) === job) onOutcome({ attachmentId: job.id, state: "uploaded" });
      },
      (error: unknown) => {
        job.state = "failed";
        if (!disposed && jobs.get(job.id) === job && !job.abort.signal.aborted) {
          onOutcome({ attachmentId: job.id, state: "failed", message: error instanceof Error ? error.message : String(error) });
        }
      },
    );
    job.stopped = attempt;
  };

  return {
    /** Starts a transfer and answers at once when the daemon can name the attachment up front. */
    async begin(file: File): Promise<StartedUpload> {
      const supported = await capabilities().then((values) => values.includes(ATTACHMENT_RESERVE_CAPABILITY), () => false);
      if (!supported) return { ...await client.upload(file), uploading: false };
      const id = crypto.randomUUID().replaceAll("-", "");
      const job: Job = { id, file, abort: new AbortController(), stopped: Promise.resolve(), state: "active" };
      jobs.set(id, job);
      run(job);
      return { id, name: file.name, mime_type: file.type || "application/octet-stream", size: file.size, uploading: true };
    },
    /** Starts a failed transfer again. */
    retry(id: string) {
      const job = jobs.get(id);
      if (!job) throw new Error("not_found: This attachment is no longer being uploaded");
      if (job.state === "failed") run(job);
    },
    /** Stops a transfer and removes what reached the daemon; a message that used it keeps it. */
    async cancel(id: string) {
      const job = jobs.get(id);
      if (!job) return;
      jobs.delete(id);
      job.abort.abort();
      await job.stopped;
      await client.discardUpload(id).catch(() => undefined);
    },
    /** A message now owns the attachment. */
    forget(id: string) { jobs.delete(id); },
    dispose() {
      disposed = true;
      for (const job of jobs.values()) job.abort.abort();
      jobs.clear();
    },
  };
}
