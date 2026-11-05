// @vitest-environment node
import type { LocalPromptQueue } from "@multica/core/local-conversation/protocol";
import { reaction } from "mobx";
import { describe,expect,it,vi } from "vitest";
import {
PromptService,
type PromptServiceOptions,
type PromptStatus
} from "./prompt-service";

type Input = { agentSessionId: string; clientSubmitId: string };
type Display = {
  id: string;
  clientSubmitId?: string;
  visibleInQueue?: boolean;
};
const settled = () => new Promise((resolve) => setTimeout(resolve, 0));
const empty = (revision = 1): LocalPromptQueue => ({
  revision,
  paused: false,
  items: []
});
const restored = (id: string) => ({
  agentSessionId: "s",
  input: { agentSessionId: "s", clientSubmitId: id },
  display: { id, clientSubmitId: id, visibleInQueue: true }
});

function service(options: Partial<PromptServiceOptions<Input, Display>> = {}) {
  const update = vi.fn(async () => empty(2));
  const enqueue = vi.fn(async () => empty(2));
  const turns = options.turns ?? {
    session: () => null,
    turn: () => null,
    deleted: () => false,
    runtimeBlocked: () => false,
    interactionStatus: () => null,
    stopTarget: () => null,
    turnForSubmit: () => null
  };
  return {
    update,
    enqueue,
    turns,
    prompts: new PromptService<Input, Display>({
      enqueue,
      update,
      error: vi.fn(),
      workspaceId: "w",
      respondToInteraction: vi.fn(),
      editRetry: vi.fn(),
      recoverEditRetry: vi.fn(),
      reconcile: vi.fn(),
      reconcileWorkspace: vi.fn(),
      turns,
      ...options
    })
  };
}

describe("messages restored after a reload", () => {
  it("asks once what became of them, and drops only what no longer needs the user", async () => {
    const answers: Record<string, PromptStatus> = {
      delivered: "delivered",
      waiting: "queued",
      removed: "removed",
      refused: "failed",
      never: "none",
      maybe: "unknown"
    };
    const statuses = vi.fn(async () => answers);
    const { prompts } = service({
      statuses,
      restored: Object.keys(answers).map(restored)
    });
    prompts.receive("s", empty());
    prompts.receive("s", empty(2));
    await settled();
    expect(statuses).toHaveBeenCalledTimes(1);
    // A refused or lost message can go again; one that may have arrived waits for a decision.
    expect(
      prompts.pending("s").map((item) => item.input.clientSubmitId)
    ).toEqual(["refused", "never", "maybe"]);
  });

  it("keeps them all when the daemon cannot answer", async () => {
    const { prompts } = service({
      statuses: vi.fn(async () => {
        throw new Error("offline");
      }),
      restored: [restored("a")]
    });
    prompts.receive("s", empty());
    await settled();
    expect(prompts.pending("s")).toHaveLength(1);
  });

  it("never sends one on its own", async () => {
    const { prompts, enqueue } = service({
      statuses: vi.fn(async () => ({ a: "failed" as const })),
      restored: [restored("a")]
    });
    prompts.receive("s", empty());
    await settled();
    expect(enqueue).not.toHaveBeenCalled();
  });
});

