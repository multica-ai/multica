import type { DigestSetting, DigestModeInfo } from "./digest-mode";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { governanceKeys } from "./queries";
import type {
  CompanyStateInfo,
  GovernanceEventFilters,
  QuietHours,
  SetCompanyStateRequest,
} from "./types";

/*
 * Every write here waits for the server and then shows what the server kept:
 * the company state is set from the response, never ahead of it. A governance
 * write also adds a log entry, so the log is refetched when the write settles.
 */

function useCompanyStateWrite<Vars>(wsId: string, run: (vars: Vars) => Promise<CompanyStateInfo>) {
  const qc = useQueryClient();
  return useMutation<CompanyStateInfo, Error, Vars>({
    mutationFn: run,
    onSuccess: (info) => {
      qc.setQueryData(governanceKeys.companyState(wsId), info);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: governanceKeys.all(wsId) });
    },
  });
}

/**
 * Sets the company to running, quiet or paused. `stop_running` with
 * `paused` also cancels the running routine and delegated tasks.
 */
export function useSetCompanyState(wsId: string) {
  return useCompanyStateWrite<SetCompanyStateRequest>(wsId, (data) => api.setCompanyState(data));
}

/** Sets the Secretary's digest source (spec 4.8). */
export function useSetDigestMode(wsId: string) {
  const qc = useQueryClient();
  return useMutation<DigestModeInfo, Error, DigestSetting>({
    mutationFn: (mode) => api.setDigestMode(mode),
    onSuccess: (info) => {
      qc.setQueryData(governanceKeys.digestMode(wsId), info);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: governanceKeys.digestMode(wsId) });
    },
  });
}

/** Saves the daily quiet window. */
export function useSetQuietHours(wsId: string) {
  return useCompanyStateWrite<QuietHours>(wsId, (data) => api.setQuietHours(data));
}

/**
 * Fetches the CSV of the log for these filters. Resolves to the file's bytes;
 * saving them is the caller's job, because the core layer has no DOM.
 */
export function useExportGovernanceEvents() {
  return useMutation<Blob, Error, GovernanceEventFilters>({
    mutationFn: (filters) => api.exportGovernanceEventsCsv(filters),
  });
}
