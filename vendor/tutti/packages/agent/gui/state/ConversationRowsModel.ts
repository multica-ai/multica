import {
  isPendingActivationViable,
  normalizeAgentActivitySession,
  type AgentActivityMessage
} from "@tutti-os/agent-activity-core";
import { comparer, computed, type IComputedValue } from "mobx";
import { projectAgentGUIMessagesToTimelineItems } from "../agent-gui/agentGuiNode/controller/agentGuiController.promptHelpers";
import {
  buildAgentGUIConversationModels,
  conversationSummaryFromAgentSession
} from "../agent-gui/agentGuiNode/model/agentGuiConversationModel";
import type { ConversationServices } from "../conversationServices";
import {
  calculateAgentTranscriptComplexity,
  shouldVirtualizeAgentTranscript
} from "../shared/agentConversation/components/agentTranscriptComplexity";
import {
  buildAgentTranscriptTurnGroups,
  buildTurnGroupIndexByRowIndex,
  buildUserMessageLocatorItems,
  transcriptRowKey
} from "../shared/agentConversation/components/agentTranscriptModel";
import type { AgentConversationVM } from "../shared/agentConversation/contracts/agentConversationVM";
import type { AgentTranscriptRowVM } from "../shared/agentConversation/contracts/agentTranscriptRowVM";
import { readPendingConversationMessages } from "./pendingConversationMessages";
import { measureConversationWork } from "./conversationWorkProfile";

const EMPTY: readonly never[] = Object.freeze([]);
const TAIL = "\u0000pending";

/** One projection per committed turn; layout and row contents have separate dependencies. */
export class ConversationRowsModel {
  private readonly turns = new Map<
    string,
    IComputedValue<AgentConversationVM | null>
  >();
  private readonly structures = new Map<
    string,
    IComputedValue<AgentConversationVM | null>
  >();
  private readonly complexities = new Map<string, IComputedValue<number>>();
  private readonly virtualization: IComputedValue<boolean>;
  private readonly header: IComputedValue<
    ReturnType<ConversationServices["sessions"]["presentedSession"]>
  >;
  private readonly rows = new Map<
    string,
    IComputedValue<AgentTranscriptRowVM | null>
  >();
  private readonly keys = new Map<string, IComputedValue<readonly string[]>>();
  private readonly metadata: IComputedValue<
    ReturnType<ConversationServices["sessions"]["session"]>
  >;
  private readonly order: IComputedValue<readonly string[]>;
  private readonly projected: IComputedValue<AgentConversationVM | null>;
  private readonly layout: IComputedValue<AgentConversationVM | null>;
  private readonly locations: IComputedValue<
    ReturnType<typeof buildUserMessageLocatorItems>
  >;
  private readonly base: IComputedValue<AgentConversationVM | null>;
  private readonly nonempty: IComputedValue<boolean>;
  private readonly rowOwners = new Map<string, string>();

  constructor(
    private readonly runtime: ConversationServices,
    readonly agentSessionId: string,
    private readonly options: {
      workspaceId: string;
      userId?: string | null;
      workspaceRoot?: string;
      avoidGroupingEdits?: boolean;
      totalTurnCount?: () => number;
    }
  ) {
    this.metadata = computed(
      () => {
        const canonical = runtime.sessions.session(agentSessionId);
        const activation = canonical
          ? null
          : runtime.activations.latest(agentSessionId);
        const session =
          canonical ??
          (activation?.mode === "new"
            ? normalizeAgentActivitySession({
                agentSessionId,
                activeTurnId: null,
                workspaceId: options.workspaceId,
                userId: options.userId ?? "user",
                provider: "unknown",
                agentTargetId: activation.agentTargetId,
                title: activation.title ?? "",
                cwd: activation.cwd,
                latestTurnInteractions: [],
                pendingInteractions: []
              })
            : null);
        // A session timestamp changes on every token; it is not a turn projection input.
        return session
          ? {
              ...session,
              updatedAtUnixMs: session.createdAtUnixMs,
              messageVersion: 0,
              activeTurnId: null,
              activeTurn: null,
              latestTurn: null,
              latestTurnInteractions: [],
              pendingInteractions: []
            }
          : null;
      },
      { equals: comparer.structural }
    );
    this.order = computed(
      () => {
        const navigation = runtime.navigation?.(agentSessionId);
        const available = runtime.transcript.turnIds(agentSessionId);
        const visible = navigation ? new Set(navigation.visibleTurnIds()) : null;
        const committed = visible ? available.filter((id) => visible.has(id)) : [...available];
        const active = runtime.sessions.activeTurn(agentSessionId);
        const showLatest = !navigation?.window?.has_after;
        if (showLatest && active && !committed.includes(active.turnId))
          committed.push(active.turnId);
        return showLatest && this.optimisticMessages.length
          ? [...committed, TAIL]
          : committed;
      },
      { equals: comparer.shallow }
    );
    this.header = computed(
      () => {
        const session = runtime.sessions.presentedSession(agentSessionId);
        return session
          ? { ...session, messageVersion: 0, updatedAtUnixMs: 0 }
          : null;
      },
      { equals: comparer.structural }
    );
    this.projected = computed(() =>
      this.assemble(this.turnIds.map((id) => this.turn(id)))
    );
    this.layout = computed(() =>
      this.assemble(this.turnIds.map((id) => this.turnStructure(id)))
    );
    this.base = computed(() => {
      const base = this.project(EMPTY, null);
      const session = this.header.get();
      return base && session
        ? { ...base, sourceDetail: { ...base.sourceDetail, session } }
        : base;
    });
    this.nonempty = computed(() =>
      this.turnIds.some((id) => Boolean(this.turnStructure(id)?.rows.length))
    );
    this.virtualization = computed(() => {
      const scores = this.turnIds.map((id) => this.turnComplexity(id));
      return shouldVirtualizeAgentTranscript({
        turnCount: Math.max(scores.length, options.totalTurnCount?.() ?? 0),
        complexity: {
          totalScore: scores.reduce((sum, score) => sum + score, 0),
          maxTurnScore: Math.max(0, ...scores),
          turnScores: scores
        }
      });
    });
    this.locations = computed(
      () => {
        const rows = this.layout.get()?.rows ?? EMPTY;
        const keys = rows.map(transcriptRowKey);
        return buildUserMessageLocatorItems(
          rows,
          keys,
          buildTurnGroupIndexByRowIndex(
            buildAgentTranscriptTurnGroups(rows, keys)
          )
        );
      },
      { equals: comparer.structural }
    );
  }

