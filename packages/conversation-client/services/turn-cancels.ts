import { observable, runInAction } from "mobx";
import { commandFailure, type PromptCommands } from "./prompt-command";

const TURN_CANCEL_TIMEOUT_MS = 30_000;
// The prompt can never have started a turn after these.
const PRE_TURN_FAILURES = new Set(["agent.no_active_turn", "agent.session_no_active_turn", "agent.process_cleanup_pending"]);

export type TurnCancelStatus = "awaitingTurn" | "requested" | "accepted" | "failed";

export interface TurnCancelRecord {
  commandId: string;
  errorCode: string | null;
  errorMessage: string | null;
  status: TurnCancelStatus;
  targetClientSubmitId: string | null;
  turnId: string | null;
}

export type StopResult = { error: unknown } | undefined;

/** What the conversation data says right now; the services hold no copy of it. */
export interface PromptTurnReader {
  session(id: string): { workspaceId: string; activeTurnId: string | null } | null;
  turn(id: string, turnId: string): { turnId: string; phase: string } | null;
  deleted(id: string): boolean;
  runtimeBlocked(id: string): boolean;
  interactionStatus(id: string, turnId: string, requestId: string): string | null;
  /** The newest message on its way that may still start a turn. */
  stopTarget(id: string): string | null;
  /** The turn a message started, once history shows it. */
  turnForSubmit(id: string, clientSubmitId: string): string | null;
}

export interface TurnCancelsOptions {
  workspaceId: string;
  turns: PromptTurnReader;
  commands: PromptCommands;
  /** Pauses the queue and interrupts whatever the daemon is running. */
  stop(id: string): Promise<StopResult>;
  reconcileWorkspace(): void;
}

/**
 * A stop as this client tracks it. The daemon pauses and interrupts in one step,
 * but only what it has by then: a message still on its way un-pauses the queue and
 * starts a turn afterward, so a stop with nothing running waits for that turn and
 * stops again when it appears.
 */
export class TurnCancels {
  private readonly records = observable.map<string, TurnCancelRecord>([], { deep: false });
  private readonly expiries = new Map<string, ReturnType<typeof setTimeout>>();
  private sequence = 1;

  constructor(private readonly options: TurnCancelsOptions) {}

  status(id: string): TurnCancelStatus | null { return this.records.get(id)?.status ?? null; }
  record(id: string): TurnCancelRecord | undefined { return this.records.get(id); }

  /** A pending stop already covers another click. */
  pending(id: string): boolean {
    const status = this.records.get(id)?.status;
    return status === "requested" || status === "awaitingTurn";
  }

  /** `stopped` is the daemon's answer to the stop just sent; an active turn needs no other. */
  request(id: string, clientSubmitId: string | null | undefined, stopped: Promise<StopResult>): void {
    const { turns, workspaceId } = this.options;
    const session = turns.session(id);
    if (turns.deleted(id) || (session && session.workspaceId !== workspaceId) || turns.runtimeBlocked(id)) return;
    const turn = session?.activeTurnId ? turns.turn(id, session.activeTurnId) : null;
    const targetClientSubmitId = clientSubmitId?.trim() || (turn ? null : turns.stopTarget(id));
    const commandId = `stop:${Date.now()}:${this.sequence++}`;
    if (turn && turn.phase !== "settled" && !targetClientSubmitId) {
      this.set(id, { commandId, errorCode: null, errorMessage: null, status: "requested", targetClientSubmitId: null, turnId: turn.turnId });
      void this.settleWhen(id, commandId, stopped);
      return;
    }
    this.set(id, { commandId, errorCode: null, errorMessage: null, status: "awaitingTurn", targetClientSubmitId, turnId: null });
    this.expiries.set(id, setTimeout(() => {
      if (this.records.get(id)?.commandId === commandId) this.clear(id);
    }, TURN_CANCEL_TIMEOUT_MS));
    this.reconcile();
  }

  /** A message that failed before it could start a turn leaves its stop nothing to wait for. */
  abandonSubmit(clientSubmitId: string, error: unknown): void {
    const { errorCode, errorReason } = commandFailure(error);
    const status = error && typeof error === "object" && "status" in error ? error.status : null;
    const refused = typeof status === "number" && status >= 400 && status < 500 && status !== 408;
    if (!refused && !PRE_TURN_FAILURES.has(errorCode ?? "") && !PRE_TURN_FAILURES.has(errorReason ?? "")) return;
    for (const [id, record] of [...this.records]) {
      if (record.status === "awaitingTurn" && record.targetClientSubmitId === clientSubmitId) this.clear(id);
    }
  }

  reconcile(): void {
    const { turns, workspaceId } = this.options;
    for (const [id, stored] of [...this.records]) {
      if (turns.deleted(id)) { this.clear(id); continue; }
      let record = stored;
      if (record.status === "awaitingTurn" && record.targetClientSubmitId && !record.turnId) {
        const turnId = turns.turnForSubmit(id, record.targetClientSubmitId);
        if (turnId) { record = { ...record, turnId }; this.set(id, record); }
      }
      const session = turns.session(id);
      const active = session?.activeTurnId ? turns.turn(id, session.activeTurnId) : null;
      const targeted = record.turnId ? turns.turn(id, record.turnId) : null;
      const turn = targeted ?? active;
      if (record.status === "awaitingTurn") {
        if (session?.workspaceId === workspaceId && turn && turn.phase !== "settled"
          && (!record.targetClientSubmitId || record.turnId === turn.turnId)) {
          this.cancelExpiry(id);
          this.set(id, { ...record, status: "requested", turnId: turn.turnId });
          void this.settleWhen(id, record.commandId, this.options.stop(id));
        }
      } else if (!active || active.phase === "settled") {
        this.clear(id);
      }
    }
  }

  dispose(): void {
    for (const id of [...this.expiries.keys()]) this.cancelExpiry(id);
  }

  private async settleWhen(id: string, commandId: string, stopped: Promise<StopResult>) {
    const result = await this.options.commands.run(TURN_CANCEL_TIMEOUT_MS, () => stopped);
    const record = this.records.get(id);
    if (record?.commandId !== commandId) return;
    const failure = result.outcome === "timedOut" ? { errorCode: null, errorMessage: null }
      : result.outcome === "failed" ? result : result.value ? commandFailure(result.value.error) : null;
    if (failure) {
      this.set(id, { ...record, errorCode: failure.errorCode, errorMessage: failure.errorMessage, status: "failed" });
      return;
    }
    const turn = record.turnId ? this.options.turns.turn(id, record.turnId) : null;
    if (!turn) {
      // This client's view of the session has lost the turn it stopped.
      this.clear(id);
      this.options.reconcileWorkspace();
    } else if (turn.phase === "settled") this.clear(id);
    else this.set(id, { ...record, status: "accepted" });
  }

  private set(id: string, record: TurnCancelRecord) {
    runInAction(() => { this.records.set(id, record); });
  }

  private clear(id: string) {
    this.cancelExpiry(id);
    runInAction(() => { this.records.delete(id); });
  }

  private cancelExpiry(id: string) {
    const expiry = this.expiries.get(id);
    if (expiry !== undefined) clearTimeout(expiry);
    this.expiries.delete(id);
  }
}
