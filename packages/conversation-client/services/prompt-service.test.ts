// @vitest-environment node
import { reaction } from "mobx";
import { describe, expect, it, vi } from "vitest";
import type { LocalPromptQueue } from "@multica/core/local-conversation/protocol";
import { PromptService, type PromptServiceOptions, type PromptStatus } from "./prompt-service";

type Input = { agentSessionId: string; clientSubmitId: string };
type Display = { id: string; clientSubmitId?: string; visibleInQueue?: boolean };
const settled = () => new Promise((resolve) => setTimeout(resolve, 0));
const empty = (revision = 1): LocalPromptQueue => ({ revision, paused: false, items: [] });
const restored = (id: string) => ({ agentSessionId: "s", input: { agentSessionId: "s", clientSubmitId: id }, display: { id, clientSubmitId: id, visibleInQueue: true } });

function service(options: Partial<PromptServiceOptions<Input, Display>> = {}) {
  const update = vi.fn(async () => empty(2));
  const enqueue = vi.fn(async () => empty(2));
  return { update, enqueue, prompts: new PromptService<Input, Display>({
    enqueue, update, error: vi.fn(), workspaceId: "w", respondToInteraction: vi.fn(), editRetry: vi.fn(), recoverEditRetry: vi.fn(), reconcile: vi.fn(), reconcileWorkspace: vi.fn(),
    turns: { session: () => null, turn: () => null, deleted: () => false, runtimeBlocked: () => false, interactionStatus: () => null, stopTarget: () => null, turnForSubmit: () => null },
    ...options,
  }) };
}

describe("messages restored after a reload", () => {
  it("asks once what became of them, and drops only what no longer needs the user", async () => {
    const answers: Record<string, PromptStatus> = { delivered: "delivered", waiting: "queued", removed: "removed", refused: "failed", never: "none", maybe: "unknown" };
    const statuses = vi.fn(async () => answers);
    const { prompts } = service({ statuses, restored: Object.keys(answers).map(restored) });
    prompts.receive("s", empty());
    prompts.receive("s", empty(2));
    await settled();
    expect(statuses).toHaveBeenCalledTimes(1);
    // A refused or lost message can go again; one that may have arrived waits for a decision.
    expect(prompts.pending("s").map((item) => item.input.clientSubmitId)).toEqual(["refused", "never", "maybe"]);
  });

  it("keeps them all when the daemon cannot answer", async () => {
    const { prompts } = service({ statuses: vi.fn(async () => { throw new Error("offline"); }), restored: [restored("a")] });
    prompts.receive("s", empty());
    await settled();
    expect(prompts.pending("s")).toHaveLength(1);
  });

  it("never sends one on its own", async () => {
    const { prompts, enqueue } = service({ statuses: vi.fn(async () => ({ a: "failed" as const })), restored: [restored("a")] });
    prompts.receive("s", empty());
    await settled();
    expect(enqueue).not.toHaveBeenCalled();
  });
});

describe("presenting the queue", () => {
  it("tells observers once per change, after saving what has not been handed over", async () => {
    const order: string[] = [];
    const { prompts } = service({ save: () => order.push("save") });
    const stop = reaction(() => prompts.revision, () => order.push("notify"));
    prompts.submit({ agentSessionId: "s", clientSubmitId: "a" }, { id: "a", clientSubmitId: "a" });
    expect(order.slice(0, 2)).toEqual(["save", "notify"]);
    stop();
    await settled();
  });

  it("shows a message once while it is both pending and queued", () => {
    const { prompts } = service();
    prompts.submit({ agentSessionId: "s", clientSubmitId: "a" }, { id: "a", clientSubmitId: "a" });
    prompts.receive("s", { revision: 1, paused: false, items: [{ requestId: "a", display: { id: "a", clientSubmitId: "a" } }] as never });
    expect(prompts.prompts("s").map((item) => item.id)).toEqual(["a"]);
  });

  it("ignores an acknowledgement older than what is known", () => {
    const { prompts } = service();
    prompts.receive("s", { revision: 5, paused: true, items: [] });
    prompts.receive("s", { revision: 4, paused: false, items: [] });
    expect(prompts.queue("s")).toMatchObject({ revision: 5, paused: true });
  });
});
