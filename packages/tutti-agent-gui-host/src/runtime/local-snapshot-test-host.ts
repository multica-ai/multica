import { V2Conversation } from "@multica/core/local-conversation/conversation";
import { localV2ServerMessageSchema, localV2SnapshotSchema, type LocalV2Event } from "@multica/core/local-conversation/v2";
import type { AgentGuiSessionDetail, LocalConversationSource } from "../protocol";

type Request = (method: string, params: any, options?: { signal?: AbortSignal }) => Promise<any>;

/**
 * Test hosts describe conversations as `session.load` details. This source
 * builds conversations from the same detail, so a test's own `session.load`
 * overrides also shape them.
 */
export function localSourceFromClient(client: { request: Request; subscribe(listener: (event: never) => void): () => void }): LocalConversationSource & { opened: string[] } {
  const opened: string[] = [];
  const conversations = new Map<string, V2Conversation>();
  const details = new Map<string, AgentGuiSessionDetail>();
  const observers = new Map<string, Set<(events: readonly LocalV2Event[]) => void>>();
  const notify = (id: string, events: readonly LocalV2Event[]) => { for (const observer of [...observers.get(id) ?? []]) observer(events); };
  const read = async (sessionId: string) => {
    const detail = await client.request("session.load", { sessionId }) as AgentGuiSessionDetail;
    const conversation = V2Conversation.fromSnapshot(localV2SnapshotSchema.parse(snapshotFromDetail(detail)));
    details.set(sessionId, detail);
    conversations.set(sessionId, conversation);
    return conversation;
  };
  // Tests drive a conversation with these two events.
  client.subscribe(((event: { type: string; sessionId: string; message?: unknown }) => {
    if (event.type === "local.reload") { void read(event.sessionId).then(() => notify(event.sessionId, [])).catch(() => {}); return; }
    if (event.type !== "local.events") return;
    const parsed = localV2ServerMessageSchema.safeParse(event.message);
    if (!parsed.success) return;
    const reload = () => { void read(event.sessionId).then(() => notify(event.sessionId, [])).catch(() => {}); };
    if (parsed.data.type === "subscription.status") {
      if (["resubscribe", "snapshot_required", "rejected"].includes(parsed.data.status)) reload();
      return;
    }
    if (parsed.data.type !== "conversation.events") return;
    if (conversations.get(event.sessionId)?.apply(parsed.data.events) === "gap") { reload(); return; }
    notify(event.sessionId, parsed.data.events);
  }) as never);
  return {
    opened,
    open: (id) => { opened.push(id); return () => {}; },
    // Test hosts change their answers between reads, so this always reads.
    ensure: read,
    refresh: read,
    loadOlder: async () => {},
    observe(id, listener) {
      const set = observers.get(id) ?? new Set();
      observers.set(id, set);
      set.add(listener);
      return () => { set.delete(listener); };
    },
    conversation: (id) => conversations.get(id),
    detail: (id) => details.get(id) ?? null,
  };
}

function snapshotFromDetail(detail: AgentGuiSessionDetail) {
  const at = detail.session.updated_at;
  const ids = [...new Set([
    ...(detail.tasks ?? []).map((task) => task.id),
    ...detail.messages.map((message) => message.task_id).filter((id): id is string => Boolean(id)),
    ...(detail.pending.task_id ? [detail.pending.task_id] : []),
  ])];
  let seq = 0;
  const turns = ids.map((id, index) => {
    const task = detail.tasks?.find((value) => value.id === id);
    const running = detail.pending.task_id === id;
    const status = task?.status === "failed" || task?.status === "cancelled" ? task.status : running ? "running" : "completed";
    const own = detail.messages.filter((message) => message.task_id === id || (running && !message.task_id));
    return {
      id, turn_index: index, request_id: "", status, started_at: task?.created_at ?? at,
      messages: own.filter((message) => message.role === "user").map((message) => ({
        id: message.id, role: "user", content: message.content, attachment_ids: (message.attachments ?? []).map((attachment) => attachment.id), created_at: message.created_at,
      })),
      events: own.filter((message) => message.role === "assistant").map((message) => ({
        seq: ++seq, turn_id: id, turn_seq: seq, item_id: `reply-${message.id}`, type: "item.completed", final: true, created_at: message.created_at,
        payload: { kind: "assistant_text", status: "completed", text: message.content },
      })),
    };
  });
  return {
    version: 2, store_generation: "test", snapshot_seq: seq, has_older_turns: false, turns, pending_interactions: [],
    conversation: { conversation_id: detail.session.id, summary_seq: 1, provider: "claude", title: detail.session.title, status: detail.pending.task_id ? "running" : "idle", pinned: false, archived: false, updated_at: at },
  };
}
