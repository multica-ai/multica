import { z } from "zod";
import { localV2UploadSchema } from "@multica/core/local-conversation/v2";
import { useAIDrafts, type DraftAttachment } from "./drafts";
import { fileContentHash,retainSubmissionAsset,removeSubmissionAsset } from "./files";
import type { AISessionClient } from "./client";

const MAX_CONCURRENT_UPLOADS = 3;
/** An upload may take as long as it needs while its bytes keep moving. */
const UPLOAD_STALL_MS = 60_000;

export interface UploadAsset { uri: string; name: string; mimeType?: string | null }
type UploadClient = Pick<AISessionClient, "url" | "headers" | "request">;

/** XMLHttpRequest because fetch reports no upload progress to tell a slow file from a stalled one. */
export function sendFile(client: UploadClient, asset: UploadAsset, body: Blob, id: string, signal: AbortSignal, XHR: typeof XMLHttpRequest = XMLHttpRequest) {
  return new Promise<string>((resolve, reject) => {
    const xhr = new XHR();
    let watchdog: ReturnType<typeof setTimeout> | undefined;
    const finish = (result: Error | string) => {
      clearTimeout(watchdog);
      signal.removeEventListener("abort", aborted);
      if (typeof result === "string") resolve(result); else reject(result);
    };
    const watch = () => {
      clearTimeout(watchdog);
      watchdog = setTimeout(() => { finish(new Error("upload_stalled")); xhr.abort(); }, UPLOAD_STALL_MS);
    };
    const aborted = () => { finish(new Error("aborted")); xhr.abort(); };
    if (signal.aborted) { reject(new Error("aborted")); return; }
    signal.addEventListener("abort", aborted);
    xhr.open("POST", client.url("/attachments"));
    for (const [name, value] of Object.entries({ ...client.headers(), "Content-Type": asset.mimeType ?? "application/octet-stream", "X-File-Name": encodeURIComponent(asset.name), "X-Attachment-ID": id })) xhr.setRequestHeader(name, value);
    xhr.upload.onprogress = watch;
    xhr.onload = () => {
      const response = xhr.status >= 200 && xhr.status < 300 ? localV2UploadSchema.safeParse(JSON.parse(xhr.responseText || "null")) : null;
      finish(response?.success ? response.data.id : new Error("upload_failed"));
    };
    xhr.onerror = () => finish(new Error("upload_failed"));
    watch();
    xhr.send(body);
  });
}

/** 32 hex digits, the form the daemon accepts for a client-chosen id. */
function attachmentId() { return Array.from({ length: 32 }, () => Math.floor(Math.random() * 16).toString(16)).join(""); }

interface UploadDeps {
 retain?(key:string,uri:string):Promise<string>;
 discard?(key:string):Promise<void>;
  hash(uri: string): Promise<string | null>;
  /** Answers the stored id; a daemon too old to take the chosen one names its own. */
  transfer(client: UploadClient, asset: UploadAsset, id: string, signal: AbortSignal): Promise<string>;
  update(key: string, id: string, patch: Partial<DraftAttachment>): void;
}

interface Job {
  client: UploadClient;
  key: string;
  asset: UploadAsset;
  abort: AbortController;
  stopped: Promise<void>;
  ready?:Promise<void>;
  status: DraftAttachment["status"];
  storedId: string;
}

/**
 * Transfers a draft's attachments as soon as they are added, several at a time.
 * Each attachment has its final id from the start, so sending only has to wait
 * for the outcome. Content the device already has is not transferred again.
 */
