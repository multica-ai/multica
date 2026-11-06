import { comparer, computed, observable, runInAction, type IComputedValue, type IObservableValue } from "mobx";
import { providerForkBindingAllowsAttempt, type AgentActivityTurn } from "@tutti-os/agent-activity-core";
import type { ConversationRowsModel } from "./ConversationRowsModel";
import type { AgentConversationVM } from "../shared/agentConversation/contracts/agentConversationVM";
import {
  attachLeadingToolRowsToFollowingMessages,
  buildAgentParticipantTurnProjection,
  buildUserMessageLocatorItems,
  findTurnDividerRowIndexes,
  transcriptRowKey,
  transcriptRowEntryKey,
  type AgentTranscriptTurnGroup
} from "../shared/agentConversation/components/agentTranscriptModel";
import {
  buildAgentTurnWorkSectionModel,
  findParticipantHeaderRenderKeys
} from "../shared/agentConversation/components/agentTurnWorkSectionModel";
import { assessAgentTranscriptComplexity } from "../shared/agentConversation/components/agentTranscriptComplexity";
import { measureConversationWork } from "./conversationWorkProfile";

type Row = AgentConversationVM["rows"][number];
type Section = ReturnType<typeof buildAgentTurnWorkSectionModel>;
export type TranscriptGroupIdentity = Pick<AgentTranscriptTurnGroup, "key" | "turnId">;
interface PresentationSource {
  parts(): readonly AgentConversationVM[];
  base(): AgentConversationVM | null;
  turn(id: string): AgentActivityTurn | null | undefined;
  virtualized?: () => boolean;
  rowsModel?: ConversationRowsModel;
}
interface Partition {
  groups: Map<string, AgentTranscriptTurnGroup>;
  offsets: Map<string, number>;
  rows: Row[];
  turns: AgentConversationVM["sourceDetail"]["turns"];
  entering: ReadonlySet<string>;
  entryKeys: ReadonlyMap<string, string>;
}

export class TranscriptPresentationState {
  readonly expandedTools = new Map<string, IObservableValue<boolean>>();
  readonly presentedEntries = observable.map<string, true>([], { deep: false });
  previousEntryKeys: Set<string> | undefined;
}

/** Derived presentation only; the existing conversation models remain the data authority. */
export class TranscriptPresentationModel {
  private readonly groups = new Map<string, IComputedValue<AgentTranscriptTurnGroup | undefined>>();
  private readonly offsets = new Map<string, IComputedValue<number>>();
  private readonly activeTurns = new Map<string, IComputedValue<boolean>>();
  private readonly timingTurns = new Map<string, IComputedValue<AgentActivityTurn | null | undefined>>();
  private readonly inputs = new Map<string, IComputedValue<readonly [AgentTranscriptTurnGroup | undefined, AgentActivityTurn | null | undefined, boolean, boolean]>>();
  private readonly sections = new Map<string, IComputedValue<Section>>();
  private readonly dividers = new Map<string, IComputedValue<boolean>>();
  private readonly forks = new Map<string, IComputedValue<string | null>>();
  private readonly headers = new Map<string, Map<string, IComputedValue<boolean>>>();
  private readonly rowDividers = new Map<string, Map<string, IComputedValue<boolean>>>();
  private readonly enteringRows = new Map<string, Map<string, IComputedValue<boolean>>>();
  private readonly identities = new Map<string, TranscriptGroupIdentity>();
  private readonly attachedRows = new WeakMap<Row, Row>();
  private readonly expandedTools;
  private previous: Partition | undefined;

  private readonly partition: IComputedValue<Partition>;
  private readonly ordered: IComputedValue<readonly TranscriptGroupIdentity[]>;
  private readonly contextValue;
  private readonly locations;
  private readonly dividerIndexes;
  private readonly participantHeaders;

