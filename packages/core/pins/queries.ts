import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const pinKeys = {
  all: (wsId: string, userId: string) => ["pins", wsId, userId] as const,
  list: (wsId: string, userId: string) => [...pinKeys.all(wsId, userId), "list"] as const,
};

export function pinListOptions(wsId: string, userId: string) {
  return queryOptions({
    queryKey: pinKeys.list(wsId, userId),
    queryFn: async () => {
      const pins = await api.listPins(wsId);
      if (pins.some((pin) => pin.workspace_id !== wsId)) {
        throw new Error("Pin response does not match the requested workspace");
      }
      return pins;
    },
  });
}
