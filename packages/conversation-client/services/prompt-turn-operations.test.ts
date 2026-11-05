// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LocalPromptQueue } from "@multica/core/local-conversation/protocol";
import { PromptService, type PromptServiceOptions } from "./prompt-service";
import type { EditRetryAvailability } from "./edit-retries";
import type { PromptTurnReader } from "./turn-cancels";

type Input = { agentSessionId: string; clientSubmitId: string };
type Display = { id: string; clientSubmitId?: string; visibleInQueue?: boolean };
type Turn = { turnId: string; phase: string };

const flush = async () => { for (let index = 0; index < 8; index++) await Promise.resolve(); };
const queue = (revision = 2, paused = true): LocalPromptQueue => ({ revision, paused, items: [] });

/** The conversation data the service reads, changed by hand as the daemon's events would. */
function world() {
  const state = {
    session: { workspaceId: "w", activeTurnId: null } as { workspaceId: string; activeTurnId: string | null } | null,
    turns: new Map<string, Turn>(),
    deleted: false,
    blocked: false,
    interaction: "pending" as string | null,
    stopTarget: null as string | null,
    submits: new Map<string, string>(),
  };
  const turns: PromptTurnReader = {
    session: () => state.session,
    turn: (_id, turnId) => state.turns.get(turnId) ?? null,
    deleted: () => state.deleted,
    runtimeBlocked: () => state.blocked,
    interactionStatus: () => state.interaction,
    stopTarget: () => state.stopTarget,
    turnForSubmit: (_id, submit) => state.submits.get(submit) ?? null,
  };
  return { state, turns };
}

function service(overrides: Partial<PromptServiceOptions<Input, Display>> = {}) {
  const { state, turns } = world();
  const options = {
    update: vi.fn(async (): Promise<LocalPromptQueue> => queue()),
    enqueue: vi.fn(async (): Promise<LocalPromptQueue> => queue(2, false)),
    error: vi.fn(),
    respondToInteraction: vi.fn(async (): Promise<unknown> => ({ session: { agentSessionId: "s", workspaceId: "w" } })),
    editRetry: vi.fn(async (): Promise<unknown> => ({ historyRevision: 9, operationId: "op", retractedTurnId: "t", state: "completed" })),
    recoverEditRetry: vi.fn(async (): Promise<unknown> => ({ historyRevision: 7, operationId: "op", retractedTurnId: "t", state: "resend_pending" })),
    reconcile: vi.fn(),
    reconcileWorkspace: vi.fn(),
    ...overrides,
  };
  const prompts = new PromptService<Input, Display>({ workspaceId: "w", turns, ...options });
  const running = (turnId = "t1") => {
    state.turns.set(turnId, { turnId, phase: "running" });
    state.session = { workspaceId: "w", activeTurnId: turnId };
  };
  const settled = (turnId = "t1") => {
    state.turns.set(turnId, { turnId, phase: "settled" });
    if (state.session?.activeTurnId === turnId) state.session = { workspaceId: "w", activeTurnId: null };
  };
  return { prompts, state, options, running, settled };
}

beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(100); });
afterEach(() => { vi.useRealTimers(); });

