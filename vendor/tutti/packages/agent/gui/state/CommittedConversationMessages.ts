import type {
  ConversationLocale,
  ConversationMessage,
  ConversationModel,
  TranscriptModel
} from "@multica/conversation-client/models";
import type { LocalV2Event } from "@multica/core/local-conversation/v2";
import type { AgentActivityDurableMessage } from "@tutti-os/agent-activity-core";
import { comparer } from "mobx";

/** Which turns need committing again: named ones after events, or everything. */
export type ConversationMessageChanges = ReadonlySet<string> | "all";

/** A sequence is its turn's slot plus its rank within the turn; older turns paged in move nothing. */
const TURN_SLOT = 2 ** 20;
/** Room below a turn's slot for rows no loaded turn accounts for. */
const LOOSE_SLOT = 1024;

interface Committed {
  source: ConversationMessage;
  message: AgentActivityDurableMessage;
}

/**
 * The GUI's messages for one conversation, kept between commits: only the turns
 * that changed are read again, and every other message keeps its object.
 */
export class CommittedConversationMessages {
  private conversation?: ConversationModel;
  private rows = new Map<string, Committed>();
  private byTurn = new Map<string, AgentActivityDurableMessage[]>();

  private nextVersion = 0;

  constructor(
    private readonly model: TranscriptModel<AgentActivityDurableMessage>,
    private readonly workspaceId: string,
    private readonly agentSessionId: string,
    private readonly locale: ConversationLocale,
    private readonly legacyClientSubmitId?: (
      messageId: string,
    ) => string | undefined,
    private readonly submissionIdentity?: (requestId: string) => string,
  ) {}

  update(
    conversation: ConversationModel,
    change: ConversationMessageChanges
  ): AgentActivityDurableMessage[] {
    const messages = this.read(conversation, change);
    this.model.commit(this.agentSessionId, messages);
    return messages;
  }

  private read(
    conversation: ConversationModel,
    change: ConversationMessageChanges
  ): AgentActivityDurableMessage[] {
    const full = change === "all" || conversation !== this.conversation;
    this.conversation = conversation;
    if (full) {
      const previous = this.rows;
      this.rows = new Map();
      this.byTurn = new Map();
      for (const turn of conversation.turns.values())
        this.byTurn.set(
          turn.id,
          this.commit(turn.messages(this.locale), turn.index, previous, false)
        );
    } else {
      for (const turnId of change) {
        const turn = conversation.turns.get(turnId);
        if (!turn) continue;
        const stale = new Set(
          (this.byTurn.get(turnId) ?? []).map((message) => message.messageId)
        );
        const messages = this.commit(
          turn.messages(this.locale),
          turn.index,
          this.rows,
          true
        );
        for (const message of messages) stale.delete(message.messageId);
        for (const messageId of stale) this.rows.delete(messageId);
        this.byTurn.set(turnId, messages);
      }
    }
    const result: AgentActivityDurableMessage[] = [];
    for (const turn of conversation.turns.values()) {
      const messages = this.byTurn.get(turn.id);
      if (messages) for (const message of messages) result.push(message);
    }
    const loose = this.looseMessages(conversation);
    if (!loose.length) return result;
    result.push(...loose);
    return result.sort((a, b) => a.sequence! - b.sequence!);
  }

  /** Placed by time, before the first turn that started after them. */
  private looseMessages(
    conversation: ConversationModel
  ): AgentActivityDurableMessage[] {
    const turns = [...conversation.turns.values()];
    const next = turns.length ? turns.at(-1)!.index + 1 : 0;
    const counts = new Map<number, number>();
    return conversation.looseMessages().map((source) => {
      const index =
        turns.find(
          (turn) => Date.parse(turn.startedAt) >= source.occurredAtUnixMs
        )?.index ?? next;
      const rank = counts.get(index) ?? 0;
      counts.set(index, rank + 1);
      return this.commitOne(
        source,
        (index + 1) * TURN_SLOT - LOOSE_SLOT + rank,
        this.rows,
        false
      );
    });
  }

  private commit(
    sources: readonly ConversationMessage[],
    turnIndex: number,
    previous: ReadonlyMap<string, Committed>,
    changedSinceLast: boolean
  ): AgentActivityDurableMessage[] {
    return sources.map((source, rank) =>
      this.commitOne(
        source,
        (turnIndex + 1) * TURN_SLOT + rank,
        previous,
        changedSinceLast
      )
    );
  }

  private commitOne(
    source: ConversationMessage,
    sequence: number,
    previous: ReadonlyMap<string, Committed>,
    changedSinceLast: boolean
  ): AgentActivityDurableMessage {
    const known = previous.get(source.messageId);
    // Old receipts are consulted only while migrating messages without a native request id.
    const clientSubmitId =
      source.role === "user"
        ? typeof source.payload.clientSubmitId === "string"
          ? (this.submissionIdentity?.(source.payload.clientSubmitId) ??
            source.payload.clientSubmitId)
          : this.legacyClientSubmitId?.(source.messageId)
        : undefined;
    const submitted =
      !clientSubmitId ||
      known?.message.payload.clientSubmitId === clientSubmitId;
    if (known?.source === source && submitted) {
      const message =
        known.message.sequence === sequence
          ? known.message
          : { ...known.message, sequence };
      this.rows.set(source.messageId, { source, message });
      return message;
    }
    const raw = {
      workspaceId: this.workspaceId,
      agentSessionId: this.agentSessionId,
      ...source,
      ...(clientSubmitId
        ? { payload: { ...source.payload, clientSubmitId } }
        : {})
    } as Omit<AgentActivityDurableMessage, "version" | "sequence">;
    // Events said this changed, so reading it to find out would only cost a streamed text's length.
    const previousMessage = known?.message;
    const {
      version: _version,
      sequence: _sequence,
      ...previousRaw
    } = previousMessage ?? {};
    const version =
      !changedSinceLast &&
      previousMessage &&
      comparer.structural(previousRaw, raw)
        ? previousMessage.version
        : ++this.nextVersion;
    const message =
      known?.message.version === version && known.message.sequence === sequence
        ? known.message
        : { ...raw, version, sequence };
    this.rows.set(source.messageId, { source, message });
    return message;
  }
}

/** Adds a batch of events to what is waiting to commit; a change that is not events needs everything. */
export function mergeConversationMessageChanges(
  current: ConversationMessageChanges | undefined,
  events: readonly LocalV2Event[]
): ConversationMessageChanges {
  if (current === "all" || !events.length) return "all";
  const turns = new Set(current);
  for (const event of events) {
    if (event.turn_id) turns.add(event.turn_id);
    else if (event.type === "user.message") return "all";
  }
  return turns;
}
