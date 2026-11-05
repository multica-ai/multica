// @vitest-environment node
import { autorun } from "mobx";
import { describe,expect,it,vi } from "vitest";
import {
ConversationMutationService,
type ConversationDeleteResult,
type ConversationMutationOptions,
type MutatedConversation
} from "./conversation-mutation-service";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function harness({
  sessions: knownSessions,
  gone,
  ...fields
}: Partial<ConversationMutationOptions<MutatedConversation>> & {
  sessions?: Record<
    string,
    { title: string; pinned: boolean; forkable: boolean }
  >;
  gone?: string[];
} = {}) {
  const sessions = knownSessions ?? {
    s: { title: "Old", pinned: false, forkable: true }
  };
  const removed: string[] = [];
  const deps: ConversationMutationOptions<MutatedConversation> = {
    session: (id) => sessions[id] ?? null,
    deleted: (id) => gone?.includes(id) ?? false,
    turnForkable: () => true,
    current: (id) =>
      sessions[id] ? { agentSessionId: id, title: sessions[id].title } : null,
    rename: vi.fn(async (input) => ({
      agentSessionId: input.agentSessionId,
      title: input.title
    })),
    setPinned: vi.fn(async (input) => ({
      agentSessionId: input.agentSessionId
    })),
    deleteSessions: vi.fn(
      async (input): Promise<ConversationDeleteResult> => ({
        cleanupFailedSessionIds: [],
        removedMessages: 1,
        removedSessionIds: [...input.agentSessionIds],
        removedSessions: input.agentSessionIds.length
      })
    ),
    fork: vi.fn(async (input) => committedFork(input)),
    removed: (id) => {
      removed.push(id);
    },
    ...fields
  };
  return {
    service: new ConversationMutationService(deps),
    deps,
    removed,
    sessions
  };
}

function committedFork(input: {
  sourceAgentSessionId: string;
  targetAgentSessionId: string;
  turnId: string;
}): MutatedConversation {
  return {
    agentSessionId: input.targetAgentSessionId,
    forkedFrom: {
      operationId: "op",
      sourceAgentSessionId: input.sourceAgentSessionId,
      sourceTurnId: input.turnId,
      targetTurnId: "child-turn"
    }
  };
}

