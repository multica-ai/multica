// SE-37641: infrastructure health queries. The snapshot is served by
// GET /api/se/health (owner/admin gated) from a file produced by host-side
// cron; absence degrades to 503, which the hook surfaces as an error state.
import { queryOptions } from "@tanstack/react-query";

import { api } from "../api";
import { parseWithFallback } from "../api/schema";
import {
  SEInfraHealthSnapshotSchema,
  type SEAgentFleet,
  type SEInfraHealthSnapshot,
} from "./schemas";

export type { SEAgentFleet, SEInfraHealthSnapshot };

export const seHealthKeys = {
  infra: (wsId: string) => ["se", "health-infra", wsId] as const,
};

export async function fetchSEInfraHealth(wsId: string): Promise<SEInfraHealthSnapshot> {
  const raw = await api.getSEInfraHealthSnapshot(wsId);
  return parseWithFallback(
    raw,
    SEInfraHealthSnapshotSchema,
    { generated_at: "", overall: "unknown", targets: [], nodes: {}, history: {}, syncs: {}, agent_fleet: undefined },
    { endpoint: "GET /api/se/health" },
  );
}

export function seInfraHealthOptions(wsId: string) {
  return queryOptions({
    queryKey: seHealthKeys.infra(wsId),
    queryFn: () => fetchSEInfraHealth(wsId),
    refetchInterval: 60_000,
  });
}
