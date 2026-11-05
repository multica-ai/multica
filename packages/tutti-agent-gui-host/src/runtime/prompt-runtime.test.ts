// @vitest-environment node
import type {
LocalPromptQueue,
LocalQueuedPrompt
} from "@multica/core/local-conversation/protocol";
import { type AgentSessionSubmitPromptInput } from "@tutti-os/agent-activity-core";
import { describe,expect,it,vi } from "vitest";
import { TestAgentSessionsModel as AgentSessionsModel } from "../../../../vendor/tutti/packages/agent/gui/shared/testing/TestAgentSessionsModel";
import {
createDesktopPendingStore,
type DesktopPendingStore
} from "./desktop-pending-store";
import { createPromptRuntime } from "./prompt-runtime";
type SubmitInput = AgentSessionSubmitPromptInput;

function setup(running = false, pendingStore?: DesktopPendingStore) {
  const sendInput = vi.fn();
  const base = new AgentSessionsModel();
  base.upsertSessions([
    {
      agentSessionId: "session",
      workspaceId: "ws",
      provider: "codex",
      title: "Conversation",
      cwd: "/workspace",
      activeTurnId: running ? "active" : null,
      ...(running
        ? {
            activeTurn: {
              agentSessionId: "session",
              turnId: "active",
              origin: "user_prompt",
              phase: "running",
              startedAtUnixMs: 1,
              updatedAtUnixMs: 1
            }
          }
        : {}),
      pendingInteractions: [],
      latestTurnInteractions: []
    }
  ]);
  const messages: {
    role: string;
    turnId: string;
    payload: Record<string, unknown>;
  }[] = [];
  const options = {
    pendingStore,
    enabled: () => true,
    messages: () => messages,
    activate: vi.fn(async () => undefined),
    loadSession: vi.fn(async () => undefined),
    applyActivationResult: vi.fn(),
    updateActivationSettings: vi.fn(async () => {}),
    describeActivationError: () => ({ code: null, message: null }),
    enqueue: vi.fn(
      async (_input: SubmitInput): Promise<LocalPromptQueue> => ({
        revision: 1,
        paused: false,
        items: []
      })
    ),
    update: vi.fn(
      async (): Promise<LocalPromptQueue> => ({
        revision: 2,
        paused: true,
        items: []
      })
    ),
    error: vi.fn(),
    respondToInteraction: vi.fn(async () => ({
      session: { agentSessionId: "session", workspaceId: "ws" }
    })),
    editRetry: vi.fn(),
    recoverEditRetry: vi.fn(),
    reconcileWorkspace: vi.fn()
  };
  const adapter = createPromptRuntime(base, "ws", options);
  return {
    ...adapter,
    engine: {
      sessions: base,
      prompts: adapter.prompts,
      activations: adapter.activations,
      sessionLoads: adapter.loads,
      dispose: adapter.dispose
    },
    messages,
    options,
    sendInput
  };
}
function queued(requestId: string): LocalQueuedPrompt {
  return {
    requestId,
    content: requestId,
    display: {
      id: requestId,
      clientSubmitId: requestId,
      createdAtUnixMs: 1,
      content: [{ type: "text", text: requestId }]
    }
  };
}
const submit = (engine: ReturnType<typeof setup>["engine"], id: string) =>
  engine.prompts.submitPrompt({
    agentSessionId: "session",
    clientSubmitId: id,
    content: [{ type: "text", text: id }]
  });
const sendNow = (
  engine: ReturnType<typeof setup>["engine"],
  promptId: string
) => engine.prompts.update("session", "sendNow", promptId);
const visible = (engine: ReturnType<typeof setup>["engine"]) =>
  engine.prompts
    .queueRecord("session")
    ?.prompts.filter((prompt) => prompt.visibleInQueue !== false)
    .map((prompt) => prompt.id) ?? [];
