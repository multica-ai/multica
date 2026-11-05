import { fakeHostServices } from "./fake-host-services";
// Test rig: two GUI instances beside app UI in one document, under the app's own stylesheet.
import { ConversationServiceManager } from "@multica/conversation-client/services";
import { createFakeTransport } from "@multica/conversation-client/testing";
import { LOCAL_V2_NAVIGATION_CAPABILITY, type LocalV2SnapshotTurn } from "@multica/core/local-conversation/v2";
import { useMemo,useState } from "react";
import { createRoot } from "react-dom/client";
import "../../../apps/desktop/src/renderer/src/globals.css";
import { createHostServicesConnection } from "../src/host-services-connection";
import AgentGuiMount from "../src/mount";
import type {
AgentGuiHostEvent,
AgentGuiInitSnapshot,
LocalConversationSource
} from "../src/protocol";
import { localSourceFromClient } from "../src/runtime/local-snapshot-test-host";

const at = "2026-11-03T10:00:00Z";
const listeners = new Set<(event: AgentGuiHostEvent) => void>();
function snapshot(
  id: string,
  theme: "light" | "dark",
  sidePane: boolean
): AgentGuiInitSnapshot {
  const session = {
    id,
    workspace_id: "w",
    creator_id: "u",
    agent_id: "local:codex",
    title: id,
    status: "active",
    created_at: at,
    updated_at: at
  };
  return {
    workspaceId: "w",
    workspaceSlug: "w",
    userId: "u",
    locale: "en",
    theme,
    sidePane,
    sideWorkspace: true,
    local: true,
    hostActive: true,
    hostVisible: true,
    agents: [],
    sessions: [session],
    activeSessionId: id,
    selectedAgentId: "local:codex",
    executionTargets: [
      {
        id: "local:codex",
        provider: "codex",
        name: "Codex",
        iconUrl: "",
        canInvoke: true,
        runtimeBound: true,
        runtimeOnline: true
      }
    ]
  } as unknown as AgentGuiInitSnapshot;
}
const storage = new Map<string, string>();
const hostActions: { instance: string; method: string; params: unknown }[] = [];
Object.assign(window, { hostActions });
function service(id: string, theme: "light" | "dark", sidePane: boolean) {
  const session = snapshot(id, theme, sidePane).sessions[0]!;
  const request = async (
    method: string,
    params: { key?: string; value?: string }
  ) => {
    hostActions.push({ instance: id, method, params });
    switch (method) {
      case "workspace.load":
        return snapshot(id, theme, sidePane);
      case "session.load":
        return {
          session,
          messages: [
            {
              id: `${id}-q`,
              chat_session_id: id,
              role: "user",
              content: `Question for ${id}`,
              task_id: `${id}-t`,
              created_at: at
            },
            {
              id: `${id}-a`,
              chat_session_id: id,
              role: "assistant",
              content: `Reply in ${id}`,
              task_id: `${id}-t`,
              created_at: at
            }
          ],
          tasks: [],
          pending: {}
        };
      case "storage.read":
        return storage.get(params.key!) ?? null;
      case "storage.write":
        storage.set(params.key!, params.value!);
        return undefined;
      case "models.list":
        return {
          id: "m",
          runtime_id: "local:codex",
          status: "completed",
          supported: true,
          models: [{ id: "fixture-model", label: "Fixture model", thinking: { supported_levels: [{ value: "low", label: "Low" }, { value: "high", label: "High" }] } }]
        };
      case "usage.read":
        return {
          provider: "codex",
          context: {
            usedTokens: 100,
            limitTokens: 1000,
            model: null,
            updatedAt: null
          },
          account: {
            status: "available",
            windows: [{ id: "codex:five_hour", label: "5h", usedPercent: 20 }],
            plan: null,
            updatedAt: null
          }
        };
      case "context.tasks":
        return [];
      case "host.openSide":
        return { ...session, id: `${id}-side` };
      default:
        return undefined;
    }
  };
  return fakeHostServices(
    request,
    (listener: (event: AgentGuiHostEvent) => void) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    }
  );
}

const TOTAL_TURNS = new URLSearchParams(location.search).has("small") ? 7 : new URLSearchParams(location.search).has("navigationProfile") ? 500 : new URLSearchParams(location.search).has("navigation") ? 86 : 60,
  PAGE_TURNS = 10;
