import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { addRockCheckIn, createConnection, createRock, createStrategyItem, fetchConnections, fetchPeriods, fetchRocks, fetchSettings, fetchStrategy, fetchStrategyHistory, updateRock, updateSettings, updateStrategyItem } from "./api";
import type { ObjectConnectionInput, RockCheckInInput, RockInput, StrategyItemInput, Terminology } from "./types";

export const operatingSystemKeys = {
  all: (wsId: string) => ["cerebro", "operating-system", wsId] as const,
  settings: (wsId: string) => [...operatingSystemKeys.all(wsId), "settings"] as const,
  periods: (wsId: string) => [...operatingSystemKeys.all(wsId), "periods"] as const,
  strategy: (wsId: string) => [...operatingSystemKeys.all(wsId), "strategy"] as const,
  strategyHistory: (wsId: string) => [...operatingSystemKeys.all(wsId), "strategy-history"] as const,
  rocks: (wsId: string) => [...operatingSystemKeys.all(wsId), "rocks"] as const,
  connections: (wsId: string, objectType: string, objectId: string) => [...operatingSystemKeys.all(wsId), "connections", objectType, objectId] as const,
};
export const settingsOptions = (wsId: string) => queryOptions({ queryKey: operatingSystemKeys.settings(wsId), queryFn: fetchSettings, enabled: !!wsId });
export const periodsOptions = (wsId: string) => queryOptions({ queryKey: operatingSystemKeys.periods(wsId), queryFn: fetchPeriods, enabled: !!wsId });
export const strategyOptions = (wsId: string) => queryOptions({ queryKey: operatingSystemKeys.strategy(wsId), queryFn: fetchStrategy, enabled: !!wsId });
export const strategyHistoryOptions = (wsId: string) => queryOptions({ queryKey: operatingSystemKeys.strategyHistory(wsId), queryFn: fetchStrategyHistory, enabled: !!wsId });
export const rocksOptions = (wsId: string) => queryOptions({ queryKey: operatingSystemKeys.rocks(wsId), queryFn: fetchRocks, enabled: !!wsId });
export const connectionsOptions = (wsId: string, objectType: string, objectId: string) => queryOptions({ queryKey: operatingSystemKeys.connections(wsId, objectType, objectId), queryFn: () => fetchConnections(objectType, objectId), enabled: !!wsId && !!objectType && !!objectId });
export function useCreateStrategyItem(wsId: string) { const qc = useQueryClient(); return useMutation({ mutationFn: (input: StrategyItemInput) => createStrategyItem(input), onSettled: () => { qc.invalidateQueries({ queryKey: operatingSystemKeys.strategy(wsId) }); qc.invalidateQueries({ queryKey: operatingSystemKeys.strategyHistory(wsId) }); } }) }
export function useUpdateStrategyItem(wsId: string) { const qc = useQueryClient(); return useMutation({ mutationFn: ({ id, input }: { id: string; input: StrategyItemInput }) => updateStrategyItem(id, input), onSettled: () => { qc.invalidateQueries({ queryKey: operatingSystemKeys.strategy(wsId) }); qc.invalidateQueries({ queryKey: operatingSystemKeys.strategyHistory(wsId) }); } }) }
export function useSaveRock(wsId: string) { const qc = useQueryClient(); return useMutation({ mutationFn: ({ id, input }: { id?: string; input: RockInput }) => id ? updateRock(id, input) : createRock(input), onSettled: () => { qc.invalidateQueries({ queryKey: operatingSystemKeys.rocks(wsId) }); qc.invalidateQueries({ queryKey: operatingSystemKeys.strategy(wsId) }); } }); }
export function useRockCheckIn(wsId: string) { const qc = useQueryClient(); return useMutation({ mutationFn: ({ id, input }: { id: string; input: RockCheckInInput }) => addRockCheckIn(id, input), onSettled: () => qc.invalidateQueries({ queryKey: operatingSystemKeys.rocks(wsId) }) }); }
export function useUpdateSettings(wsId: string) { const qc = useQueryClient(); return useMutation({ mutationFn: (input: Terminology) => updateSettings(input), onSettled: () => { qc.invalidateQueries({ queryKey: operatingSystemKeys.settings(wsId) }); qc.invalidateQueries({ queryKey: operatingSystemKeys.strategy(wsId) }); qc.invalidateQueries({ queryKey: operatingSystemKeys.rocks(wsId) }); } }); }
export function useCreateConnection(wsId: string) { const qc = useQueryClient(); return useMutation({ mutationFn: (input: ObjectConnectionInput) => createConnection(input), onSettled: (_data, _error, input) => { qc.invalidateQueries({ queryKey: operatingSystemKeys.connections(wsId, input.source_type, input.source_id) }); qc.invalidateQueries({ queryKey: operatingSystemKeys.strategy(wsId) }); } }); }