  static fromRows(rows: ConversationRowsModel, participantHeaders: boolean, state?: TranscriptPresentationState) {
    return new TranscriptPresentationModel({
      parts: () => rows.turnIds.map((id) => rows.turnStructure(id)).filter((part): part is AgentConversationVM => part !== null),
      base: () => rows.presentationBase,
      turn: (id) => rows.canonicalTurn(id),
      virtualized: () => rows.shouldVirtualize,
      rowsModel: rows
    }, rows.agentSessionId, participantHeaders, state);
  }

  static fromConversation(conversation: AgentConversationVM, participantHeaders: boolean, state?: TranscriptPresentationState) {
    const turns = new Map(conversation.sourceDetail.sessionTurns?.map((turn) => [turn.turnId, turn]));
    return new TranscriptPresentationModel({
      parts: () => [conversation], base: () => conversation, turn: (id) => turns.get(id)
    }, conversation.sourceDetail.session.agentSessionId, participantHeaders, state);
  }

  constructor(private readonly source: PresentationSource, readonly sessionId: string, readonly participantMode: boolean, private readonly state = new TranscriptPresentationState()) {
    this.expandedTools = state.expandedTools;
    this.partition = computed(() => {
      const parts = source.parts();
      return measureConversationWork(sessionId, "partition", () => this.partitionParts(parts));
    });
    this.ordered = computed(() => [...this.partition.get().groups.values()].map((group) => {
      let identity = this.identities.get(group.key);
      if (!identity || identity.turnId !== group.turnId) {
        identity = { key: group.key, turnId: group.turnId };
        this.identities.set(group.key, identity);
      }
      return identity;
    }), { equals: comparer.shallow });
    this.contextValue = computed(() => {
      const base = source.base();
      return {
        cwd: base?.sourceDetail.cwd ?? "",
        workspaceRoot: base?.workspaceRoot ?? null,
        provider: base?.activity.agentProvider,
        imported: Boolean(base?.sourceDetail.session.imported),
        canFork: base?.sourceDetail.session.kind === "root" && base.sourceDetail.session.lifecycleCapabilities.forkThroughTurn === true
      };
    }, { equals: comparer.shallow });
    this.locations = computed(() => {
      const partition = this.partition.get();
      return measureConversationWork(sessionId, "locator", () => {
        const indexes = new Map<number, number>();
        [...partition.groups.values()].forEach((group, groupIndex) => {
          const offset = partition.offsets.get(group.key)!;
          group.rows.forEach(({ rowIndex }) => indexes.set(offset + rowIndex, groupIndex));
        });
        return buildUserMessageLocatorItems(partition.rows, partition.rows.map(transcriptRowKey), indexes);
      });
    }, { equals: comparer.structural });
    this.dividerIndexes = computed(() => {
      const { rows, turns } = this.partition.get();
      return participantMode
        ? buildAgentParticipantTurnProjection(rows).dividerRowIndexes
        : findTurnDividerRowIndexes(new Map(turns.map((turn, index) => [turn.id, index])), rows);
    });
    this.participantHeaders = computed(() => {
      if (!participantMode) return new Set<string>();
      const partition = this.partition.get();
      const sections = new Map([...partition.groups.keys()].map((key) => [key, this.workSection(key)]));
      return measureConversationWork(sessionId, "participant_headers", () => findParticipantHeaderRenderKeys(
        [...partition.groups.values()], partition.rows.map(transcriptRowKey), sections,
        buildAgentParticipantTurnProjection(partition.rows).turnIndexByRowIndex, partition.offsets
      ));
    }, { equals: comparer.shallow });
  }

  get viewportGroups() { return this.ordered.get(); }
  get context() { return this.contextValue.get(); }
  get locatorItems() { return this.locations.get(); }
  get rowsModel() { return this.source.rowsModel; }
  get shouldVirtualize() {
    return this.source.virtualized?.() ?? assessAgentTranscriptComplexity([...this.partition.get().groups.values()]).shouldVirtualize;
  }

