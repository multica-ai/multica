import type { PendingSend } from "./drafts";

/** Rebuild exclusively from the persisted request, so retries cannot change recipients. */
export function aiSubmission(request: PendingSend,lifecycle=false,settings?:{model?:string|null;reasoning_effort?:string|null;permission_mode?:string}) {
  const prepared = request.prepared;
  const text = prepared?.text ?? request.text;
  const context = prepared ? { prompt: prepared.prompt, references: prepared.references, agents: prepared.agents } : {};
  if(lifecycle)return {type:"submit",item:{requestId:request.requestId,content:prepared?.text??request.queueContent??request.text,attachmentIds:request.attachments,...context,model:settings?.model??null,reasoningEffort:settings?.reasoning_effort??null,permissionMode:settings?.permission_mode??"default",display:{id:request.requestId,clientSubmitId:request.requestId,createdAtUnixMs:request.createdAt,content:[{type:"text",text}]}}};
  if (!request.queued) return { type: "send", request_id: request.requestId, text, attachment_ids: request.attachments, ...context };
  return { type: "queue_enqueue", paused: request.queuePaused ?? false, item: {
    requestId: request.requestId, content: prepared?.text ?? request.queueContent ?? request.text, attachmentIds: request.attachments,
    ...context,
    display: { id: request.requestId, clientSubmitId: request.requestId, createdAtUnixMs: request.createdAt, content: [{ type: "text", text }] },
  } };
}

/** Only an unconfirmed request with retained history in the same executor store may replay automatically. */
export function canReplaySubmission(record:{confirmed:boolean;lifecycle:boolean;generation:string},reply:{known_history:boolean;store_generation:string}){
 return !record.confirmed&&record.lifecycle&&record.generation===reply.store_generation&&reply.known_history;
}
