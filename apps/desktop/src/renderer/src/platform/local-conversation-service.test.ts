// @vitest-environment node
import { CREATE_TASK_MENTION_ID } from "@multica/core/local-conversation/protocol";
import type { LocalV2Transport } from "@multica/core/local-conversation/v2";
import type { StorageAdapter } from "@multica/core/types";
import { QueryClient } from "@tanstack/react-query";
import { afterEach,beforeEach,describe,expect,it,vi } from "vitest";
import { createLocalConversationParentService } from "./local-conversation-service";

const mock = vi.hoisted(() => ({
  api: {
    getBaseUrl: vi.fn(),
    listAgents: vi.fn(),
    listMembers: vi.fn(),
    listRuntimes: vi.fn(),
    listChatSessions: vi.fn(),
    listPendingChatTasks: vi.fn(),
    getIssue: vi.fn()
  }
}));
vi.mock("@multica/core/api", async (original) => ({
  ...(await original<typeof import("@multica/core/api")>()),
  api: mock.api
}));

const scope = { userId: "user", workspaceId: "ws" };
const at = "2026-10-29T00:00:00Z";
const summary = (id: string, fields: Record<string, unknown> = {}) => ({
  conversation_id: id,
  summary_seq: 1,
  provider: "codex",
  title: id,
  status: "idle",
  cwd: "/repo",
  project_id: null,
  pinned: false,
  archived: false,
  updated_at: at,
  ...fields
});
const snapshot = (id: string, fields: Record<string, unknown> = {}) => ({
  version: 2,
  store_generation: "g",
  snapshot_seq: 4,
  conversation: summary(id),
  turns: [],
  settings: {
    model: "gpt",
    reasoning_effort: "high",
    permission_mode: "default"
  },
  queue: { revision: 2, paused: false, items: [] },
  created_at: at,
  ...fields
});

type Route = (
  method: string,
  path: string,
  body?: Record<string, unknown>
) => unknown;
let routes: Route[];
let requests: {
  method: string;
  path: string;
  body?: Record<string, unknown>;
}[];
let sent: unknown[];
let messages: ((scopeValue: typeof scope, message: unknown) => void)[];
let connections: Set<(scopeValue: typeof scope, connected: boolean) => void>;
const disposers: (() => void)[] = [];

function transport(): LocalV2Transport {
  return {
    request: async (_scope, method, path, body) => {
      requests.push({
        method,
        path,
        body: body as Record<string, unknown> | undefined
      });
      for (const route of routes) {
        const value = route(
          method,
          path,
          body as Record<string, unknown> | undefined
        );
        if (value !== undefined)
          return value instanceof Error ? Promise.reject(value) : value;
      }
      throw new Error(`http_404: ${method} ${path}`);
    },
    send: async (_scope, message) => {
      sent.push(message);
    },
    subscribe: (listener) => {
      messages.push(listener);
      return () => {};
    },
    onConnection: (listener) => {
      connections.add(listener);
      return () => {
        connections.delete(listener);
      };
    },
    fileUrl: (_scope, path) => `v2:${path}`,
    upload: async (_scope, file) => ({
      id: "att-1",
      name: file.name,
      mime_type: file.type,
      size: file.size
    }),
    choose: vi.fn()
  };
}

function setup(
  options: {
    queryClient?: QueryClient;
    getSelectionIntent?: () => string;
    getSelectedSessionId?: () => string | null;
    storagePrefix?: string;
    userId?: string;
    workspaceId?: string;
    storage?: StorageAdapter;
  } = {},
  deviceId?: string,
  using = transport()
) {
  const callbacks = {
    onLocalSelect: vi.fn(),
    onOpenSide: vi.fn(),
    onShowMessage: vi.fn()
  };
  const qc = options.queryClient ?? new QueryClient();
  const service = createLocalConversationParentService(
    {
      queryClient: qc,
      workspaceId: "ws",
      workspaceSlug: "workspace",
      userId: "user",
      isCurrent: () => true,
      resolveAvatar: (value) => value,
      onOpenTask: vi.fn(),
      ...callbacks,
      ...options
    },
    Object.assign(using, deviceId ? { deviceId } : {})
  );
  disposers.push(() => {
    service.dispose();
    qc.clear();
  });
  return { service, ...callbacks };
}