  group(key: string) {
    return this.read(this.groups, key, () => this.partition.get().groups.get(key));
  }
  rowOffset(key: string) {
    return this.read(this.offsets, key, () => this.partition.get().offsets.get(key) ?? 0);
  }
  isActive(key: string) {
    return this.read(this.activeTurns, key, () => {
      const id = this.group(key)?.turnId;
      return Boolean(id && id === this.source.base()?.sourceDetail.session.activeTurnId && this.source.turn(id)?.phase !== "settled");
    });
  }
  isCompleted(key: string): boolean {
    const id = this.group(key)?.turnId;
    const turn = id ? this.source.turn(id) : null;
    return turn?.phase === "settled" && turn.outcome === "completed";
  }
  workSection(key: string): Section {
    return this.read(this.sections, key, () => {
      const [group, turn, active, imported] = this.read(this.inputs, key, () => {
        const turn = this.read(this.timingTurns, key, () => {
          const id = this.group(key)?.turnId;
          return id ? this.source.turn(id) : null;
        }, sameTiming);
        return [this.group(key), turn, this.isActive(key), this.context.imported] as const;
      }, comparer.shallow);
      return group ? measureConversationWork(this.sessionId, "work_section", () => buildAgentTurnWorkSectionModel(group, turn, active, { collapseIntermediateAssistantReplies: !imported })) : null;
    });
  }
  showDivider(key: string) {
    return this.read(this.dividers, key, () => {
      const offset = this.rowOffset(key);
      return this.group(key)?.rows.some(({ rowIndex }) => this.dividerIndexes.get().has(offset + rowIndex)) ?? false;
    });
  }
  headerShown(groupKey: string, renderKey: string) {
    let headers = this.headers.get(groupKey);
    if (!headers) this.headers.set(groupKey, headers = new Map());
    return this.read(headers, renderKey, () => this.participantHeaders.get().has(renderKey));
  }
  rowHasDivider(groupKey: string, rowIndex: number) {
    let dividers = this.rowDividers.get(groupKey);
    if (!dividers) this.rowDividers.set(groupKey, dividers = new Map());
    return this.read(dividers, String(rowIndex), () => this.dividerIndexes.get().has(this.rowOffset(groupKey) + rowIndex));
  }
  rowEntering(groupKey: string, renderKey: string) {
    let rows = this.enteringRows.get(groupKey);
    if (!rows) this.enteringRows.set(groupKey, rows = new Map());
    return this.read(rows, renderKey, () => {
      const partition = this.partition.get();
      const entryKey = partition.entryKeys.get(renderKey);
      return partition.entering.has(renderKey) && entryKey !== undefined && !this.state.presentedEntries.has(entryKey);
    });
  }
  markRowPresented(row: Row) {
    const entryKey = transcriptRowEntryKey(row, transcriptRowKey(row));
    runInAction(() => this.state.presentedEntries.set(entryKey, true));
  }
  forkTurnId(key: string) {
    return this.read(this.forks, key, () => {
      const id = this.group(key)?.turnId;
      const turn = id ? this.source.turn(id) : null;
      return this.context.canFork && turn?.phase === "settled" && providerForkBindingAllowsAttempt(turn) ? id! : null;
    });
  }
  private toolState(key: string) {
    let state = this.expandedTools.get(key);
    if (!state) this.expandedTools.set(key, state = observable.box(false));
    return state;
  }
  toolExpanded(key: string) { return this.toolState(key).get(); }
  setToolExpanded(key: string, expanded: boolean) {
    runInAction(() => this.toolState(key).set(expanded));
  }

  private read<T>(cache: Map<string, IComputedValue<T>>, key: string, derive: () => T, equals?: (a: T, b: T) => boolean): T {
    let value = cache.get(key);
    if (!value) {
      value = computed(derive, { equals });
      cache.set(key, value);
    }
    return value.get();
  }

