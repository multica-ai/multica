import type { LocalV2Scope, LocalV2Transport } from "@multica/core/local-conversation/v2";
import { ConversationServiceManager, type ConversationService } from "@multica/conversation-client/services";
import { createLocalV2SnapshotStore } from "./local-v2-snapshot-store";

export const conversationServices = new ConversationServiceManager({ snapshots: createLocalV2SnapshotStore });

/** The service of a workspace on the device behind a transport. */
export function conversationService(transport: LocalV2Transport, scope: LocalV2Scope): ConversationService {
  return conversationServices.get(transport, scope);
}
