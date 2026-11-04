import { observable, runInAction } from "mobx";
import type { PromptCommands } from "./prompt-command";

const EDIT_RETRY_TIMEOUT_MS = 60_000;

export type EditRetryRecoveryState = "prepared" | "rolling_back" | "resend_pending" | "recovery_required" | "completed";
export type EditRetryRecoveryAction = "reconcile" | "retry_replacement";
export type EditRetryReasonCode =
  | "provider_unsupported" | "turn_not_found" | "turn_not_latest" | "turn_not_settled" | "history_revision_conflict"
  | "operation_conflict" | "recovery_required" | "provider_outcome_unknown" | "replacement_not_proven_absent";

export interface EditRetryAvailability {
  supported: boolean;
  eligible: boolean;
  turnId?: string;
  historyRevision: number;
  recoveryState: EditRetryRecoveryState;
  operationId?: string;
  availableActions: readonly EditRetryRecoveryAction[];
  reasonCode?: EditRetryReasonCode;
}

export interface EditRetryResult {
  operationId: string;
  state: EditRetryRecoveryState;
  retractedTurnId: string;
  replacementTurnId?: string;
  historyRevision: number;
  reasonCode?: EditRetryReasonCode;
}

export type EditRetryStatus = "failed" | "idle" | "pending" | "reconciling" | "succeeded";

export interface EditRetryOperation {
  clientOperationId: string | null;
  errorCode: string | null;
  errorMessage: string | null;
  requestKey: string | null;
  result: EditRetryResult | null;
  status: EditRetryStatus;
  workspaceId: string | null;
}

export interface PromptSessionReconcile {
  agentSessionId: string;
  authoritativeMessages?: boolean;
  needsMessages: boolean;
  needsState: boolean;
  requiredHistoryRevision?: number;
}

export interface EditRetriesOptions {
  workspaceId: string;
  commands: PromptCommands;
  editRetry(input: { agentSessionId: string; clientOperationId: string; editedText: string; expectedHistoryRevision: number; turnId: string; workspaceId: string }, options: { signal: AbortSignal }): Promise<unknown>;
  recoverEditRetry(input: { action: EditRetryRecoveryAction; agentSessionId: string; operationId: string; workspaceId: string }, options: { signal: AbortSignal }): Promise<unknown>;
  reconcile(input: PromptSessionReconcile): void;
}

const IDLE: EditRetryOperation = { clientOperationId: null, errorCode: null, errorMessage: null, requestKey: null, result: null, status: "idle", workspaceId: null };

/**
 * Editing a settled message and sending it again. The daemon's availability is
 * the authority: a command's success only says it was accepted, and the operation
 * is finished when availability confirms the result.
 */
export class EditRetries {
  private readonly availabilities = observable.map<string, EditRetryAvailability>([], { deep: false });
  private readonly operations = observable.map<string, EditRetryOperation>([], { deep: false });
  private readonly running = new Map<string, Promise<EditRetryOperation>>();

  constructor(private readonly options: EditRetriesOptions) {}

  availability(id: string): EditRetryAvailability | null { return this.availabilities.get(id.trim()) ?? null; }
  operation(id: string): EditRetryOperation { return this.operations.get(id.trim()) ?? IDLE; }

  receiveAvailability(rawId: string, availability: EditRetryAvailability): void {
    const id = rawId.trim();
    if (!id) return;
    const operation = this.operations.get(id);
    runInAction(() => {
      if (!availabilityEqual(this.availabilities.get(id), availability)) this.availabilities.set(id, { ...availability, availableActions: [...availability.availableActions] });
      if (operation?.status === "reconciling" && operation.result && availabilityConfirms(availability, operation.result)) {
        this.operations.set(id, { ...operation, result: null, status: "succeeded" });
      }
    });
  }

  /** Settles with the operation once it left pending; rejects when it failed or did not start. */
  request(input: { agentSessionId: string; editedText: string; turnId: string }): Promise<EditRetryOperation> {
    const id = input.agentSessionId.trim();
    const turnId = input.turnId.trim();
    const availability = this.availabilities.get(id);
    const current = this.operations.get(id) ?? IDLE;
    if (current.status === "pending") return this.running.get(id) ?? notStarted();
    if (!id || !turnId || availability?.eligible !== true || availability.turnId?.trim() !== turnId) return notStarted();
    const requestKey = JSON.stringify([turnId, availability.historyRevision, input.editedText]);
    // The same edit keeps its id, so resending after a lost reply cannot apply twice.
    const clientOperationId = current.requestKey === requestKey && current.clientOperationId ? current.clientOperationId : clientOperationIdFor(requestKey);
    const { workspaceId } = this.options;
    return this.begin(id, { clientOperationId, errorCode: null, errorMessage: null, requestKey, result: null, status: "pending", workspaceId }, (signal) => this.options.editRetry({
      agentSessionId: id, clientOperationId, editedText: input.editedText, expectedHistoryRevision: availability.historyRevision, turnId, workspaceId,
    }, { signal }));
  }

