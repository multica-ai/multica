import {
  mergeSubmission,
  type LocalSubmission,
} from "@multica/core/local-conversation/submission";
import type { LocalPromptQueue } from "@multica/core/local-conversation/protocol";
import {
action,
comparer,
computed,
makeObservable,
observable,
reaction,
type IComputedValue
} from "mobx";
import "../mobx-config";
import {
EditRetries,
type EditRetriesOptions,
type EditRetryAvailability,
type EditRetryOperation,
type EditRetryRecoveryAction,
type PromptSessionReconcile
} from "./edit-retries";
import {
InteractionResponses,
type InteractionResponseInput,
type InteractionResponseRecord,
type InteractionResponsesOptions
} from "./interaction-responses";
import { PromptCommands } from "./prompt-command";
import {
TurnCancels,
type PromptTurnReader,
type StopResult,
type TurnCancelRecord,
type TurnCancelStatus
} from "./turn-cancels";

export type PromptQueueAction =
  | "pause"
  | "stop"
  | "resume"
  | "remove"
  | "sendNow";

export interface PromptInput {
  agentSessionId: string;
  clientSubmitId: string;
}
export interface PromptDisplay {
  id: string;
  clientSubmitId?: string;
  visibleInQueue?: boolean;
  createdAtUnixMs?: number;
}

interface PendingPrompt<Input, Display> {
  input: Input;
  display: Display;
  admission?: Promise<LocalPromptQueue>;
  confirmed?: boolean;
  restored?: boolean;
}

export interface PromptError {
  message: string;
  promptId?: string;
  origin: "admission" | "operation";
}
export interface SavedPrompts<Display> {
  agentSessionId: string;
  prompts: Display[];
}
/** What the daemon says became of a message; see the queue status route. */
export type PromptStatus =
  | "queued"
  | "dispatching"
  | "delivered"
  | "failed"
  | "unknown"
  | "removed"
  | "none";

export interface PromptServiceOptions<
  Input extends PromptInput,
  Display extends PromptDisplay
> {
  enqueue(input: Input): Promise<LocalPromptQueue>;
  checkpoint?(input: Input, display: Display): Promise<void>;
  update(
    id: string,
    action: PromptQueueAction,
    requestId?: string,
    targetTurnId?: string,
  ): Promise<LocalPromptQueue>;
  error(error: unknown): void;
  /** Messages a previous document had not handed to the daemon. */
  restored?: { agentSessionId: string; input: Input; display: Display }[];
  recoveryWarning?(): string;
  /** Keeps what has not been handed over; runs before anyone is told of a change. */
  save?(
    queues: {
      agentSessionId: string;
      items: { input: Input; display: Display }[];
    }[]
  ): void;
  /** Reports each message's fate; absent on a daemon that cannot. */
  statuses?(
    id: string,
    requestIds: string[]
  ): Promise<Record<string, PromptStatus>>;
  workspaceId: string;
  turns: PromptTurnReader;
  respondToInteraction: InteractionResponsesOptions["respond"];
  editRetry: EditRetriesOptions["editRetry"];
  recoverEditRetry: EditRetriesOptions["recoverEditRetry"];
  /** Asks for the session's state again. */
  reconcile(input: PromptSessionReconcile): void;
  reconcileWorkspace(): void;
  /** Brings what `turns` reads up to date; runs before each operation decides against it. */
  beforeTurnOperation?(): void;
  /** The presentation boundary validates and constructs the admitted draft. */
  admit?(input: Input, queued: boolean): Display | null;
  canSubmit?(id: string): boolean;
}

/**
 * Sending and the queue, as far as this client knows: what is on its way to the
 * daemon, what the daemon has queued, and what failed. The daemon owns admission,
 * persistence and draining, and keeps running with no window open; this only keeps
 * the user's view immediate and honest in between.
 */
export class PromptService<
  Input extends PromptInput,
  Display extends PromptDisplay
