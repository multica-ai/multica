import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { autopilotKeys } from "../autopilots/queries";
import { charterKeys } from "./charter-queries";
import type {
  CreateSquadRoutineRequest,
  SquadCharter,
  SquadRoutine,
  UpdateSquadCharterRequest,
} from "./charter-schema";

/*
 * Nothing here patches optimistically. Saves can be refused (a stale revision
 * is a 409), and a charter save may create or pause the operating review, so
 * the autopilot list and the routines are refetched once the server answers.
 * Routine state is derived on the server, so it is never predicted client-side.
 */

/**
 * Saves the charter at the revision the editor read. On a 409 the cache is
 * refetched, so the editor can show the current revision and offer a reload.
 */
export function useSaveSquadCharter(wsId: string, squadId: string) {
  const qc = useQueryClient();
  return useMutation<SquadCharter, Error, UpdateSquadCharterRequest>({
    mutationFn: (data) => api.updateSquadCharter(squadId, data),
    onSuccess: (saved) => {
      qc.setQueryData(charterKeys.charter(wsId, squadId), saved);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: charterKeys.charter(wsId, squadId) });
      qc.invalidateQueries({ queryKey: charterKeys.routines(wsId, squadId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.list(wsId) });
    },
  });
}

/** Links an existing autopilot or creates one on a cadence. */
export function useCreateSquadRoutine(wsId: string, squadId: string) {
  const qc = useQueryClient();
  return useMutation<SquadRoutine, Error, CreateSquadRoutineRequest>({
    mutationFn: (data) => api.createSquadRoutine(squadId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: charterKeys.routines(wsId, squadId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.list(wsId) });
    },
  });
}

/**
 * Unlinks a routine. The autopilot keeps its schedule and history; `pause` also
 * pauses it, so an unlinked routine does not keep running unowned.
 */
export function useUnlinkSquadRoutine(wsId: string, squadId: string) {
  const qc = useQueryClient();
  return useMutation<void, Error, { autopilotId: string; pause: boolean }>({
    mutationFn: ({ autopilotId, pause }) => api.deleteSquadRoutine(squadId, autopilotId, { pause }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: charterKeys.routines(wsId, squadId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.list(wsId) });
    },
  });
}

/**
 * Runs a routine's autopilot now, through the same endpoint as the autopilot
 * page's Run button. The run's effect on the routine shows on the next read.
 */
export function useRunSquadRoutine(wsId: string, squadId: string) {
  const qc = useQueryClient();
  return useMutation<unknown, Error, string>({
    mutationFn: (autopilotId) => api.triggerAutopilot(autopilotId),
    onSettled: (_data, _err, autopilotId) => {
      qc.invalidateQueries({ queryKey: charterKeys.routines(wsId, squadId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.runs(wsId, autopilotId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.detail(wsId, autopilotId) });
    },
  });
}

/** Pauses or resumes a routine's autopilot through the autopilot endpoint. */
export function useSetSquadRoutinePaused(wsId: string, squadId: string) {
  const qc = useQueryClient();
  return useMutation<unknown, Error, { autopilotId: string; paused: boolean }>({
    mutationFn: ({ autopilotId, paused }) => api.updateAutopilot(autopilotId, { status: paused ? "paused" : "active" }),
    onSettled: (_data, _err, { autopilotId }) => {
      qc.invalidateQueries({ queryKey: charterKeys.routines(wsId, squadId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.detail(wsId, autopilotId) });
    },
  });
}
