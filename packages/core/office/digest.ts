import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { DigestParams } from "./digest-schema";

export const digestKeys = {
  all: (wsId: string) => ["office-digest", wsId] as const,
  since: (wsId: string, params: DigestParams = {}) => [...digestKeys.all(wsId), params.since ?? ""] as const,
};

/**
 * The company digest. Computed on demand and cheap to recompute, so it does not
 * stay fresh for long; WebSocket events do not patch it.
 */
export function digestOptions(wsId: string, params: DigestParams = {}) {
  return queryOptions({
    queryKey: digestKeys.since(wsId, params),
    queryFn: () => api.getOfficeDigest(params),
    staleTime: 60_000,
  });
}
