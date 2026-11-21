import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Query key namespace for everything infoflow-installation-related. Realtime
 * sync invalidates `installations(wsId)` on `infoflow_installation:*` events so
 * the Settings panel updates without a manual refetch when another tab or an
 * admin on another machine connects or disconnects a robot.
 */
export const infoflowKeys = {
  all: (wsId: string) => ["infoflow", wsId] as const,
  installations: (wsId: string) =>
    [...infoflowKeys.all(wsId), "installations"] as const,
};

export const infoflowInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: infoflowKeys.installations(wsId),
    queryFn: () => api.listInfoFlowInstallations(wsId),
    enabled: !!wsId,
  });