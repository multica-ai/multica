/**
 * Workspace custom property definitions (active only). The new-issue form
 * reads them to offer pinned and required select properties.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const propertyKeys = {
  all: (wsId: string | null) => ["properties", wsId] as const,
};

export const propertyListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: propertyKeys.all(wsId),
    queryFn: async ({ signal }) => {
      const res = await api.listProperties({ signal });
      return res.properties;
    },
    enabled: !!wsId,
  });