describe("desktop queue presentation", () => {
  it.each([
    { targetTurnId: "active", routing: "send_now" as const },
    {
      capabilityRefs: [
        { capability: "tutti" as const, source: "slash_command" as const }
      ]
    }
  ])(
    "rejects unsupported submission before accepting or checkpointing it: %j",
    (unsupported) => {
      const store = createDesktopPendingStore();
      const { engine, options } = setup(true, store);
      try {
        expect(
          engine.prompts.submitPrompt({
            agentSessionId: "session",
            clientSubmitId: "unsupported",
            content: [{ type: "text", text: "Keep my draft" }],
            ...unsupported
          })
        ).toEqual({ accepted: false, queued: false });
        expect(options.enqueue).not.toHaveBeenCalled();
        expect(options.error).toHaveBeenCalledOnce();
        expect(store.getState().queues).toEqual([]);
        expect(engine.prompts.queueRecord("session")).toBeNull();
        expect(engine.prompts.isSubmitting("session")).toBe(false);
      } finally {
        engine.dispose();
      }
    }
  );
  it("submits steering immediately and keeps it pending after acknowledgement until dispatch", async () => {
    const { engine, receive, options } = setup(true);
    let acknowledge!: (queue: LocalPromptQueue) => void;
    options.update.mockImplementation(
      () =>
        new Promise((resolve) => {
          acknowledge = resolve;
        })
    );
    receive("session", {
      revision: 1,
      paused: false,
      items: [queued("next"), queued("later")]
    });
    try {
      sendNow(engine, "next");
      expect(options.update).toHaveBeenCalledWith("session", "sendNow", "next");
      expect(engine.prompts.queueRecord("session")).toMatchObject({
        inFlight: { kind: "send", promptId: "next" },
        sendNextPromptId: "next"
      });
      // Cancelling the old turn first publishes a paused queue.
      receive("session", {
        revision: 2,
        paused: true,
        items: [queued("next"), queued("later")]
      });
      acknowledge({
        revision: 3,
        paused: false,
        items: [queued("next"), queued("later")]
      });
      await new Promise((resolve) => setTimeout(resolve, 0));
      sendNow(engine, "next");
      expect(options.update).toHaveBeenCalledOnce();
      expect(engine.prompts.queueRecord("session")?.inFlight?.promptId).toBe(
        "next"
      );
      expect(visible(engine)).toEqual(["next", "later"]);
      receive("session", {
        revision: 4,
        paused: false,
        dispatchingRequestId: "next",
        items: [queued("next"), queued("later")]
      });
      expect(engine.prompts.queueRecord("session")?.inFlight).toBeNull();
      expect(
        engine.prompts.queueRecord("session")?.sendNextPromptId
      ).toBeNull();
      sendNow(engine, "next");
      expect(options.update).toHaveBeenCalledOnce();
      expect(visible(engine)).toEqual(["later"]);
    } finally {
      engine.dispose();
    }
  });
  it.each(["stop", "failure", "removed"] as const)(
    "releases acknowledged steering on %s and permits explicit retry",
    async (outcome) => {
      const { engine, receive, options } = setup(true);
      options.update.mockResolvedValue({
        revision: 2,
        paused: false,
        items: [queued("next")]
      });
      receive("session", {
        revision: 1,
        paused: false,
        items: [queued("next")]
      });
      try {
        sendNow(engine, "next");
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(engine.prompts.queueRecord("session")?.inFlight?.promptId).toBe(
          "next"
        );
        receive("session", {
          revision: 3,
          paused: outcome !== "removed",
          items: outcome === "removed" ? [] : [queued("next")],
          ...(outcome === "failure" ? { error: "Could not dispatch" } : {})
        });
        expect(engine.prompts.queueRecord("session")?.inFlight).toBeNull();
        if (outcome !== "removed") {
          options.update.mockResolvedValue({
            revision: 4,
            paused: false,
            items: [queued("next")]
          });
          sendNow(engine, "next");
          expect(options.update).toHaveBeenCalledTimes(2);
        }
      } finally {
        engine.dispose();
      }
    }
  );
  it("settles a late steer acknowledgement against a newer stopped snapshot", async () => {
    const { engine, receive, options } = setup(true);
    let acknowledge!: (queue: LocalPromptQueue) => void;
    options.update.mockImplementation(
      () =>
        new Promise((resolve) => {
          acknowledge = resolve;
        })
    );
    receive("session", { revision: 1, paused: false, items: [queued("next")] });
    try {
      sendNow(engine, "next");
      receive("session", {
        revision: 4,
        paused: true,
        items: [queued("next")]
      });
      acknowledge({ revision: 3, paused: false, items: [queued("next")] });
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(engine.prompts.queueRecord("session")).toMatchObject({
        inFlight: null,
        suspendReason: "user_stop"
      });
      sendNow(engine, "next");
      expect(options.update).toHaveBeenCalledTimes(2);
    } finally {
      engine.dispose();
    }
  });
  it("rejects the 101st prompt before admission or checkpointing and accepts after a slot opens", () => {
    const store = createDesktopPendingStore();
    const { engine, receive, options } = setup(true, store);
    const items = Array.from({ length: 100 }, (_, index) =>
      queued(`saved-${index}`)
    );
    receive("session", { revision: 1, paused: true, items });
    try {
      expect(submit(engine, "overflow")).toEqual({
        accepted: false,
        queued: false
      });
      expect(options.enqueue).not.toHaveBeenCalled();
      expect(options.error).toHaveBeenCalledWith(
        expect.objectContaining({ message: expect.stringContaining("100") })
      );
      expect(store.getState().queues).toEqual([]);
      expect(visible(engine)).toHaveLength(100);
      receive("session", { revision: 2, paused: true, items: items.slice(1) });
      expect(submit(engine, "overflow").accepted).toBe(true);
      expect(options.enqueue).toHaveBeenCalledOnce();
    } finally {
      engine.dispose();
    }
  });
  it("reserves capacity synchronously for unanswered submissions, including hidden dispatches", () => {
    const store = createDesktopPendingStore();
    const { engine, receive, options } = setup(true, store);
    const items = Array.from({ length: 99 }, (_, index) =>
      queued(`saved-${index}`)
    );
    options.enqueue.mockImplementation(() => new Promise(() => {}));
    receive("session", {
      revision: 1,
      paused: false,
      items,
      dispatchingRequestId: "saved-0"
    });
    try {
      expect(submit(engine, "last-slot").accepted).toBe(true);
      expect(submit(engine, "overflow")).toEqual({
        accepted: false,
        queued: false
      });
      expect(options.enqueue).toHaveBeenCalledOnce();
      expect(
        store.getState().queues[0]?.prompts.map((prompt) => prompt.id)
      ).toEqual(["last-slot"]);
      // A canonical confirmation transfers ownership without consuming a second slot.
      receive("session", {
        revision: 2,
        paused: true,
        items: [...items, queued("last-slot")]
      });
      expect(store.getState().queues).toEqual([]);
      expect(submit(engine, "still-full").accepted).toBe(false);
      receive("session", { revision: 3, paused: true, items });
      expect(submit(engine, "new-slot").accepted).toBe(true);
    } finally {
      engine.dispose();
    }
  });
  it("counts restored unconfirmed submissions toward the per-conversation limit without replaying them", () => {
    const store = createDesktopPendingStore();
    store
      .getState()
      .setQueues([
        { agentSessionId: "session", prompts: [queued("restored").display] }
      ]);
    const { engine, receive, options } = setup(true, store);
    receive("session", {
      revision: 1,
      paused: true,
      items: Array.from({ length: 99 }, (_, index) => queued(`saved-${index}`))
    });
    try {
      expect(submit(engine, "overflow").accepted).toBe(false);
      expect(options.enqueue).not.toHaveBeenCalled();
      expect(
        engine.prompts.submitPrompt({
          agentSessionId: "other-session",
          clientSubmitId: "independent",
          content: [{ type: "text", text: "independent" }]
        }).accepted
      ).toBe(true);
      expect(options.enqueue).toHaveBeenCalledOnce();
    } finally {
      engine.dispose();
    }
  });
  it("retains send-now errors across snapshots until the operation succeeds", async () => {
    const { engine, receive, options } = setup(true);
    receive("session", { revision: 1, paused: false, items: [queued("next")] });
    options.update.mockRejectedValue(new Error("Queue update unavailable"));
    try {
      engine.prompts.update("session", "sendNow", "next");
      await vi.waitFor(() => expect(options.error).toHaveBeenCalledOnce());
      expect(visible(engine)).toEqual(["next"]);
      expect(engine.prompts.queueRecord("session")?.inFlight).toBeNull();
      expect(engine.prompts.queueRecord("session")?.failureMessage).toBe(
        "Queue update unavailable"
      );
      receive("session", {
        revision: 3,
        paused: false,
        items: [queued("next")]
      });
      expect(engine.prompts.queueRecord("session")?.failureMessage).toBe(
        "Queue update unavailable"
      );
      options.update.mockResolvedValue({
        revision: 4,
        paused: false,
        items: []
      });
      sendNow(engine, "next");
      await vi.waitFor(() => expect(visible(engine)).toEqual([]));
      expect(engine.prompts.queueRecord("session")?.failureMessage).toBeNull();
    } finally {
      engine.dispose();
    }
  });
  it("waits for an in-flight image admission and coalesces repeated send-now clicks", async () => {
    const { engine, receive, options } = setup(true);
    let accept!: (queue: LocalPromptQueue) => void;
    options.enqueue.mockImplementation(
      () =>
        new Promise((resolve) => {
          accept = resolve;
        })
    );
    const input = {
      agentSessionId: "session",
      clientSubmitId: "image",
      content: [
        { type: "text" as const, text: "use this image" },
        {
          type: "image" as const,
          attachmentId: "saved-image",
          mimeType: "image/png"
        }
      ]
    };
    engine.prompts.submitPrompt(input);
    sendNow(engine, "image");
    sendNow(engine, "image");
    expect(options.update).not.toHaveBeenCalled();
    expect(options.enqueue).toHaveBeenCalledOnce();
    receive("session", { revision: 0, paused: false, items: [] });
    expect(engine.prompts.queueRecord("session")?.inFlight?.promptId).toBe(
      "image"
    );
    accept({
      revision: 1,
      paused: false,
      items: [{ ...queued("image"), attachmentIds: ["saved-image"] }]
    });
    await vi.waitFor(() => expect(options.update).toHaveBeenCalledOnce());
    expect(options.update).toHaveBeenCalledWith("session", "sendNow", "image");
    expect(options.enqueue).toHaveBeenCalledWith(input);
    engine.dispose();
  });
  it("retries failed admission explicitly with the original submission and preserves its error on refresh", async () => {
    const { engine, receive, options } = setup(true);
    options.enqueue.mockRejectedValueOnce(new Error("Admission unavailable"));
    const input = {
      agentSessionId: "session",
      clientSubmitId: "retry",
      content: [
        { type: "text" as const, text: "original" },
        { type: "image" as const, attachmentId: "saved-image" }
      ]
    };
    engine.prompts.submitPrompt(input);
    await vi.waitFor(() => expect(options.error).toHaveBeenCalledOnce());
    receive("session", { revision: 1, paused: false, items: [] });
    expect(engine.prompts.queueRecord("session")?.failureMessage).toBe(
      "Admission unavailable"
    );
    expect(options.enqueue).toHaveBeenCalledOnce();
    input.content[0]!.text = "changed after submission";
    options.enqueue.mockResolvedValue({
      revision: 2,
      paused: false,
      items: [queued("retry")]
    });
    sendNow(engine, "retry");
    sendNow(engine, "retry");
    await vi.waitFor(() => expect(options.update).toHaveBeenCalledOnce());
    expect(options.enqueue).toHaveBeenCalledTimes(2);
    expect(options.enqueue.mock.calls[1]?.[0]).toMatchObject({
      clientSubmitId: "retry",
      content: [
        { type: "text", text: "original" },
        { type: "image", attachmentId: "saved-image" }
      ]
    });
    expect(engine.prompts.queueRecord("session")?.failureMessage).toBeNull();
    engine.dispose();
  });
  it("confirms a lost admission receipt with the same ID without sending an already executed prompt again", async () => {
    const { engine, receive, options } = setup();
    options.enqueue.mockRejectedValueOnce(
      new Error("The host request timed out")
    );
    submit(engine, "completed");
    await vi.waitFor(() => expect(options.error).toHaveBeenCalledOnce());
    receive("session", { revision: 2, paused: false, items: [] });
    options.enqueue.mockResolvedValue({
      revision: 3,
      paused: false,
      items: []
    });
    sendNow(engine, "completed");
    await vi.waitFor(() => expect(visible(engine)).toEqual([]));
    expect(options.enqueue).toHaveBeenCalledTimes(2);
    expect(options.enqueue.mock.calls[1]?.[0]).toEqual(
      options.enqueue.mock.calls[0]?.[0]
    );
    expect(options.update).not.toHaveBeenCalled();
    engine.dispose();
  });
  it("uses an authoritative admission snapshot even when the RPC acknowledgement fails later", async () => {
    const { engine, receive, options } = setup();
    let reject!: (error: Error) => void;
    options.enqueue.mockImplementation(
      () =>
        new Promise((_resolve, fail) => {
          reject = fail;
        })
    );
    submit(engine, "confirmed");
    receive("session", {
      revision: 1,
      paused: false,
      items: [queued("confirmed")],
      dispatchingRequestId: "confirmed"
    });
    receive("session", { revision: 2, paused: false, items: [] });
    reject(new Error("The host request timed out"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(visible(engine)).toEqual([]);
    expect(options.error).not.toHaveBeenCalled();
    engine.dispose();
  });
  it("does not interrupt a prompt that starts dispatching while send-now awaits admission", async () => {
    const { engine, options } = setup(true);
    let accept!: (queue: LocalPromptQueue) => void;
    options.enqueue.mockImplementation(
      () =>
        new Promise((resolve) => {
          accept = resolve;
        })
    );
    submit(engine, "dispatching");
    sendNow(engine, "dispatching");
    accept({
      revision: 1,
      paused: false,
      items: [queued("dispatching")],
      dispatchingRequestId: "dispatching"
    });
    await vi.waitFor(() => expect(visible(engine)).toEqual([]));
    expect(options.update).not.toHaveBeenCalled();
    engine.dispose();
  });
  it("keeps a newer operation error when a late admission response succeeds", async () => {
    const { engine, receive, options } = setup(true);
    let accept!: (queue: LocalPromptQueue) => void;
    options.enqueue.mockImplementation(
      () =>
        new Promise((resolve) => {
          accept = resolve;
        })
    );
    submit(engine, "next");
    const receipt = { revision: 1, paused: false, items: [queued("next")] };
    receive("session", receipt);
    options.update.mockRejectedValue(new Error("Cancellation failed"));
    sendNow(engine, "next");
    await vi.waitFor(() => expect(options.error).toHaveBeenCalledOnce());
    accept(receipt);
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(engine.prompts.queueRecord("session")?.failureMessage).toBe(
      "Cancellation failed"
    );
    engine.dispose();
  });
  it("transfers admission to Electron and does not drain or pause it on GUI disposal", async () => {
    const { engine, options, sendInput } = setup();
    expect(
      engine.prompts.submitPrompt({
        agentSessionId: "session",
        clientSubmitId: "submit",
        content: [{ type: "text", text: "continue" }]
      })
    ).toEqual({ accepted: true, queued: false });
    await vi.waitFor(() => expect(options.enqueue).toHaveBeenCalledOnce());
    engine.dispose();
    expect(sendInput).not.toHaveBeenCalled();
    expect(options.update).not.toHaveBeenCalled();
  });
  it.each([undefined, "first"])("keeps an idle submit out of the queue before and after admission (dispatching: %s)", async (dispatchingRequestId) => {
    const { engine, receive, options } = setup();
    let accept!: (queue: LocalPromptQueue) => void;
    options.enqueue.mockImplementation(
      () =>
        new Promise((resolve) => {
          accept = resolve;
        })
    );
    expect(submit(engine, "first")).toEqual({ accepted: true, queued: false });
    expect(visible(engine)).toEqual([]);
    expect(engine.prompts.isSubmitting("session")).toBe(true);
    const receipt = {
      revision: 1,
      paused: false,
      items: [queued("first")],
      dispatchingRequestId
    };
    receive("session", receipt);
    accept(receipt);
    await vi.waitFor(() => expect(options.enqueue).toHaveBeenCalledOnce());
    await Promise.resolve();
    expect(visible(engine)).toEqual([]);
    expect(engine.prompts.isSubmitting("session")).toBe(true);
    receive("session", { revision: 2, paused: false, items: [] });
    expect(engine.prompts.isSubmitting("session")).toBe(false);
    engine.dispose();
  });
  it("shows only later submissions while the first idle admission is pending", async () => {
    const { engine, options } = setup();
    options.enqueue.mockImplementation(() => new Promise(() => {}));
    expect(submit(engine, "first").queued).toBe(false);
    expect(submit(engine, "second").queued).toBe(true);
    expect(visible(engine)).toEqual(["second"]);
    engine.dispose();
  });
  it("shows a single waiting submission when a previous turn is active", () => {
    const { engine, options } = setup(true);
    options.enqueue.mockImplementation(() => new Promise(() => {}));
    expect(submit(engine, "waiting").queued).toBe(true);
    expect(visible(engine)).toEqual(["waiting"]);
    engine.dispose();
  });
  it("reveals a previously hidden submission if admission fails", async () => {
    const { engine, options } = setup();
    options.enqueue.mockRejectedValue(new Error("Admission unavailable"));
    submit(engine, "retry-me");
    expect(visible(engine)).toEqual([]);
    await vi.waitFor(() => expect(options.error).toHaveBeenCalledOnce());
    expect(visible(engine)).toEqual(["retry-me"]);
    expect(engine.prompts.queueRecord("session")?.failureMessage).toBe(
      "Admission unavailable"
    );
    expect(engine.prompts.isSubmitting("session")).toBe(false);
    engine.dispose();
  });
  it("projects dispatching separately from waiting, paused and failed desktop records", () => {
    const { engine, receive } = setup();
    receive("session", {
      revision: 1,
      paused: false,
      dispatchingRequestId: "first",
      items: [queued("first"), queued("second")]
    });
    expect(visible(engine)).toEqual(["second"]);
    receive("session", {
      revision: 2,
      paused: true,
      items: [queued("first"), queued("second")]
    });
    expect(visible(engine)).toEqual(["first", "second"]);
    receive("session", {
      revision: 3,
      paused: false,
      error: "Try again",
      items: [queued("first")]
    });
    expect(visible(engine)).toEqual(["first"]);
    engine.dispose();
  });
  it("projects durable queues on remount and ignores stale replies", () => {
    const { engine, receive } = setup();
    const item = {
      requestId: "submit",
      content: "next",
      display: {
        id: "submit",
        clientSubmitId: "submit",
        createdAtUnixMs: 1,
        content: [{ type: "text" as const, text: "next" }]
      }
    };
    receive("session", { revision: 5, paused: false, items: [item] });
    receive("session", { revision: 4, paused: true, items: [] });
    const queue = engine.prompts.queueRecord("session")!;
    expect(queue.prompts).toEqual([item.display]);
    expect(queue.suspendReason).toBeNull();
    expect(engine.prompts.queueRecord("session")).toBe(
      engine.prompts.queueRecord("session")
    );
    engine.dispose();
  });
  it("stops the turn a message on its way starts after the stop", async () => {
    const { engine, messages, options, prompts } = setup();
    let admit!: (queue: LocalPromptQueue) => void;
    options.enqueue.mockImplementation(
      () =>
        new Promise((resolve) => {
          admit = resolve;
        })
    );
    try {
      submit(engine, "first");
      prompts.stop("session");
      expect(prompts.cancelRecord("session")).toMatchObject({
        status: "awaitingTurn",
        targetClientSubmitId: "first"
      });
      admit({ revision: 3, paused: false, items: [] });
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(options.update).toHaveBeenCalledOnce();
      messages.push({
        role: "user",
        turnId: "started",
        payload: { clientSubmitId: "first" }
      });
      const turn = {
        agentSessionId: "session",
        turnId: "started",
        origin: "user_prompt" as const,
        phase: "running" as const,
        startedAtUnixMs: 2,
        updatedAtUnixMs: 2
      };
      engine.sessions.replaceHistory(
        {
          agentSessionId: "session",
          workspaceId: "ws",
          provider: "codex",
          title: "Conversation",
          cwd: "/workspace",
          activeTurn: turn,
          activeTurnId: "started",
          latestTurn: turn,
          pendingInteractions: [],
          latestTurnInteractions: []
        },
        [turn]
      );
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(options.update.mock.calls).toEqual([
        ["session", "stop", undefined],
        ["session", "stop", undefined]
      ]);
      expect(prompts.cancelRecord("session")).toMatchObject({
        status: "accepted",
        turnId: "started"
      });
    } finally {
      engine.dispose();
    }
  });
  it("answers an interaction until the conversation shows it answered", async () => {
    const { engine, options, prompts } = setup(true);
    const interaction = {
      agentSessionId: "session",
      turnId: "active",
      requestId: "question",
      kind: "question" as const,
      status: "pending" as const,
      createdAtUnixMs: 2,
      updatedAtUnixMs: 2
    };
    try {
      const session = engine.sessions.session("session")!;
      const report = (
        reported:
          | typeof interaction
          | (Omit<typeof interaction, "status"> & { status: "answered" })
      ) =>
        engine.sessions.upsertSession({
          ...session,
          pendingInteractions: [],
          latestTurnInteractions: [reported]
        });
      report(interaction);
      expect(
        prompts.respondToInteraction({
          action: "submit",
          agentSessionId: "session",
          requestId: "question",
          turnId: "active",
          payload: { answersByQuestionId: { scope: "all" } }
        })
      ).toBe(true);
      expect(options.respondToInteraction).toHaveBeenCalledOnce();
      expect(prompts.isRespondingToInteraction("session")).toBe(true);
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(prompts.isRespondingToInteraction("session")).toBe(true);
      report({ ...interaction, status: "answered", updatedAtUnixMs: 3 });
      expect(prompts.isRespondingToInteraction("session")).toBe(false);
    } finally {
      engine.dispose();
    }
  });
  it("routes queue removal, send-now and stop to Electron", async () => {
    const { engine, options, prompts } = setup();
    engine.prompts.update("session", "remove", "removed");
    engine.prompts.update("session", "sendNow", "next");
    prompts.stop("session");
    expect(options.update.mock.calls).toEqual([
      ["session", "remove", "removed"],
      ["session", "sendNow", "next"],
      ["session", "stop", undefined]
    ]);
    await Promise.resolve();
    engine.dispose();
  });
});

function pendingStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
    removeItem: (key: string) => {
      values.delete(key);
    }
  };
}