  private partitionParts(parts: readonly AgentConversationVM[]): Partition {
    const originalRows = parts.flatMap((part) => part.rows);
    const rows = this.participantMode ? this.attachTools(originalRows) : originalRows;
    const nextTurns: (string | null)[] = new Array(rows.length);
    let nextTurn: string | null = null;
    for (let index = rows.length - 1; index >= 0; index--) {
      nextTurns[index] = nextTurn;
      if (rows[index]!.turnId !== null) nextTurn = rows[index]!.turnId;
    }
    const groups = new Map<string, AgentTranscriptTurnGroup>();
    const offsets = new Map<string, number>();
    let current: { key: string; turnId: string | null; rows: Row[] } | undefined;
    const finish = () => {
      if (!current) return;
      const old = this.previous?.groups.get(current.key);
      const same = old && old.turnId === current.turnId && old.rows.length === current.rows.length &&
        old.rows.every((entry, index) => entry.row === current!.rows[index]);
      groups.set(current.key, same ? old : {
        key: current.key, turnId: current.turnId,
        rows: current.rows.map((row, rowIndex) => ({ row, rowIndex }))
      });
    };
    rows.forEach((row, index) => {
      const turnId = row.turnId ?? (current?.turnId && nextTurns[index] === current.turnId ? current.turnId : null);
      if (!current || current.turnId !== turnId) {
        finish();
        const baseKey = turnId ?? `orphan:${transcriptRowKey(row)}`;
        const key = groups.has(baseKey) ? `${baseKey}:segment:${encodeURIComponent(transcriptRowKey(row))}` : baseKey;
        current = { key, turnId, rows: [] };
        offsets.set(current.key, index);
      }
      current.rows.push(row);
    });
    finish();
    const removed = [...(this.previous?.groups.keys() ?? [])].filter((key) => !groups.has(key));
    for (const key of removed) {
      for (const cache of [this.groups, this.offsets, this.activeTurns, this.timingTurns, this.inputs, this.sections, this.dividers, this.forks, this.identities, this.headers, this.rowDividers, this.enteringRows]) cache.delete(key);
    }
    // A presentation window excludes history without deleting its conversation-owned UI state.
    const turns = parts.flatMap((part) => part.sourceDetail.turns);
    const previous = this.previous;
    if (previous && comparer.shallow([...previous.groups.keys()], [...groups.keys()]) &&
        [...groups].every(([key, group]) => previous.groups.get(key) === group && previous.offsets.get(key) === offsets.get(key)) &&
        comparer.shallow(previous.turns.map((turn) => turn.id), turns.map((turn) => turn.id))) return previous;
    const entering = new Set<string>();
    const entryKeys = new Set<string>();
    const entryKeysByRow = new Map<string, string>();
    for (const row of rows) {
      const key = transcriptRowKey(row);
      const identity = transcriptRowEntryKey(row, key);
      entryKeys.add(identity);
      entryKeysByRow.set(key, identity);
      if (this.state.previousEntryKeys && !this.state.previousEntryKeys.has(identity)) entering.add(key);
    }
    this.state.previousEntryKeys = entryKeys;
    return this.previous = { groups, offsets, rows, turns, entering, entryKeys: entryKeysByRow };
  }

  private attachTools(rows: Row[]): Row[] {
    const originals = new Map(rows.map((row) => [transcriptRowKey(row), row]));
    return attachLeadingToolRowsToFollowingMessages(rows).map((row) => {
      const original = originals.get(transcriptRowKey(row))!;
      if (original === row || row.kind !== "message") return row;
      const old = this.attachedRows.get(original);
      if (old?.kind === "message" && comparer.shallow(old.leadingToolRows, row.leadingToolRows)) return old;
      this.attachedRows.set(original, row);
      return row;
    });
  }
}

function sameTiming(a: AgentActivityTurn | null | undefined, b: AgentActivityTurn | null | undefined) {
  return a === b || (a?.turnId === b?.turnId && a?.phase === b?.phase && a?.outcome === b?.outcome &&
    a?.startedAtUnixMs === b?.startedAtUnixMs && a?.settledAtUnixMs === b?.settledAtUnixMs);
}
