import { act, renderHook } from "@testing-library/react";
import { StrictMode, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import type { ConversationServices } from "../../../conversationServices";
import { createTestRuntimeState } from "../../../shared/testing/createTestRuntimeState";
import { useAgentConversationMessagePaging } from "./useAgentConversationMessagePaging";

function runtime(loadOlderSessionMessages: () => Promise<void>) {
  return {
    conversations: { loadOlder: loadOlderSessionMessages },
    transcript: {
      messages: () => [{ version: 5 }],
      hasOlderMessages: () => true,
      conversationIds: () => ["session-1"]
    }
  } as unknown as ConversationServices;
}

describe("useAgentConversationMessagePaging", () => {
  it("binds the AgentGUI runtime to the shared message controller", async () => {
    const engine = createTestRuntimeState("workspace-1");
    const loadOlderSessionMessages = vi.fn(async () => {});
    const onOlderPageLoadingChanged = vi.fn();
    const { result, unmount } = renderHook(() =>
      useAgentConversationMessagePaging({
        diagnostics: { error: vi.fn(), page: vi.fn() },
        getActiveSessionId: () => "session-1",
        isMounted: () => true,
        onOlderPageLoadingChanged,
        runtime: runtime(loadOlderSessionMessages),
        conversationServices: engine,
        workspaceId: "workspace-1"
      })
    );

    await act(async () => {
      await result.current.loadOlderMessages();
    });

    expect(loadOlderSessionMessages).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      workspaceId: "workspace-1"
    });
    expect(onOlderPageLoadingChanged.mock.calls).toEqual([
      [false],
      [true],
      [false]
    ]);

    unmount();
    engine.dispose();
  });

  it("disposes its request-owning controller on unmount", async () => {
    const engine = createTestRuntimeState("workspace-1");
    let resolvePage!: () => void;
    const onOlderPageLoadingChanged = vi.fn();
    const { result, unmount } = renderHook(() =>
      useAgentConversationMessagePaging({
        diagnostics: { error: vi.fn(), page: vi.fn() },
        getActiveSessionId: () => "session-1",
        isMounted: () => true,
        onOlderPageLoadingChanged,
        runtime: runtime(
          () =>
            new Promise<void>((resolve) => {
              resolvePage = resolve;
            })
        ),
        conversationServices: engine,
        workspaceId: "workspace-1"
      })
    );

    let request!: Promise<void>;
    act(() => {
      request = result.current.loadOlderMessages();
    });
    expect(onOlderPageLoadingChanged).toHaveBeenLastCalledWith(true);

    unmount();
    await Promise.resolve();
    onOlderPageLoadingChanged.mockClear();
    resolvePage();
    await request;
    expect(onOlderPageLoadingChanged).not.toHaveBeenCalled();

    engine.dispose();
  });

  it("remains usable after the Strict Mode lifecycle probe", async () => {
    const engine = createTestRuntimeState("workspace-1");
    const loadOlderSessionMessages = vi.fn(async () => {});
    const { result, unmount } = renderHook(
      () =>
        useAgentConversationMessagePaging({
          diagnostics: { error: vi.fn(), page: vi.fn() },
          getActiveSessionId: () => "session-1",
          isMounted: () => true,
          onOlderPageLoadingChanged: vi.fn(),
          runtime: runtime(loadOlderSessionMessages),
          conversationServices: engine,
          workspaceId: "workspace-1"
        }),
      {
        wrapper: ({ children }: { children: ReactNode }) => (
          <StrictMode>{children}</StrictMode>
        )
      }
    );

    await act(async () => {
      await result.current.loadOlderMessages();
    });

    expect(loadOlderSessionMessages).toHaveBeenCalledOnce();
    unmount();
    engine.dispose();
  });
});