describe("stopping a turn", () => {
  it("stops the active turn once, however many times it is asked", async () => {
    const { prompts, options, running } = service();
    running();
    prompts.stop(" s ");
    prompts.stop("s");
    expect(prompts.cancelStatus("s")).toBe("requested");
    await flush();
    expect(vi.mocked(options.update).mock.calls).toEqual([
      ["s", "stop", undefined, "t1"],
    ]);
    expect(prompts.cancelRecord("s")?.turnId).toBe("t1");
  });

  it("keeps an accepted stop until the turn settles", async () => {
    const { prompts, running, settled } = service();
    running();
    prompts.stop("s");
    await flush();
    expect(prompts.cancelStatus("s")).toBe("accepted");
    settled();
    prompts.lifecycleChanged();
    expect(prompts.cancelStatus("s")).toBeNull();
  });

  it("is done at once when the turn settled before the daemon answered", async () => {
    const { prompts, running, settled, options } = service();
    running();
    prompts.stop("s");
    settled();
    await flush();
    expect(prompts.cancelStatus("s")).toBeNull();
    expect(options.reconcileWorkspace).not.toHaveBeenCalled();
  });

  it("asks for the workspace again when this client lost the turn it stopped", async () => {
    const { prompts, state, running, options } = service();
    running();
    prompts.stop("s");
    state.turns.clear();
    await flush();
    expect(prompts.cancelStatus("s")).toBeNull();
    expect(options.reconcileWorkspace).toHaveBeenCalledOnce();
  });

  it("waits for a first turn, then stops it", async () => {
    const { prompts, options, running } = service();
    prompts.stop("s");
    await flush();
    expect(prompts.cancelStatus("s")).toBe("awaitingTurn");
    expect(options.update).toHaveBeenCalledTimes(1);
    prompts.stop("s");
    expect(options.update).toHaveBeenCalledTimes(1);

    running();
    prompts.lifecycleChanged();
    expect(prompts.cancelStatus("s")).toBe("requested");
    await flush();
    expect(vi.mocked(options.update).mock.calls).toEqual([["s", "stop", undefined], ["s", "stop", undefined]]);
    expect(prompts.cancelStatus("s")).toBe("accepted");
  });

  it("sends the second stop after the first, which may predate the turn", async () => {
    const answers: ((value: LocalPromptQueue) => void)[] = [];
    const { prompts, options, running } = service({ update: vi.fn(() => new Promise<LocalPromptQueue>((resolve) => { answers.push(resolve); })) });
    prompts.stop("s");
    running();
    prompts.lifecycleChanged();
    expect(options.update).toHaveBeenCalledTimes(1);
    answers[0]!(queue());
    await flush();
    expect(options.update).toHaveBeenCalledTimes(2);
    answers[1]!(queue(3));
    await flush();
    expect(prompts.cancelStatus("s")).toBe("accepted");
  });

  it("survives the session being absent from a snapshot while it waits", () => {
    const { prompts, state, running } = service();
    state.session = null;
    prompts.stop("s");
    prompts.lifecycleChanged();
    expect(prompts.cancelStatus("s")).toBe("awaitingTurn");
    running();
    prompts.lifecycleChanged();
    expect(prompts.cancelStatus("s")).toBe("requested");
  });

  it("stays waiting through changes that bring no turn", () => {
    const { prompts } = service();
    prompts.stop("s");
    prompts.lifecycleChanged();
    expect(prompts.cancelStatus("s")).toBe("awaitingTurn");
  });

  it("expires after 30 seconds and cannot stop a later turn", async () => {
    const { prompts, options, running } = service();
    prompts.stop("s");
    await flush();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(prompts.cancelStatus("s")).toBeNull();
    running();
    prompts.lifecycleChanged();
    await flush();
    expect(options.update).toHaveBeenCalledOnce();
  });

  it("targets the newest message on its way, not the next unrelated turn", async () => {
    const { prompts, state, options, running } = service();
    state.stopTarget = "submit-1";
    prompts.stop("s");
    await flush();
    expect(prompts.cancelRecord("s")?.targetClientSubmitId).toBe("submit-1");

    running("unrelated");
    prompts.lifecycleChanged();
    await flush();
    expect(prompts.cancelStatus("s")).toBe("awaitingTurn");
    expect(options.update).toHaveBeenCalledOnce();

    state.submits.set("submit-1", "t2");
    running("t2");
    prompts.lifecycleChanged();
    await flush();
    expect(prompts.cancelRecord("s")).toMatchObject({ status: "accepted", turnId: "t2" });
    expect(options.update).toHaveBeenCalledTimes(2);
  });

  it("waits for a target's turn that history names before the turn itself loads", async () => {
    const { prompts, state, running } = service();
    prompts.stop("s", "submit-1");
    state.submits.set("submit-1", "t2");
    prompts.lifecycleChanged();
    expect(prompts.cancelRecord("s")).toMatchObject({ status: "awaitingTurn", turnId: "t2" });
    running("t2");
    prompts.lifecycleChanged();
    expect(prompts.cancelStatus("s")).toBe("requested");
  });

  it("does not target a message with an active turn, unless told to", () => {
    const { prompts, state, running } = service();
    running();
    state.stopTarget = "submit-1";
    prompts.stop("s");
    expect(prompts.cancelRecord("s")).toMatchObject({ status: "requested", targetClientSubmitId: null });

    const explicit = service();
    explicit.running();
    explicit.prompts.stop("s", "submit-explicit");
    expect(explicit.prompts.cancelRecord("s")).toMatchObject({ status: "awaitingTurn", targetClientSubmitId: "submit-explicit" });
  });

  it("gives up when its message was refused before it could start a turn", async () => {
    const refused = Object.assign(new Error("cleanup"), { code: "agent.process_cleanup_pending" });
    const { prompts, state } = service({ enqueue: vi.fn(async () => { throw refused; }) });
    state.stopTarget = "submit-1";
    prompts.submit({ agentSessionId: "s", clientSubmitId: "submit-1" }, { id: "submit-1" });
    prompts.stop("s");
    expect(prompts.cancelStatus("s")).toBe("awaitingTurn");
    await flush();
    expect(prompts.cancelStatus("s")).toBeNull();
  });

  it("keeps waiting when its message's fate is unknown", async () => {
    const { prompts, state } = service({ enqueue: vi.fn(async () => { throw new Error("connection lost"); }) });
    state.stopTarget = "submit-1";
    prompts.submit({ agentSessionId: "s", clientSubmitId: "submit-1" }, { id: "submit-1" });
    prompts.stop("s");
    await flush();
    expect(prompts.cancelStatus("s")).toBe("awaitingTurn");
  });

  it("records a failed stop and clears it when the turn settles", async () => {
    const { prompts, running, settled } = service({ update: vi.fn(async () => { throw Object.assign(new Error("offline"), { code: "offline" }); }) });
    running();
    prompts.stop("s");
    await flush();
    expect(prompts.cancelRecord("s")).toMatchObject({ status: "failed", errorCode: "offline", errorMessage: "offline" });
    prompts.stop("s");
    settled();
    prompts.lifecycleChanged();
    expect(prompts.cancelStatus("s")).toBeNull();
  });

  it("fails after 30 seconds without an answer, and can be asked again", async () => {
    const { prompts, options, running } = service({ update: vi.fn(() => new Promise<LocalPromptQueue>(() => {})) });
    running();
    prompts.stop("s");
    await vi.advanceTimersByTimeAsync(30_000);
    expect(prompts.cancelRecord("s")).toMatchObject({ status: "failed", errorMessage: null });
    prompts.stop("s");
    expect(options.update).toHaveBeenCalledOnce();
    expect(prompts.cancelStatus("s")).toBe("requested");
  });

  it("a late answer for turn A does not touch the stop of turn B", async () => {
    let answer!: (value: LocalPromptQueue) => void;
    const { prompts, options, running, settled } = service({ update: vi.fn(() => new Promise<LocalPromptQueue>((resolve) => { answer = resolve; })) });
    running("a");
    prompts.stop("s");
    settled("a");
    prompts.lifecycleChanged();
    expect(prompts.cancelStatus("s")).toBeNull();
    running("b");
    answer(queue());
    await flush();
    expect(prompts.cancelStatus("s")).toBeNull();
    expect(options.update).toHaveBeenCalledOnce();
  });

  it("still reaches the daemon for a session this client would not track", async () => {
    for (const change of [{ blocked: true }, { deleted: true }, { session: { workspaceId: "other", activeTurnId: null } }]) {
      const { prompts, state, options } = service();
      Object.assign(state, change);
      prompts.stop("s");
      expect(prompts.cancelStatus("s")).toBeNull();
      await flush();
      expect(options.update).toHaveBeenCalledOnce();
    }
  });

  it("forgets a stop when the session is removed", () => {
    const { prompts, state } = service();
    prompts.stop("s");
    state.deleted = true;
    prompts.lifecycleChanged();
    expect(prompts.cancelStatus("s")).toBeNull();
  });
});