describe("presenting the queue", () => {
  it("keeps a tentative dispatch queued while another turn is still running", () => {
    const { prompts, turns } = service();
    vi.spyOn(turns, "session").mockReturnValue({ workspaceId: "w", activeTurnId: "previous" });
    vi.spyOn(turns, "turn").mockReturnValue({ turnId: "previous", phase: "running" });
    prompts.receive("s", {
      revision: 1, paused: false, dispatchingRequestId: "next",
      items: [{ requestId: "next", display: { id: "next", clientSubmitId: "next" } }]
    } as LocalPromptQueue);
    expect(prompts.visibleQueuedPrompts("s").map((item) => item.id)).toEqual(["next"]);
    expect(prompts.pendingSubmits("s")).toEqual([]);
    vi.spyOn(turns, "session").mockReturnValue({ workspaceId: "w", activeTurnId: null });
    prompts.receive("s", { ...prompts.queue("s")!, revision: 2 });
    expect(prompts.visibleQueuedPrompts("s")).toEqual([]);
    expect(prompts.pendingSubmits("s")).toMatchObject([{ clientSubmitId: "next" }]);
    prompts.dispose();
  });

  it.each(["ready", "paused", "failed", "other-turn"])("presents an immediate submission against the authoritative %s queue state", async (state) => {
    const { prompts, turns } = service();
    const turn = vi.spyOn(turns, "turnForSubmit");
    if (state === "other-turn")
      vi.spyOn(turns, "session").mockReturnValue({ workspaceId: "w", activeTurnId: "other" });
    prompts.submit(
      { agentSessionId: "s", clientSubmitId: "a" },
      { id: "a", clientSubmitId: "a", visibleInQueue: false }
    );
    await settled();
    prompts.receive("s", {
      revision: 3, paused: state === "paused", error: state === "failed" ? "Retry" : undefined,
      items: [{ requestId: "a", display: { id: "a", clientSubmitId: "a" } }] as never
    });
    expect(prompts.visibleQueuedPrompts("s").map((item) => item.id)).toEqual(state === "ready" ? [] : ["a"]);
    turn.mockReturnValue("turn-a");
    prompts.lifecycleChanged();
    expect(prompts.prompts("s")).toEqual([]);
    prompts.dispose();
  });
  it.each([false, true])("retains an accepted immediate message until its transcript arrives (snapshot first: %s)", async (snapshotFirst) => {
    const saved: unknown[] = [];
    const { prompts, turns, enqueue } = service({ save: (queues) => saved.push(queues) });
    const delivered = vi.spyOn(turns, "turnForSubmit");
    prompts.submit(
      { agentSessionId: "s", clientSubmitId: "a" },
      { id: "a", clientSubmitId: "a", visibleInQueue: false }
    );
    if (snapshotFirst) {
      prompts.receive("s", {
        revision: 1, paused: false, dispatchingRequestId: "a",
        items: [{ requestId: "a", display: { id: "a", clientSubmitId: "a" } }] as never
      });
      prompts.receive("s", empty(3));
    }
    await settled();
    expect(prompts.pending("s")).toEqual([]);
    expect(saved.at(-1)).toEqual([]);
    expect(prompts.visibleQueuedPrompts("s")).toEqual([]);
    expect(prompts.pendingSubmits("s")).toMatchObject([{ clientSubmitId: "a", status: "accepted" }]);
    expect(prompts.isSubmitting("s")).toBe(false);
    expect(prompts.hasUnconfirmedSubmit("s")).toBe(true);
    expect(prompts.hasPrompt("s", "a")).toBe(true);
    expect(enqueue).toHaveBeenCalledOnce();
    delivered.mockReturnValue("turn-a");
    prompts.lifecycleChanged();
    expect(prompts.pendingSubmits("s")).toEqual([]);
    expect(prompts.hasPrompt("s", "a")).toBe(false);
    prompts.dispose();
  });

  it("does not restore an optimistic message when its transcript precedes admission acknowledgement", async () => {
    const { prompts, turns } = service();
    vi.spyOn(turns, "turnForSubmit").mockReturnValue("turn-a");
    prompts.submit(
      { agentSessionId: "s", clientSubmitId: "a" },
      { id: "a", clientSubmitId: "a", visibleInQueue: false }
    );
    await settled();
    expect(prompts.pendingSubmits("s")).toEqual([]);
    prompts.dispose();
  });

  it("clears the accepted presentation after explicit removal", async () => {
    const { prompts, enqueue } = service();
    prompts.submit(
      { agentSessionId: "s", clientSubmitId: "a" },
      { id: "a", clientSubmitId: "a", visibleInQueue: false }
    );
    await settled();
    expect(prompts.pendingSubmits("s")).toHaveLength(1);
    prompts.update("s", "remove", "a");
    await settled();
    expect(prompts.pendingSubmits("s")).toEqual([]);
    expect(enqueue).toHaveBeenCalledOnce();
    prompts.dispose();
  });
  it("tells observers once per change, after saving what has not been handed over", async () => {
    const order: string[] = [];
    const { prompts } = service({ save: () => order.push("save") });
    const stop = reaction(
      () => prompts.revision,
      () => order.push("notify")
    );
    prompts.submit(
      { agentSessionId: "s", clientSubmitId: "a" },
      { id: "a", clientSubmitId: "a" }
    );
    expect(order.slice(0, 2)).toEqual(["save", "notify"]);
    stop();
    await settled();
  });

  it("shows a message once while it is both pending and queued", () => {
    const { prompts } = service();
    prompts.submit(
      { agentSessionId: "s", clientSubmitId: "a" },
      { id: "a", clientSubmitId: "a" }
    );
    prompts.receive("s", {
      revision: 1,
      paused: false,
      items: [
        { requestId: "a", display: { id: "a", clientSubmitId: "a" } }
      ] as never
    });
    expect(prompts.prompts("s").map((item) => item.id)).toEqual(["a"]);
  });

  it("ignores an acknowledgement older than what is known", () => {
    const { prompts } = service();
    prompts.receive("s", { revision: 5, paused: true, items: [] });
    prompts.receive("s", { revision: 4, paused: false, items: [] });
    expect(prompts.queue("s")).toMatchObject({ revision: 5, paused: true });
  });
});