const paging = {
  navigation: [] as string[],
  navigationDelay: 100,
  failNavigation: false,
  reads: 0,
  historyDelay: 20,
  failing: new URLSearchParams(location.search).has("failOlder"),
  started: [] as number[]
};
Object.assign(window, { paging });
function turnOf(index: number): LocalV2SnapshotTurn {
  const id = `turn-${index}`;
  const started = new Date(Date.parse(at) + index * 60_000).toISOString();
  return {
    id,
    turn_index: index,
    request_id: id,
    status: "completed",
    started_at: started,
    completed_at: started,
    messages: [
      {
        id: `${id}-q`,
        role: "user",
        content: `Question ${index}`,
        attachment_ids: [],
        steered: false,
        failed: false,
        created_at: started
      }
    ],
    events: [
      ...(index === TOTAL_TURNS - 1
        ? [
            {
              seq: index * 2 + 1,
              turn_id: id,
              turn_seq: 1,
              item_id: `${id}-command`,
              type: "item.completed",
              final: true,
              created_at: started,
              payload: {
                kind: "command",
                status: "completed",
                command: "fixture-inspect",
                output: "Fixture tool output",
                exit_code: 0,
                duration_ms: 100
              }
            }
          ]
        : []),
      {
        seq: index * 2 + 2,
        turn_id: id,
        turn_seq: index === TOTAL_TURNS - 1 ? 2 : 1,
        item_id: `${id}-a`,
        type: "item.completed",
        final: true,
        created_at: started,
        payload: {
          kind: "assistant_text",
          status: "completed",
          text: `Answer ${index}\n\n${"Line of a long reply. ".repeat(40)}`
        }
      }
    ]
  };
}
/** The real service paging a conversation, with the daemon's answers given here. */
function pagedSource(id: string): LocalConversationSource {
  const scope = { userId: "u", workspaceId: "w" };
  const conversationSummary = {
    conversation_id: id,
    summary_seq: 1,
    provider: "codex",
    title: id,
    status: "idle",
    pinned: false,
    archived: false,
    updated_at: at,
    turn_count: TOTAL_TURNS
  };
  const transport = createFakeTransport(scope, async (_method, path) => {
    if (path.includes("/navigation")) {
      paging.navigation.push(path);
      const query = new URL(path, "http://fixture").searchParams;
      const base = { store_generation: "g", snapshot_seq: TOTAL_TURNS * 2, node_count: TOTAL_TURNS };
      if (!query.has("node_index") && !query.has("before_index") && !query.has("after_index")) return { ...base, nodes: [], turns: [], range: null };
      await new Promise((resolve) => setTimeout(resolve, paging.navigationDelay));
      if (paging.failNavigation) throw new Error("Navigation fixture unavailable");
      const center = Number(query.get("node_index"));
      const limit = Number(query.get("limit") ?? PAGE_TURNS);
      const before = Number(query.get("before") ?? 4);
      const after = Number(query.get("after") ?? 5);
      const start = query.has("before_index") ? Math.max(0, Number(query.get("before_index")) - limit) : query.has("after_index") ? Number(query.get("after_index")) + 1 : Math.max(0, center - before);
      const end = Math.min(TOTAL_TURNS - 1, query.has("before_index") ? Number(query.get("before_index")) - 1 : query.has("after_index") ? start + limit - 1 : center + after);
      const turns = Array.from({ length: end - start + 1 }, (_, index) => turnOf(start + index));
      const range = { start_index: start, end_index: end, has_before: start > 0, has_after: end < TOTAL_TURNS - 1 };
      return { ...base, turns: query.get("include_body") === "false" ? [] : turns, directory_range: range, range: query.get("include_body") === "false" ? null : range,
        nodes: turns.map((turn) => ({ node_index: turn.turn_index, turn_id: turn.id, turn_index: turn.turn_index, message_id: `${turn.id}-q`, summary: `Question ${turn.turn_index}`, has_agent_response: true })) };
    }
    if (path.includes("/snapshot")) {
      const count = Math.min(TOTAL_TURNS, Number(new URL(path, "http://fixture").searchParams.get("turn_limit") ?? PAGE_TURNS));
      return {
        version: 2,
        store_generation: "g",
        snapshot_seq: TOTAL_TURNS * 2,
        has_older_turns: true,
        conversation: conversationSummary,
        turns: Array.from({ length: count }, (_, i) =>
          turnOf(TOTAL_TURNS - count + i)
        )
      };
    }
    const before = Number(/before_index=(\d+)/.exec(path)?.[1] ?? 0);
    if (!path.includes("/history")) return {};
    paging.reads++;
    paging.started.push(before);
    await new Promise((resolve) => setTimeout(resolve, paging.historyDelay));
    if (paging.failing) throw new Error("device_unreachable: offline");
    const limit = Number(new URL(path, "http://fixture").searchParams.get("turn_limit") ?? PAGE_TURNS);
    const from = Math.max(0, before - limit);
    return {
      store_generation: "g",
      has_older_turns: from > 0,
      turns: Array.from({ length: before - from }, (_, i) => turnOf(from + i))
    };
  });
  transport.capabilities = async () => new URLSearchParams(location.search).has("navigation") ? [LOCAL_V2_NAVIGATION_CAPABILITY] : [];
  const service = new ConversationServiceManager({
    snapshots: () => ({ read: async () => null, write: async () => {} })
  }).get(transport, scope);
  const session = snapshot(id, "light", false).sessions[0]!;
  return {
    navigation: () => service.navigation(id),
    open: () => service.open(id),
    ensure: () => service.ensure(id),
    refresh: () => service.refresh(id),
    loadOlder: () => service.loadOlder(id),
    observe: (_id, listener) => service.observe(id, listener),
    conversation: () => service.conversation(id),
    detail: () =>
      ({
        session,
        model: null,
        reasoningEffort: null,
        permissionMode: "default",
        promptQueue: { revision: 0, paused: false, items: [] },
        messages: [],
        pending: {},
        tasks: []
      }) as never
  };
}

