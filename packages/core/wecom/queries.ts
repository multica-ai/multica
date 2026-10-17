import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Query key namespace for everything wecom-installation-related. Realtime sync
 * invalidates `installations(wsId)` on `wecom_installation:*` events so the
 * Settings panel updates without a manual refetch when another tab / an admin
 * on another machine connects or disconnects a bot.
 */
export const wecomKeys = {
  all: (wsId: string) => ["wecom", wsId] as const,
  installations: (wsId: string) => [...wecomKeys.all(wsId), "installations"] as const,
  /** The calling member's own WeCom bindings + notification-target choice
   * (SHAR-146 B1). Separate key from installations: the two surfaces change
   * for different reasons (an admin connects a bot vs. a member re-points
   * their notifications). */
  bindings: (wsId: string) => [...wecomKeys.all(wsId), "bindings"] as const,
};

export const wecomInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: wecomKeys.installations(wsId),
    queryFn: () => api.listWecomInstallations(wsId),
    enabled: !!wsId,
  });

export const wecomBindingsOptions = (wsId: string) =>
  queryOptions({
    queryKey: wecomKeys.bindings(wsId),
    queryFn: () => api.listWecomBindings(wsId),
    enabled: !!wsId,
  });