beforeEach(() => {
  vi.resetAllMocks();
  routes = [];
  requests = [];
  sent = [];
  messages = [];
  connections = new Set();
  mock.api.getBaseUrl.mockReturnValue("https://example.test");
  mock.api.listAgents.mockResolvedValue([
    {
      id: "design",
      workspace_id: "ws",
      owner_id: "user",
      name: "Design reviewer",
      description: "",
      runtime_id: "cloud-runtime",
      runtime_bound: true,
      runtime_config: {},
      visibility: "workspace",
      archived_at: null,
      permission_mode: "private",
      invocation_targets: []
    }
  ]);
  mock.api.listMembers.mockResolvedValue([{ user_id: "user", role: "owner" }]);
  mock.api.listRuntimes.mockResolvedValue([
    { id: "cloud-runtime", provider: "codex" }
  ]);
  mock.api.listChatSessions.mockResolvedValue([]);
  mock.api.listPendingChatTasks.mockResolvedValue({ tasks: [] });
  routes.push((method, path) => {
    if (path === "/providers")
      return {
        providers: [
          { id: "codex", name: "Codex", available: true },
          { id: "claude", name: "Claude Code", available: false }
        ]
      };
    if (path === "/folder-projects" && method === "GET")
      return {
        projects: [
          { id: "folder-1", title: "Repo", path: "/repo", created_at: at }
        ]
      };
    if (path.startsWith("/sessions?"))
      return {
        scope_seq: 3,
        summaries: [
          summary("idle"),
          summary("busy", { status: "running", active_turn_id: "turn-9" })
        ],
        has_more: false
      };
    return undefined;
  });
});
afterEach(() => {
  disposers.splice(0).forEach((dispose) => dispose());
});

