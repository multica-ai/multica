import { queryOptions } from "@tanstack/react-query";
import { api } from "@multica/core/api";

export const attachmentKeys = {
  all: (wsId: string) => ["attachments", wsId] as const,
  detail: (wsId: string, id: string) =>
    [...attachmentKeys.all(wsId), "detail", id] as const,
};

export function attachmentDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: attachmentKeys.detail(wsId, id),
    queryFn: () => api.getAttachment(id),
    enabled: Boolean(wsId && id),
  });
}
