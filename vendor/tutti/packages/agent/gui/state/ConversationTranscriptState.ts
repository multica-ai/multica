import { computed, makeObservable, type IComputedValue } from "mobx";
import type { AgentGUITranscriptReader } from "../conversationServices";

const models = new WeakMap<
  AgentGUITranscriptReader,
  Map<string, ConversationTranscriptState>
>();

/** Metadata reads never subscribe a page to streamed assistant text. */
export class ConversationTranscriptState {
  private readonly userTurns = new Map<string, IComputedValue<boolean>>();
  constructor(
    private readonly transcript: AgentGUITranscriptReader,
    readonly agentSessionId: string
  ) {
    makeObservable(this, {
      oldestLoadedVersion: computed,
      hasOlderMessages: computed,
      hasUserMessage: computed
    });
  }
  get oldestLoadedVersion() {
    let oldest: number | null = null;
    for (const message of this.transcript.messages(this.agentSessionId)) {
      if (
        Number.isFinite(message.version) &&
        (oldest === null || message.version < oldest)
      )
        oldest = message.version;
    }
    return oldest;
  }
  get hasOlderMessages() {
    return this.transcript.hasOlderMessages(this.agentSessionId);
  }
  get hasUserMessage() {
    return this.transcript.turnIds(this.agentSessionId).some((turnId) => {
      let value = this.userTurns.get(turnId);
      if (!value) {
        value = computed(() =>
          this.transcript
            .turnMessages(this.agentSessionId, turnId)
            .some((message) => message.role === "user")
        );
        this.userTurns.set(turnId, value);
      }
      return value.get();
    });
  }
}
export function conversationTranscriptStateFor(
  transcript: AgentGUITranscriptReader,
  id: string
): ConversationTranscriptState {
  let scope = models.get(transcript);
  if (!scope) {
    scope = new Map();
    models.set(transcript, scope);
  }
  let state = scope.get(id);
  if (!state) {
    state = new ConversationTranscriptState(transcript, id);
    scope.set(id, state);
  }
  return state;
}