  recover(input: { action: EditRetryRecoveryAction; agentSessionId: string }): Promise<EditRetryOperation> {
    const id = input.agentSessionId.trim();
    const availability = this.availabilities.get(id);
    const operationId = availability?.operationId?.trim() ?? "";
    const current = this.operations.get(id) ?? IDLE;
    if (current.status === "pending") return this.running.get(id) ?? notStarted();
    if (!id || !operationId || !availability?.availableActions.includes(input.action)) return notStarted();
    const { workspaceId } = this.options;
    return this.begin(id, { ...current, errorCode: null, errorMessage: null, result: null, status: "pending", workspaceId }, (signal) => this.options.recoverEditRetry({
      action: input.action, agentSessionId: id, operationId, workspaceId,
    }, { signal }));
  }

  remove(id: string): void {
    runInAction(() => { this.availabilities.delete(id); this.operations.delete(id); });
  }

  sessionIds(): string[] { return [...new Set([...this.availabilities.keys(), ...this.operations.keys()])]; }

  private begin(id: string, operation: EditRetryOperation, execute: (signal: AbortSignal) => Promise<unknown>): Promise<EditRetryOperation> {
    runInAction(() => { this.operations.set(id, operation); });
    const running = this.options.commands.run(EDIT_RETRY_TIMEOUT_MS, execute).then((result) => {
      this.running.delete(id);
      const current = this.operations.get(id);
      if (!current) throw new Error("agent_edit_retry_command_failed");
      const parsed = result.outcome === "succeeded" ? parseResult(result.value) : null;
      if (!parsed) {
        const errorMessage = (result.outcome === "failed" ? result.errorMessage.trim() : "") || (result.outcome === "succeeded" ? "agent_edit_retry_invalid_result" : "agent_edit_retry_command_failed");
        runInAction(() => { this.operations.set(id, { ...current, errorCode: result.outcome === "failed" ? result.errorCode : null, errorMessage, result: null, status: "failed" }); });
        this.options.reconcile({ agentSessionId: id, needsMessages: true, needsState: true });
        throw new Error(errorMessage);
      }
      const next: EditRetryOperation = { ...current, errorCode: null, errorMessage: null, result: parsed, status: "reconciling" };
      runInAction(() => { this.operations.set(id, next); });
      this.options.reconcile({ agentSessionId: id, authoritativeMessages: true, needsMessages: false, needsState: false, requiredHistoryRevision: parsed.historyRevision });
      return next;
    });
    this.running.set(id, running);
    return running;
  }
}

function notStarted(): Promise<never> { return Promise.reject(new Error("agent_edit_retry_command_not_started")); }

function parseResult(value: unknown): EditRetryResult | null {
  if (!value || typeof value !== "object") return null;
  const result = value as Partial<EditRetryResult>;
  return typeof result.operationId === "string" && typeof result.state === "string" && typeof result.retractedTurnId === "string" && typeof result.historyRevision === "number"
    ? result as EditRetryResult : null;
}

function availabilityConfirms(availability: EditRetryAvailability, result: EditRetryResult): boolean {
  if (availability.historyRevision < result.historyRevision) return false;
  if (result.state === "completed") return availability.recoveryState === "prepared";
  return availability.operationId === result.operationId && availability.recoveryState === result.state;
}

function availabilityEqual(left: EditRetryAvailability | undefined, right: EditRetryAvailability): boolean {
  return left?.supported === right.supported && left.eligible === right.eligible && left.turnId === right.turnId
    && left.historyRevision === right.historyRevision && left.recoveryState === right.recoveryState
    && left.operationId === right.operationId && left.reasonCode === right.reasonCode
    && left.availableActions.length === right.availableActions.length
    && left.availableActions.every((action, index) => action === right.availableActions[index]);
}

function clientOperationIdFor(requestKey: string): string {
  return `edit-retry-${hex32(hash32(requestKey, 0x811c9dc5))}-${hex32(hash32(requestKey, 0x9e3779b9))}`;
}

function hash32(value: string, seed: number): number {
  let hash = seed >>> 0;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
    hash ^= hash >>> 13;
  }
  return hash >>> 0;
}

function hex32(value: number): string { return value.toString(16).padStart(8, "0"); }
