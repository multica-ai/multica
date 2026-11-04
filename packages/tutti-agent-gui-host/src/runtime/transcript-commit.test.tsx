// @vitest-environment jsdom
import { COMMIT_INTERVAL_MS } from "@multica/conversation-client/services";
import { reaction } from "mobx";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Agent, ChatSession } from "@multica/core/types";
import type { AgentActivityMessage } from "@tutti-os/agent-activity-core";
import type { AgentGuiHostEvent, AgentGuiInitSnapshot, AgentGuiRpcClient } from "../protocol";
import { projectAgentGUIMessagesToTimelineItems } from "../../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/controller/agentGuiController.promptHelpers";
import { projectAgentConversationVM } from "../../../../vendor/tutti/packages/agent/gui/shared/agentConversation/projection/agentConversationProjection";
import { useEngineSelector, useModelValue } from "../../../../vendor/tutti/packages/agent/gui/shared/engine/useEngineSelector";
import { buildCanonicalWorkspaceAgentDetailView } from "../../../../vendor/tutti/packages/agent/gui/shared/workspaceAgentTimelineCanonical";
import { createMulticaAgentGuiRuntime } from "./create-multica-agent-gui-runtime";
import { localSourceFromClient } from "./local-snapshot-test-host";

const time = "2026-11-04T01:00:00Z";
const agent = { id: "agent-1", name: "Codex", model: "m", archived_at: null } as Agent;
const session = { id: "s", workspace_id: "w", agent_id: agent.id, creator_id: "u", title: "c", status: "active", created_at: time, updated_at: time, has_unread: false } as ChatSession;
const init: AgentGuiInitSnapshot = {
  workspaceId: "w", workspaceSlug: "dev", userId: "u", locale: "en", theme: "dark", activeSessionId: null, local: true, queueOwner: "desktop",
  agents: [{ agent, provider: "codex", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true }],
  sessions: [session],
};

const event = (seq: number, type: string, payload: Record<string, unknown>, extra: Record<string, unknown> = {}) =>
  ({ seq, turn_id: "t1", turn_seq: seq, item_id: null, type, final: type === "item.completed" || type === "turn.completed", created_at: time, payload, ...extra });
const batch = (events: ReturnType<typeof event>[]) => ({ version: 2, type: "conversation.events", conversation_id: session.id, from_seq: events[0]!.seq, to_seq: events.at(-1)!.seq, events });
const started = [
  event(1, "user.message", { message_id: "m1", content: "hi" }),
  event(2, "turn.started", { request_id: "r1" }),
  event(3, "item.delta", { kind: "assistant_text", field: "text", text: "Hel" }, { item_id: "it-1" }),
];
const finished = [
  event(4, "item.completed", { kind: "assistant_text", status: "completed", text: "Hello!" }, { item_id: "it-1" }),
  event(5, "turn.completed", { status: "completed" }),
];

const handles: { dispose(): void }[] = [];
let root: Root | null = null;
afterEach(() => {
  root?.unmount();
  root = null;
  for (const handle of handles.splice(0)) handle.dispose();
  vi.restoreAllMocks();
});

