import { ConversationServiceManager } from "@multica/conversation-client/services";
import { createFakeTransport } from "@multica/conversation-client/testing";
import { localV2ServerMessageSchema } from "@multica/core/local-conversation/v2";
import type {
AgentGuiSessionDetail,
LocalConversationSource
} from "../protocol";

type Request = (
  method: string,
  params: any,
  options?: { signal?: AbortSignal }
) => Promise<any>;

const scope = { userId: "test", workspaceId: "test" };

/**
 * Test hosts describe conversations as `session.load` details. This answers the
 * real service from the same detail, so a test's own `session.load` overrides
 * also shape what it reads.
 */
export function localSourceFromClient(
  client: import("../protocol").AgentGuiHostServices
): LocalConversationSource & { opened: string[] } {
  const opened: string[] = [];
  const details = new Map<string, AgentGuiSessionDetail>();
  const transport = createFakeTransport(scope, async (method, path) => {
    const id = /^\/sessions\/([^/]+)\/snapshot/.exec(path)?.[1];
    if (method !== "GET" || !id) return {};
    const sessionId = decodeURIComponent(id);
    const detail = (await client.session.load({
      sessionId
    })) as AgentGuiSessionDetail;
    details.set(sessionId, detail);
    return snapshotFromDetail(detail);
  });
  const service = new ConversationServiceManager({
    snapshots: () => ({ read: async () => null, write: async () => {} })
  }).get(transport, scope);
  const reload = (id: string) => {
    void service.refresh(id).catch(() => {});
  };
  // Tests drive a conversation with these two events.
  client.subscribe(((event: {
    type: string;
    sessionId: string;
    message?: unknown;
  }) => {
    if (event.type === "local.reload") {
      reload(event.sessionId);
      return;
    }
    if (event.type !== "local.events") return;
    const parsed = localV2ServerMessageSchema.safeParse(event.message);
    if (!parsed.success) return;
    if (parsed.data.type === "subscription.status") {
      if (
        ["resubscribe", "snapshot_required", "rejected"].includes(
          parsed.data.status
        )
      )
        reload(event.sessionId);
      return;
    }
    if (parsed.data.type !== "conversation.events") return;
    const conversation = service.conversation(event.sessionId);
    const after = conversation?.seq ?? 0;
    // A daemon answers a missed sequence with a snapshot here, rather than replaying.
    if (
      parsed.data.events.some((value, index) => value.seq > after + index + 1)
    ) {
      reload(event.sessionId);
      return;
    }
    transport.deliver({ ...parsed.data, conversation_id: event.sessionId });
  }) as never);
  return {
    opened,
    open: (id) => {
      opened.push(id);
      return service.open(id);
    },
    // Test hosts change their answers between reads, so this reads unless one is under way.
    ensure: (id) =>
      service.status(id).loading ? service.ensure(id) : service.refresh(id),
    refresh: (id) => service.refresh(id),
    loadOlder: async () => {},
    observe: (id, listener) => service.observe(id, listener),
    conversation: (id) => service.conversation(id),
    detail: (id) => details.get(id) ?? null
  };
}

function snapshotFromDetail(detail: AgentGuiSessionDetail) {
  const at = detail.session.updated_at;
  const ids = [
    ...new Set([
      ...(detail.tasks ?? []).map((task) => task.id),
      ...detail.messages
        .map((message) => message.task_id)
        .filter((id): id is string => Boolean(id)),
      ...(detail.pending.task_id ? [detail.pending.task_id] : [])
    ])
  ];
  let seq = 0;
  const turns = ids.map((id, index) => {
    const task = detail.tasks?.find((value) => value.id === id);
    const running = detail.pending.task_id === id;
    const status =
      task?.status === "failed" || task?.status === "cancelled"
        ? task.status
        : running
          ? "running"
          : "completed";
    const own = detail.messages.filter(
      (message) => message.task_id === id || (running && !message.task_id)
    );
    return {
      id,
      turn_index: index,
      request_id: "",
      status,
      started_at: task?.created_at ?? at,
      messages: own
        .filter((message) => message.role === "user")
        .map((message) => ({
          id: message.id,
          role: "user",
          content: message.content,
          attachment_ids: (message.attachments ?? []).map(
            (attachment) => attachment.id
          ),
          created_at: message.created_at
        })),
      events: own
        .filter((message) => message.role === "assistant")
        .map((message) => ({
          seq: ++seq,
          turn_id: id,
          turn_seq: seq,
          item_id: `reply-${message.id}`,
          type: "item.completed",
          final: true,
          created_at: message.created_at,
          payload: {
            kind: "assistant_text",
            status: "completed",
            text: message.content
          }
        }))
    };
  });
  return {
    version: 2,
    store_generation: "test",
    snapshot_seq: seq,
    has_older_turns: false,
    turns,
    pending_interactions: [],
    conversation: {
      conversation_id: detail.session.id,
      summary_seq: 1,
      provider: "claude",
      title: detail.session.title,
      status: detail.pending.task_id ? "running" : "idle",
      pinned: false,
      archived: false,
      updated_at: at
    }
  };
}
