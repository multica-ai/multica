import type { PendingSend } from "./drafts";

/** Rebuild exclusively from the persisted request, so retries cannot change recipients. */
export function aiSubmission(request: PendingSend) {
  const prepared = request.prepared;
  const text = prepared?.text ?? request.text;
  const context = prepared ? { prompt: prepared.prompt, references: prepared.references, agents: prepared.agents } : {};
  if (!request.queued) return { type: "send", request_id: request.requestId, text, attachment_ids: request.attachments, ...context };
  return { type: "queue_enqueue", paused: request.queuePaused ?? false, item: {
    requestId: request.requestId, content: prepared?.text ?? request.queueContent ?? request.text, attachmentIds: request.attachments,
    ...context,
    display: { id: request.requestId, clientSubmitId: request.requestId, createdAtUnixMs: request.createdAt, content: [{ type: "text", text }] },
  } };
}