async function open() {
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  const client: AgentGuiRpcClient = {
    request: (async (method: string) => {
      switch (method) {
        case "storage.read": return null;
        case "workspace.load": return { agents: init.agents, sessions: [session], local: true, queueOwner: "desktop" };
        case "session.load": return { session, messages: [], pending: {}, tasks: [] };
        case "models.list": return { supported: true, models: [] };
        case "queue.enqueue": return { revision: 1, paused: false, items: [] };
        default: return undefined;
      }
    }) as never,
    subscribe: (listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
  };
  const source = localSourceFromClient(client as never);
  const stopObservers: ((events: readonly never[]) => void)[] = [];
  const observe = source.observe.bind(source);
  source.observe = (id, listener) => { stopObservers.push(listener as never); return observe(id, listener); };
  const handle = createMulticaAgentGuiRuntime(client, init, source);
  handles.push(handle);
  await handle.initialize();
  handle.runtime.ensureSessionSynchronized!({ workspaceId: "w", agentSessionId: session.id });
  const engine = handle.runtime.getSessionEngine!("w");
  const emit = (events: ReturnType<typeof event>[]) => {
    for (const listener of listeners) listener({ type: "local.events", sessionId: session.id, message: batch(events) } as never);
  };
  const messages = () => handle.runtime.transcript.messages(session.id) as readonly AgentActivityMessage[];
  const content = (id: string) => messages().find((message) => message.messageId === id)?.payload.content;
  const phase = (state = engine.getSnapshot()) => Object.values(state.sessionLifecycle.turnsById).find((turn) => turn.turnId === "t1")?.phase;
  await vi.waitFor(() => expect(engine.getSnapshot().sessionLifecycle.sessionsById[session.id]).toBeTruthy());
  await new Promise((resolve) => setTimeout(resolve, 10));
  return { client, content, emit, engine, handle, messages, notifyChanged: () => { for (const listener of stopObservers) listener([]); }, phase };
}

const interval = () => new Promise((resolve) => setTimeout(resolve, COMMIT_INTERVAL_MS + 20));

describe("committing a conversation's transcript", () => {
  it("leaves the engine with turns and the user's messages, and the transcript with everything", async () => {
    const { emit, engine, messages, phase } = await open();
    emit(started);
    await interval();
    expect(messages().map((message) => message.messageId)).toEqual(["m1", "it-1"]);
    const stored = engine.getSnapshot().sessionMessages.messagesBySessionId[session.id]!;
    expect(stored.map((message) => message.messageId)).toEqual(["m1"]);
    expect(stored[0]).toBe(messages()[0]);
    expect(phase()).toBe("running");
  });

  it("commits a burst of event messages once, after the interval", async () => {
    const { content, emit, handle } = await open();
    const seen: unknown[] = [];
    const stop = reaction(() => handle.runtime.transcript.messages(session.id), (value) => seen.push(value));
    emit(started);
    emit([event(4, "item.delta", { kind: "assistant_text", field: "text", text: "lo" }, { item_id: "it-1" })]);
    await Promise.resolve();
    expect(seen).toHaveLength(0);
    await interval();
    expect(seen).toHaveLength(1);
    expect(content("it-1")).toBe("Hello");
    stop();
  });

  it("queues a send made while the turn runs", async () => {
    const { emit, engine } = await open();
    emit(started);
    await interval();
    expect(engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "next", content: [{ type: "text", text: "next" }] }).queued).toBe(true);
  });

  it("commits at once when the user acts, so a send follows the turn that just ended", async () => {
    const { content, emit, engine, phase } = await open();
    emit(started);
    await interval();
    emit(finished);
    expect(phase()).toBe("running");
    const result = engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "next", content: [{ type: "text", text: "next" }] });
    expect(phase()).toBe("settled");
    expect(content("it-1")).toBe("Hello!");
    expect(result).toEqual({ accepted: true, queued: false });
  });

  it("commits at once before deciding whether a run blocks changing the model", async () => {
    const { emit, handle, phase } = await open();
    emit(started);
    await interval();
    expect(handle.isModelSelectionBusy(session.id)).toBe(true);
    emit(finished);
    expect(phase()).toBe("running");
    expect(handle.isModelSelectionBusy(session.id)).toBe(false);
    expect(phase()).toBe("settled");
  });

  it("commits at once for an intent the user caused", async () => {
    const { content, emit, engine } = await open();
    emit(started);
    expect(content("it-1")).toBeUndefined();
    engine.dispatch({ type: "engine/connectionChanged", status: "connected", workspaceId: "w" });
    expect(content("it-1")).toBe("Hel");
  });

  it("commits a change that is not events within the tick's microtasks", async () => {
    const { engine, notifyChanged } = await open();
    const notified = vi.fn();
    const stop = engine.subscribe(notified);
    notifyChanged();
    expect(notified).not.toHaveBeenCalled();
    await Promise.resolve();
    expect(notified).toHaveBeenCalled();
    stop();
  });

  it("commits nothing after it is disposed", async () => {
    const { emit, handle, messages } = await open();
    emit(started);
    const before = messages();
    handle.dispose();
    await interval();
    expect(messages()).toBe(before);
  });
});