describe("operations on turn data still being committed", () => {
  it("stops the turn the pending commit brings, instead of waiting for one", () => {
    const context = service({ beforeTurnOperation: () => context.running() });
    context.prompts.stop("s");
    expect(context.prompts.cancelRecord("s")).toMatchObject({ status: "requested", turnId: "t1" });
  });

  it("lets a pending commit release a stop that was still waiting", () => {
    const beforeTurnOperation = vi.fn();
    const context = service({ beforeTurnOperation });
    context.prompts.stop("s");
    context.running();
    beforeTurnOperation.mockImplementation(() => context.prompts.lifecycleChanged());
    context.prompts.stop("s");
    expect(context.prompts.cancelRecord("s")).toMatchObject({ status: "requested", turnId: "t1" });
  });

  it("refuses an answer to an interaction the pending commit closes", () => {
    const context = service({ beforeTurnOperation: () => { context.state.interaction = "answered"; } });
    context.state.session = { workspaceId: "w", activeTurnId: null };
    expect(context.prompts.respondToInteraction({ agentSessionId: "s", requestId: "r", turnId: "t1" })).toBe(false);
  });

  it("commits before an edit retry and a recovery decide", async () => {
    const beforeTurnOperation = vi.fn();
    const { prompts } = service({ beforeTurnOperation });
    await prompts.requestEditRetry({ agentSessionId: "s", editedText: "edited", turnId: "t1" }).catch(() => {});
    await prompts.recoverEditRetry({ action: "reconcile", agentSessionId: "s" }).catch(() => {});
    expect(beforeTurnOperation).toHaveBeenCalledTimes(2);
  });

  it("does not commit for a daemon-driven second stop", async () => {
    const beforeTurnOperation = vi.fn();
    const { prompts, running } = service({ beforeTurnOperation });
    prompts.stop("s");
    running();
    prompts.lifecycleChanged();
    await flush();
    expect(beforeTurnOperation).toHaveBeenCalledOnce();
  });
});