describe("rename", () => {
  it("answers with what the server committed, title trimmed", async () => {
    const { service, deps } = harness();
    await expect(
      service.rename({ agentSessionId: " s ", title: " New " })
    ).resolves.toMatchObject({ agentSessionId: "s", title: "New" });
    expect(deps.rename).toHaveBeenCalledWith(
      { agentSessionId: "s", title: "New" },
      expect.any(AbortSignal)
    );
  });

  it("refuses an empty title or a conversation it does not have, before reaching the server", async () => {
    const { service, deps } = harness();
    await expect(
      service.rename({ agentSessionId: "s", title: "   " })
    ).rejects.toThrow("not accepted");
    await expect(
      service.rename({ agentSessionId: "other", title: "New" })
    ).rejects.toThrow("not accepted");
    expect(deps.rename).not.toHaveBeenCalled();
  });

  it("does nothing for the title it already has", async () => {
    const { service, deps } = harness();
    await expect(
      service.rename({ agentSessionId: "s", title: "Old" })
    ).resolves.toMatchObject({ title: "Old" });
    expect(deps.rename).not.toHaveBeenCalled();
  });

  it("treats a different committed title as an unknown outcome", async () => {
    const { service } = harness({
      rename: async () => ({ agentSessionId: "s", title: "Other" })
    });
    await expect(
      service.rename({ agentSessionId: "s", title: "New" })
    ).rejects.toMatchObject({
      code: "invalid_command_result",
      message: "session mutation unknown"
    });
  });

  it("times out after 30 seconds as unknown, and aborts the request", async () => {
    vi.useFakeTimers();
    try {
      let signal!: AbortSignal;
      const { service } = harness({
        rename: (_input, value) => {
          signal = value;
          return new Promise(() => {});
        }
      });
      const renaming = service.rename({ agentSessionId: "s", title: "New" });
      const outcome = expect(renaming).rejects.toMatchObject({
        code: "timeout"
      });
      await vi.advanceTimersByTimeAsync(30_000);
      await outcome;
      expect(signal.aborted).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("aborts the request when the caller cancels, and refuses an already cancelled one", async () => {
    let signal!: AbortSignal;
    const { service, deps } = harness({
      rename: vi.fn((_input, value: AbortSignal) => {
        signal = value;
        return new Promise<MutatedConversation>(() => {});
      })
    });
    const controller = new AbortController();
    const renaming = service.rename({
      agentSessionId: "s",
      title: "New",
      signal: controller.signal
    });
    await Promise.resolve();
    controller.abort("stop");
    await expect(renaming).rejects.toMatchObject({
      code: "aborted",
      name: "AbortError",
      message: "stop"
    });
    expect(signal.aborted).toBe(true);
    await expect(
      service.rename({
        agentSessionId: "s",
        title: "Again",
        signal: controller.signal
      })
    ).rejects.toMatchObject({ code: "aborted" });
    expect(deps.rename).toHaveBeenCalledTimes(1);
  });

  it("carries a failure's code and message to the caller", async () => {
    const { service } = harness({
      rename: async () => {
        throw Object.assign(new Error("nope"), { code: "denied" });
      }
    });
    await expect(
      service.rename({ agentSessionId: "s", title: "New" })
    ).rejects.toMatchObject({ code: "denied", message: "nope" });
  });

  it("refuses a change to a conversation already being changed, and allows it afterwards", async () => {
    const answer = deferred<MutatedConversation>();
    const { service } = harness({ rename: () => answer.promise });
    const renaming = service.rename({ agentSessionId: "s", title: "New" });
    await Promise.resolve();
    await expect(
      service.setPinned({ agentSessionId: "s", pinned: true })
    ).rejects.toThrow("not accepted");
    await expect(
      service.deleteSessions({ agentSessionIds: ["s"] })
    ).rejects.toThrow("not accepted");
    answer.resolve({ agentSessionId: "s", title: "New" });
    await renaming;
    await expect(
      service.setPinned({ agentSessionId: "s", pinned: true })
    ).resolves.toBeTruthy();
  });
});

describe("pin", () => {
  it("answers with what the server committed", async () => {
    const { service, deps } = harness();
    await expect(
      service.setPinned({ agentSessionId: "s", pinned: true })
    ).resolves.toMatchObject({ agentSessionId: "s" });
    expect(deps.setPinned).toHaveBeenCalledWith(
      { agentSessionId: "s", pinned: true },
      expect.any(AbortSignal)
    );
  });

  it("does nothing when already in that state", async () => {
    const { service, deps } = harness();
    await service.setPinned({ agentSessionId: "s", pinned: false });
    expect(deps.setPinned).not.toHaveBeenCalled();
  });

  it("treats an answer about another conversation as an unknown outcome", async () => {
    const { service } = harness({
      setPinned: async () => ({ agentSessionId: "other" })
    });
    await expect(
      service.setPinned({ agentSessionId: "s", pinned: true })
    ).rejects.toMatchObject({ code: "invalid_command_result" });
  });
});

describe("delete", () => {
  it("removes nothing until the server answers, then the requested and cascaded conversations", async () => {
    const answer = deferred<ConversationDeleteResult>();
    const { service, removed } = harness({
      deleteSessions: () => answer.promise
    });
    const deleting = service.deleteSessions({
      agentSessionIds: [" s ", "s", "child-of-nothing"]
    });
    await Promise.resolve();
    expect(removed).toEqual([]);
    answer.resolve({
      cleanupFailedSessionIds: [],
      removedMessages: 3,
      removedSessionIds: ["cascaded"],
      removedSessions: 2
    });
    await expect(deleting).resolves.toMatchObject({ removedSessions: 2 });
    expect(removed).toEqual(["s", "child-of-nothing", "cascaded"]);
  });

  it("removes nothing when the server refuses", async () => {
    const { service, removed } = harness({
      deleteSessions: async () => {
        throw Object.assign(new Error("no"), { code: "unsupported" });
      }
    });
    await expect(
      service.deleteSessions({ agentSessionIds: ["s"] })
    ).rejects.toMatchObject({ code: "unsupported" });
    expect(removed).toEqual([]);
  });

  it("accepts what is already gone without asking the server", async () => {
    const { service, deps } = harness({ gone: ["gone"] });
    await expect(
      service.deleteSessions({ agentSessionIds: ["gone"] })
    ).resolves.toEqual({
      cleanupFailedSessionIds: [],
      removedMessages: 0,
      removedSessionIds: [],
      removedSessions: 0
    });
    expect(deps.deleteSessions).not.toHaveBeenCalled();
  });

  it("asks the server only about what is live", async () => {
    const { service, deps } = harness({ gone: ["gone"] });
    await service.deleteSessions({ agentSessionIds: ["gone", "s"] });
    expect(deps.deleteSessions).toHaveBeenCalledWith(
      { agentSessionIds: ["s"] },
      expect.any(AbortSignal)
    );
  });

  it("treats a malformed answer as an unknown outcome and removes nothing", async () => {
    const { service, removed } = harness({
      deleteSessions: async () =>
        ({ removedMessages: 1 }) as unknown as ConversationDeleteResult
    });
    await expect(
      service.deleteSessions({ agentSessionIds: ["s"] })
    ).rejects.toMatchObject({ code: "invalid_command_result" });
    expect(removed).toEqual([]);
  });

  it("refuses an empty list", async () => {
    const { service } = harness();
    await expect(
      service.deleteSessions({ agentSessionIds: [" "] })
    ).rejects.toThrow("not accepted");
  });
});

describe("fork through a turn", () => {
  const fork = { sourceAgentSessionId: "s", turnId: "t" };

  it("answers with the child, and shows the turn as pending meanwhile", async () => {
    const answer = deferred<MutatedConversation>();
    const { service, deps } = harness({ fork: vi.fn(() => answer.promise) });
    const seen: (readonly string[])[] = [];
    const stop = autorun(() => {
      seen.push(service.pendingForkTurnIds("s"));
    });
    const forking = service.forkThroughTurn({
      sourceAgentSessionId: " s ",
      turnId: " t "
    });
    const input = vi.mocked(deps.fork).mock.calls[0]![0];
    expect(input).toMatchObject(fork);
    expect(input.requestId).not.toBe(input.targetAgentSessionId);
    answer.resolve(committedFork(input));
    await expect(forking).resolves.toMatchObject({
      status: "succeeded",
      requestId: input.requestId,
      targetAgentSessionId: input.targetAgentSessionId
    });
    stop();
    expect(seen).toEqual([[], ["t"], []]);
    expect(service.pendingForkTurnIds("s")).toBe(
      service.pendingForkTurnIds("other")
    );
  });

  it("refuses a conversation that cannot fork, or a turn that cannot be its boundary", async () => {
    const closed = harness({
      sessions: { s: { title: "t", pinned: false, forkable: false } }
    });
    await expect(closed.service.forkThroughTurn(fork)).rejects.toThrow(
      "not accepted"
    );
    expect(closed.deps.fork).not.toHaveBeenCalled();
    const unbound = harness({ turnForkable: () => false });
    await expect(unbound.service.forkThroughTurn(fork)).rejects.toThrow(
      "not accepted"
    );
    expect(unbound.deps.fork).not.toHaveBeenCalled();
  });

  it("asks the host, not a running turn or pending question, whether a turn can fork", async () => {
    const asked: [string, string][] = [];
    const { service } = harness({
      turnForkable: (source, turn) => {
        asked.push([source, turn]);
        return true;
      }
    });
    await expect(service.forkThroughTurn(fork)).resolves.toMatchObject({
      status: "succeeded"
    });
    expect(asked).toEqual([["s", "t"]]);
  });

  it("adopts a server-assigned fork identity after checking its exact source and turn", async () => {
    const { service } = harness({
      fork: async () => ({
        agentSessionId: "server-assigned",
        forkedFrom: {
          operationId: "receipt",
          sourceAgentSessionId: "s",
          sourceTurnId: "t",
          targetTurnId: "copied"
        }
      })
    });
    await expect(service.forkThroughTurn(fork)).resolves.toMatchObject({
      status: "succeeded",
      targetAgentSessionId: "server-assigned"
    });
  });

  it("treats a child without its exact lineage as an unknown outcome", async () => {
    const lineages = [
      (input: { targetAgentSessionId: string }) => ({
        agentSessionId: input.targetAgentSessionId
      }),
      (input: {
        targetAgentSessionId: string;
        sourceAgentSessionId: string;
        turnId: string;
      }) => ({
        agentSessionId: input.targetAgentSessionId,
        forkedFrom: {
          operationId: "op",
          sourceAgentSessionId: input.sourceAgentSessionId,
          sourceTurnId: input.turnId,
          targetTurnId: " "
        }
      }),
      (input: {
        targetAgentSessionId: string;
        sourceAgentSessionId: string;
      }) => ({
        agentSessionId: input.targetAgentSessionId,
        forkedFrom: {
          operationId: "op",
          sourceAgentSessionId: input.sourceAgentSessionId,
          sourceTurnId: "other",
          targetTurnId: "c"
        }
      }),
      () => ({
        agentSessionId: "s",
        forkedFrom: {
          operationId: "op",
          sourceAgentSessionId: "s",
          sourceTurnId: "t",
          targetTurnId: "c"
        }
      })
    ];
    for (const lineage of lineages) {
      const { service } = harness({ fork: async (input) => lineage(input) });
      await expect(service.forkThroughTurn(fork)).rejects.toMatchObject({
        code: "invalid_command_result"
      });
    }
  });

  it("starts anew after a certain failure", async () => {
    const { service, deps } = harness({
      fork: vi.fn(async () => {
        throw Object.assign(new Error("full"), {
          code: "agent_session_fork_failed"
        });
      })
    });
    await expect(service.forkThroughTurn(fork)).rejects.toMatchObject({
      code: "agent_session_fork_failed",
      message: "full"
    });
    await expect(service.forkThroughTurn(fork)).rejects.toThrow("full");
    const [first, second] = vi
      .mocked(deps.fork)
      .mock.calls.map((call) => call[0]);
    expect(second!.requestId).not.toBe(first!.requestId);
    expect(second!.targetAgentSessionId).not.toBe(first!.targetAgentSessionId);
  });

  it("retries with the same ids after a typed delivery-unknown failure", async () => {
    const calls: { requestId: string; targetAgentSessionId: string }[] = [];
    const { service } = harness({
      fork: async (input) => {
        calls.push(input);
        if (calls.length === 1)
          throw Object.assign(new Error("lost"), {
            code: "agent_session_fork_delivery_unknown"
          });
        return committedFork(input);
      }
    });
    await expect(service.forkThroughTurn(fork)).rejects.toMatchObject({
      code: "agent_session_fork_delivery_unknown"
    });
    await expect(service.forkThroughTurn(fork)).resolves.toMatchObject({
      status: "succeeded"
    });
    expect(calls[1]).toEqual(calls[0]);
  });

  it("replays the same ids after a timeout, and settles when the child is already here", async () => {
    vi.useFakeTimers();
    try {
      const calls: {
        requestId: string;
        targetAgentSessionId: string;
        sourceAgentSessionId: string;
        turnId: string;
      }[] = [];
      const { service } = harness({
        fork: (input) => {
          calls.push(input);
          return calls.length === 1
            ? new Promise(() => {})
            : Promise.resolve(committedFork(input));
        }
      });
      const first = expect(
        service.forkThroughTurn({ ...fork, timeoutMs: 1_000 })
      ).rejects.toMatchObject({ code: "timeout" });
      await vi.advanceTimersByTimeAsync(1_000);
      await first;
      await service.forkThroughTurn(fork);
      expect(calls[1]).toEqual(calls[0]);

      const late = harness({
        fork: vi.fn(() => new Promise<MutatedConversation>(() => {}))
      });
      const lost = expect(
        late.service.forkThroughTurn({ ...fork, timeoutMs: 1_000 })
      ).rejects.toMatchObject({ code: "timeout" });
      await vi.advanceTimersByTimeAsync(1_000);
      await lost;
      const target = vi.mocked(late.deps.fork).mock.calls[0]![0]
        .targetAgentSessionId;
      late.sessions[target] = { title: "Child", pinned: false, forkable: true };
      await expect(late.service.forkThroughTurn(fork)).resolves.toMatchObject({
        targetAgentSessionId: target
      });
      expect(late.deps.fork).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("answers a repeat while one is under way from the same attempt", async () => {
    const answer = deferred<MutatedConversation>();
    const { service, deps } = harness({ fork: vi.fn(() => answer.promise) });
    const first = service.forkThroughTurn(fork);
    const second = service.forkThroughTurn(fork);
    expect(deps.fork).toHaveBeenCalledTimes(1);
    answer.resolve(committedFork(vi.mocked(deps.fork).mock.calls[0]![0]));
    await expect(second).resolves.toEqual(await first);
  });

  it("lets a finished fork of a turn be followed by a new one", async () => {
    const { service, deps } = harness();
    const first = await service.forkThroughTurn(fork);
    const second = await service.forkThroughTurn(fork);
    expect(second.requestId).not.toBe(first.requestId);
    expect(deps.fork).toHaveBeenCalledTimes(2);
  });

  it("allows forks of different turns at once, and is not blocked by another change", async () => {
    const answers = [
      deferred<MutatedConversation>(),
      deferred<MutatedConversation>()
    ];
    let call = 0;
    const { service, deps } = harness({
      fork: vi.fn(() => answers[call++]!.promise)
    });
    const pin = service.setPinned({ agentSessionId: "s", pinned: true });
    const first = service.forkThroughTurn({
      sourceAgentSessionId: "s",
      turnId: "t1"
    });
    const second = service.forkThroughTurn({
      sourceAgentSessionId: "s",
      turnId: "t2"
    });
    expect(service.pendingForkTurnIds("s")).toEqual(["t1", "t2"]);
    await pin;
    answers.forEach((answer, index) =>
      answer.resolve(committedFork(vi.mocked(deps.fork).mock.calls[index]![0]))
    );
    await Promise.all([first, second]);
    expect(service.pendingForkTurnIds("s")).toEqual([]);
  });
});

describe("history", () => {
  it("keeps settled mutations bounded while unknown forks stay retryable", async () => {
    vi.useFakeTimers();
    try {
      const sessions: Record<
        string,
        { title: string; pinned: boolean; forkable: boolean }
      > = { s: { title: "t", pinned: false, forkable: true } };
      for (let index = 0; index < 200; index++)
        sessions[`p${index}`] = { title: "t", pinned: false, forkable: false };
      const calls: { requestId: string }[] = [];
      const { service } = harness({
        sessions,
        fork: (input) => {
          calls.push(input);
          return calls.length === 1
            ? new Promise(() => {})
            : Promise.resolve(committedFork(input));
        }
      });
      const unknown = expect(
        service.forkThroughTurn({
          sourceAgentSessionId: "s",
          turnId: "t",
          timeoutMs: 1
        })
      ).rejects.toMatchObject({ code: "timeout" });
      await vi.advanceTimersByTimeAsync(1);
      await unknown;
      for (let index = 0; index < 200; index++)
        await service.setPinned({ agentSessionId: `p${index}`, pinned: true });
      const kept = (service as unknown as { mutations: Map<string, unknown> })
        .mutations.size;
      expect(kept).toBeLessThanOrEqual(129);
      await service.forkThroughTurn({ sourceAgentSessionId: "s", turnId: "t" });
      expect(calls[1]!.requestId).toBe(calls[0]!.requestId);
    } finally {
      vi.useRealTimers();
    }
  });
});