describe("local conversation parent service over v2", () => {
  it("loads member user IDs and avatars and sends a member mention without invoking an agent", async () => {
    mock.api.listMembers.mockResolvedValue([{ id: "membership-row", user_id: "alex-id", name: "Alex", avatar_url: "https://example.test/alex.png", role: "member" }]);
    const { service } = setup();
    await vi.waitFor(async () => expect((await service.load()).members).toEqual([{ userId: "alex-id", name: "Alex", iconUrl: "https://example.test/alex.png" }]));
    routes.unshift((_method, path) => path === "/sessions/new/commands" ? { turn_id: "turn" } : undefined);
    const content = "Assign this to [@Alex](mention://workspace-member/alex-id?workspaceId=ws&userId=alex-id)";
    const references = [{ kind: "member" as const, id: "alex-id", workspaceId: "ws" }];
    await service.session.send({ sessionId: "new", content, references });
    expect(requests.find(entry => entry.body?.type === "send")?.body).toMatchObject({ text: content, prompt: content, references });
    expect(requests.find(entry => entry.body?.type === "send")?.body).not.toHaveProperty("agents");
  });

  it("rejects foreign or removed members before sending or admitting a queued prompt", async () => {
    const { service } = setup();
    for (const workspaceId of ["other-ws", "ws"]) {
      const references = [{ kind: "member" as const, id: "removed-user", workspaceId }];
      await expect(service.session.send({ sessionId: "new", content: "Assign", references })).rejects.toThrow(workspaceId === "ws" ? "no longer" : "outside");
      const params = { sessionId: "new", queued: true as const, item: { requestId: "r", content: "Assign", references, display: { id: "r", createdAtUnixMs: 1, content: [{ type: "text" as const, text: "Assign" }] } } };
      await expect(service.queue.prepare!(params)).rejects.toThrow();
      await expect(service.queue.enqueue(params)).rejects.toThrow();
    }
    expect(requests).toEqual([]);
  });
  it("preserves Side and turn send metadata without removing Side sessions", async () => {
    routes.unshift((_method, path) => path.startsWith("/sessions?") ? {
      scope_seq: 4,
      summaries: [
        summary("side", { side_source_id: "main", last_turn_sent_at: at }),
        summary("empty", { last_turn_sent_at: null }),
        summary("older-daemon")
      ],
      has_more: false
    } : undefined);
    const { service } = setup();
    const state = await service.load();
    expect(state.sessions.map(({ id, localConversation }) => ({ id, localConversation }))).toEqual([
      { id: "side", localConversation: { sideSourceId: "main", lastTurnSentAt: at } },
      { id: "empty", localConversation: { sideSourceId: undefined, lastTurnSentAt: null } },
      { id: "older-daemon", localConversation: { sideSourceId: undefined, lastTurnSentAt: undefined } }
    ]);
  });

  it("reads attachment metadata without downloading content and rejects malformed responses", async () => {
    const { service } = setup();
    let response: unknown = { name: "clip.mp4", mime_type: "video/mp4", size: 123 };
    routes.unshift((_method, path) => path === "/attachments/video/metadata" ? response : undefined);
    expect(await service.attachment.metadata({ attachmentId: "video" })).toMatchObject({ name: "clip.mp4", mimeType: "video/mp4", sizeBytes: 123 });
    expect(requests.some((request) => request.path === "/attachments/video")).toBe(false);
    response = { name: "bad", mime_type: 42, size: -1 };
    await expect(service.attachment.metadata({ attachmentId: "video" })).rejects.toThrow("Invalid attachment metadata");
  });
  it("publishes connection changes of its own scope and refreshes the workspace on return", async () => {
    const { service } = setup();
    const events: unknown[] = [];
    service.subscribe((event) => events.push(event));
    for (const listener of connections)
      listener({ ...scope, workspaceId: "other" }, true);
    expect(events).toEqual([]);
    for (const listener of connections) listener(scope, false);
    expect(events).toEqual([{ type: "connection", connected: false }]);
    for (const listener of connections) listener(scope, true);
    expect(events).toContainEqual({ type: "connection", connected: true });
    expect(events).toContainEqual({ type: "invalidate", scope: "workspace" });
  });
  it("loads providers, folder projects and every conversation with its running turn", async () => {
    const { service } = setup();
    const state = await service.load();
    expect(
      state.executionTargets?.map((target) => [target.id, target.canInvoke])
    ).toEqual([
      ["local:codex", true],
      ["local:claude", false]
    ]);
    expect(state.sessions.map((session) => session.id)).toEqual([
      "idle",
      "busy"
    ]);
    expect(state.folderProjects).toEqual([
      { id: "folder-1", title: "Repo", path: "/repo", createdAt: at }
    ]);
    expect(state.pendingTasks).toEqual([
      { task_id: "turn-9", status: "running", chat_session_id: "busy" }
    ]);
    expect(sent).toContainEqual({
      version: 2,
      type: "subscribe_scope",
      after_scope_seq: 3
    });
  });

  describe("independent sources", () => {
    const never = new Promise(() => {});
    const hang = (...paths: string[]) =>
      routes.unshift((_method, path) =>
        paths.some((entry) => path.startsWith(entry)) ? never : undefined
      );

    it("starts without waiting for folder projects, then asks the GUI to reload when they arrive", async () => {
      let release!: (value: unknown) => void;
      const folders = new Promise((resolve) => {
        release = resolve;
      });
      routes.unshift((method, path) =>
        path === "/folder-projects" && method === "GET" ? folders : undefined
      );
      const { service } = setup();
      const events: unknown[] = [];
      service.subscribe((event) => {
        if (event.type === "invalidate") events.push(event);
      });

      const state = await service.load();
      expect(state.sessions).toHaveLength(2);
      expect(state.folderProjects).toEqual([]);
      await new Promise((resolve) => setTimeout(resolve, 60));
      events.length = 0;

      release({
        projects: [
          { id: "folder-1", title: "Repo", path: "/repo", created_at: at }
        ]
      });
      await vi.waitFor(() =>
        expect(events).toContainEqual({
          type: "invalidate",
          scope: "workspace"
        })
      );
      expect(
        (await service.load()).folderProjects?.map((project) => project.id)
      ).toEqual(["folder-1"]);
    });

    it("answers a later load from what it knows while every source hangs", async () => {
      const { service } = setup();
      await service.load();
      hang("/providers", "/folder-projects", "/sessions?");
      mock.api.listAgents.mockReturnValue(never);
      service.reconnect();
      for (const listener of connections) listener(scope, true);

      const state = await service.load();
      expect(state.executionTargets?.map((target) => target.id)).toEqual([
        "local:codex",
        "local:claude"
      ]);
      expect(state.sessions.map((session) => session.id)).toEqual([
        "idle",
        "busy"
      ]);
    });

    it("keeps what it knows when a later refresh fails", async () => {
      const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
      const { service } = setup();
      await service.load();
      routes.unshift((_method, path) =>
        path === "/providers" || path === "/folder-projects"
          ? new Error("daemon_offline: down")
          : undefined
      );

      const state = await service.load();
      expect(state.executionTargets).toHaveLength(2);
      await vi.waitFor(() => expect(warn).toHaveBeenCalled());
      warn.mockRestore();
    });

    it("cannot start without this device's providers or conversations", async () => {
      const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
      routes.unshift((_method, path) =>
        path === "/providers" ? new Error("daemon_offline: down") : undefined
      );
      await expect(setup().service.load()).rejects.toThrow("down");
      routes.shift();
      routes.unshift((_method, path) =>
        path.startsWith("/sessions?")
          ? new Error("daemon_offline: down")
          : undefined
      );
      await expect(setup().service.load()).rejects.toThrow("down");
      warn.mockRestore();
    });

    it("gives a rebuilt page the known sources and agents at once", async () => {
      const shared = transport();
      // A rebuilt page shares the app's query client and the store.
      const queryClient = new QueryClient();
      const first = setup({ queryClient }, undefined, shared).service;
      await vi.waitFor(async () =>
        expect((await first.load()).agents).toHaveLength(1)
      );
      await vi.waitFor(async () =>
        expect((await first.load()).folderProjects).toHaveLength(1)
      );
      first.dispose();

      hang("/providers", "/folder-projects", "/sessions?");
      mock.api.listAgents.mockReturnValue(never);
      const state = await setup(
        { queryClient },
        undefined,
        shared
      ).service.load();
      expect(state.agents.map((entry) => entry.agent.id)).toEqual(["design"]);
      expect(state.executionTargets).toHaveLength(2);
      expect(state.folderProjects).toHaveLength(1);
    });
  });

  describe("cloud agent directory", () => {
    type Service = ReturnType<typeof setup>["service"];
    const watchInvalidations = (service: Service) => {
      const events: unknown[] = [];
      service.subscribe((event) => {
        if (event.type === "invalidate") events.push(event);
      });
      return events;
    };
    const loadedAgentIds = async (service: Service) =>
      (await service.load()).agents.map((entry) => entry.agent.id);
    const agentsLoaded = (service: Service) =>
      vi.waitFor(async () =>
        expect(await loadedAgentIds(service)).toEqual(["design"])
      );
    // Lets the summary list's coalesced invalidation and reconnect's notification land first.
    const earlierInvalidations = () =>
      new Promise((resolve) => setTimeout(resolve, 60));

    it("returns local data while the cloud hangs, then asks the GUI to reload when agents arrive", async () => {
      const agents = await mock.api.listAgents();
      let release!: (value: unknown) => void;
      mock.api.listAgents.mockReturnValue(
        new Promise((resolve) => {
          release = resolve;
        })
      );
      const { service } = setup();
      const events = watchInvalidations(service);

      const state = await service.load();
      expect(state.sessions.map((session) => session.id)).toEqual([
        "idle",
        "busy"
      ]);
      expect(state.agents).toEqual([]);
      await earlierInvalidations();
      events.length = 0;

      release(agents);
      await vi.waitFor(() =>
        expect(events).toEqual([{ type: "invalidate", scope: "workspace" }])
      );
      expect(await loadedAgentIds(service)).toEqual(["design"]);
    });

    it("returns local data when the cloud fails, and a later load recovers the agents", async () => {
      const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
      mock.api.listAgents.mockRejectedValueOnce(new Error("network down"));
      const { service } = setup();
      const events = watchInvalidations(service);

      expect((await service.load()).sessions).toHaveLength(2);
      await vi.waitFor(() => expect(warn).toHaveBeenCalled());
      await earlierInvalidations();
      events.length = 0;

      await service.load();
      await vi.waitFor(() => expect(events).toHaveLength(1));
      expect(await loadedAgentIds(service)).toEqual(["design"]);
      warn.mockRestore();
    });

    it("reloads the GUI only when a refresh changes the agents", async () => {
      const [agent] = await mock.api.listAgents();
      const { service } = setup();
      const events = watchInvalidations(service);
      await agentsLoaded(service);

      service.reconnect();
      await earlierInvalidations();
      events.length = 0;
      const callsBefore = mock.api.listAgents.mock.calls.length;
      await service.load();
      await vi.waitFor(() =>
        expect(mock.api.listAgents.mock.calls.length).toBeGreaterThan(
          callsBefore
        )
      );
      await earlierInvalidations();
      expect(events).toEqual([]);

      mock.api.listAgents.mockResolvedValue([
        { ...agent, name: "Renamed reviewer" }
      ]);
      service.reconnect();
      await earlierInvalidations();
      events.length = 0;
      await service.load();
      await vi.waitFor(() => expect(events).toHaveLength(1));
    });

    it("keeps the last known agents while a refresh hangs", async () => {
      const { service } = setup();
      await agentsLoaded(service);

      mock.api.listAgents.mockReturnValue(new Promise(() => {}));
      service.reconnect();
      expect(await loadedAgentIds(service)).toEqual(["design"]);
    });
  });

  it("creates a conversation in a folder project and sends with mentions and quoted references", async () => {
    routes.unshift((method, path, body) => {
      if (method === "POST" && path === "/sessions")
        return summary(String(body!.id), { cwd: "/repo" });
      if (path === "/sessions/c1/snapshot")
        return snapshot("c1", {
          turns: [
            {
              id: "t",
              turn_index: 0,
              request_id: "r",
              status: "completed",
              started_at: at,
              completed_at: at,
              messages: [
                {
                  id: "m",
                  role: "user",
                  content: "earlier question",
                  created_at: at
                }
              ],
              events: []
            }
          ]
        });
      if (method === "POST" && path === "/sessions/new/commands")
        return body!.type === "send"
          ? { turn_ids: ["turn-1", "turn-2"] }
          : summary("new");
      return undefined;
    });
    const { service } = setup();
    await service.session.create({ agentId: "local:codex", cwd: "/repo/" });
    expect(
      requests.find(
        (entry) => entry.path === "/sessions" && entry.method === "POST"
      )?.body
    ).toMatchObject({ provider: "codex", folder_project_id: "folder-1" });
    const result = await service.session.send({
      sessionId: "new",
      content: "Review this",
      requestId: "req-1",
      model: "gpt-5",
      references: [
        { kind: "agent", id: "design" },
        { kind: "session", id: "c1" }
      ]
    } as never);
    expect(result).toMatchObject({
      message_id: "msg-req-1",
      task_id: "turn-1"
    });
    const send = requests.find((entry) => entry.body?.type === "send")!.body!;
    expect(send).toMatchObject({
      request_id: "req-1",
      text: "Review this",
      agents: [{ id: "design", name: "Design reviewer" }]
    });
    expect(send.prompt).toContain("Referenced context (quoted data)");
    expect(send.prompt).toContain(
      'read_context(kind="conversation", id="c1", source="local")'
    );
    expect(send.prompt).not.toContain("earlier question");
    expect(
      requests.some((entry) => entry.path === "/sessions/c1/snapshot")
    ).toBe(false);
    // Model choices belong to the local provider; agent sends leave them alone.
    expect(
      requests.some((entry) => entry.body?.type === "update_settings")
    ).toBe(false);
  });

  it("gives message images a file URL instead of their bytes", async () => {
    const { service } = setup();
    await expect(
      service.attachment.url({ attachmentId: "a/b" })
    ).resolves.toEqual({ url: "v2:/attachments/a%2Fb" });
    await expect(
      service.image.url({ sessionId: "c1", path: "/tmp/a b.png" })
    ).resolves.toEqual({
      url: "v2:/sessions/c1/image?path=%2Ftmp%2Fa%20b.png"
    });
  });

  it("applies composer settings before a local send", async () => {
    routes.unshift((method, path, body) =>
      method === "POST" && path === "/sessions/c1/commands"
        ? body!.type === "send"
          ? { turn_id: "turn-1" }
          : { status: "ok" }
        : undefined
    );
    const { service } = setup();
    await service.session.send({
      sessionId: "c1",
      content: "hi",
      model: "gpt-5",
      reasoningEffort: "low"
    } as never);
    expect(
      requests
        .filter((entry) => entry.method === "POST")
        .map((entry) => entry.body?.type)
    ).toEqual(["update_settings", "send"]);
    expect(
      requests.find((entry) => entry.body?.type === "update_settings")!.body
    ).toEqual({
      type: "update_settings",
      model: "gpt-5",
      reasoning_effort: "low"
    });
  });

  it("rejects task creation mixed with agent mentions before sending", async () => {
    const { service } = setup();
    await expect(
      service.session.send({
        sessionId: "c1",
        content: "x",
        references: [
          { kind: "agent", id: "design" },
          { kind: "issue", id: CREATE_TASK_MENTION_ID }
        ]
      } as never)
    ).rejects.toThrow("New task requires the current local provider");
    expect(requests.some((entry) => entry.body?.type === "send")).toBe(false);
  });

  it("projects the snapshot's settings, queue and fork origin into the session detail", async () => {
    // The copied turn keeps its start time; a turn sent after the fork must not move the branch point.
    const turn = (id: string, startedAt: string) => ({
      id,
      turn_index: 0,
      request_id: id,
      status: "completed",
      started_at: startedAt,
      completed_at: startedAt,
      messages: [],
      events: []
    });
    const turns = [
      turn("copy-3", "2026-10-28T23:00:00Z"),
      turn("after", "2026-10-29T01:00:00Z")
    ];
    routes.unshift((_method, path) =>
      path.startsWith("/sessions/c1/snapshot")
        ? snapshot("c1", {
            forked_from_id: "src",
            forked_from_turn_id: "t-3",
            turns,
            queue: { revision: 5, paused: true, error: "stopped", items: [] }
          })
        : undefined
    );
    const { service } = setup();
    await service.localConversations!.ensure("c1");
    const detail = service.localConversations!.detail("c1");
    expect(detail).toMatchObject({
      model: "gpt",
      reasoningEffort: "high",
      permissionMode: "default",
      promptQueue: { revision: 5, paused: true, error: "stopped", items: [] },
      forkedFrom: { sessionId: "src", turnId: "t-3", targetTurnId: "copy-3" }
    });
  });

  it("maps queue, fork, answer and cancel to conversation commands", async () => {
    routes.unshift((method, path, body) => {
      if (method !== "POST" || path !== "/sessions/c1/commands")
        return undefined;
      if (body!.type === "fork") return summary("forked");
      if (String(body!.type).startsWith("queue"))
        return {
          revision: 3,
          paused: false,
          dispatching_request_id: "q1",
          items: []
        };
      return { status: "requested" };
    });
    const { service } = setup();
    expect(
      await service.queue.update({
        sessionId: "c1",
        action: "sendNow",
        requestId: "q1"
      } as never)
    ).toMatchObject({ revision: 3, dispatchingRequestId: "q1" });
    expect(
      await service.session.fork({
        sessionId: "c1",
        turnId: "t-1",
        requestId: "f-1"
      } as never)
    ).toMatchObject({ id: "forked" });
    await service.interaction.respond({
      sessionId: "c1",
      requestId: "question:t:q",
      answers: { a: "b" }
    } as never);
    await service.session.cancel({ sessionId: "c1", taskId: "t-1" } as never);
    expect(requests.map((entry) => entry.body)).toEqual([
      { type: "queue_update", action: "send_now", request_id: "q1",operation_id:expect.any(String) },
      { type: "fork", turn_id: "t-1", request_id: "f-1" },
      { type: "answer", interaction_id: "question:t:q", answer: { a: "b" } },
      { type: "interrupt" }
    ]);
  });

  it("maps the daemon's usage into the composer's shape", async () => {
    routes.unshift((_method, path) =>
      path.startsWith("/usage?")
        ? {
            provider: "claude",
            context: {
              used_tokens: 120,
              limit_tokens: 1000,
              model: null,
              updated_at: null
            },
            account: {
              status: "available",
              plan: "max",
              windows: [
                { id: "5h", label: "5 hours", used_percent: 40, resets_at: at }
              ],
              updated_at: at,
              stale: true
            }
          }
        : undefined
    );
    const { service } = setup();
    expect(
      await service.usage.read({
        agentId: "local:claude",
        sessionId: "c1"
      } as never)
    ).toEqual({
      provider: "claude",
      context: {
        usedTokens: 120,
        limitTokens: 1000,
        model: null,
        updatedAt: null
      },
      account: {
        status: "available",
        plan: "max",
        updatedAt: at,
        stale: true,
        windows: [{ id: "5h", label: "5 hours", usedPercent: 40, resetsAt: at }]
      }
    });
    expect(requests[0]!.path).toBe("/usage?provider=claude&conversation_id=c1");
  });

  it("routes compaction to the selected local conversation and validates its acknowledgement", async () => {
    let reply: unknown = { turn_id: "turn-compact-1" };
    routes.unshift((method, path) =>
      method === "POST" && path === "/sessions/c1/commands" ? reply : undefined
    );
    const { service } = setup();
    expect(
      await service.session.compact({ sessionId: "c1", requestId: "compact-1" })
    ).toEqual({ turnId: "turn-compact-1" });
    expect(requests[0]).toMatchObject({
      method: "POST",
      path: "/sessions/c1/commands",
      body: { type: "compact", request_id: "compact-1" }
    });
    reply = {};
    await expect(
      service.session.compact({ sessionId: "c1", requestId: "compact-2" })
    ).rejects.toThrow();
  });

  it("keeps only the latest GUI selection when reads finish in reverse order", async () => {
    let intent = "a";
    let release: () => void = () => {};
    routes.unshift((_method, path) =>
      path === "/sessions/slow/snapshot"
        ? new Promise((resolve) => {
            release = () => resolve(snapshot("slow"));
          })
        : path === "/sessions/fast/snapshot"
          ? snapshot("fast")
          : undefined
    );
    const { service, onLocalSelect } = setup({
      getSelectionIntent: () => intent
    });
    const slow = service.hostApi.selectSession({ sessionId: "slow" } as never);
    intent = "b";
    await service.hostApi.selectSession({ sessionId: "fast" } as never);
    release();
    await slow;
    expect(onLocalSelect.mock.calls).toEqual([["fast", "codex"]]);
  });

  it("refuses to delete local history from the GUI", async () => {
    const { service } = setup();
    await expect(
      service.session.delete({ sessionId: "c1" } as never)
    ).rejects.toThrow("Archive local conversations");
  });
});

