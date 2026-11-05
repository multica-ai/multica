import { action, makeObservable, observable } from "mobx";
import "../mobx-config";
import {
  ActivationModel,
  type ActivationRequest,
} from "../models/activation-model";

/** Starting a new conversation spawns a process and initializes it, which takes longer than attaching. */
export const NEW_ACTIVATION_TIMEOUT_MS = 90_000;
export const EXISTING_ACTIVATION_TIMEOUT_MS = 30_000;
export const ACTIVATION_CONFIRMATION_TIMEOUT_MS = 120_000;

export interface ActivationSession {
  agentSessionId: string;
  workspaceId: string;
  createdAtUnixMs?: number;
  updatedAtUnixMs?: number;
}

export interface ActivationFailure {
  code: string | null;
  message: string | null;
}

export interface ActivationServiceOptions<
  Request extends ActivationRequest<Settings>,
  Settings,
  Result,
> {
  workspaceId: string;
  nowUnixMs(): number;
  isDeleted(agentSessionId: string): boolean;
  activate(request: Request, signal: AbortSignal): Promise<Result>;
  checkpoint?(request: Request): Promise<void>;
  /** Shows what the activation returned; a session in it comes back through `observeSessions`. */
  applyResult(request: Request, result: Result): void;
  updateSettings(agentSessionId: string, settings: Settings): Promise<void>;
  /** Asks for the conversation's state, to learn what became of an activation with no answer. */
  reconcile(agentSessionId: string): void;
  describeError(error: unknown): ActivationFailure;
}

interface ActivationRun {
  abort: AbortController;
  expiry: ReturnType<typeof setTimeout> | null;
  timeout: ReturnType<typeof setTimeout> | null;
  settings: number;
}

/** New conversations and attachments while they are on their way, and how each one ended. */
export class ActivationService<
  Request extends ActivationRequest<Settings>,
  Settings,
  Result,