describe("unconfirmed desktop submissions across document lifetimes", () => {
  it("checkpoints before returning accepted and restores rejection without replay, retrying the same ID", async () => {
    const storage = pendingStorage();
    const restore = () => createDesktopPendingStore(storage, "scope");
    const first = setup(false, restore());
    first.options.enqueue.mockRejectedValue(new Error("Admission rejected"));
    submit(first.engine, "original-id");
    expect(restore().getState().queues[0]?.prompts[0]?.clientSubmitId).toBe(
      "original-id"
    );
    await vi.waitFor(() => expect(first.options.error).toHaveBeenCalledOnce());
    first.engine.dispose();
    const next = setup(false, restore());
    try {
      expect(visible(next.engine)).toEqual(["original-id"]);
      expect(next.engine.prompts.isSubmitting("session")).toBe(false);
      expect(next.options.enqueue).not.toHaveBeenCalled();
      sendNow(next.engine, "original-id");
      await vi.waitFor(() => expect(visible(next.engine)).toEqual([]));
      expect(next.options.enqueue).toHaveBeenCalledOnce();
      expect(next.options.enqueue.mock.calls[0]?.[0]).toMatchObject(
        first.options.enqueue.mock.calls[0]![0]
      );
      expect(next.options.update).not.toHaveBeenCalled();
      expect(restore().getState().queues).toEqual([]);
    } finally {
      next.engine.dispose();
    }
  });
  it("restores in-flight admissions, reconciles canonical ownership, and ignores an old document's late failure", async () => {
    const storage = pendingStorage();
    const restore = () => createDesktopPendingStore(storage, "scope");
    const first = setup(false, restore());
    let reject!: (error: Error) => void;
    first.options.enqueue.mockImplementation(
      () =>
        new Promise((_resolve, fail) => {
          reject = fail;
        })
    );
    submit(first.engine, "in-flight");
    first.engine.dispose();
    const next = setup(false, restore());
    try {
      expect(visible(next.engine)).toEqual(["in-flight"]);
      expect(next.options.enqueue).not.toHaveBeenCalled();
      next.receive("session", {
        revision: 1,
        paused: false,
        items: [queued("in-flight")],
        dispatchingRequestId: "in-flight"
      });
      expect(visible(next.engine)).toEqual([]);
      expect(restore().getState().queues).toEqual([]);
      reject(new Error("Lost acknowledgement"));
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(restore().getState().queues).toEqual([]);
      expect(first.options.error).not.toHaveBeenCalled();
    } finally {
      next.engine.dispose();
    }
  });
  it("keeps a rejected submission on failed removal and clears it only after confirmed removal", async () => {
    const storage = pendingStorage();
    const restore = () => createDesktopPendingStore(storage, "scope");
    const handle = setup(false, restore());
    handle.options.enqueue.mockRejectedValue(new Error("Admission rejected"));
    submit(handle.engine, "discard");
    await vi.waitFor(() => expect(handle.options.error).toHaveBeenCalledOnce());
    const remove = () =>
      handle.engine.prompts.update("session", "remove", "discard");
    try {
      handle.options.update.mockRejectedValueOnce(new Error("Removal failed"));
      remove();
      await vi.waitFor(() =>
        expect(handle.options.error).toHaveBeenCalledTimes(2)
      );
      expect(restore().getState().queues[0]?.prompts[0]?.id).toBe("discard");
      remove();
      await vi.waitFor(() => expect(visible(handle.engine)).toEqual([]));
      expect(restore().getState().queues).toEqual([]);
      expect(handle.options.enqueue).toHaveBeenCalledOnce();
    } finally {
      handle.engine.dispose();
    }
  });
});