describe("file menu", () => {
  it("names the device whose conversation the file belongs to", async () => {
    const show = vi.fn(async () => undefined);
    vi.stubGlobal("window", { desktopAPI: { localFileContextMenu: { show } } });
    const params = {
      sessionId: "c1",
      url: "file:///repo/a.ts",
      hasSelection: false
    };
    await setup({}, "peer-1").service.hostApi.showFileContextMenu(
      params as never
    );
    await setup().service.hostApi.showFileContextMenu(params as never);
    expect(show.mock.calls.map((call) => (call as unknown[])[2])).toEqual([
      "peer-1",
      "local"
    ]);
    vi.unstubAllGlobals();
  });
});

it("shares the global permission between main, Side and remote-device conversations", async () => {
  const records = new Map<string, string>();
  const storage: StorageAdapter = {
    getItem: (key) => records.get(key) ?? null,
    setItem: (key, value) => { records.set(key, value); },
    removeItem: (key) => { records.delete(key); }
  };
  const main = setup({ storage }).service;
  const side = setup({ storage, storagePrefix: "side:c1:" }).service;
  const remote = setup({ storage, workspaceId: "another-ws" }, "peer-1").service;
  const key = "global-execution-permission-v1";
  const changed = vi.fn();
  side.storage.subscribe!({ key }, changed);
  await main.storage.write({ key, value: '"auto"' });
  expect(await side.storage.read({ key })).toBe('"auto"');
  expect(await remote.storage.read({ key })).toBe('"auto"');
  expect(changed).toHaveBeenCalledExactlyOnceWith('"auto"');
});

