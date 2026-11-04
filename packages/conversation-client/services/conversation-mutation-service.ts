import { action, makeObservable, observable } from "mobx";
import "../mobx-config";

const MUTATION_TIMEOUT_MS = 30_000;
const MAX_SETTLED_MUTATIONS = 128;
const FORK_DELIVERY_UNKNOWN = "agent_session_fork_delivery_unknown";
const NO_TURNS: readonly string[] = [];

export type ConversationMutationStatus = "failed" | "inFlight" | "succeeded" | "unknown";

export interface ConversationDeleteResult {
  cleanupFailedSessionIds: readonly string[];
  removedMessages: number;
  removedSessionIds: readonly string[];
  removedSessions: number;
}

export interface ConversationFork {
  /** A later choice to fork the same turn may retry it; only this record knows the ids to retry with. */
  requestId: string;
  sourceAgentSessionId: string;
  status: ConversationMutationStatus;
  targetAgentSessionId: string;
  turnId: string;
  errorCode: string | null;
  errorMessage: string | null;
}

/** What a conversation must say about itself for a fork to be its own. */
export interface MutatedConversation {
  agentSessionId: string;
  title?: string;
  forkedFrom?: { operationId: string; sourceAgentSessionId: string; sourceTurnId: string; targetTurnId: string } | null;
}

export interface ConversationMutationOptions<Session extends MutatedConversation> {
  /** What deciding reads of a conversation; null when it is not here, or was deleted. */
  session(id: string): { title: string; pinned: boolean; forkable: boolean } | null;
  deleted(id: string): boolean;
  turnForkable(sourceId: string, turnId: string): boolean;
  /** The conversation as it stands, to answer a change that had nothing to do. */
  current(id: string): Session | null;
  rename(input: { agentSessionId: string; title: string }, signal: AbortSignal): Promise<Session>;
  setPinned(input: { agentSessionId: string; pinned: boolean }, signal: AbortSignal): Promise<Session>;
  deleteSessions(input: { agentSessionIds: readonly string[] }, signal: AbortSignal): Promise<ConversationDeleteResult>;
  /** Forks and has the child ready to show: it answers with what was committed. */
  fork(input: { requestId: string; sourceAgentSessionId: string; targetAgentSessionId: string; turnId: string }, signal: AbortSignal): Promise<Session>;
  /** The server deleted this conversation. */
  removed(id: string): void;
}

interface Mutation {
  id: string;
  sessionIds: readonly string[];
  status: ConversationMutationStatus;
  /** Set for a fork, whose outcome stays readable for a retry. */
  fork?: ConversationFork;
}

class NotAccepted extends Error {
  constructor() { super("session mutation was not accepted"); }
}

/**
 * Renames, pins, deletes and forks. Each waits for the server before anything
 * changes here; a timeout or cancellation leaves the outcome unknown rather than
 * failed, and overlapping changes to one conversation are refused.
 */
export class ConversationMutationService<Session extends MutatedConversation> {
  private readonly mutations = new Map<string, Mutation>();
  private readonly forksUnderWay = new Map<string, Promise<ConversationFork>>();
  private readonly pendingForkTurns = observable.map<string, readonly string[]>([], { deep: false });
  private nextId = 1;
  private disposed = false;

  constructor(private readonly deps: ConversationMutationOptions<Session>) {
    makeObservable<ConversationMutationService<Session>, "syncPendingForks">(this, { syncPendingForks: action });
  }

  /** The turns of a conversation being forked right now. */
  pendingForkTurnIds(sourceId: string | null | undefined): readonly string[] {
    return this.pendingForkTurns.get(sourceId?.trim() ?? "") ?? NO_TURNS;
  }

  async rename(input: { agentSessionId: string; title: string; signal?: AbortSignal }): Promise<Session> {
    const id = input.agentSessionId.trim();
    const title = input.title.trim();
    const session = this.deps.session(id);
    if (!id || !title || !session || this.overlaps([id])) throw new NotAccepted();
    if (session.title === title) return this.currentOrMissing(id, "rename");
    return this.run([id], input.signal, MUTATION_TIMEOUT_MS,
      (signal) => this.deps.rename({ agentSessionId: id, title }, signal),
      (result) => result.agentSessionId?.trim() === id && result.title === title);
  }

  async setPinned(input: { agentSessionId: string; pinned: boolean; signal?: AbortSignal }): Promise<Session> {
    const id = input.agentSessionId.trim();
    const session = this.deps.session(id);
    if (!id || !session || this.overlaps([id])) throw new NotAccepted();
    if (session.pinned === input.pinned) return this.currentOrMissing(id, "pin");
    return this.run([id], input.signal, MUTATION_TIMEOUT_MS,
      (signal) => this.deps.setPinned({ agentSessionId: id, pinned: input.pinned }, signal),
      (result) => result.agentSessionId?.trim() === id);
  }

