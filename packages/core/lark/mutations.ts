import type { QueryClient } from "@tanstack/react-query";
import { mutationOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { LarkNotificationEventType } from "../types/lark";
import { larkKeys } from "./queries";

export const updateLarkNotificationEventsMutationOptions = (
  wsId: string,
  queryClient: QueryClient,
) => mutationOptions({
  mutationFn: (eventTypes: LarkNotificationEventType[]) =>
    api.updateLarkNotificationEvents(wsId, eventTypes),
  onSuccess: () => queryClient.invalidateQueries({ queryKey: larkKeys.installations(wsId) }),
});