it("shares agent selection counts across main and Side while isolating API, user and workspace", async () => {
  const records = new Map<string, string>();
  const storage = {
    getItem: (key: string) => records.get(key) ?? null,
    setItem: (key: string, value: string) => {
      records.set(key, value);
    },
    removeItem: (key: string) => {
      records.delete(key);
    },
    keys: () => [...records.keys()]
  };
  const main = setup({ storage }).service;
  const side = setup({ storage, storagePrefix: "side:c1:" }).service;
  await Promise.all([
    main.storage.write({ key: "agent-mention-selection:v1", value: "design" }),
    side.storage.write({ key: "agent-mention-selection:v1", value: "design" })
  ]);
  const usage = await main.storage.read({ key: "agent-mention-usage:v1" });
  expect(JSON.parse(usage!)).toMatchObject({
    version: 1,
    agents: { design: { count: 2 } }
  });
  expect(await side.storage.read({ key: "agent-mention-usage:v1" })).toBe(
    usage
  );
  for (const options of [
    { userId: "other-user" },
    { workspaceId: "other-ws" }
  ]) {
    expect(
      await setup({ storage, ...options }).service.storage.read({
        key: "agent-mention-usage:v1"
      })
    ).toBeNull();
  }
  mock.api.getBaseUrl.mockReturnValue("https://other.test");
  expect(
    await setup({ storage }).service.storage.read({
      key: "agent-mention-usage:v1"
    })
  ).toBeNull();
  mock.api.getBaseUrl.mockReturnValue("https://example.test");
  expect(
    await setup({ storage }).service.storage.read({
      key: "agent-mention-usage:v1"
    })
  ).toBe(usage);
  await main.storage.write({ key: "draft", value: "main" });
  await side.storage.write({ key: "draft", value: "side" });
  expect(await main.storage.read({ key: "draft" })).toBe("main");
  expect(await side.storage.read({ key: "draft" })).toBe("side");
});