  /** Removes nothing here until the server has deleted them. */
  async deleteSessions(input: { agentSessionIds: readonly string[]; signal?: AbortSignal }): Promise<ConversationDeleteResult> {
    const ids = [...new Set(input.agentSessionIds.map((id) => id.trim()).filter(Boolean))];
    if (!ids.length || this.overlaps(ids)) throw new NotAccepted();
    const live = ids.filter((id) => !this.deps.deleted(id));
    if (!live.length) return { cleanupFailedSessionIds: [], removedMessages: 0, removedSessionIds: [], removedSessions: 0 };
    const result = await this.run(ids, input.signal, MUTATION_TIMEOUT_MS,
      (signal) => this.deps.deleteSessions({ agentSessionIds: live }, signal), validDeleteResult);
    for (const id of new Set([...ids, ...result.removedSessionIds.map((removed) => removed.trim())])) this.deps.removed(id);
    return result;
  }

  /**
   * Forks a conversation through a turn. A fork already under way or of unknown
   * outcome answers for the same turn with its own ids, so the provider can
   * recognise the retry; one that failed for certain lets a new attempt begin.
   */
  forkThroughTurn(input: { sourceAgentSessionId: string; turnId: string; timeoutMs?: number; signal?: AbortSignal }): Promise<ConversationFork> {
    const sourceAgentSessionId = input.sourceAgentSessionId.trim();
    const turnId = input.turnId.trim();
    const existing = this.latestUnresolvedFork(sourceAgentSessionId, turnId);
    if (existing) {
      const fork = existing.fork!;
      if (this.deps.session(fork.targetAgentSessionId)) {
        const landed: ConversationFork = { ...fork, status: "succeeded", errorCode: null, errorMessage: null };
        this.record({ ...existing, status: "succeeded", fork: landed });
        return Promise.resolve(landed);
      }
      const underWay = this.forksUnderWay.get(fork.requestId);
      if (underWay) return underWay;
      return this.startFork(fork.requestId, fork.targetAgentSessionId, sourceAgentSessionId, turnId, input);
    }
    const source = this.deps.session(sourceAgentSessionId);
    if (!sourceAgentSessionId || !turnId || !source?.forkable || !this.deps.turnForkable(sourceAgentSessionId, turnId)) return Promise.reject(new NotAccepted());
    return this.startFork(crypto.randomUUID(), crypto.randomUUID(), sourceAgentSessionId, turnId, input);
  }

  dispose(): void { this.disposed = true; }

  private startFork(requestId: string, targetAgentSessionId: string, sourceAgentSessionId: string, turnId: string, input: { timeoutMs?: number; signal?: AbortSignal }): Promise<ConversationFork> {
    const fork: ConversationFork = { requestId, sourceAgentSessionId, targetAgentSessionId, turnId, status: "inFlight", errorCode: null, errorMessage: null };
    const operation = this.run([sourceAgentSessionId], input.signal, input.timeoutMs,
      (signal) => this.deps.fork({ requestId, sourceAgentSessionId, targetAgentSessionId, turnId }, signal),
      (session) => validForkLineage(session, fork), { id: requestId, fork })
      .then(() => this.mutations.get(requestId)!.fork!)
      .finally(() => { this.forksUnderWay.delete(requestId); });
    this.forksUnderWay.set(requestId, operation);
    return operation;
  }

  /** The newest fork of this exact turn whose outcome is not settled; a finished one does not capture a later fork. */
  private latestUnresolvedFork(sourceId: string, turnId: string): Mutation | null {
    for (const mutation of [...this.mutations.values()].reverse()) {
      if (mutation.fork?.sourceAgentSessionId === sourceId && mutation.fork.turnId === turnId) {
        return mutation.status === "inFlight" || mutation.status === "unknown" ? mutation : null;
      }
    }
    return null;
  }

  private overlaps(ids: readonly string[]) {
    return [...this.mutations.values()].some((mutation) => mutation.status === "inFlight" && mutation.sessionIds.some((id) => ids.includes(id)));
  }

  private currentOrMissing(id: string, kind: string): Session {
    const session = this.deps.current(id);
    if (!session) throw new Error(`agent_session_${kind}_result_missing`);
    return session;
  }