const answer = { agentSessionId: "s", optionId: "allow", requestId: "r", turnId: "t1" };

describe("answering an interaction", () => {
  it("normalizes its identity and reports what was sent", async () => {
    const { prompts, options } = service();
    expect(prompts.respondToInteraction({ action: " approve ", agentSessionId: " s ", optionId: " allow ", payload: { text: "why" }, requestId: " r ", turnId: " t1 " })).toBe(true);
    expect(prompts.interactionResponse("s", "t1", "r")?.status).toBe("responding");
    expect(prompts.isRespondingToInteraction("s")).toBe(true);
    expect(vi.mocked(options.respondToInteraction).mock.calls[0]?.[0]).toEqual({ action: "approve", agentSessionId: "s", optionId: "allow", payload: { text: "why" }, requestId: "r", turnId: "t1", workspaceId: "w" });
    expect(prompts.interactionResponse("s", "t1", "r")?.commandId).toBe("interaction:100:1");
  });

  it("sends one answer at a time", () => {
    const { prompts, options } = service({ respondToInteraction: vi.fn(() => new Promise<unknown>(() => {})) });
    expect(prompts.respondToInteraction(answer)).toBe(true);
    expect(prompts.respondToInteraction(answer)).toBe(false);
    expect(options.respondToInteraction).toHaveBeenCalledOnce();
  });

  it("stays unconfirmed after the acknowledgement, until the interaction is answered", async () => {
    const { prompts, state } = service();
    prompts.respondToInteraction(answer);
    await flush();
    expect(prompts.interactionResponse("s", "t1", "r")).toMatchObject({ status: "unknown", errorCode: null });
    expect(prompts.isRespondingToInteraction("s")).toBe(true);
    expect(prompts.respondToInteraction(answer)).toBe(false);
    state.interaction = "answered";
    prompts.lifecycleChanged();
    expect(prompts.interactionResponse("s", "t1", "r")).toBeNull();
    expect(prompts.isRespondingToInteraction("s")).toBe(false);
  });

  it("ignores the acknowledgement of an answer the data already confirmed", async () => {
    const { prompts, state } = service();
    prompts.respondToInteraction(answer);
    state.interaction = "answered";
    prompts.lifecycleChanged();
    await flush();
    expect(prompts.interactionResponse("s", "t1", "r")).toBeNull();
  });

  it("treats a removed interaction as confirming too", async () => {
    const { prompts, state } = service();
    prompts.respondToInteraction(answer);
    await flush();
    state.interaction = null;
    prompts.lifecycleChanged();
    expect(prompts.interactionResponse("s", "t1", "r")).toBeNull();
  });

  it("does not call a timeout or a result for another scope a success", async () => {
    const late = service({ respondToInteraction: vi.fn(() => new Promise<unknown>(() => {})) });
    late.prompts.respondToInteraction(answer);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(late.prompts.interactionResponse("s", "t1", "r")).toMatchObject({ status: "unknown", errorCode: "timeout" });
    expect(late.prompts.respondToInteraction(answer)).toBe(false);
    expect(vi.mocked(late.options.respondToInteraction).mock.calls[0]?.[1].signal.aborted).toBe(true);

    const other = service({ respondToInteraction: vi.fn(async () => ({ session: { agentSessionId: "other", workspaceId: "w" } })) });
    other.prompts.respondToInteraction(answer);
    await flush();
    expect(other.prompts.interactionResponse("s", "t1", "r")).toMatchObject({ status: "unknown", errorCode: "invalid_command_result" });
  });

  it("retries a failed answer only when it is the same answer", async () => {
    let fail: ((error: Error) => void) | undefined;
    const { prompts, options } = service({ respondToInteraction: vi.fn(() => new Promise<unknown>((_resolve, reject) => { fail = reject; })) });
    expect(prompts.respondToInteraction(answer)).toBe(true);
    fail!(new Error("temporary failure"));
    await flush();
    expect(prompts.interactionResponse("s", "t1", "r")?.status).toBe("failed");
    expect(prompts.interactionError("s")).toBe("temporary failure");
    expect(prompts.isRespondingToInteraction("s")).toBe(false);

    vi.setSystemTime(200);
    expect(prompts.respondToInteraction({ agentSessionId: "s", requestId: "r", turnId: "t1" })).toBe(false);
    expect(prompts.respondToInteraction({ ...answer, optionId: "deny" })).toBe(false);
    expect(prompts.respondToInteraction(answer)).toBe(true);
    expect(options.respondToInteraction).toHaveBeenCalledTimes(2);
    expect(prompts.interactionResponse("s", "t1", "r")?.commandId).toBe("interaction:200:4");
  });

  it("asks for the session again when the answer was for a stale request", async () => {
    const stale = Object.assign(new Error("stale"), { reason: "agent_interactive_request_stale" });
    const { prompts, options } = service({ respondToInteraction: vi.fn(async () => { throw stale; }) });
    prompts.respondToInteraction(answer);
    await flush();
    expect(prompts.interactionResponse("s", "t1", "r")?.status).toBe("failed");
    expect(options.reconcile).toHaveBeenCalledWith({ agentSessionId: "s", needsMessages: true, needsState: true });
  });

  it("refuses what is not this workspace's pending interaction, or a blocked runtime", () => {
    for (const change of [{ interaction: null }, { interaction: "answered" }, { blocked: true }, { session: null }, { session: { workspaceId: "other", activeTurnId: null } }]) {
      const { prompts, state, options } = service();
      Object.assign(state, change);
      expect(prompts.respondToInteraction(answer)).toBe(false);
      expect(options.respondToInteraction).not.toHaveBeenCalled();
    }
  });

  it("refuses an empty identity", () => {
    const { prompts } = service();
    expect(prompts.respondToInteraction({ ...answer, requestId: " " })).toBe(false);
  });

  it("tells subscribers when a response changes", async () => {
    const { prompts } = service({ respondToInteraction: vi.fn(async () => { throw new Error("no"); }) });
    const listener = vi.fn();
    const stop = prompts.subscribeInteractionResponses(listener);
    prompts.respondToInteraction(answer);
    await flush();
    expect(listener).toHaveBeenCalledTimes(2);
    stop();
  });
});