export function createDraftUploads({ hash, transfer, update,retain,discard }: UploadDeps) {
  const jobs = new Map<string, Job>();
  const queue: (() => void)[] = [];
  let active = 0;
  const slot = async () => {
    if (active >= MAX_CONCURRENT_UPLOADS) await new Promise<void>((resolve) => { queue.push(resolve); });
    active++;
  };

  const attempt = async (job: Job, id: string) => {
    await job.ready;
    await slot();
    try {
      if (job.abort.signal.aborted) throw new Error("aborted");
      const sha256 = await hash(job.asset.uri);
      if (sha256) {
        try {
          await job.client.request("POST", "/attachments/link", localV2UploadSchema, { id, sha256, name: job.asset.name, mime_type: job.asset.mimeType ?? "application/octet-stream" });
          return id;
        } catch { if (job.abort.signal.aborted) throw new Error("aborted"); }
      }
      return await transfer(job.client, job.asset, id, job.abort.signal);
    } finally { active--; queue.shift()?.(); }
  };

  const run = (id: string, job: Job) => {
    job.status = "uploading";
    job.abort = new AbortController();
    update(job.key, id, { status: "uploading" });
    job.stopped = attempt(job, id).then(
      (storedId) => {
        job.status = "uploaded";
        job.storedId = storedId;
        if (jobs.get(id) === job) update(job.key, id, { status: "uploaded", ...(storedId === id ? {} : { id: storedId }) });
      },
      () => { job.status = "failed"; if (jobs.get(id) === job) update(job.key, id, { status: "failed" }); },
    );
  };

  return {
    /** Adds the attachment to the draft and starts moving it. */
    start(client: UploadClient, key: string, asset: UploadAsset) {
      const id = attachmentId();
      const job: Job = { client, key, asset, abort: new AbortController(), stopped: Promise.resolve(), status: "uploading", storedId: id };
      if(retain)job.ready=retain(`${key}:${id}`,asset.uri).then((uri)=>{job.asset={...asset,uri};if(jobs.get(id)===job)update(key,id,{uri,mimeType:asset.mimeType});});
      jobs.set(id, job);
      update(key, id, { id, name: asset.name, status: "uploading" });
      run(id, job);
      return id;
    },
    async prepare(ids:readonly string[]){
      return Promise.all(ids.flatMap((id)=>{const job=jobs.get(id);return job?[Promise.resolve(job.ready).then(()=>({id,uri:job.asset.uri,name:job.asset.name,mimeType:job.asset.mimeType}))]:[];}));
    },
    restore(client:UploadClient,key:string,files:readonly {id:string;uri:string;name:string;mimeType?:string|null}[]){
      for(const file of files){if(jobs.has(file.id))continue;const job:Job={client,key,asset:file,abort:new AbortController(),stopped:Promise.resolve(),status:"failed",storedId:file.id};jobs.set(file.id,job);}
    },
    /** Waits for these attachments and answers their stored ids; an earlier failure gets another attempt. Attachments it does not know are stored. */
    async ensure(ids: readonly string[]) {
      return Promise.all(ids.map(async (id) => {
        const job = jobs.get(id);
        if (!job) return id;
        if (job.status === "failed") run(id, job);
        await job.stopped;
        // run() may have just set "uploading", but the attempt has settled it since.
        if ((job.status as DraftAttachment["status"]) === "failed") throw new Error("upload_failed");
        return job.storedId;
      }));
    },
    /** A message owns these attachments now. */
    forget(ids: readonly string[]) { for (const id of ids){const job=jobs.get(id);jobs.delete(id);if(job)void discard?.(`${job.key}:${id}`).catch(()=>{});} },
    /** The draft dropped it: stop the transfer and discard what arrived. */
    remove(client: UploadClient, id: string) {
      const job = jobs.get(id);
      jobs.delete(id);
      job?.abort.abort();
      if(job)void discard?.(`${job.key}:${id}`).catch(()=>{});
      void (job?.stopped ?? Promise.resolve()).then(() => client.request("DELETE", `/attachments/${encodeURIComponent(id)}`, z.unknown())).catch(() => undefined);
    },
  };
}

export const draftUploads = createDraftUploads({
  hash: fileContentHash,
  retain:retainSubmissionAsset,discard:removeSubmissionAsset,
  transfer: async (client, asset, id, signal) => {
    const local = await fetch(asset.uri);
    return sendFile(client, asset, await local.blob(), id, signal);
  },
  update: (key, id, patch) => useAIDrafts.getState().setAttachments(key, (files) => files.some((file) => file.id === id)
    ? files.map((file) => file.id === id ? { ...file, ...patch } : file)
    : [...files, patch as DraftAttachment]),
});
