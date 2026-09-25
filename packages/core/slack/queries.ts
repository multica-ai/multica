import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for everything Slack-installation-related. Realtime
 * sync invalidates `installations(wsId)` on `slack_installation:*` events so
 * the Settings panel updates without a manual refetch (e.g. after the OAuth
 * callback lands the install in another tab / the system browser). */
export const slackKeys = {
  all: (wsId: string) => ["slack", wsId] as const,
  installations: (wsId: string) => [...slackKeys.all(wsId), "installations"] as const,
};

export const slackInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: slackKeys.installations(wsId),
    queryFn: () => api.listSlackInstallations(wsId),
    enabled: !!wsId,
  });