  get turnIds(): readonly string[] {
    return this.order.get();
  }
  /** Only layout consumers use this value. Text consumers must resolve a row by key. */
  get structure(): AgentConversationVM | null {
    return this.layout.get();
  }
  get presentationBase(): AgentConversationVM | null {
    return this.base.get();
  }
  get hasRows(): boolean {
    return this.nonempty.get();
  }
  canonicalTurn(id: string) {
    return this.runtime.sessions.turn(this.agentSessionId, id);
  }
  get conversation(): AgentConversationVM | null {
    return this.projected.get();
  }
  get shouldVirtualize(): boolean {
    return this.virtualization.get();
  }
  get locatorItems() {
    return this.locations.get();
  }

  rowKeys(turnId: string): readonly string[] {
    let value = this.keys.get(turnId);
    if (!value) {
      value = computed(
        () =>
          (this.turn(turnId)?.rows ?? EMPTY).map((row) => {
            const key = transcriptRowKey(row);
            this.rowOwners.set(key, turnId);
            return key;
          }),
        { equals: comparer.shallow }
      );
      this.keys.set(turnId, value);
    }
    return value.get();
  }

  row(key: string): AgentTranscriptRowVM | null {
    let value = this.rows.get(key);
    if (!value) {
      value = computed(() => this.readRow(key), {
        equals: comparer.structural
      });
      this.rows.set(key, value);
    }
    return value.get();
  }

  private readRow(key: string): AgentTranscriptRowVM | null {
    let owner = this.rowOwners.get(key);
    if (owner === undefined) {
      for (const id of this.turnIds) {
        if (this.rowKeys(id).includes(key)) {
          owner = id;
          break;
        }
      }
    }
    return owner === undefined
      ? null
      : (this.turn(owner)?.rows.find((row) => transcriptRowKey(row) === key) ??
          null);
  }

  /** Work sections split message rows; resolve their exact message ids against the current row. */
  resolve(descriptor: AgentTranscriptRowVM): AgentTranscriptRowVM | null {
    const row = this.row(transcriptRowKey(descriptor));
    if (!row) return null;
    if (row.kind !== "message" || descriptor.kind !== "message") return row;
    const messageIds = new Set(
      descriptor.messages.map((message) => message.id)
    );
    const thinkingIds = new Set(
      descriptor.thinking.map((message) => message.id)
    );
    return {
      ...row,
      messages: row.messages.filter((message) => messageIds.has(message.id)),
      thinking: row.thinking.filter((message) => thinkingIds.has(message.id)),
      ...(descriptor.leadingToolRows
        ? {
            leadingToolRows: descriptor.leadingToolRows
              .map((tool) => this.resolve(tool))
              .filter(
                (tool): tool is (typeof descriptor.leadingToolRows)[number] =>
                  tool?.kind === "tool-group"
              )
          }
        : {})
    };
  }

  private assemble(
    parts: readonly (AgentConversationVM | null)[]
  ): AgentConversationVM | null {
    const present = parts.filter(
      (value): value is AgentConversationVM => value !== null
    );
    const last = present.at(-1) ?? this.project(EMPTY, null);
    if (!last) return null;
    const session = this.header.get();
    return {
      ...last,
      rows: present.flatMap((part) => part.rows),
      sourceDetail: {
        ...last.sourceDetail,
        ...(session ? { session } : {}),
        sessionTurns: [...this.runtime.sessions.turns(this.agentSessionId)],
        turns: present.flatMap((part) => part.sourceDetail.turns)
      }
    };
  }