> {
  private readonly authorities = new Map<
    string,
    Map<string, LocalSubmission>
  >();
  private readonly generations = new Map<string, string>();
  private readonly queues = new Map<string, LocalPromptQueue>();
  private readonly pendingBySession = new Map<
    string,
    PendingPrompt<Input, Display>[]
  >();
  private readonly awaitingTranscript = new Map<string, Map<string, Display>>();
  private readonly errors = new Map<string, PromptError>();
  private readonly operations = new Map<string, Promise<StopResult>>();
  private readonly steering = new Map<
    string,
    {
      sessionId: string;
      requestId?: string;
      promptId: string;
      accepted: boolean;
    }
  >();
  private readonly reconciled = new Set<string>();
  private readonly commands = new PromptCommands();
  private readonly cancels: TurnCancels;
  private readonly interactions: InteractionResponses;
  private readonly editRetries: EditRetries;
  private disposed = false;
  private readonly saving = new Map<string, Set<string>>();
  private readonly presented = new Map<string, IComputedValue<unknown>>();

  /** Changes with what is sent and queued, for whoever presents it. */
  revision = 0;

  constructor(private readonly options: PromptServiceOptions<Input, Display>) {
    for (const item of options.restored ?? []) {
      const list = this.pendingBySession.get(item.agentSessionId) ?? [];
      list.push({ input: item.input, display: item.display, restored: true });
      this.pendingBySession.set(item.agentSessionId, list);
    }
    makeObservable(this, { revision: observable, bump: action });
    const { workspaceId, turns, reconcile } = options;
    const { commands } = this;
    this.cancels = new TurnCancels({
      workspaceId,
      turns,
      commands,
      stop: (id, requestId, turnId) => this.sendStop(id, requestId, turnId),
      reconcileWorkspace: options.reconcileWorkspace,
    });
    this.interactions = new InteractionResponses({
      workspaceId,
      turns,
      commands,
      respond: options.respondToInteraction,
      reconcile
    });
    this.editRetries = new EditRetries({
      workspaceId,
      commands,
      editRetry: options.editRetry,
      recoverEditRetry: options.recoverEditRetry,
      reconcile
    });
  }

  bump(): void {
    this.revision++;
  }

  /** Displays recovered drafts without admitting or replaying them. */
  restorePending(
    items: NonNullable<PromptServiceOptions<Input, Display>["restored"]>
  ): void {
    if (this.disposed) return;
    let changed = false;
    for (const item of items) {
      if (this.hasPrompt(item.agentSessionId, item.input.clientSubmitId))
        continue;
      this.pendingBySession.set(item.agentSessionId, [
        ...this.pending(item.agentSessionId),
        { input: item.input, display: item.display, restored: true }
      ]);
      changed = true;
    }
    if (changed) this.publish();
  }

  /** One-time conversion of restored pre-service queue identities; never touches an active admission. */
  migrateRestoredSessions(resolve: (id: string) => string): void {
    let changed = false;
    for (const [oldId, items] of [...this.pendingBySession]) {
      const id = resolve(oldId);
      if (
        id === oldId ||
        items.some((item) => !item.restored || item.admission)
      )
        continue;
      const existing = this.pendingBySession.get(id) ?? [];
      const moved = items
        .filter(
          (item) =>
            !existing.some(
              (current) =>
                current.input.clientSubmitId === item.input.clientSubmitId
            )
        )
        .map((item) => ({
          ...item,
          input: { ...item.input, agentSessionId: id }
        }));
      this.pendingBySession.set(id, [...existing, ...moved]);
      this.pendingBySession.delete(oldId);
      changed = true;
    }
    if (changed) this.publish();
  }

  sessionIds(): string[] {
    return [
      ...new Set([
        ...this.queues.keys(),
        ...this.pendingBySession.keys(),
        ...this.awaitingTranscript.keys(),
        ...this.errors.keys()
      ])
    ];
  }
  queue(id: string): LocalPromptQueue | undefined {
    return this.queues.get(id);
  }
  pending(id: string): readonly PendingPrompt<Input, Display>[] {
    return this.pendingBySession.get(id) ?? [];
  }
  error(id: string): PromptError | undefined {
    void this.revision;
    const error = this.errors.get(id);
    if (error) return error;
    const recovered = this.pending(id).find((item) => item.restored);
    const message = recovered ? this.options.recoveryWarning?.() : undefined;
    return message
      ? { message, promptId: recovered!.display.id, origin: "admission" }
      : undefined;
  }
  /** The message being sent ahead of the queue, and the operation doing it. */
  sending(id: string): { key: string; promptId: string } | undefined {
    for (const [key, value] of this.steering)
      if (value.sessionId === id) return { key, promptId: value.promptId };
    return undefined;
  }
  hasPrompt(id: string, requestId: string | undefined): boolean {
    return (
      this.pending(id).some(
        (item) => item.input.clientSubmitId === requestId
      ) ||
      !!(requestId && this.awaitingTranscript.get(id)?.has(requestId)) ||
      !!this.queues.get(id)?.items.some((item) => item.requestId === requestId)
    );
  }

  /** Everything queued for the session, each message once: one the daemon has is not also pending. */
  prompts(id: string): Display[] {
    const queue = this.queues.get(id);
    const activeTurnId = this.options.turns.session(id)?.activeTurnId;
    const activeTurn = activeTurnId ? this.options.turns.turn(id, activeTurnId) : null;
    // A daemon can report an attempted dispatch before rejecting it as busy.
    const dispatching =
      queue?.paused || queue?.error || (activeTurnId && activeTurn?.phase !== "settled")
        ? undefined : queue?.dispatchingRequestId;
    const items = (queue?.items
      .filter((item) => !this.options.turns.turnForSubmit(id, item.requestId))
      .map((item) =>
      item.requestId === dispatching ||
      (!queue.paused && !queue.error && !this.options.turns.runtimeBlocked(id) &&
        this.awaitingTranscript.get(id)?.has(item.requestId) &&
        (!activeTurnId || this.options.turns.turnForSubmit(id, item.requestId) === activeTurnId))
        ? { ...item.display, visibleInQueue: false }
        : item.display
    ) ?? []) as unknown as Display[];
    return [
      ...items,
      ...[
        ...this.pending(id).map((item) => item.display),
        ...(this.awaitingTranscript.get(id)?.values() ?? [])
      ]
        .filter(
          (item) =>
            !items.some(
              (existing) => existing.clientSubmitId === item.clientSubmitId
            )
        )
        .filter(
          (item, index, all) =>
            all.findIndex((other) => other.id === item.id) === index,
        ),
    ]
      .filter(
        (prompt) =>
          this.authorities.get(id)?.get(prompt.clientSubmitId || prompt.id)
            ?.state !== "removed",
      )
      .map((prompt) => {
        const authority = this.authorities
          .get(id)
          ?.get(prompt.clientSubmitId || prompt.id);
        if (!authority) return prompt;
        return {
          ...prompt,
          visibleInQueue:
            authority.state === "queued" ||
            authority.state === "recovery_required",
        };
      });
  }

  submission(id: string, requestId: string): LocalSubmission | undefined {
    void this.revision;
    return this.authorities.get(id)?.get(requestId);
  }
  requestIdentity(id: string, requestId: string): string {
    void this.revision;
    for (const value of this.authorities.get(id)?.values() ?? [])
      if (value.attempt?.request_id === requestId) return value.request_id;
    return requestId;
  }

  private read<T>(area: string, id: string, derive: () => T): T {
    const key = JSON.stringify([area, id]);
    let value = this.presented.get(key) as IComputedValue<T> | undefined;
    if (!value) {
      value = computed(
        () => {
          void this.revision;
          return derive();
        },
        { equals: comparer.structural, keepAlive: true }
      );
      this.presented.set(key, value);
    }
    return value.get();
  }

  queueRecord(rawId: string | null | undefined) {
    const id = rawId?.trim() ?? "";
    return this.read("queue", id, () => {
      if (!this.sessionIds().includes(id)) return null;
      const prompts = this.prompts(id),
        queue = this.queue(id),
        error = this.error(id),
        sending = this.sending(id);
      const failureMessage = error?.message ?? queue?.error ?? null;
      return {
        agentSessionId: id,
        workspaceId: this.options.workspaceId,
        prompts,
        deliveryBarrierTurnId: null,
        failedPromptId: failureMessage
          ? (error?.promptId ?? prompts[0]?.id ?? null)
          : null,
        failureMessage,
        inFlight: sending
          ? {
              commandId: sending.key,
              kind: "send" as const,
              promptId: sending.promptId
            }
          : null,
        sendNextPromptId: sending?.promptId ?? null,
        suspendReason:
          queue?.paused ||
          error ||
          this.pending(id).some((item) => item.restored)
            ? ("user_stop" as const)
            : null,
        uncertainDelivery: null
      };
    });
  }

  visibleQueuedPrompts(rawId: string | null | undefined): readonly Display[] {
    const id = rawId?.trim() ?? "";
    return this.read("visible", id, () => {
      const queue = this.queueRecord(id);
      return (queue?.prompts ?? []).filter(
        (prompt) =>
          prompt.visibleInQueue !== false &&
          prompt.id !== queue?.inFlight?.promptId &&
          prompt.id !== queue?.sendNextPromptId
      );
    });
  }

  queuedPrompt(
    id: string | null | undefined,
    promptId: string | null | undefined
  ): Display | null {
    return (
      this.queueRecord(id)?.prompts.find(
        (prompt) => prompt.id === promptId?.trim()
      ) ?? null
    );
  }
  queueError(id: string | null | undefined): string | null {
    return this.queueRecord(id)?.failureMessage ?? null;
  }
  pendingSubmits(rawId: string | null | undefined) {
    const id = rawId?.trim() ?? "";
    return this.read("submits", id, () =>
      this.prompts(id)
        .filter((prompt) => prompt.visibleInQueue === false)
        .map((prompt) => ({
          ...prompt,
          agentSessionId: id,
          clientSubmitId: prompt.clientSubmitId || prompt.id,
          workspaceId: this.options.workspaceId,
          status: (this.awaitingTranscript.get(id)?.has(prompt.clientSubmitId || prompt.id) &&
            !this.queue(id)?.items.some((item) => item.requestId === (prompt.clientSubmitId || prompt.id))
            ? "accepted" : "requested") as
            | "requested"
            | "uncertain"
            | "accepted"
            | "confirmed"
            | "failed",
          requestedAtUnixMs: prompt.createdAtUnixMs ?? 0,
          expiresAtUnixMs: Number.MAX_SAFE_INTEGER,
          acceptedSessionVersion: null,
          errorCode: null,
          errorMessage: null,
          errorReason: null,
          turnId: null as string | null
        }))
    );
  }
  latestPendingSubmit(id: string | null | undefined) {
    return this.pendingSubmits(id).reduce<
      ReturnType<typeof this.pendingSubmits>[number] | null
    >(
      (latest, pending) =>
        !latest || pending.requestedAtUnixMs >= latest.requestedAtUnixMs
          ? pending
          : latest,
      null
    );
  }
  isSubmitting(id: string | null | undefined): boolean {
    return this.pendingSubmits(id).some(
      (pending) =>
        pending.status === "requested" || pending.status === "uncertain"
    );
  }
  hasUnconfirmedSubmit(id: string | null | undefined): boolean {
    const session = id ? this.options.turns.session(id) : null;
    return this.pendingSubmits(id).some(
      (pending) =>
        pending.status === "accepted" &&
        (!pending.turnId ||
          (this.options.turns.turn(pending.agentSessionId, pending.turnId)
            ?.phase !== "settled" &&
            session?.activeTurnId === pending.turnId))
    );
  }
  latestStopTarget(id: string | null | undefined) {
    return this.pendingSubmits(id)
      .filter(
        (pending) =>
          pending.status !== "failed" &&
          (!pending.turnId ||
            this.options.turns.turn(pending.agentSessionId, pending.turnId)
              ?.phase !== "settled")
      )
      .reduce<ReturnType<typeof this.pendingSubmits>[number] | null>(
        (latest, pending) =>
          !latest ||
          pending.requestedAtUnixMs > latest.requestedAtUnixMs ||
          (pending.requestedAtUnixMs === latest.requestedAtUnixMs &&
            pending.clientSubmitId.localeCompare(latest.clientSubmitId) > 0)
            ? pending
            : latest,
        null
      );
  }
  hasStopTarget(id: string | null | undefined): boolean {
    return this.latestStopTarget(id) !== null;
  }
  hasVisibleQueuedSubmit(
    id: string | null | undefined,
    clientSubmitId: string | null | undefined
  ): boolean {
    return (
      Boolean(clientSubmitId?.trim()) &&
      (this.queueRecord(id)?.prompts ?? []).some(
        (prompt) =>
          prompt.clientSubmitId === clientSubmitId?.trim() &&
          prompt.visibleInQueue !== false
      )
    );
  }
  wouldBeVisibleInQueue(id: string | null | undefined): boolean {
    if (!id) return false;
    const queue = this.queueRecord(id),
      session = this.options.turns.session(id);
    const turn = session?.activeTurnId
      ? this.options.turns.turn(id, session.activeTurnId)
      : null;
    return Boolean(
      this.saving.get(id)?.size ||
        queue?.prompts.length ||
        queue?.inFlight ||
        (this.options.canSubmit
          ? !this.options.canSubmit(id)
          : !session ||
            this.options.turns.runtimeBlocked(id) ||
            (turn && turn.phase !== "settled"))
    );
  }
  submitPrompt(input: Input): {
    accepted: boolean;
    queued: boolean;
    ready?: Promise<boolean>;
  } {
    this.options.beforeTurnOperation?.();
    const queued = this.wouldBeVisibleInQueue(input.agentSessionId);
    const saved = structuredClone(input);
    const display = this.options.admit?.(saved, queued);
    if (!display) return { accepted: false, queued: false };
    if (this.options.checkpoint) {
      const id = input.agentSessionId;
      const saving = this.saving.get(id) ?? new Set<string>();
      this.saving.set(id, saving);
      saving.add(input.clientSubmitId);
      const ready = this.options.checkpoint(saved, display).then(
        () => {
          saving.delete(input.clientSubmitId);
          if (this.disposed) return false;
          this.submit(saved, display);
          return true;
        },
        (error: unknown) => {
          saving.delete(input.clientSubmitId);
          this.options.error(error);
          return false;
        },
      );
      return { accepted: true, queued, ready };
    }
    this.submit(saved, display);
    return { accepted: true, queued };
  }

  private publish() {
    if (this.disposed) return;
    // Checkpoint before anyone may clear the composer: even a message on its way
    // must survive this document being destroyed.
    this.options.save?.(
      [...this.pendingBySession].flatMap(([agentSessionId, items]) =>
        items.length
          ? [
              {
                agentSessionId,
                items: items.map((item) => ({
                  input: item.input,
                  display: item.display
                }))
              }
            ]
          : []
      )
    );
    this.bump();
  }

  private clearError(
    id: string,
    promptId: string | undefined,
    origin: PromptError["origin"]
  ) {
    const error = this.errors.get(id);
    if (error && error.promptId === promptId && error.origin === origin)
      this.errors.delete(id);
  }

  private fail(
    id: string,
    error: unknown,
    promptId?: string,
    origin: PromptError["origin"] = "operation"
  ) {
    this.errors.set(id, {
      message: error instanceof Error ? error.message : String(error),
      promptId,
      origin
    });
    this.publish();
    this.options.error(error);
  }

  /** Status queries are partial: they never replace the queue or advance its revision. */
  receiveSubmissions(
    id: string,
    submissions: readonly LocalSubmission[],
    generation?: string,
    publish = true,
  ): void {
    if (this.disposed) return;
    if (generation) {
      const previous = this.generations.get(id);
      if (previous && previous !== generation) {
        this.authorities.delete(id);
        this.queues.delete(id);
      }
      this.generations.set(id, generation);
    }
    if (submissions) {
      const values =
        this.authorities.get(id) ?? new Map<string, LocalSubmission>();
      this.authorities.set(id, values);
      for (const value of submissions)
        values.set(
          value.request_id,
          mergeSubmission(values.get(value.request_id), value),
        );
      this.pendingBySession.set(
        id,
        this.pending(id).filter(
          (item) =>
            !item.restored ||
            item.admission ||
            !["attached", "removed"].includes(
              values.get(item.input.clientSubmitId)?.state ?? "",
            ),
        ),
      );
    }
    if (publish) this.publish();
  }

  /** The queue as the daemon reports it. An acknowledgement can arrive after a newer snapshot, so revisions only rise. */
  receive(id: string, incoming?: LocalPromptQueue): void {
    if (this.disposed || !incoming) return;
    this.receiveSubmissions(
      id,
      incoming.submissions ?? [],
      incoming.storeGeneration,
      false,
    );
    const known = this.queues.get(id);
    const queue =
      known && incoming.revision < known.revision ? known : incoming;
    this.queues.set(id, queue);
    for (const [key, request] of this.steering)
      if (
        request.sessionId === id &&
        ((!queue.items.some((item) => item.requestId === request.requestId) &&
          !this.pending(id).some(
            (item) =>
              item.input.clientSubmitId === request.requestId && !item.confirmed
          )) ||
          queue.dispatchingRequestId === request.requestId ||
          (request.accepted && (queue.paused || queue.error)))
      )
        this.steering.delete(key);
    this.pendingBySession.set(
      id,
      this.pending(id).filter((item) => {
        if (
          !queue.items.some(
            (prompt) => prompt.requestId === item.input.clientSubmitId
          )
        )
          return true;
        item.confirmed = true;
        this.retainUntilTranscript(id, item);
        this.clearError(id, item.display.id, "admission");
        return false;
      })
    );
    this.publish();
    this.reconcileRestored(id);
  }

  /**
   * Restored messages may have gone out before the reload, possibly in a turn
   * not loaded here, so ask. One that may have been delivered stays, for the user
   * to judge; sending it again is not this client's call.
   */
  private reconcileRestored(id: string) {
    if (!this.options.statuses || this.reconciled.has(id)) return;
    const restored = this.pending(id).filter(
      (item) => item.restored && !item.admission
    );
    if (!restored.length) return;
    this.reconciled.add(id);
    void this.options
      .statuses(
        id,
        restored.map((item) => item.input.clientSubmitId)
      )
      .then((statuses) => {
        if (this.disposed) return;
        const settled = new Set<PromptStatus>([
          "queued",
          "dispatching",
          "delivered",
          "removed"
        ]);
        const before = this.pending(id).length;
        this.pendingBySession.set(
          id,
          this.pending(id).filter(
            (item) =>
              !item.restored ||
              !settled.has(statuses[item.input.clientSubmitId] ?? "none")
          )
        );
        if (this.pending(id).length !== before) this.publish();
      })
      .catch(() => {
        /* A daemon that cannot answer leaves them for the user, as before. */
      });
  }

  /** Takes a message on its way: shown at once, with its failure and retry here. */
  submit(input: Input, display: Display): void {
    const item: PendingPrompt<Input, Display> = { input, display };
    this.pendingBySession.set(input.agentSessionId, [
      ...this.pending(input.agentSessionId),
      item
    ]);
    this.publish();
    void this.admit(input.agentSessionId, item).catch(() => {});
  }

  private admit(
    id: string,
    item: PendingPrompt<Input, Display>
  ): Promise<LocalPromptQueue> {
    if (item.admission) return item.admission;
    item.admission = this.options
      .enqueue(item.input)
      .then(
        (queue) => {
          if (this.disposed) return queue;
          item.confirmed = true;
          this.retainUntilTranscript(id, item);
          this.pendingBySession.set(
            id,
            this.pending(id).filter((value) => value !== item)
          );
          this.clearError(id, item.display.id, "admission");
          this.receive(id, queue);
          this.publish();
          return this.queues.get(id)!;
        },
        (error: unknown) => {
          if (this.disposed) throw error;
          // A canonical queue snapshot can confirm admission before its reply.
          if (item.confirmed) return this.queues.get(id)!;
          this.cancels.abandonSubmit(item.input.clientSubmitId, error);
          item.display = { ...item.display, visibleInQueue: true };
          this.fail(id, error, item.display.id, "admission");
          throw error;
        }
      )
      .finally(() => {
        delete item.admission;
      });
    return item.admission;
  }

  update(id: string, queueAction: PromptQueueAction, promptId?: string): void {
    this.options.beforeTurnOperation?.();
    void this.runUpdate(id, queueAction, promptId);
  }

  private runUpdate(
    id: string,
    queueAction: PromptQueueAction,
    promptId?: string,
    targetTurnId?: string,
  ): Promise<StopResult> | undefined {
    const prompt = this.prompts(id).find((item) => item.id === promptId);
    const requestId = prompt?.clientSubmitId || promptId;
    const key = JSON.stringify([id, queueAction, requestId]);
    if (this.operations.has(key) || this.steering.has(key)) return undefined;
    const request =
      queueAction === "sendNow" && promptId
        ? { sessionId: id, requestId, promptId, accepted: false }
        : undefined;
    if (request) {
      this.steering.set(key, request);
      this.publish();
    }
    const operation = (async (): Promise<StopResult> => {
      const item = this.pending(id).find(
        (value) => value.input.clientSubmitId === requestId
      );
      if (queueAction === "sendNow" && item) {
        try {
          await this.admit(id, item);
        } catch {
          return undefined;
        }
      }
      if (queueAction === "remove" && item?.admission) {
        // Wait for the original admission before removing its canonical item.
        // A rejected admission can still be discarded explicitly.
        try {
          await item.admission;
        } catch {
          /* Removal also handles rejection. */
        }
      }
      if (this.disposed) return undefined;
      // Re-admission uses the original id. A missing or dispatching item was
      // already accepted for execution; never interrupt it or send it again.
      const current = this.queues.get(id);
      if (
        queueAction === "sendNow" &&
        current &&
        (!current.items.some((value) => value.requestId === requestId) ||
          current.dispatchingRequestId === requestId)
      )
        return undefined;
      try {
        const queue = await (targetTurnId
          ? this.options.update(id, queueAction, requestId, targetTurnId)
          : this.options.update(id, queueAction, requestId));
        if (this.disposed) return undefined;
        if (queueAction === "remove" && requestId)
          this.awaitingTranscript.get(id)?.delete(requestId);
        if (queueAction === "remove" && item) {
          this.pendingBySession.set(
            id,
            this.pending(id).filter((value) => value !== item)
          );
          this.clearError(id, item.display.id, "admission");
        }
        this.clearError(id, promptId, "operation");
        if (request) request.accepted = true;
        this.receive(id, queue);
        this.publish();
        return undefined;
      } catch (error) {
        this.steering.delete(key);
        this.fail(id, error, promptId);
        return { error };
      }
    })();
    this.operations.set(key, operation);
    void operation.finally(() => {
      this.operations.delete(key);
      // An acknowledgement only accepts the steer. Keep presenting it until the
      // daemon reports dispatch, removal, failure or an explicit stop.
      if (request && !request.accepted) {
        this.steering.delete(key);
        this.publish();
      }
    });
    return operation;
  }

  /** A stop still on its way may predate the turn this one is for, so it goes after it. */
  private sendStop(
    id: string,
    requestId?: string,
    turnId?: string,
  ): Promise<StopResult> {
    const running = this.operations.get(
      JSON.stringify([id, "stop", requestId]),
    );
    return running
      ? running.then(() => this.sendStop(id, requestId, turnId))
      : this.runUpdate(id, "stop", requestId, turnId)!;
  }

  /** Stops the session; a message on its way is stopped once it has started a turn. */
  stop(rawId: string, clientSubmitId?: string | null): void {
    const id = rawId.trim();
    if (!id || this.disposed) return;
    this.options.beforeTurnOperation?.();
    if (this.cancels.pending(id)) return;
    // Sent whatever this client knows, since the daemon may be running what it has not
    // heard of, and it persists the pause before the cancellation can release the next turn.
    this.cancels.request(
      id,
      clientSubmitId,
      this.sendStop(
        id,
        clientSubmitId ?? undefined,
        this.options.turns.session(id)?.activeTurnId ?? undefined,
      ),
    );
  }

  cancelStatus(id: string | null | undefined): TurnCancelStatus | null {
    return this.cancels.status(id?.trim() ?? "");
  }
  cancelRecord(id: string): TurnCancelRecord | undefined {
    return this.cancels.record(id.trim());
  }

  respondToInteraction(input: InteractionResponseInput): boolean {
    if (this.disposed) return false;
    this.options.beforeTurnOperation?.();
    return this.interactions.respond(input);
  }
  interactionResponse(
    id: string,
    turnId: string,
    requestId: string
  ): InteractionResponseRecord | null {
    return this.interactions.response(id, turnId, requestId);
  }
  isRespondingToInteraction(id: string | null | undefined): boolean {
    return this.interactions.isResponding(id?.trim() ?? "");
  }
  interactionError(id: string | null | undefined): string | null {
    return this.interactions.error(id?.trim() ?? "");
  }
  subscribeInteractionResponses(listener: () => void): () => void {
    return reaction(() => this.interactions.all(), listener, {
      equals: comparer.shallow
    });
  }

  editRetryAvailability(
    id: string | null | undefined
  ): EditRetryAvailability | null {
    return this.editRetries.availability(id ?? "");
  }
  editRetryOperation(id: string | null | undefined): EditRetryOperation {
    return this.editRetries.operation(id ?? "");
  }
  receiveEditRetryAvailability(
    id: string,
    availability: EditRetryAvailability
  ): void {
    this.editRetries.receiveAvailability(id, availability);
  }
  requestEditRetry(input: {
    agentSessionId: string;
    editedText: string;
    turnId: string;
  }): Promise<EditRetryOperation> {
    this.options.beforeTurnOperation?.();
    return this.editRetries.request(input);
  }
  recoverEditRetry(input: {
    action: EditRetryRecoveryAction;
    agentSessionId: string;
  }): Promise<EditRetryOperation> {
    this.options.beforeTurnOperation?.();
    return this.editRetries.recover(input);
  }

  /** The conversation data this service reads has changed. */
  lifecycleChanged(): void {
    if (this.disposed) return;
    let changed = false;
    for (const [id, messages] of this.awaitingTranscript) {
      for (const requestId of messages.keys()) {
        if (this.options.turns.deleted(id) || this.options.turns.turnForSubmit(id, requestId)) {
          messages.delete(requestId);
          changed = true;
        }
      }
      if (!messages.size) this.awaitingTranscript.delete(id);
    }
    if (changed) this.publish();
    this.cancels.reconcile();
    this.interactions.reconcile();
    for (const id of this.editRetries.sessionIds())
      if (this.options.turns.deleted(id)) this.editRetries.remove(id);
  }

  dispose(): void {
    this.disposed = true;
    this.cancels.dispose();
    this.commands.dispose();
    this.awaitingTranscript.clear();
  }

  private retainUntilTranscript(id: string, item: PendingPrompt<Input, Display>): void {
    // Admission can empty the queue before the corresponding user.message is committed.
    if (item.display.visibleInQueue !== false || this.options.turns.turnForSubmit(id, item.input.clientSubmitId)) return;
    let messages = this.awaitingTranscript.get(id);
    if (!messages) this.awaitingTranscript.set(id, messages = new Map());
    messages.set(item.input.clientSubmitId, item.display);
  }
}
