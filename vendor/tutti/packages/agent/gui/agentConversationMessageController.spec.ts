import { describe, expect, it, vi } from "vitest";
import { createTestAgentSessionEngine } from "./shared/testing/createTestAgentSessionEngine";
import { createAgentConversationMessageController } from "./agentConversationMessageController";

describe("createAgentConversationMessageController", () => {
  it("asks for the state and messages on first open, and only messages for the latest", () => {
    const engine = createTestAgentSessionEngine("workspace-1");
    const dispatch = vi.spyOn(engine, "dispatch");
    const controller = createController({ engine });

    controller.setActiveSession("session-1");
    controller.requestInitial("session-1");
    controller.requestLatest("session-1");

    expect(dispatch.mock.calls.map(([intent]) => intent)).toEqual([
      expect.objectContaining({ type: "session/reconcileRequested", agentSessionId: "session-1", needsMessages: true, needsState: true }),
      expect.objectContaining({ type: "session/reconcileRequested", agentSessionId: "session-1", needsMessages: true, needsState: false })
    ]);

    controller.dispose();
    engine.dispose();
  });

  it("loads older history only when the history has more", async () => {
    const engine = createTestAgentSessionEngine("workspace-1");
    const loadOlderSessionMessages = vi.fn(async () => {});
    let window = { hasOlderMessages: false, oldestLoadedVersion: 446 as number | null };
    const controller = createController({ engine, loadOlderSessionMessages, olderMessages: () => window });
    controller.setActiveSession("session-1");

    await controller.loadOlder();
    expect(loadOlderSessionMessages).not.toHaveBeenCalled();

    window = { hasOlderMessages: true, oldestLoadedVersion: 446 };
    await controller.loadOlder();
    expect(loadOlderSessionMessages).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      workspaceId: "workspace-1"
    });

    controller.dispose();
    engine.dispose();
  });

  it("deduplicates an in-flight cursor and permits retry after failure", async () => {
    const engine = createTestAgentSessionEngine("workspace-1");
    let rejectFirst: ((error: unknown) => void) | null = null;
    const loadOlderSessionMessages = vi
      .fn<() => Promise<void>>()
      .mockImplementationOnce(
        () =>
          new Promise<void>((_resolve, reject) => {
            rejectFirst = reject;
          })
      )
      .mockResolvedValueOnce(undefined);
    const controller = createController({ engine, loadOlderSessionMessages });
    controller.setActiveSession("session-1");

    const first = controller.loadOlder();
    const duplicate = controller.loadOlder();
    expect(loadOlderSessionMessages).toHaveBeenCalledTimes(1);
    rejectFirst!(new Error("temporary failure"));
    await first;
    await duplicate;
    expect(controller.getSnapshot().olderPagePhase).toBe("error");

    await controller.loadOlder();
    expect(loadOlderSessionMessages).toHaveBeenCalledTimes(2);
    expect(controller.getSnapshot().olderPagePhase).toBe("idle");

    controller.dispose();
    engine.dispose();
  });

  it("fences an older page after the active Session changes", async () => {
    const engine = createTestAgentSessionEngine("workspace-1");
    let resolvePage: (() => void) | null = null;
    const onOlderPageApplied = vi.fn();
    const controller = createController({
      engine,
      onOlderPageApplied,
      loadOlderSessionMessages: () =>
        new Promise<void>((resolve) => {
          resolvePage = resolve;
        })
    });
    controller.setActiveSession("session-1");

    const request = controller.loadOlder();
    controller.setActiveSession("session-2");
    resolvePage!();
    await request;

    expect(onOlderPageApplied).not.toHaveBeenCalled();
    expect(controller.getSnapshot()).toEqual({
      agentSessionId: "session-2",
      error: null,
      olderPagePhase: "idle"
    });

    controller.dispose();
    engine.dispose();
  });

  it("retains synchronization only for the active Session", () => {
    const engine = createTestAgentSessionEngine("workspace-1");
    const releases: string[] = [];
    const ensureSessionSynchronized = vi.fn(
      ({ agentSessionId }: { agentSessionId: string }) =>
        () => {
          releases.push(agentSessionId);
        }
    );
    const controller = createController({
      engine,
      ensureSessionSynchronized
    });

    controller.setActiveSession("session-1");
    controller.setActiveSession("session-1");
    controller.setActiveSession("session-2");
    controller.setActiveSession(null);
    controller.setActiveSession("session-3");
    controller.dispose();

    expect(ensureSessionSynchronized.mock.calls).toEqual([
      [
        expect.objectContaining({
          agentSessionId: "session-1",
          workspaceId: "workspace-1"
        })
      ],
      [
        expect.objectContaining({
          agentSessionId: "session-2",
          workspaceId: "workspace-1"
        })
      ],
      [
        expect.objectContaining({
          agentSessionId: "session-3",
          workspaceId: "workspace-1"
        })
      ]
    ]);
    expect(releases).toEqual(["session-1", "session-2", "session-3"]);

    engine.dispose();
  });

  it("rejects a stale page when the host focus changes before synchronization", async () => {
    const engine = createTestAgentSessionEngine("workspace-1");
    let focusedSessionId = "session-1";
    let resolvePage: (() => void) | null = null;
    const onOlderPageApplied = vi.fn();
    const controller = createController({
      engine,
      onOlderPageApplied,
      isAvailable: (agentSessionId) =>
        !agentSessionId || agentSessionId === focusedSessionId,
      loadOlderSessionMessages: () =>
        new Promise<void>((resolve) => {
          resolvePage = resolve;
        })
    });
    controller.setActiveSession("session-1");

    const request = controller.loadOlder();
    focusedSessionId = "session-2";
    resolvePage!();
    await request;

    expect(onOlderPageApplied).not.toHaveBeenCalled();

    controller.dispose();
    engine.dispose();
  });
});

function createController(input: {
  engine: ReturnType<typeof createTestAgentSessionEngine>;
  ensureSessionSynchronized?: Parameters<
    typeof createAgentConversationMessageController
  >[0]["ensureSessionSynchronized"];
  isAvailable?: (agentSessionId?: string | null) => boolean;
  loadOlderSessionMessages?: () => Promise<void>;
  olderMessages?: () => { hasOlderMessages: boolean; oldestLoadedVersion: number | null };
  onOlderPageApplied?: (agentSessionId: string) => void;
}) {
  return createAgentConversationMessageController({
    engine: input.engine,
    ensureSessionSynchronized: input.ensureSessionSynchronized,
    isAvailable: input.isAvailable ?? (() => true),
    loadOlderSessionMessages: input.loadOlderSessionMessages ?? (async () => {}),
    olderMessages: input.olderMessages ?? (() => ({ hasOlderMessages: true, oldestLoadedVersion: 5 })),
    onOlderPageApplied: input.onOlderPageApplied,
    workspaceId: "workspace-1"
  });
}
