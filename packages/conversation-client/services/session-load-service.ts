import { action, makeObservable, observable } from "mobx";
import "../mobx-config";

export interface SessionLoadFailure { code: string | null; message: string | null }

export interface SessionLoadRecord {
  readonly agentSessionId: string;
  /** What the read in flight will bring, or null when none is. */
  readonly inFlight: "state" | "messages" | "state_and_messages" | null;
  /** Asked for while a read was already in flight, so another follows. */
  readonly pendingState: boolean;
  readonly pendingMessages: boolean;
  /** Whether this conversation's messages have been received at least once. */
  readonly messagesHydrated: boolean;
  readonly errorCode: string | null;
  readonly errorMessage: string | null;
}

export interface SessionLoadServiceOptions {
  /** A conversation being deleted or still being created has nothing to read. */
  canLoad(agentSessionId: string): boolean;
  load(agentSessionId: string): Promise<unknown>;
  describeError(error: unknown): SessionLoadFailure;
}

/** Which conversations are being read, one read each at a time, and how the last one ended. */
export class SessionLoadService {
  private readonly records = observable.map<string, SessionLoadRecord>([], { deep: false });
  private disposed = false;
  /** Advances with every change, for whatever presents this as a whole. */
  revision = 0;

  constructor(private readonly options: SessionLoadServiceOptions) {
    makeObservable<this, "write">(this, { revision: observable, write: action, remove: action, dispose: action });
  }

  all(): SessionLoadRecord[] { return [...this.records.values()]; }

  private write(agentSessionId: string, patch: Partial<SessionLoadRecord>): SessionLoadRecord {
    const record: SessionLoadRecord = {
      agentSessionId, inFlight: null, pendingState: false, pendingMessages: false, messagesHydrated: false, errorCode: null, errorMessage: null,
      ...this.records.get(agentSessionId), ...patch,
    };
    this.records.set(agentSessionId, record);
    this.revision++;
    return record;
  }

  request(agentSessionId: string, needs: { state: boolean; messages: boolean }): void {
    const id = agentSessionId.trim();
    if (this.disposed || !id || (!needs.state && !needs.messages)) return;
    if (!this.options.canLoad(id)) return;
    const current = this.records.get(id);
    if (current?.inFlight) {
      this.write(id, { pendingState: current.pendingState || needs.state, pendingMessages: current.pendingMessages || needs.messages });
      return;
    }
    const inFlight = needs.state && needs.messages ? "state_and_messages" : needs.messages ? "messages" : "state";
    this.write(id, { inFlight, pendingState: false, pendingMessages: false, errorCode: null, errorMessage: null });
    void Promise.resolve().then(() => this.options.load(id)).then(
      () => this.settle(id, needs.messages, null),
      (error: unknown) => this.settle(id, needs.messages, this.options.describeError(error)),
    );
  }

  private settle(id: string, readMessages: boolean, failure: SessionLoadFailure | null): void {
    const current = this.records.get(id);
    if (this.disposed || !current) return;
    const { pendingState, pendingMessages } = current;
    this.write(id, {
      inFlight: null, pendingState: false, pendingMessages: false,
      messagesHydrated: current.messagesHydrated || (!failure && readMessages),
      errorCode: failure?.code ?? null, errorMessage: failure?.message ?? null,
    });
    if (pendingState || pendingMessages) this.request(id, { state: pendingState, messages: pendingMessages });
  }

  /** Messages arrived by another route, such as a conversation's own updates. */
  markHydrated(agentSessionId: string): void {
    if (this.records.get(agentSessionId)?.messagesHydrated) return;
    this.write(agentSessionId, { messagesHydrated: true });
  }

  remove(agentSessionId: string): void {
    if (this.records.delete(agentSessionId)) this.revision++;
  }

  dispose(): void { this.disposed = true; }
}
