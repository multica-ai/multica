// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { fakeHostServices } from "../e2e-harness/fake-host-services";
import { AgentGuiApplication } from "./application";
import type {
  AgentGuiHostEvent,
  AgentGuiHostServices,
  AgentGuiInitSnapshot,
  AgentGuiOperationInvoker
} from "./protocol";
import { createMulticaAgentGuiRuntime } from "./runtime";
import { localSourceFromClient } from "./runtime/local-snapshot-test-host";

const renders = vi.hoisted(() => ({
  frame: 0,
  nodeView: 0,
  detailPane: 0,
  composer: 0,
  composerView: 0
}));

// Each pass-through counts a render that its parent caused, so counting the child
// of an observer also catches that observer re-rendering itself.
vi.mock(
  "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/AgentGUIFrame",
  async (original) => {
    const actual = await original<Record<string, any>>();
    const { createElement } = await import("react");
    return {
      ...actual,
      AgentGUIFrame: (props: object) => {
        renders.frame++;
        return createElement(actual.AgentGUIFrame, props);
      }
    };
  }
);
vi.mock(
  "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/AgentGUINodeView",
  async (original) => {
    const actual = await original<Record<string, any>>();
    const { createElement } = await import("react");
    return {
      ...actual,
      AgentGUINodeView: (props: object) => {
        renders.nodeView++;
        return createElement(actual.AgentGUINodeView, props);
      }
    };
  }
);
vi.mock(
  "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/view/AgentGUIDetailPane",
  async (original) => {
    const actual = await original<Record<string, any>>();
    const { createElement } = await import("react");
    return {
      ...actual,
      AgentGUIDetailPane: (props: object) => {
        renders.detailPane++;
        return createElement(actual.AgentGUIDetailPane, props);
      }
    };
  }
);
vi.mock(
  "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/AgentComposer",
  async (original) => {
    const actual = await original<Record<string, any>>();
    const { createElement } = await import("react");
    return {
      ...actual,
      AgentComposer: (props: object) => {
        renders.composer++;
        return createElement(actual.AgentComposer, props);
      }
    };
  }
);
vi.mock(
  "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/composer/AgentComposerView",
  async (original) => {
    const actual = await original<Record<string, any>>();
    const { createElement } = await import("react");
    return {
      ...actual,
      AgentComposerView: (props: object) => {
        renders.composerView++;
        return createElement(actual.AgentComposerView, props);
      }
    };
  }
);

const COMMIT_INTERVAL_MS = 45;

it("streams into the real application without rerendering the page or composer, then ends the turn", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  );
  Element.prototype.scrollIntoView = () => {};
  Element.prototype.scrollTo = () => {};
  const at = "2026-11-04T10:00:00Z";
  const session = {
    id: "session",
    workspace_id: "workspace",
    agent_id: "local:codex",
    creator_id: "user",
    title: "Session",
    status: "active" as const,
    created_at: at,
    updated_at: at,
    has_unread: false
  };
  const initial: AgentGuiInitSnapshot = {
    workspaceId: "workspace",
    workspaceSlug: "workspace",
    userId: "user",
    locale: "en",
    theme: "dark",
    local: true,
    queueOwner: "desktop",
    activeSessionId: "session",
    agents: [],
    sessions: [session],
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
  };
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  const message = (id: string, turn: string, role: string, content: string) => ({
    id,
    chat_session_id: "session",
    task_id: turn,
    role,
    content,
    created_at: at
  });
  const client: AgentGuiHostServices = fakeHostServices(
    vi.fn(async (method: string) => {
      if (method === "workspace.load") return initial;
      if (method === "storage.read") return null;
      if (method === "storage.write") return;
      if (method === "session.load")
        return {
          session,
          pending: { task_id: "running" },
          tasks: [
            { id: "done", status: "completed", created_at: at },
            { id: "running", status: "running", created_at: at }
          ],
          messages: [
            message("q1", "done", "user", "Prompt one"),
            message("a1", "done", "assistant", "First reply"),
            message("q2", "running", "user", "Prompt two")
          ]
        };
      if (method === "models.list")
        return {
          id: "catalog",
          runtime_id: "local:codex",
          status: "completed",
          supported: true,
          models: []
        };
      if (method === "host.selectSession") return;
      if (method === "context.tasks") return { issues: [], hasMore: false };
      if (method === "usage.read") return null;
      throw new Error(`Unexpected request: ${method}`);
    }) as AgentGuiOperationInvoker,
    (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    }
  );
  const handle = createMulticaAgentGuiRuntime(
    client,
    initial,
    localSourceFromClient(client as never)
  );
  await handle.initialize();
  const container = document.body.appendChild(document.createElement("div"));
  const root = createRoot(container);
  let seq = 1;
  const deliver = async (
    type: string,
    payload: Record<string, unknown>,
    itemId: string | null = "reply"
  ) => {
    seq++;
    const event = {
      seq,
      turn_id: "running",
      turn_seq: seq,
      item_id: itemId,
      type,
      final: type === "item.completed" || type === "turn.completed",
      created_at: at,
      payload
    };
    await act(async () => {
      for (const listener of listeners)
        listener({
          type: "local.events",
          sessionId: "session",
          message: {
            version: 2,
            type: "conversation.events",
            conversation_id: "session",
            from_seq: seq,
            to_seq: seq,
            events: [event]
          }
        } as unknown as AgentGuiHostEvent);
      await new Promise((resolve) => setTimeout(resolve, COMMIT_INTERVAL_MS));
    });
  };
  const delta = (text: string) =>
    deliver("item.delta", { kind: "assistant_text", field: "text", text });
  const stopButton = () =>
    container.querySelector('[data-testid="agent-gui-composer-stop-active-turn"]');
  try {
    await act(async () =>
      root.render(
        <AgentGuiApplication
          client={client}
          initial={initial}
          handle={handle}
          root={container}
        />
      )
    );
    await vi.waitFor(() => {
      expect(container.textContent).toContain("Prompt two");
      expect(stopButton()).not.toBeNull();
    });

    // The first event settles the turn's presentation; later text must not reach the page.
    await delta("first ");
    await vi.waitFor(() => expect(container.textContent).toContain("first"));
    const settled = { ...renders };
    for (let index = 0; index < 20; index++) await delta(`word${index} `);
    await vi.waitFor(() => expect(container.textContent).toContain("word19"));
    expect(renders).toEqual(settled);

    await deliver("item.completed", {
      kind: "assistant_text",
      status: "completed",
      text: "Complete reply"
    });
    await deliver("turn.completed", { status: "completed" }, null);
    await vi.waitFor(() => {
      expect(container.textContent).toContain("Complete reply");
      expect(stopButton()).toBeNull();
    });
  } finally {
    await act(async () => root.unmount());
    handle.dispose();
    container.remove();
    vi.unstubAllGlobals();
  }
}, 30_000);
