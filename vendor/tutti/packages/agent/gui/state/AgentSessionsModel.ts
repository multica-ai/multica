import type { ConversationModel } from "@multica/conversation-client/models";
import {
  normalizeAgentActivitySession,
  type AgentActivityInteraction,
  type AgentActivitySession,
  type AgentActivitySessionInput,
  type AgentActivitySnapshot,
  type AgentActivityTurn,
  type AgentSessionFamilySnapshot,
  type AgentSubmitAvailability,
  type CanonicalAgentSession,
  type SessionRuntimeAvailability,
  type WorkspaceAgentConsumerSession
} from "@tutti-os/agent-activity-core";
import {
  action,
  comparer,
  computed,
  makeObservable,
  observable,
  untracked,
  type IComputedValue
} from "mobx";
import { readNativeSessionTurns } from "./nativeSessionPresentation";

const EMPTY: readonly never[] = Object.freeze([]);
type Id = string | null | undefined;
const key = (id: Id) => id?.trim() ?? "";

export interface AgentSessionsSource {
  ids(): readonly string[];
  metadata(id: string): AgentActivitySessionInput | null;
  conversation(id: string): ConversationModel | undefined;
  commitVersion(id: string): unknown;
  availability(id: string): SessionRuntimeAvailability | null;
}

/** GUI presentation entities. Services own commands and their pending state. */
export class AgentSessionsModel {
  private readonly removed = observable.set<string>();
  private readonly consumers = new Map<
    string,
    IComputedValue<WorkspaceAgentConsumerSession | null>
  >();
  private readonly derived = new Map<string, IComputedValue<unknown>>();
  private readonly pending = new Map<
    string,
    IComputedValue<readonly AgentActivityInteraction[]>
  >();

  constructor(private readonly source?: AgentSessionsSource) {
    makeObservable(this, {
      removeSession: action,
      restoreSession: action,
      consumerSessions: computed({ keepAlive: true }),
      allConsumerSessions: computed({ keepAlive: true }),
      rootSessionIdsWithPendingInteractions: computed({ keepAlive: true })
    });
  }

  removeSession(rawId: string): void {
    const id = key(rawId);
    if (!id) return;
    this.removed.add(id);
    this.consumers.delete(id);
    this.pending.delete(id);
  }