> {
  private readonly records = observable.map<
    string,
    ActivationModel<Request, Settings>
  >([], { deep: false });
  private readonly inactive = observable.set<string>();
  private readonly runs = new Map<string, ActivationRun>();
  private disposed = false;
  private readonly savedRequests = new Map<string, Promise<boolean>>();
  /** Advances with every change, for whatever presents this as a whole. */
  revision = 0;

  constructor(
    private readonly options: ActivationServiceOptions<
      Request,
      Settings,
      Result
    >,
  ) {
    makeObservable<this, "bump">(this, { revision: observable, bump: action });
  }

  private bump(): void {
    this.revision++;
  }

  activations(): ActivationModel<Request, Settings>[] {
    return [...this.records.values()];
  }

  inactiveSessionIds(): string[] {
    return [...this.inactive];
  }

  /** Nothing can be read for this conversation yet: its creation has not been answered. */
  isCreating(agentSessionId: string): boolean {
    return this.activations().some(
      (record) =>
        record.request.agentSessionId === agentSessionId &&
        record.request.mode === "new" &&
        record.status === "requested",
    );
  }

  request(request: Request): boolean {
    const { agentSessionId, requestId } = request;
    if (
      this.disposed ||
      !agentSessionId ||
      !requestId ||
      this.records.has(requestId) ||
      this.options.isDeleted(agentSessionId)
    )
      return false;
    for (const record of this.activations()) {
      if (
        record.request.agentSessionId === agentSessionId &&
        record.status !== "failed"
      )
        this.remove(record.request.requestId);
    }
    const requestedAt = this.options.nowUnixMs();
    const record = new ActivationModel<Request, Settings>(
      request,
      requestedAt,
      requestedAt + ACTIVATION_CONFIRMATION_TIMEOUT_MS,
    );
    this.inactive.delete(agentSessionId);
    this.records.set(requestId, record);
    this.bump();
    this.run(record);
    return true;
  }

  private run(record: ActivationModel<Request, Settings>): void {
    const { request } = record;
    const run: ActivationRun = {
      abort: new AbortController(),
      expiry: null,
      timeout: null,
      settings: 0,
    };
    this.runs.set(request.requestId, run);
    run.expiry = setTimeout(
      () => this.expire(record),
      ACTIVATION_CONFIRMATION_TIMEOUT_MS,
    );
    run.timeout = setTimeout(
      () => {
        run.timeout = null;
        run.abort.abort();
        if (record.status !== "requested") return;
        record.markUncertain();
        this.bump();
        if (request.mode === "new")
          this.options.reconcile(request.agentSessionId);
      },
      request.mode === "new"
        ? NEW_ACTIVATION_TIMEOUT_MS
        : EXISTING_ACTIVATION_TIMEOUT_MS,
    );
    const checkpoint = this.options.checkpoint?.(request);
    if (checkpoint)
      this.savedRequests.set(
        request.requestId,
        checkpoint.then(
          () => true,
          () => false,
        ),
      );
    void Promise.resolve(checkpoint)
      .then(() => this.options.activate(request, run.abort.signal))
      .then(
        (result) => this.settle(record, run, result),
        (error: unknown) => this.settleFailure(record, run, error),
      );
  }

  saved(requestId: string): Promise<boolean> | undefined {
    return this.savedRequests.get(requestId);
  }

  private isCurrent(
    record: ActivationModel<Request, Settings>,
    run: ActivationRun,
  ): boolean {
    return !this.disposed && this.runs.get(record.request.requestId) === run;
  }

  private settle(
    record: ActivationModel<Request, Settings>,
    run: ActivationRun,
    result: Result,
  ): void {
    if (!this.isCurrent(record, run) || run.timeout === null) return;
    clearTimeout(run.timeout);
    run.timeout = null;
    this.options.applyResult(record.request, result);
    // An answer about this very request settles it, whatever the clocks say about the session.
    if (record.pending) this.confirm(record);
  }

  private settleFailure(
    record: ActivationModel<Request, Settings>,
    run: ActivationRun,
    error: unknown,
  ): void {
    if (!this.isCurrent(record, run) || run.timeout === null) return;
    clearTimeout(run.timeout);
    run.timeout = null;
    if (!record.pending) return;
    const failure = this.options.describeError(error);
    record.fail(failure.code, failure.message);
    this.bump();
  }

  private expire(record: ActivationModel<Request, Settings>): void {
    if (this.disposed || !record.pending) return;
    record.fail(
      record.errorCode ?? "activation_confirmation_expired",
      record.errorMessage,
    );
    this.bump();
    if (record.request.mode === "new")
      this.options.reconcile(record.request.agentSessionId);
  }

  private confirm(record: ActivationModel<Request, Settings>): void {
    const run = this.runs.get(record.request.requestId);
    if (run?.expiry) clearTimeout(run.expiry);
    this.inactive.delete(record.request.agentSessionId);
    record.confirm();
    this.bump();
    this.sendSettings(record);
  }

  /** A session that exists settles the activation that was waiting for it. */
  observeSessions(sessions: readonly ActivationSession[]): void {
    for (const session of sessions) {
      for (const record of this.activations()) {
        if (
          !record.pending ||
          record.request.agentSessionId !== session.agentSessionId ||
          session.workspaceId !== this.options.workspaceId
        )
          continue;
        // A conversation from before the request says nothing about whether it was created.
        if (
          record.request.mode === "new" &&
          !(
            (session.createdAtUnixMs ?? -Infinity) >=
              record.requestedAtUnixMs ||
            (session.updatedAtUnixMs ?? -Infinity) >= record.requestedAtUnixMs
          )
        )
          continue;
        this.confirm(record);
      }
    }
  }

  /** Settings chosen before the conversation exists wait for it. */
  patchSettings(agentSessionId: string, settings: Settings): void {
    const record = this.activations()
      .filter(
        (candidate) =>
          candidate.request.agentSessionId === agentSessionId &&
          candidate.request.mode === "new" &&
          candidate.status !== "failed",
      )
      .sort(
        (left, right) => right.requestedAtUnixMs - left.requestedAtUnixMs,
      )[0];
    if (!record) return;
    record.patchSettings(settings);
    this.bump();
    if (record.status === "confirmed") this.sendSettings(record);
  }

  private sendSettings(record: ActivationModel<Request, Settings>): void {
    const settings = record.settingsPatch;
    const run = this.runs.get(record.request.requestId);
    if (!settings || !Object.keys(settings).length || !run) return;
    const attempt = ++run.settings;
    const latest = () =>
      !this.disposed &&
      this.records.get(record.request.requestId) === record &&
      run.settings === attempt;
    record.beginSettings();
    this.bump();
    void Promise.resolve()
      .then(() =>
        this.options.updateSettings(record.request.agentSessionId, settings),
      )
      .then(
        () => {
          if (!latest()) return;
          record.settingsApplied();
          this.bump();
        },
        (error: unknown) => {
          if (!latest()) return;
          const failure = this.options.describeError(error);
          record.settingsFailed(
            failure.code ?? "settings_update_failed",
            failure.message,
          );
          this.bump();
        },
      );
  }

  recordFailure(request: Request, failure: ActivationFailure): void {
    if (this.records.has(request.requestId)) return;
    const now = this.options.nowUnixMs();
    const record = new ActivationModel<Request, Settings>(
      request,
      now,
      Number.MAX_SAFE_INTEGER,
    );
    record.fail(failure.code, failure.message);
    this.records.set(request.requestId, record);
    this.bump();
  }

  clearFailure(agentSessionId: string): void {
    for (const record of this.activations()) {
      if (
        record.request.agentSessionId === agentSessionId &&
        record.status === "failed"
      )
        this.remove(record.request.requestId);
    }
    this.bump();
  }

  /** Forgets a conversation's activations, so it reads as not started. */
  unactivate(agentSessionId: string): void {
    this.forget(agentSessionId);
    this.inactive.add(agentSessionId);
    this.bump();
  }

  removeSession(agentSessionId: string): void {
    this.forget(agentSessionId);
    this.inactive.delete(agentSessionId);
    this.bump();
  }

  private forget(agentSessionId: string): void {
    for (const record of this.activations()) {
      if (record.request.agentSessionId === agentSessionId)
        this.remove(record.request.requestId);
    }
  }

  private remove(requestId: string): void {
    const run = this.runs.get(requestId);
    if (run) {
      if (run.expiry) clearTimeout(run.expiry);
      if (run.timeout) clearTimeout(run.timeout);
      this.runs.delete(requestId);
    }
    this.records.delete(requestId);
    this.savedRequests.delete(requestId);
  }

  dispose(): void {
    this.disposed = true;
    for (const run of this.runs.values()) {
      if (run.expiry) clearTimeout(run.expiry);
      if (run.timeout) clearTimeout(run.timeout);
      run.abort.abort();
    }
    this.runs.clear();
    this.savedRequests.clear();
  }
}