describe("migrating restored identities", () => {
  it("moves only restored drafts and checkpoints the canonical identity before notifying", () => {
    const saved: unknown[] = [];
    const { prompts, enqueue } = service({
      restored: [restored("old")],
      save: (queues) => saved.push(queues)
    });
    prompts.migrateRestoredSessions((id) => (id === "s" ? "canonical" : id));
    expect(prompts.pending("s")).toEqual([]);
    expect(prompts.pending("canonical")[0]?.input).toMatchObject({
      agentSessionId: "canonical",
      clientSubmitId: "old"
    });
    expect(saved).toHaveLength(1);
    prompts.migrateRestoredSessions((id) => id);
    expect(saved).toHaveLength(1);
    expect(enqueue).not.toHaveBeenCalled();
    prompts.dispose();
  });
});

it("restores a late migration draft once, checkpoints before notification, and never sends it automatically", () => {
  const saved: unknown[] = [];
  const { prompts, enqueue } = service({
    save: (queues) => saved.push(queues),
    recoveryWarning: () => "Review before retry"
  });
  prompts.restorePending([restored("legacy")]);
  prompts.restorePending([restored("legacy")]);
  expect(prompts.pending("s")).toHaveLength(1);
  expect(prompts.queueRecord("s")?.suspendReason).toBe("user_stop");
  expect(saved).toHaveLength(1);
  expect(prompts.error("s")?.message).toBe("Review before retry");
  expect(enqueue).not.toHaveBeenCalled();
  prompts.dispose();
});

it("notifies observable readers when an admission fails", async () => {
  const { prompts } = service({
    enqueue: vi.fn(async () => {
      throw new Error("offline");
    })
  });
  const errors: (string | undefined)[] = [];
  const stop = reaction(
    () => prompts.error("s")?.message,
    (message) => errors.push(message)
  );
  prompts.submit(
    { agentSessionId: "s", clientSubmitId: "failed" },
    { id: "failed", clientSubmitId: "failed" }
  );
  await settled();
  expect(errors).toEqual(["offline"]);
  stop();
  prompts.dispose();
});

describe("durable submission admission", () => {
  it("does not send or publish before storage commits, and preserves a draft on failure", async () => {
    let reject!: (error: Error) => void;
    const checkpoint = vi.fn(
      () =>
        new Promise<void>((_resolve, fail) => {
          reject = fail;
        }),
    );
    const { prompts, enqueue } = service({
      checkpoint,
      admit: (input, queued) => ({
        id: input.clientSubmitId,
        visibleInQueue: queued,
      }),
    });
    const result = prompts.submitPrompt({
      agentSessionId: "s",
      clientSubmitId: "disk-failed",
    });
    expect(result.accepted).toBe(true);
    expect(enqueue).not.toHaveBeenCalled();
    expect(prompts.pending("s")).toHaveLength(0);
    reject(new Error("disk full"));
    expect(await result.ready).toBe(false);
    expect(enqueue).not.toHaveBeenCalled();
    expect(prompts.pending("s")).toHaveLength(0);
  });
  it("starts the request after local commit without awaiting a slow network", async () => {
    let resolve!: () => void;
    const enqueue = vi.fn(() => new Promise<LocalPromptQueue>(() => {}));
    const { prompts } = service({
      enqueue,
      checkpoint: () =>
        new Promise<void>((done) => {
          resolve = done;
        }),
      admit: (input, queued) => ({
        id: input.clientSubmitId,
        visibleInQueue: queued,
      }),
    });
    const result = prompts.submitPrompt({
      agentSessionId: "s",
      clientSubmitId: "persisted",
    });
    resolve();
    expect(await result.ready).toBe(true);
    await settled();
    expect(enqueue).toHaveBeenCalledTimes(1);
  });
  it("merges partial authority without replacing a full queue or accepting older state", () => {
    const { prompts } = service();
    const queue: LocalPromptQueue = {
      ...empty(30),
      paused: true,
      items: [
        {
          requestId: "r",
          content: "hello",
          display: {
            id: "r",
            content: [{ type: "text", text: "hello" }],
            createdAtUnixMs: 1,
          },
        },
      ],
    };
    prompts.receive("s", queue);
    const value = {
      request_id: "r",
      revision: 3,
      state: "starting" as const,
      item: {},
      position: null,
      blocked_reason: "",
      message_id: "",
      attempt: {
        attempt_id: "attempt",
        request_id: "native-retry",
        turn_ids: [],
        delivery: "not_sent" as const,
        settings: {},
      },
    };
    prompts.receiveSubmissions("s", [value], "g");
    prompts.receiveSubmissions(
      "s",
      [{ ...value, revision: 2, state: "queued" }],
      "g",
    );
    expect(prompts.queue("s")).toEqual(queue);
    expect(prompts.submission("s", "r")?.state).toBe("starting");
    expect(prompts.requestIdentity("s", "native-retry")).toBe("r");
  });
});
