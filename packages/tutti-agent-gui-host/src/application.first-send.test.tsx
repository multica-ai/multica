// @vitest-environment jsdom
import type { AgentGUIProps } from "@tutti-os/agent-gui/agent-gui";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { fakeHostServices } from "../e2e-harness/fake-host-services";
import { AgentGuiApplication } from "./application";
import type { AgentGuiHostEvent, AgentGuiInitSnapshot, AgentGuiOperationInvoker } from "./protocol";
import { createMulticaAgentGuiRuntime } from "./runtime";
import { localSourceFromClient } from "./runtime/local-snapshot-test-host";

const captured = vi.hoisted(() => ({ props: null as AgentGUIProps | null }));
vi.mock("@tutti-os/agent-gui/agent-gui", async (original) => {
  const actual = await original<Record<string, any>>();
  const { createElement } = await import("react");
  return { ...actual, AgentGUI: (props: AgentGUIProps) => {
    captured.props = props;
    return createElement(actual.AgentGUI, props);
  } };
});

it.each([false, true])("keeps the first send visible without flashing a queue before dispatch (admission still queued: %s)", async (queued) => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Element.prototype.scrollIntoView = () => {};
  Element.prototype.scrollTo = () => {};
  const at = new Date().toISOString();
  const session = {
    id: "first-session", workspace_id: "workspace", creator_id: "user",
    agent_id: "local:codex", title: "Test session", status: "active" as const,
    created_at: at, updated_at: at, has_unread: false
  };
  const initial: AgentGuiInitSnapshot = {
    workspaceId: "workspace", workspaceSlug: "workspace", userId: "user",
    locale: "en", theme: "dark", local: true, queueOwner: "desktop",
    activeSessionId: null, selectedAgentId: "local:codex", agents: [], sessions: [],
    executionTargets: [{ id: "local:codex", provider: "codex", name: "Codex", iconUrl: "",
      canInvoke: true, runtimeBound: true, runtimeOnline: true }]
  };
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  let created = false;
  let finishCreate!: () => void;
  let acknowledge!: () => void;
  const request = vi.fn(async (method: string) => {
    if (method === "workspace.load") return { ...initial, sessions: created ? [session] : [] };
    if (method === "storage.read" || method === "usage.read") return null;
    if (method === "storage.write" || method === "host.selectSession") return;
    if (method === "context.tasks") return { issues: [], hasMore: false };
    if (method === "models.list") return { supported: true, models: [] };
    if (method === "session.create") return new Promise((resolve) => {
      finishCreate = () => { created = true; resolve(session); };
    });
    if (method === "session.load") return { session, messages: [], tasks: [], pending: {} };
    if (method === "queue.enqueue") return new Promise((resolve) => {
      acknowledge = () => resolve({ revision: 1, paused: false, items: queued ? [{
        requestId: "first-submit", content: "First send regression message",
        display: { id: "first-submit", clientSubmitId: "first-submit", createdAtUnixMs: Date.now(), content: [{ type: "text", text: "First send regression message" }] }
      }] : [] });
    });
    throw new Error(`Unexpected first-send request: ${method}`);
  });
  const client = fakeHostServices(request as AgentGuiOperationInvoker, (listener) => {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  });
  const handle = createMulticaAgentGuiRuntime(client, initial, localSourceFromClient(client));
  await handle.initialize();
  const container = document.body.appendChild(document.createElement("div"));
  const root = createRoot(container);
  const prompt = "First send regression message";
  const expectConversation = () => {
    expect(captured.props?.state.lastActiveAgentSessionId).toBe(session.id);
    expect(container.querySelector(".multica-empty-conversation-welcome")).toBeNull();
    expect(container.textContent?.split(prompt)).toHaveLength(2);
    expect(container.querySelector(".agent-gui-node__composer-queued-prompts")).toBeNull();
    expect(container.querySelector('[data-testid="agent-gui-composer-editor"]')?.getAttribute("aria-label")).toBe("Request follow-up changes from Codex");
    expect(container.querySelector('[data-agent-rich-text-placeholder]')?.getAttribute("data-agent-rich-text-placeholder")).toBe("Request follow-up changes from Codex");
  };
  try {
    await act(async () => root.render(<AgentGuiApplication client={client} initial={initial} handle={handle} root={container} />));
    const initialEditor = container.querySelector('[data-testid="agent-gui-composer-editor"]');
    expect(initialEditor).not.toBeNull();
    expect(initialEditor?.getAttribute("aria-label")).toBe("Type @ to add a reference");
    await act(async () => {
      handle.runtime.activations.request({ mode: "new", agentSessionId: session.id,
        agentTargetId: session.agent_id, requestId: "activation", clientSubmitId: "first-submit",
        initialContent: [{ type: "text", text: prompt }] });
      captured.props!.hostActions.onUpdateNode((node) => ({ ...node, lastActiveAgentSessionId: session.id }));
    });
    await vi.waitFor(() => expect(finishCreate).toBeTypeOf("function"));
    expectConversation();
    expect(container.querySelector('[data-testid="agent-gui-composer-editor"]')).toBe(initialEditor);
    await act(async () => finishCreate());
    await vi.waitFor(() => expect(acknowledge).toBeTypeOf("function"));
    expectConversation();
    expect(container.querySelector('[data-testid="agent-gui-composer-editor"]')).toBe(initialEditor);
    await act(async () => {
      for (const listener of listeners) listener({ type: "init", snapshot: { ...initial, sessions: [session], activeSessionId: session.id } });
      acknowledge();
    });
    expectConversation();
    await act(async () => {
      for (const listener of listeners) listener({
        type: "local.events", sessionId: session.id,
        message: { version: 2, type: "conversation.events", conversation_id: session.id, from_seq: 1, to_seq: 2,
          events: [
            { seq: 1, turn_id: "turn", turn_seq: 1, item_id: null, type: "user.message", final: false, created_at: at,
              payload: { message_id: "message", request_id: "first-submit", content: prompt } },
            { seq: 2, turn_id: "turn", turn_seq: 2, item_id: null, type: "turn.started", final: false, created_at: at,
              payload: { request_id: "first-submit" } }
          ] }
      } as unknown as AgentGuiHostEvent);
      await new Promise((resolve) => setTimeout(resolve, 70));
    });
    expectConversation();
    expect(handle.runtime.prompts.pendingSubmits(session.id)).toEqual([]);
    expect(container.querySelector('[data-testid="agent-gui-composer-editor"]')).toBe(initialEditor);
    const durableRow = container.querySelector('[data-agent-transcript-row-speaker="user"]');
    expect(durableRow?.getAttribute("data-agent-transcript-row-enter")).not.toBe("true");
    expect(request.mock.calls.filter(([method]) => method === "queue.enqueue")).toHaveLength(1);
  } finally {
    await act(async () => root.unmount());
    handle.dispose();
    container.remove();
    vi.unstubAllGlobals();
  }
});
