// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect,it,vi } from "vitest";
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

it("mounts the real conversation application without a selection and switches between loaded conversations", async () => {
  const warnings = vi.spyOn(console, "warn");
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
  const sessions = ["first", "second"].map((id) => ({
    id,
    workspace_id: "workspace",
    agent_id: "local:codex",
    creator_id: "user",
    title: id,
    status: "active" as const,
    created_at: at,
    updated_at: at,
    has_unread: false
  }));
  const initial: AgentGuiInitSnapshot = {
    workspaceId: "workspace",
    workspaceSlug: "workspace",
    userId: "user",
    locale: "en",
    theme: "dark",
    local: true,
    queueOwner: "desktop",
    activeSessionId: null,
    agents: [],
    sessions,
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
  const storage = new Map<string, string>();
  const client: AgentGuiHostServices = fakeHostServices(
    vi.fn(async (method: string, params: any) => {
      if (method === "workspace.load") return initial;
      if (method === "storage.read") return storage.get(params.key) ?? null;
      if (method === "storage.write") {
        storage.set(params.key, params.value);
        return;
      }
      if (method === "session.load")
        return {
          session: sessions.find((session) => session.id === params.sessionId),
          pending: {},
          tasks: [],
          messages: [
            {
              id: `message-${params.sessionId}`,
              chat_session_id: params.sessionId,
              task_id: `turn-${params.sessionId}`,
              role: "user",
              content: `Prompt for ${params.sessionId}`,
              created_at: at
            }
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
      throw new Error(`Unexpected smoke request: ${method}`);
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
    expect(container.querySelector('[contenteditable="true"]')).not.toBeNull();
    expect(container.textContent).not.toMatch(
      /Unable to load|Maximum update depth/
    );
    for (const id of ["first", "second", "first"]) {
      await act(async () => {
        for (const listener of listeners)
          listener({
            type: "init",
            snapshot: { ...initial, activeSessionId: id }
          });
      });
      await vi.waitFor(() =>
        expect(container.textContent).toContain(`Prompt for ${id}`)
      );
    }
  } finally {
    await act(async () => root.unmount());
    handle.dispose();
    container.remove();
    vi.unstubAllGlobals();
    const unbatched = warnings.mock.calls.filter(args => args.some(value => typeof value === "string" && value.includes("Since strict-mode")));
    warnings.mockRestore();
    expect(unbatched).toEqual([]);
  }
});