function Instance({
  id,
  theme,
  sidePane
}: {
  id: string;
  theme: "light" | "dark";
  sidePane: boolean;
}) {
  const { connection, source } = useMemo(() => {
    const host = service(id, theme, sidePane);
    return {
      connection: createHostServicesConnection(host as never, {
        interactive: () => true,
        current: () => true
      }),
      source:
        id === "paged-conversation"
          ? pagedSource(id)
          : localSourceFromClient(host as never)
    };
  }, [id, theme, sidePane]);
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col" data-instance={id}>
      <AgentGuiMount
        client={connection.client}
        initial={snapshot(id, theme, sidePane)}
        localConversations={source}
      />
    </div>
  );
}

function Page() {
  const [mounted, setMounted] = useState(false);
  const [theme, setTheme] = useState<"light" | "dark">("light");
  const [paged, setPaged] = useState(false);
  return (
    <div className="flex h-dvh bg-background text-foreground">
      <aside
        data-testid="app-aside"
        className="flex w-56 shrink-0 flex-col gap-3 border-r p-4 text-sm"
      >
        <button
          id="toggle"
          className="rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground"
          onClick={() => setMounted((value) => !value)}
        >
          Toggle GUI
        </button>
        <button
          id="paged"
          className="rounded-md border px-3 py-2 text-sm"
          onClick={() => setPaged((value) => !value)}
        >
          Toggle long conversation
        </button>
        <button
          id="theme"
          className="rounded-md border px-3 py-2 text-sm"
          onClick={() =>
            setTheme((value) => (value === "dark" ? "light" : "dark"))
          }
        >
          Toggle theme
        </button>
        <h1 id="heading" className="text-lg font-semibold">
          App heading
        </h1>
        <p id="paragraph" className="text-sm text-muted-foreground">
          App text must not change when the GUI mounts.
        </p>
      </aside>
      {paged && (
        <div className="flex min-h-0 min-w-0 flex-1">
          <Instance id="paged-conversation" theme={theme} sidePane={false} />
        </div>
      )}
      {mounted && (
        <div key={theme} className="flex min-h-0 min-w-0 flex-1">
          <Instance id="main-conversation" theme={theme} sidePane={false} />
          <div className="w-px bg-border" />
          <Instance id="side-conversation" theme={theme} sidePane />
        </div>
      )}
    </div>
  );
}

createRoot(document.getElementById("app-root")!).render(<Page />);
