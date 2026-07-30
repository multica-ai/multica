import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for everything Lark-installation-related. Realtime
 * sync invalidates `installations(wsId)` on `lark_installation:*` events
 * so the Settings panel updates without a refetch. */
export const larkKeys = {
  all: (wsId: string) => ["lark", wsId] as const,
  installations: (wsId: string) => [...larkKeys.all(wsId), "installations"] as const,
  projectBindings: (wsId: string, installationId: string) =>
    [...larkKeys.all(wsId), "project-bindings", installationId] as const,
};

export const larkInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: larkKeys.installations(wsId),
    queryFn: () => api.listLarkInstallations(wsId),
    enabled: !!wsId,
  });

export const larkProjectBindingsOptions = (wsId: string, installationId: string) =>
  queryOptions({
    queryKey: larkKeys.projectBindings(wsId, installationId),
    queryFn: () => api.listLarkProjectBindings(wsId, installationId),
    enabled: !!wsId && !!installationId,
  });
