import { getCurrentWsId } from "@multica/core/platform";
import type { ConversationService } from "@multica/conversation-client/services";

const services = new Map<string, ConversationService>();
const bridge = (
  window as unknown as {
    multicaVerificationDev?: {
      onRequest(callback: (request: unknown) => unknown): void;
    };
  }
).multicaVerificationDev;
bridge?.onRequest((raw) => {
  const request = raw as {
    subjectRef?: string;
    userId?: string;
    workspaceId?: string;
  };
  const workspaceId = getCurrentWsId();
  const service = [...services.values()].find(
    (item) =>
      item.scope.userId === request.userId &&
      item.scope.workspaceId === (request.workspaceId ?? workspaceId),
  );
  const model =
    request.subjectRef && service
      ? service.conversation(request.subjectRef)
      : null;
  return {
    schemaVersion: 1,
    workspaceId,
    route: location.pathname,
    title: document.title,
    visibility: document.visibilityState,
    ...(model
      ? {
          conversation: {
            id: model.summary.conversation_id,
            seq: model.seq,
            generation: model.storeGeneration,
            summary: model.summary,
            queue: {
              itemsRevision: model.queue.itemsRevision,
              itemsStale: model.queue.itemsStale(),
              count: model.queue.count,
              revision: model.queue.revision,
              paused: model.queue.paused,
              items: model.queue.items,
            },
            turns: [...model.turns.values()].map((turn) => ({
              id: turn.id,
              status: turn.status,
            })),
          },
        }
      : { conversationUnavailable: true }),
  };
});
export const verificationRenderer = {
  registerService(service: ConversationService) {
    const key = JSON.stringify([
      service.scope.userId,
      service.scope.workspaceId,
    ]);
    services.set(key, service);
    if (services.size > 100) {
      const oldest = services.keys().next().value;
      if (oldest !== undefined) services.delete(oldest);
    }
  },
};
