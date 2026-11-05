import { ConversationServiceManager } from "@multica/conversation-client/services";
import { createFakeTransport } from "@multica/conversation-client/testing";
import { createRoot } from "react-dom/client";
import "../../../apps/desktop/src/renderer/src/globals.css";
import AgentGuiMount from "../src/mount";
import type {
AgentGuiHostEvent,
AgentGuiHostServices,
AgentGuiInitSnapshot,
AgentGuiOperationInvoker,
LocalConversationSource
} from "../src/protocol";
import { fakeHostServices } from "./fake-host-services";

const at = "2026-11-04T10:00:00Z";
const scope = { userId: "fixture-user", workspaceId: "fixture-workspace" };
const sessions = ["first", "second"].map((id) => ({
  id,
  workspace_id: scope.workspaceId,
  creator_id: scope.userId,
  has_unread: false,
  agent_id: "local:codex",
  title: id,
  status: "active" as const,
  created_at: at,
  updated_at: at
}));
let initial: AgentGuiInitSnapshot = {
  workspaceId: scope.workspaceId,
  workspaceSlug: "fixture",
  userId: scope.userId,
  locale: "en",
  theme: "light",
  local: true,
  hostActive: true,
  hostVisible: true,
  agents: [],
  sessions,
  activeSessionId: "first",
  selectedAgentId: "local:codex",
  executionTargets: [
    {
      id: "local:codex",
      provider: "codex",
      name: "Codex",
      iconUrl: "",
      canInvoke: true,
      runtimeBound: true,
      runtimeOnline: true,
      canFork: true
    }
  ]
};
const listeners = new Set<(event: AgentGuiHostEvent) => void>();
const storage = new Map<string, string>();
let seq = 1_000,
  delivered = 0;
const transport = createFakeTransport(scope, (_method, path) => {
  const id = decodeURIComponent(
    /^\/sessions\/([^/]+)/.exec(path)?.[1] ?? "first"
  );
  return {
    version: 2,
    store_generation: "fixture",
    snapshot_seq: 1_000,
    has_older_turns: false,
    conversation: {
      conversation_id: id,
      summary_seq: 1,
      provider: "codex",
      title: id,
      status: "running",
      pinned: false,
      archived: false,
      updated_at: at
    },
    turns: Array.from({ length: 120 }, (_, index) => {
      const live = index === 119,
        turnId = `turn-${index}`,
        stamp = new Date(Date.parse(at) + index * 1_000).toISOString();
      return {
        id: turnId,
        turn_index: index,
        request_id: `request-${index}`,
        status: live ? "running" : "completed",
        started_at: stamp,
        ...(!live ? { completed_at: stamp } : {}),
        messages: [
          {
            id: `prompt-${index}`,
            role: "user",
            content: `Prompt ${index} in ${id}`,
            attachment_ids: [],
            created_at: stamp
          }
        ],
        events: [
          {
            seq: live ? 1_000 : index + 1,
            turn_id: turnId,
            turn_seq: 1,
            item_id: `reply-${index}`,
            type: live ? "item.started" : "item.completed",
            final: !live,
            created_at: stamp,
            payload: {
              kind: "assistant_text",
              status: live ? "in_progress" : "completed",
              text: live
                ? "Live reply"
                : `Reply ${index} in ${id}. ${"Historical answer. ".repeat(25)}`
            }
          }
        ]
      };
    })
  };
});
const service = new ConversationServiceManager({
  snapshots: () => ({ read: async () => null, write: async () => {} })
}).get(transport, scope);
const source: LocalConversationSource = {
  open: (id) => service.open(id),
  ensure: (id) => service.ensure(id),
  refresh: (id) => service.refresh(id),
  loadOlder: (id) => service.loadOlder(id),
  observe: (id, listener) => service.observe(id, listener),
  conversation: (id) => service.conversation(id),
  detail: (id) => ({
    session: sessions.find((session) => session.id === id)!,
    messages: [],
    tasks: [],
    pending: {},
    promptQueue: { revision: 0, paused: false, items: [] }
  })
};
const client: AgentGuiHostServices = fakeHostServices(
  (async (method: string, params: any) => {
    if (method === "workspace.load") return initial;
    if (method === "storage.read") return storage.get(params.key) ?? null;
    if (method === "storage.write") {
      storage.set(params.key, params.value);
      return;
    }
    if (method === "host.selectSession" || method === "host.showMessage")
      return;
    if (method === "models.list") return { supported: true, models: [] };
    if (method === "usage.read") return null;
    if (method === "context.tasks") return [];
    throw new Error(`Unexpected verification request: ${method}`);
  }) as AgentGuiOperationInvoker,
  (listener) => {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  }
);
function publish() {
  for (const listener of listeners)
    listener({ type: "init", snapshot: initial });
}
const fixture = {
  version: 1,
  turnCount: 120,
  select(id: string) {
    initial = { ...initial, activeSessionId: id };
    publish();
  },
  theme(theme: "light" | "dark") {
    initial = { ...initial, theme };
    publish();
  },
  async stream(count = 360, rate = 120) {
    const started = performance.now();
    return new Promise<{ delivered: number; elapsedMs: number }>((resolve) => {
      let emitted = 0;
      const timer = setInterval(() => {
        emitted++;
        delivered++;
        const event = {
          seq: ++seq,
          turn_id: "turn-119",
          turn_seq: seq,
          item_id: "reply-119",
          type: "item.delta",
          final: false,
          created_at: at,
          payload: {
            kind: "assistant_text",
            field: "text",
            text: ` streamed-token-${String(delivered).padStart(4, "0")}`
          }
        };
        transport.deliver({
          version: 2,
          type: "conversation.events",
          conversation_id: "first",
          from_seq: seq,
          to_seq: seq,
          events: [event]
        });
        if (emitted === count) {
          clearInterval(timer);
          resolve({ delivered, elapsedMs: performance.now() - started });
        }
      }, 1_000 / rate);
    });
  }
};
Object.assign(window, { conversationFixture: fixture });
createRoot(document.getElementById("app-root")!).render(
  <AgentGuiMount
    client={client}
    initial={initial}
    localConversations={source}
  />
);