const available: EditRetryAvailability = { availableActions: [], eligible: true, historyRevision: 7, recoveryState: "prepared", supported: true, turnId: "t1" };
const edit = { agentSessionId: "s", editedText: "edited", turnId: "t1" };

describe("editing and retrying a message", () => {
  it("keeps one operation id for the same edit, and a new one for another", async () => {
    const { prompts, options } = service({ editRetry: vi.fn(async () => { throw new Error("offline"); }) });
    prompts.receiveEditRetryAvailability("s", available);
    await expect(prompts.requestEditRetry(edit)).rejects.toThrow("offline");
    expect(prompts.editRetryOperation("s").status).toBe("failed");
    expect(options.reconcile).toHaveBeenCalledWith({ agentSessionId: "s", needsMessages: true, needsState: true });
    await expect(prompts.requestEditRetry(edit)).rejects.toThrow();
    await expect(prompts.requestEditRetry({ ...edit, editedText: "different" })).rejects.toThrow();
    const [first, retried, changed] = vi.mocked(options.editRetry).mock.calls.map((call) => call[0].clientOperationId);
    expect(retried).toBe(first);
    expect(changed).not.toBe(first);
    expect(vi.mocked(options.editRetry).mock.calls[0]?.[0]).toMatchObject({ expectedHistoryRevision: 7, turnId: "t1", workspaceId: "w" });
  });

  it("waits for availability to confirm a success, and asks for it", async () => {
    const { prompts, options } = service();
    prompts.receiveEditRetryAvailability("s", available);
    await expect(prompts.requestEditRetry(edit)).resolves.toMatchObject({ status: "reconciling" });
    expect(prompts.editRetryAvailability("s")?.historyRevision).toBe(7);
    expect(options.reconcile).toHaveBeenCalledWith({ agentSessionId: "s", authoritativeMessages: true, needsMessages: false, needsState: false, requiredHistoryRevision: 9 });

    prompts.receiveEditRetryAvailability("s", { ...available, historyRevision: 8 });
    expect(prompts.editRetryOperation("s").status).toBe("reconciling");
    prompts.receiveEditRetryAvailability("s", { ...available, eligible: false, historyRevision: 9 });
    expect(prompts.editRetryOperation("s").status).toBe("succeeded");
  });

  it("settles a recovery on availability even when nothing in it changed", async () => {
    const recovering: EditRetryAvailability = { ...available, availableActions: ["reconcile"], eligible: false, operationId: "op", recoveryState: "resend_pending", turnId: undefined };
    const { prompts, options } = service();
    prompts.receiveEditRetryAvailability("s", recovering);
    await prompts.recoverEditRetry({ action: "reconcile", agentSessionId: "s" });
    expect(vi.mocked(options.recoverEditRetry).mock.calls[0]?.[0]).toEqual({ action: "reconcile", agentSessionId: "s", operationId: "op", workspaceId: "w" });
    prompts.receiveEditRetryAvailability("s", recovering);
    expect(prompts.editRetryOperation("s").status).toBe("succeeded");
  });

  it("does not start without eligibility for this turn, an action on offer, or while another runs", async () => {
    const { prompts, options } = service({ editRetry: vi.fn(() => new Promise<unknown>(() => {})) });
    await expect(prompts.requestEditRetry(edit)).rejects.toThrow("agent_edit_retry_command_not_started");
    prompts.receiveEditRetryAvailability("s", available);
    await expect(prompts.requestEditRetry({ ...edit, turnId: "other" })).rejects.toThrow("agent_edit_retry_command_not_started");
    await expect(prompts.recoverEditRetry({ action: "reconcile", agentSessionId: "s" })).rejects.toThrow("agent_edit_retry_command_not_started");
    void prompts.requestEditRetry(edit);
    void prompts.requestEditRetry(edit);
    expect(options.editRetry).toHaveBeenCalledOnce();
  });

  it("treats a malformed result as a failure", async () => {
    const { prompts } = service({ editRetry: vi.fn(async () => ({ state: "completed" })) });
    prompts.receiveEditRetryAvailability("s", available);
    await expect(prompts.requestEditRetry(edit)).rejects.toThrow("agent_edit_retry_invalid_result");
  });

  it("forgets a removed session", () => {
    const { prompts, state } = service();
    prompts.receiveEditRetryAvailability("s", available);
    state.deleted = true;
    prompts.lifecycleChanged();
    expect(prompts.editRetryAvailability("s")).toBeNull();
  });
});