describe("the engine and the transcript within one commit", () => {
  it("agree on the turn whenever either one notifies", async () => {
    const { content, emit, engine, handle, phase } = await open();
    const seen: { from: string; text: unknown; phase: unknown }[] = [];
    const record = (from: string) => seen.push({ from, text: content("it-1"), phase: phase() });
    const stops = [engine.subscribe(() => record("engine")), reaction(() => handle.runtime.transcript.messages(session.id), () => record("transcript"))];
    emit(started);
    await interval();
    emit(finished);
    await interval();
    stops.forEach((stop) => stop());
    expect(seen.some((entry) => entry.from === "transcript" && entry.text === "Hello!")).toBe(true);
    expect(seen.some((entry) => entry.from === "engine" && entry.phase === "settled")).toBe(true);
    for (const entry of seen) {
      if (entry.text === "Hello!") expect(entry.phase).toBe("settled");
      if (entry.phase === "settled") expect(entry.text).toBe("Hello!");
      if (entry.text === "Hel") expect(entry.phase).toBe("running");
    }
  });

  it("render once per commit, never showing one ahead of the other", async () => {
    const { emit, engine, handle, phase } = await open();
    const renders: { text: unknown; phase: unknown }[] = [];
    function View() {
      const messages = useModelValue(() => handle.runtime.transcript.messages(session.id), []);
      const turnPhase = useEngineSelector(engine, (state) => phase(state));
      renders.push({ text: messages.find((message) => message.messageId === "it-1")?.payload.content, phase: turnPhase });
      return null;
    }
    root = createRoot(document.body.appendChild(document.createElement("div")));
    root.render(<View />);
    await vi.waitFor(() => expect(renders.length).toBeGreaterThan(0));
    renders.length = 0;
    emit(started);
    await interval();
    expect(renders).toEqual([{ text: "Hel", phase: "running" }]);
    renders.length = 0;
    emit(finished);
    await interval();
    expect(renders).toEqual([{ text: "Hello!", phase: "settled" }]);
  });
});

describe("what is built from the transcript", () => {
  it("keeps the objects of an unchanged turn through every stage", async () => {
    const { emit, messages } = await open();
    const activity = { sessionId: session.id } as never;
    const gui = { agentSessionId: session.id, workspaceId: "w", provider: "codex", title: "c", status: "active", cwd: "", createdAtUnixMs: 1, updatedAtUnixMs: 2 } as never;
    const view = () => {
      const timeline = projectAgentGUIMessagesToTimelineItems(messages());
      const detail = buildCanonicalWorkspaceAgentDetailView({ activity, session: gui, sessionTurns: [], timelineItems: timeline });
      const rows = projectAgentConversationVM(detail).rows;
      return { messages: messages(), timeline, turn: detail.turns[0], rows };
    };
    emit(started);
    emit(finished);
    await interval();
    const first = view();
    const second = event(6, "user.message", { message_id: "m2", content: "again" }, { turn_id: "t2" });
    emit([second, event(7, "turn.started", { request_id: "r2" }, { turn_id: "t2" }), event(8, "item.delta", { kind: "assistant_text", field: "text", text: "Sure" }, { item_id: "it-2", turn_id: "t2" })]);
    await interval();
    const next = view();
    expect(next.messages).not.toBe(first.messages);
    expect(next.messages.length).toBeGreaterThan(first.messages.length);
    first.messages.forEach((message, index) => expect(next.messages[index]).toBe(message));
    first.timeline.forEach((item, index) => expect(next.timeline[index]).toBe(item));
    expect(next.turn).toBe(first.turn);

    emit([event(9, "item.delta", { kind: "assistant_text", field: "text", text: " thing" }, { item_id: "it-2", turn_id: "t2" })]);
    await interval();
    const streamed = view();
    first.messages.forEach((message, index) => expect(streamed.messages[index]).toBe(message));
    expect(streamed.messages[next.messages.length - 1]).not.toBe(next.messages.at(-1));
    expect(streamed.messages[next.messages.length - 2]).toBe(next.messages.at(-2));
    expect(streamed.turn).toBe(first.turn);
    const earlier = next.rows.filter((row) => row.turnId === "t1");
    expect(earlier.length).toBeGreaterThan(0);
    expect(streamed.rows.filter((row) => row.turnId === "t1")).toEqual(earlier);
  });
});