  restoreSession(id: string): void {
    this.removed.delete(key(id));
  }
  session(rawId: Id): CanonicalAgentSession | null {
    const id = key(rawId);
    return this.removed.has(id)
      ? null
      : this.source
        ? this.derive("canonical", id, () => this.native(id)?.session ?? null)
        : null;
  }
  isDeleted(id: Id): boolean {
    return this.removed.has(key(id));
  }
  turns(id: Id): readonly AgentActivityTurn[] {
    return this.source
      ? this.derive(
          "turns",
          key(id),
          () => this.native(key(id))?.turns ?? EMPTY
        )
      : EMPTY;
  }
  turn(id: Id, turnId: Id): AgentActivityTurn | null {
    return this.derive(
      "turn",
      JSON.stringify([key(id), key(turnId)]),
      () => this.turns(id).find((turn) => turn.turnId === key(turnId)) ?? null
    );
  }
  activeTurn(id: Id): AgentActivityTurn | null {
    return this.turn(id, this.session(id)?.activeTurnId);
  }
  latestTurn(id: Id): AgentActivityTurn | null {
    return this.turns(id).at(-1) ?? null;
  }
  interactions(id: Id): readonly AgentActivityInteraction[] {
    return this.source
      ? this.derive(
          "interactions",
          key(id),
          () => this.native(key(id))?.interactions ?? EMPTY
        )
      : EMPTY;
  }
  interaction(
    id: Id,
    turnId: Id,
    requestId: Id
  ): AgentActivityInteraction | null {
    if (!this.turn(id, turnId)) return null;
    return (
      this.interactions(id).find(
        (value) =>
          value.turnId === key(turnId) && value.requestId === key(requestId)
      ) ?? null
    );
  }
  pendingInteractions(rawId: Id): readonly AgentActivityInteraction[] {
    const id = key(rawId);
    let value = this.pending.get(id);
    if (!value) {
      value = computed(
        () =>
          this.interactions(id).filter(
            (interaction) =>
              interaction.status === "pending" &&
              this.turn(id, interaction.turnId)
          ),
        { equals: comparer.shallow, keepAlive: true }
      );
      this.pending.set(id, value);
    }
    return value.get();
  }
  hasPendingInteractions(id: Id): boolean {
    return this.pendingInteractions(id).length > 0;
  }
  runtimeAvailability(id: Id): SessionRuntimeAvailability | null {
    return this.source ? this.source.availability(key(id)) : null;
  }
  runtimeActivity(_id: Id): "idle" | "running" {
    return "idle";
  }
  submitAvailability(id: Id): AgentSubmitAvailability | null {
    return this.derive("submit", key(id), () =>
      this.readSubmitAvailability(id)
    );
  }
  private readSubmitAvailability(id: Id): AgentSubmitAvailability | null {
    if (!this.session(id)) return null;
    const runtime = this.runtimeAvailability(id);
    if (runtime?.state === "blocked") return runtime;
    if (this.interactions(id).some((value) => value.status === "pending"))
      return { state: "blocked", reason: "waiting" };
    const turn = this.activeTurn(id);
    return turn && turn.phase !== "settled"
      ? { state: "blocked", reason: "active_turn" }
      : { state: "available" };
  }
  consumerSession(rawId: Id): WorkspaceAgentConsumerSession | null {
    const id = key(rawId);
    let value = this.consumers.get(id);
    if (!value) {
      value = computed(
        () => {
          const session = this.session(id);
          if (!session) return null;
          const activeTurn = this.activeTurn(id),
            latestTurn = this.latestTurn(id);
          const pendingInteractions = this.pendingInteractions(id);
          let displayStatus: WorkspaceAgentConsumerSession["displayStatus"] =
            "idle";
          if (pendingInteractions.length) displayStatus = "waiting";
          else if (activeTurn && activeTurn.phase !== "settled")
            displayStatus =
              activeTurn.phase === "waiting" ? "waiting" : "working";
          else if (latestTurn?.phase === "settled") {
            if (latestTurn.outcome === "failed") displayStatus = "failed";
            else if (
              latestTurn.outcome === "canceled" ||
              latestTurn.outcome === "interrupted"
            )
              displayStatus = "canceled";
            else if (latestTurn.outcome === "completed")
              displayStatus = "completed";
          }
          return {
            session,
            activeTurn,
            latestTurn,
            pendingInteractions,
            displayStatus
          };
        },
        { keepAlive: true }
      );
      this.consumers.set(id, value);
    }
    return value.get();
  }
  protected sessionIds(): readonly string[] {
    return this.source?.ids() ?? EMPTY;
  }
  get allConsumerSessions(): readonly WorkspaceAgentConsumerSession[] {
    return this.sessionIds()
      .filter((id) => !this.removed.has(id))
      .map((id) => this.consumerSession(id))
      .filter(
        (value): value is WorkspaceAgentConsumerSession => value !== null
      );
  }
  get consumerSessions(): readonly WorkspaceAgentConsumerSession[] {
    return this.allConsumerSessions.filter(
      ({ session }) => session.kind === "root" && session.visible !== false
    );
  }
  get rootSessionIdsWithPendingInteractions(): readonly string[] {
    return [
      ...new Set(
        this.allConsumerSessions
          .map(({ session }) => session)
          .filter((session) =>
            this.hasPendingInteractions(session.agentSessionId)
          )
          .map((session) =>
            session.kind === "child"
              ? session.rootAgentSessionId
              : session.agentSessionId
          )
          .filter((id): id is string => Boolean(id))
      )
    ];
  }
  presentedSession(rawId: Id): AgentActivitySession | null {
    const id = key(rawId);
    return this.derive("session", id, () => {
      const session = this.session(id);
      if (!session) return null;
      const latestTurn = this.latestTurn(id);
      return {
        ...session,
        activeTurn: this.activeTurn(id),
        latestTurn,
        latestTurnInteractions: latestTurn
          ? this.interactions(id).filter(
              (value) => value.turnId === latestTurn.turnId
            )
          : EMPTY,
        pendingInteractions: this.pendingInteractions(id)
      };
    });
  }
  family(rawId: Id): AgentSessionFamilySnapshot {
    const id = key(rawId);
    return this.derive("family", id, () => {
      const children = this.allConsumerSessions
        .filter(
          ({ session }) =>
            session.kind === "child" && session.rootAgentSessionId === id
        )
        .map(({ session }) => this.presentedSession(session.agentSessionId)!);
      const rootSession = this.presentedSession(id);
      return {
        rootSession,
        childSessions: children,
        pendingInteractions: [rootSession, ...children]
          .flatMap((session) => session?.pendingInteractions ?? EMPTY)
          .sort(
            (a, b) =>
              a.createdAtUnixMs - b.createdAtUnixMs ||
              a.updatedAtUnixMs - b.updatedAtUnixMs ||
              a.agentSessionId.localeCompare(b.agentSessionId) ||
              a.turnId.localeCompare(b.turnId) ||
              a.requestId.localeCompare(b.requestId)
          )
      };
    });
  }
  snapshot(workspaceId: string): AgentActivitySnapshot {
    return this.derive("snapshot", workspaceId, () => ({
      workspaceId,
      presences: [],
      sessions: this.allConsumerSessions.map(
        ({ session }) => this.presentedSession(session.agentSessionId)!
      )
    }));
  }
  private native(id: string) {
    return this.derive("native", id, () => {
      const source = this.source!;
      const metadata = source.metadata(id);
      source.commitVersion(id);
      if (!metadata || this.removed.has(id)) return null;
      // Messages and turn state are read at the same commit boundary. Raw events can
      // arrive during the cadence interval without publishing half a presentation.
      return untracked(() => {
        const normalized = normalizeAgentActivitySession(metadata);
        const conversation = source.conversation(id);
        const native = conversation
          ? readNativeSessionTurns(conversation, { agentSessionId: id })
          : null;
        const turns = native
          ? native.turns.map((turn) => ({
              ...turn,
              providerForkBindingAvailable:
                normalized.lifecycleCapabilities.forkThroughTurn === true
            }))
          : [normalized.activeTurn, normalized.latestTurn].filter(
              (turn): turn is AgentActivityTurn => Boolean(turn)
            );
        const interactions = native?.interactions ?? [
          ...normalized.latestTurnInteractions,
          ...normalized.pendingInteractions
        ];
        const {
          activeTurn,
          latestTurn,
          latestTurnInteractions,
          pendingInteractions,
          ...session
        } = normalized;
        const projected = {
          ...session,
          ...(native ? { activeTurnId: native.activeTurnId } : {})
        };
        return {
          session: projected,
          turns: this.ordered([
            ...new Map(turns.map((turn) => [turn.turnId, turn])).values()
          ]),
          interactions
        };
      });
    });
  }

  private derive<T>(kind: string, id: string, read: () => T): T {
    const identity = JSON.stringify([kind, id]);
    let value = this.derived.get(identity) as IComputedValue<T> | undefined;
    if (!value) {
      value = computed(read, { keepAlive: true, equals: comparer.structural });
      this.derived.set(identity, value);
    }
    return value.get();
  }
  private ordered(turns: AgentActivityTurn[]) {
    return turns.sort(
      (a, b) =>
        a.startedAtUnixMs - b.startedAtUnixMs ||
        a.turnId.localeCompare(b.turnId)
    );
  }
}
