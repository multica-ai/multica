import { act, renderHook } from "@testing-library/react";
import type { AgentActivityMessage } from "@tutti-os/agent-activity-core";
import { observable, runInAction } from "mobx";
import type { PropsWithChildren } from "react";
import { describe, expect, it } from "vitest";
import {
  AgentGUIRuntimeProvider,
  useAgentActivitySessionMessages,
  type AgentGUIRuntime
} from "./agentActivityRuntime";

describe("useAgentActivitySessionMessages", () => {
  it("delivers streaming text without rendering an unrelated Session", () => {
    const messagesA = [message("session-a", "partial")];
    const messagesB = [message("session-b", "settled")];
    const store = createRuntimeStore({
      "session-a": messagesA,
      "session-b": messagesB
    });
    const wrapper = ({ children }: PropsWithChildren) => (
      <AgentGUIRuntimeProvider runtime={store.runtime}>
        {children}
      </AgentGUIRuntimeProvider>
    );
    let rendersA = 0;
    let rendersB = 0;
    const renderedA = renderHook(
      () => {
        rendersA += 1;
        return useAgentActivitySessionMessages(["session-a"]);
      },
      { wrapper }
    );
    renderHook(
      () => {
        rendersB += 1;
        return useAgentActivitySessionMessages(["session-b"]);
      },
      { wrapper }
    );
    const initialRendersA = rendersA;
    const initialRendersB = rendersB;

    act(() => {
      store.commit("session-a", [message("session-a", "partial text")]);
    });

    expect(renderedA.result.current["session-a"]?.[0]?.payload.text).toBe(
      "partial text"
    );
    expect(rendersA).toBeGreaterThan(initialRendersA);
    expect(rendersB).toBe(initialRendersB);
  });

  it("subscribes to messages for child Sessions in the family", () => {
    const rootMessages = [message("root", "root text")];
    const store = createRuntimeStore({
      root: rootMessages,
      child: [message("child", "child partial")]
    });
    const wrapper = ({ children }: PropsWithChildren) => (
      <AgentGUIRuntimeProvider runtime={store.runtime}>
        {children}
      </AgentGUIRuntimeProvider>
    );
    const rendered = renderHook(
      () => useAgentActivitySessionMessages(["root", "child"]),
      { wrapper }
    );
    const before = rendered.result.current;

    act(() => {
      store.commit("child", [message("child", "child partial text")]);
    });

    expect(rendered.result.current.child?.[0]?.payload.text).toBe(
      "child partial text"
    );
    expect(rendered.result.current.root).toBe(before.root);
  });

  it("hands over the committed list itself", () => {
    const list = [message("session-a", "text")];
    const store = createRuntimeStore({ "session-a": list });
    const wrapper = ({ children }: PropsWithChildren) => (
      <AgentGUIRuntimeProvider runtime={store.runtime}>
        {children}
      </AgentGUIRuntimeProvider>
    );
    const rendered = renderHook(
      () => useAgentActivitySessionMessages(["session-a"]),
      { wrapper }
    );

    expect(rendered.result.current["session-a"]).toBe(list);
    const before = rendered.result.current;
    rendered.rerender();
    expect(rendered.result.current).toBe(before);
  });

  it("follows a change of Session", () => {
    const store = createRuntimeStore({
      "session-a": [message("session-a", "a")],
      "session-b": [message("session-b", "b")]
    });
    const wrapper = ({ children }: PropsWithChildren) => (
      <AgentGUIRuntimeProvider runtime={store.runtime}>
        {children}
      </AgentGUIRuntimeProvider>
    );
    const rendered = renderHook(
      ({ id }: { id: string }) => useAgentActivitySessionMessages([id]),
      { initialProps: { id: "session-a" }, wrapper }
    );

    rendered.rerender({ id: "session-b" });
    expect(rendered.result.current["session-b"]?.[0]?.payload.text).toBe("b");

    act(() => {
      store.commit("session-b", [message("session-b", "b2")]);
    });
    expect(rendered.result.current["session-b"]?.[0]?.payload.text).toBe("b2");
  });
});

const NO_MESSAGES: readonly AgentActivityMessage[] = [];

function createRuntimeStore(
  initial: Record<string, readonly AgentActivityMessage[]>
) {
  const committed = observable.map(Object.entries(initial), { deep: false });
  const runtime = {
    origin: "test",
    transcript: {
      messages: (id: string) => committed.get(id) ?? NO_MESSAGES
    }
  } as unknown as AgentGUIRuntime;
  return {
    commit(id: string, messages: readonly AgentActivityMessage[]) {
      runInAction(() => committed.set(id, messages));
    },
    runtime
  };
}

function message(agentSessionId: string, text: string): AgentActivityMessage {
  return {
    agentSessionId,
    kind: "text",
    messageId: `message-${agentSessionId}`,
    occurredAtUnixMs: 1,
    payload: { text },
    role: "assistant",
    sequence: 1,
    status: "streaming",
    turnId: "turn-1",
    version: 1,
    workspaceId: "workspace-1"
  };
}
