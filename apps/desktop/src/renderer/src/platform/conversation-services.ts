import type { LocalV2Scope, LocalV2Transport } from "@multica/core/local-conversation/v2";
import { verificationRenderer } from "#verification-dev/renderer";
import { ConversationServiceManager, type ConversationService } from "@multica/conversation-client/services";
import { createLocalV2SnapshotStore } from "./local-v2-snapshot-store";
import { api } from "@multica/core/api";
import { createCompletionReadStorage } from "./local-completion-storage";

export const conversationServices = new ConversationServiceManager({
  snapshots: createLocalV2SnapshotStore,
  completions: (scope, deviceId) => createCompletionReadStorage(() => JSON.stringify([api.getBaseUrl(), scope.userId, scope.workspaceId, deviceId])),
});

/** The service of a workspace on the device behind a transport. */
export function conversationService(transport: LocalV2Transport, scope: LocalV2Scope): ConversationService {
  const service = conversationServices.get(transport, scope);
  verificationRenderer?.registerService(service);
  return service;
}
