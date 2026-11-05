import {
  normalizeAgentActivitySession,
  type AgentActivityInteraction,
  type AgentActivitySessionInput,
  type AgentActivityTurn,
  type CanonicalAgentSession,
  type SessionRuntimeAvailability
} from "@tutti-os/agent-activity-core";
import { action, comparer, makeObservable, observable } from "mobx";
import { AgentSessionsModel } from "../../state/AgentSessionsModel";
const EMPTY: readonly never[] = Object.freeze([]);
const key = (id: string | null | undefined) => id?.trim() ?? "";
/** Controlled session data for tests; production models only derive native state. */
export class TestAgentSessionsModel extends AgentSessionsModel {
  private readonly sessions = observable.map<string, CanonicalAgentSession>(
    [],
    { deep: false }
  );
  private readonly turnsBySession = observable.map<
    string,
    readonly AgentActivityTurn[]
  >([], { deep: false });
  private readonly interactionsBySession = observable.map<
    string,
    readonly AgentActivityInteraction[]
  >([], { deep: false });
  private readonly availability = observable.map<
    string,
    SessionRuntimeAvailability
  >([], { deep: false });

  constructor() {
    super();
    makeObservable(this, {
      upsertSession: action,
      upsertSessions: action,
      replaceHistory: action,
      setRuntimeAvailability: action
    });
  }
  upsertSessions(sources: readonly AgentActivitySessionInput[]): void {
    for (const source of sources) this.upsertSession(source);
  }

  upsertSession(source: AgentActivitySessionInput): void {
    const id = key(source.agentSessionId);
    if (!id || this.isDeleted(id)) return;
    const normalized = normalizeAgentActivitySession(source);
    const {
      activeTurn,
      latestTurn,
      latestTurnInteractions,
      pendingInteractions,
      ...session
    } = normalized;
    this.write(this.sessions, id, session);
    const turns = new Map(this.turns(id).map((turn) => [turn.turnId, turn]));
    for (const turn of [activeTurn, latestTurn]) {
      if (turn?.agentSessionId === id)
        turns.set(
          turn.turnId,
          comparer.structural(turns.get(turn.turnId), turn)
            ? turns.get(turn.turnId)!
            : turn
        );
    }
    this.write(
      this.turnsBySession,
      id,
      this.orderFixtureTurns([...turns.values()])
    );
    const incoming = new Map<string, AgentActivityInteraction>();
    for (const interaction of [
      ...latestTurnInteractions,
      ...pendingInteractions
    ]) {
      if (interaction.agentSessionId === id)
        incoming.set(this.interactionKey(interaction), interaction);
    }
    for (const interaction of this.interactionsBySession.get(id) ?? EMPTY) {
      const identity = this.interactionKey(interaction);
      if (interaction.status !== "pending" && !incoming.has(identity))
        incoming.set(identity, interaction);
    }
    this.write(
      this.interactionsBySession,
      id,
      [...incoming.values()].sort(
        (a, b) =>
          a.createdAtUnixMs - b.createdAtUnixMs ||
          a.requestId.localeCompare(b.requestId)
      )
    );
    if (!this.availability.has(id))
      this.availability.set(id, { state: "available" });
  }

  replaceHistory(
    source: AgentActivitySessionInput,
    turns: readonly AgentActivityTurn[]
  ): void {
    const id = key(source.agentSessionId);
    this.upsertSession(source);
    if (!id || this.isDeleted(id)) return;
    const previous = new Map(this.turns(id).map((turn) => [turn.turnId, turn]));
    const incoming = new Map(
      turns
        .filter((turn) => key(turn.agentSessionId) === id && key(turn.turnId))
        .map((turn) => [
          turn.turnId,
          comparer.structural(previous.get(turn.turnId), turn)
            ? previous.get(turn.turnId)!
            : turn
        ])
    );
    this.write(
      this.turnsBySession,
      id,
      this.orderFixtureTurns([...incoming.values()])
    );
    this.write(
      this.interactionsBySession,
      id,
      (this.interactionsBySession.get(id) ?? EMPTY).filter((interaction) =>
        incoming.has(interaction.turnId)
      )
    );
  }

  setRuntimeAvailability(id: string, value: SessionRuntimeAvailability): void {
    if (this.sessions.has(key(id)))
      this.write(this.availability, key(id), value);
  }

  override session(rawId: string | null | undefined) {
    const id = key(rawId);
    return this.isDeleted(id) ? null : (this.sessions.get(id) ?? null);
  }
  override turns(id: string | null | undefined): readonly AgentActivityTurn[] {
    return this.turnsBySession.get(key(id)) ?? EMPTY;
  }
  override interactions(
    id: string | null | undefined
  ): readonly AgentActivityInteraction[] {
    return this.interactionsBySession.get(key(id)) ?? EMPTY;
  }
  override runtimeAvailability(id: string | null | undefined) {
    return this.availability.get(key(id)) ?? null;
  }
  protected override sessionIds(): readonly string[] {
    return [...this.sessions.keys()];
  }
  override removeSession(id: string) {
    super.removeSession(id);
    this.sessions.delete(id);
    this.turnsBySession.delete(id);
    this.interactionsBySession.delete(id);
    this.availability.delete(id);
  }
  private interactionKey(value: AgentActivityInteraction): string {
    return JSON.stringify([value.turnId, value.requestId]);
  }
  private write<T>(map: Map<string, T>, id: string, value: T): void {
    if (!comparer.structural(map.get(id), value)) map.set(id, value);
  }

  private orderFixtureTurns(turns: AgentActivityTurn[]) {
    return turns.sort(
      (a, b) =>
        a.startedAtUnixMs - b.startedAtUnixMs ||
        a.turnId.localeCompare(b.turnId)
    );
  }
}