  private run<Result>(
    sessionIds: readonly string[],
    callerSignal: AbortSignal | undefined,
    timeoutMs: number | undefined,
    execute: (signal: AbortSignal) => Promise<Result>,
    valid: (result: Result) => boolean,
    identity?: { id: string; fork: ConversationFork },
  ): Promise<Result> {
    if (callerSignal?.aborted) return Promise.reject(abortError(callerSignal));
    const id = identity?.id ?? `mutation:${this.nextId++}`;
    const mutation: Mutation = { id, sessionIds, status: "inFlight", ...(identity ? { fork: identity.fork } : {}) };
    this.record(mutation);
    return new Promise((resolve, reject) => {
      const controller = new AbortController();
      let settled = false;
      let timer: ReturnType<typeof setTimeout> | undefined;
      const finish = (status: ConversationMutationStatus, code: string | null, message: string | null) => {
        settled = true;
        clearTimeout(timer);
        callerSignal?.removeEventListener("abort", onAbort);
        this.record({ ...mutation, status, ...(mutation.fork ? { fork: { ...mutation.fork, status, errorCode: code, errorMessage: message } } : {}) });
      };
      const fail = (status: ConversationMutationStatus, code: string | null, message: string | null, reason: unknown = mutationError(status, code, message)) => {
        finish(status, code, message);
        reject(reason);
      };
      const onAbort = () => {
        if (settled) return;
        const error = abortError(callerSignal);
        controller.abort(error);
        fail("unknown", "aborted", null, error);
      };
      callerSignal?.addEventListener("abort", onAbort, { once: true });
      if (timeoutMs !== undefined) {
        timer = setTimeout(() => {
          if (settled) return;
          controller.abort(new Error("session mutation timed out"));
          fail("unknown", "timeout", null);
        }, timeoutMs);
      }
      execute(controller.signal).then(
        (result) => {
          if (settled || this.disposed) return;
          if (!valid(result)) { fail("unknown", "invalid_command_result", null); return; }
          finish("succeeded", null, null);
          resolve(result);
        },
        (error: unknown) => {
          if (settled || this.disposed) return;
          const code = errorIdentity(error);
          const message = errorMessage(error);
          const unknown = code === "aborted" || (mutation.fork && code === FORK_DELIVERY_UNKNOWN);
          fail(unknown ? "unknown" : "failed", code, message);
        },
      );
    });
  }

  private record(mutation: Mutation) {
    this.mutations.delete(mutation.id);
    this.mutations.set(mutation.id, mutation);
    this.pruneSettled();
    if (mutation.fork) this.syncPendingForks(mutation.fork.sourceAgentSessionId);
  }

  /** Unknown forks stay: a later retry needs their ids. */
  private pruneSettled() {
    const settled = [...this.mutations.values()].filter((mutation) => mutation.status !== "inFlight" && !(mutation.fork && mutation.status === "unknown"));
    for (const mutation of settled.slice(0, Math.max(0, settled.length - MAX_SETTLED_MUTATIONS))) this.mutations.delete(mutation.id);
  }

  private syncPendingForks(sourceId: string) {
    const turns = [...this.mutations.values()].flatMap((mutation) => mutation.status === "inFlight" && mutation.fork?.sourceAgentSessionId === sourceId ? [mutation.fork.turnId] : []);
    if (turns.length) this.pendingForkTurns.set(sourceId, turns);
    else this.pendingForkTurns.delete(sourceId);
  }
}

function validForkLineage(session: MutatedConversation, fork: ConversationFork): boolean {
  const lineage = session.forkedFrom;
  return session.agentSessionId?.trim() === fork.targetAgentSessionId
    && !!lineage?.operationId?.trim()
    && lineage.sourceAgentSessionId?.trim() === fork.sourceAgentSessionId
    && lineage.sourceTurnId?.trim() === fork.turnId
    && !!lineage.targetTurnId?.trim();
}

function validDeleteResult(result: ConversationDeleteResult): boolean {
  return typeof result?.removedMessages === "number" && typeof result.removedSessions === "number"
    && Array.isArray(result.cleanupFailedSessionIds) && result.cleanupFailedSessionIds.every((id) => typeof id === "string")
    && Array.isArray(result.removedSessionIds) && result.removedSessionIds.every((id) => typeof id === "string");
}

function mutationError(status: ConversationMutationStatus, code: string | null, message: string | null) {
  const error = new Error(message ?? `session mutation ${status}`) as Error & { code?: string };
  if (code) error.code = code;
  return error;
}

function abortError(signal?: AbortSignal) {
  const reason: unknown = signal?.reason;
  const error = new Error(reason instanceof Error ? reason.message : typeof reason === "string" && reason.trim() ? reason : "session mutation aborted") as Error & { code: string };
  error.code = "aborted";
  error.name = "AbortError";
  return error;
}

function errorIdentity(error: unknown): string | null {
  const record = error && typeof error === "object" ? error as { reason?: unknown; code?: unknown; name?: unknown } : null;
  if (typeof record?.reason === "string" && record.reason.trim()) return record.reason.trim();
  if (typeof record?.code === "string" && record.code.trim()) return record.code.trim();
  return record?.name === "AbortError" ? "aborted" : null;
}

function errorMessage(error: unknown): string | null {
  const message = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  return message.trim() || null;
}
