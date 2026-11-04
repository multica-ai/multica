import { observable, runInAction } from "mobx";
import type { PromptCommands } from "./prompt-command";
import type { PromptTurnReader } from "./turn-cancels";

const INTERACTION_RESPONSE_TIMEOUT_MS = 30_000;
const STALE_INTERACTIVE_REQUEST = "agent_interactive_request_stale";

export type InteractionResponseStatus = "responding" | "failed" | "unknown";

export interface InteractionResponseInput {
  action?: string;
  agentSessionId: string;
  optionId?: string;
  payload?: Readonly<Record<string, unknown>>;
  requestId: string;
  turnId: string;
}

export interface InteractionResponseRecord {
  action: string | null;
  agentSessionId: string;
  commandId: string;
  errorCode: string | null;
  errorMessage: string | null;
  optionId: string | null;
  payload: Readonly<Record<string, unknown>> | null;
  requestId: string;
  status: InteractionResponseStatus;
  turnId: string;
  workspaceId: string;
}

export interface InteractionResponsesOptions {
  workspaceId: string;
  turns: PromptTurnReader;
  commands: PromptCommands;
  respond(input: InteractionResponseInput & { workspaceId: string }, options: { signal: AbortSignal }): Promise<unknown>;
  reconcile(input: { agentSessionId: string; needsMessages: boolean; needsState: boolean }): void;
}

const key = (agentSessionId: string, turnId: string, requestId: string) => JSON.stringify([agentSessionId, turnId, requestId]);

/**
 * Answers to questions and approvals. A response leaves here only when the
 * conversation data shows the interaction is no longer pending: an acknowledgement
 * says the daemon got it, not that it took effect.
 */
export class InteractionResponses {
  private readonly records = observable.map<string, InteractionResponseRecord>([], { deep: false });
  private sequence = 1;

  constructor(private readonly options: InteractionResponsesOptions) {}

  response(agentSessionId: string, turnId: string, requestId: string): InteractionResponseRecord | null {
    return this.records.get(key(agentSessionId.trim(), turnId.trim(), requestId.trim())) ?? null;
  }

  isResponding(id: string): boolean {
    return [...this.records.values()].some((response) => response.agentSessionId === id && (response.status === "responding" || response.status === "unknown"));
  }

  error(id: string): string | null {
    return [...this.records.values()].filter((response) => response.agentSessionId === id)
      .sort((left, right) => right.commandId.localeCompare(left.commandId))
      .find((response) => response.errorMessage)?.errorMessage ?? null;
  }

  all(): InteractionResponseRecord[] { return [...this.records.values()]; }

  /** Whether this answer went out. Only a failed one can go again, and only unchanged. */
  respond(input: InteractionResponseInput): boolean {
    const { turns, workspaceId } = this.options;
    const agentSessionId = input.agentSessionId.trim();
    const requestId = input.requestId.trim();
    const turnId = input.turnId.trim();
    if (!agentSessionId || !requestId || !turnId) return false;
    const action = input.action?.trim() || null;
    const optionId = input.optionId?.trim() || null;
    const payload = input.payload ? { ...input.payload } : null;
    const responseKey = key(agentSessionId, turnId, requestId);
    const existing = this.records.get(responseKey);
    const commandId = `interaction:${Date.now()}:${this.sequence++}`;
    if (
      turns.session(agentSessionId)?.workspaceId !== workspaceId
      || turns.runtimeBlocked(agentSessionId)
      || turns.interactionStatus(agentSessionId, turnId, requestId) !== "pending"
      || (existing && (existing.status !== "failed" || existing.action !== action || existing.optionId !== optionId || JSON.stringify(existing.payload) !== JSON.stringify(payload)))
    ) return false;
    this.set(responseKey, { action, agentSessionId, commandId, errorCode: null, errorMessage: null, optionId, payload, requestId, status: "responding", turnId, workspaceId });
    void this.send(responseKey, commandId);
    return true;
  }

  reconcile(): void {
    for (const [responseKey, response] of [...this.records]) {
      if (this.options.turns.interactionStatus(response.agentSessionId, response.turnId, response.requestId) !== "pending") {
        runInAction(() => { this.records.delete(responseKey); });
      }
    }
  }

  private async send(responseKey: string, commandId: string) {
    const { action, optionId, payload, agentSessionId, requestId, turnId, workspaceId } = this.records.get(responseKey)!;
    const result = await this.options.commands.run(INTERACTION_RESPONSE_TIMEOUT_MS, (signal) => this.options.respond({
      ...(action ? { action } : {}), agentSessionId, ...(optionId ? { optionId } : {}), ...(payload ? { payload } : {}), requestId, turnId, workspaceId,
    }, { signal }));
    const current = this.records.get(responseKey);
    if (current?.commandId !== commandId) return;
    if (result.outcome === "failed") {
      this.set(responseKey, { ...current, errorCode: result.errorCode, errorMessage: result.errorMessage.trim() || null, status: "failed" });
      if (result.errorReason === STALE_INTERACTIVE_REQUEST) this.options.reconcile({ agentSessionId, needsMessages: true, needsState: true });
      return;
    }
    const inScope = result.outcome === "succeeded" && scopedSession(result.value, agentSessionId, workspaceId);
    this.set(responseKey, { ...current, errorCode: result.outcome === "timedOut" ? "timeout" : inScope ? null : "invalid_command_result", errorMessage: null, status: "unknown" });
  }

  private set(responseKey: string, response: InteractionResponseRecord) {
    runInAction(() => { this.records.set(responseKey, response); });
  }
}

function scopedSession(value: unknown, agentSessionId: string, workspaceId: string): boolean {
  const session = value && typeof value === "object" ? (value as { session?: { agentSessionId?: unknown; workspaceId?: unknown } }).session : null;
  return typeof session?.agentSessionId === "string" && typeof session.workspaceId === "string"
    && session.agentSessionId.trim() === agentSessionId && session.workspaceId.trim() === workspaceId;
}