  turnStructure(id: string): AgentConversationVM | null {
    let value = this.structures.get(id);
    if (!value) {
      value = computed(() => this.turn(id), { equals: sameLayout });
      this.structures.set(id, value);
    }
    return value.get();
  }

  private turnComplexity(id: string): number {
    let value = this.complexities.get(id);
    if (!value) {
      value = computed(() => {
        const rows = this.turn(id)?.rows ?? EMPTY;
        return measureConversationWork(this.agentSessionId, "complexity", () => calculateAgentTranscriptComplexity([
          { rows: rows.map((row) => ({ row })) }
        ]).totalScore);
      });
      this.complexities.set(id, value);
    }
    return value.get();
  }

  private turn(id: string): AgentConversationVM | null {
    let value = this.turns.get(id);
    if (!value) {
      value = computed(() => {
        const messages =
          id === TAIL
            ? this.optimisticMessages
            : this.runtime.transcript.turnMessages(this.agentSessionId, id);
        const canonical = this.runtime.sessions.turn(this.agentSessionId, id);
        return this.project(messages, canonical);
      });
      this.turns.set(id, value);
    }
    return value.get();
  }

  private project(
    messages: readonly AgentActivityMessage[],
    turn: ReturnType<ConversationServices["sessions"]["turn"]>
  ): AgentConversationVM | null {
    const session = this.metadata.get();
    if (!session) return null;
    const active = turn?.phase !== "settled" ? turn : null;
    const source = {
      ...session,
      activeTurn: active,
      latestTurn: turn,
      activeTurnId: active?.turnId ?? null,
      latestTurnInteractions: [],
      pendingInteractions: []
    };
    return measureConversationWork(this.agentSessionId, "rows_projection", () => buildAgentGUIConversationModels({
      conversation: {
        ...conversationSummaryFromAgentSession(session),
        activeTurn: active,
        sessionTurns: turn ? [turn] : []
      },
      canonicalSession: source,
      timelineItems: projectAgentGUIMessagesToTimelineItems(messages),
      workspaceRoot: this.options.workspaceRoot,
      avoidGroupingEdits: this.options.avoidGroupingEdits
    }).conversation);
  }

  private get optimisticMessages(): readonly AgentActivityMessage[] {
    const runtime = this.runtime,
      id = this.agentSessionId;
    const activation = runtime.activations.latest(id);
    const submissions = runtime.prompts.pendingSubmits(id);
    if (
      !submissions.length &&
      (!activation ||
        activation.mode !== "new" ||
        !isPendingActivationViable(activation))
    )
      return EMPTY;
    if (!submissions.length && activation?.initialPromptRetracted) return EMPTY;
    const storedMessages = runtime.transcript.messages(id);
    return readPendingConversationMessages({
      activeConversationId: id,
      activePendingActivation: activation,
      activePendingSubmits: submissions,
      activeQueuedPrompts: runtime.prompts.visibleQueuedPrompts(id),
      currentUserId: this.options.userId,
      storedMessages,
      workspaceId: this.options.workspaceId
    }).activeMessages.filter(
      (message) => message.payload.__agentGuiOptimisticPrompt === true
    );
  }
}

/** Layout depends on semantic structure, never on streamed assistant/tool text. */
function layoutSignature(conversation: AgentConversationVM | null): unknown {
  if (!conversation) return null;
  return {
    session: {
      ...conversation.sourceDetail.session,
      messageVersion: 0,
      updatedAtUnixMs: 0
    },
    turns: conversation.sourceDetail.sessionTurns,
    cwd: conversation.sourceDetail.cwd,
    workspaceRoot: conversation.workspaceRoot,
    rows: conversation.rows.map((row) => {
      if (row.kind !== "message")
        return {
          id: row.id,
          key: transcriptRowKey(row),
          kind: row.kind,
          turnId: row.turnId
        };
      return {
        id: row.id,
        kind: row.kind,
        turnId: row.turnId,
        speaker: row.speaker,
        editable: row.rawFirstTextBlock != null,
        thinking: row.thinking.map((message) => message.id),
        messages: row.messages.map((message) => ({
          id: message.id,
          body:
            row.speaker === "user"
              ? message.body
              : Boolean(message.body.trim()),
          final: message.isTurnFinalText,
          contentKind: message.contentKind,
          presentation: message.presentationKind,
          error: Boolean(message.visibleError),
          notice: Boolean(message.systemNotice),
          time: row.speaker === "user" ? message.occurredAtUnixMs : null
        }))
      };
    })
  };
}
function sameLayout(
  a: AgentConversationVM | null,
  b: AgentConversationVM | null
): boolean {
  return comparer.structural(layoutSignature(a), layoutSignature(b));
}
