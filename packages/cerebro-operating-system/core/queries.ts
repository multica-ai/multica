import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { createConnection, createStrategyItem, fetchConnections, fetchRocks, fetchSettings, fetchStrategy, updateSettings, upsertRock } from "./api";
import type { ObjectConnectionInput, RockInput, StrategyItemInput, Terminology } from "./types";

export const operatingSystemKeys = {
  all: (wsId: string) => ["cerebro", "operating-system", wsId] as const,
  settings: (wsId: string) => [...operatingSystemKeys.all(wsId), "settings"] as const,
  strategy: (wsId: string) => [...operatingSystemKeys.all(wsId), "strategy"] as const,
  rocks: (wsId: string) => [...operatingSystemKeys.all(wsId), "rocks"] as const,
  connections: (wsId: string, objectType: string, objectId: string) => [...operatingSystemKeys.all(wsId), "connections", objectType, objectId] as const,
};
export const settingsOptions = (wsId: string) => queryOptions({ queryKey: operatingSystemKeys.settings(wsId), queryFn: fetchSettings, enabled: !!wsId });
export const strategyOptions = (wsId: string) => queryOptions({ queryKey: operatingSystemKeys.strategy(wsId), queryFn: fetchStrategy, enabled: !!wsId });
export const rocksOptions = (wsId: string) => queryOptions({ queryKey: operatingSystemKeys.rocks(wsId), queryFn: fetchRocks, enabled: !!wsId });
export const connectionsOptions = (wsId: string, objectType: string, objectId: string) => queryOptions({ queryKey: operatingSystemKeys.connections(wsId, objectType, objectId), queryFn: () => fetchConnections(objectType, objectId), enabled: !!wsId && !!objectType && !!objectId });
export function useCreateStrategyItem(wsId: string) { const qc = useQueryClient(); return useMutation({ mutationFn: (input: StrategyItemInput) => createStrategyItem(input), onSettled: () => qc.invalidateQueries({ queryKey: operatingSystemKeys.strategy(wsId) }) }); }
export function useUpsertRock(wsId: string) { const qc = useQueryClient(); return useMutation({ mutationFn: (input: RockInput) => upsertRock(input), onSettled: () => qc.invalidateQueries({ queryKey: operatingSystemKeys.rocks(wsId) }) }); }
export function useUpdateSettings(wsId: string) { const qc = useQueryClient(); return useMutation({ mutationFn: (input: Terminology) => updateSettings(input), onSettled: () => { qc.invalidateQueries({ queryKey: operatingSystemKeys.settings(wsId) }); qc.invalidateQueries({ queryKey: operatingSystemKeys.strategy(wsId) }); qc.invalidateQueries({ queryKey: operatingSystemKeys.rocks(wsId) }); } }); }
export function useCreateConnection(wsId: string) { const qc = useQueryClient(); return useMutation({ mutationFn: (input: ObjectConnectionInput) => createConnection(input), onSettled: (_data, _error, input) => { qc.invalidateQueries({ queryKey: operatingSystemKeys.connections(wsId, input.source_type, input.source_id) }); qc.invalidateQueries({ queryKey: operatingSystemKeys.strategy(wsId) }); } }); }
