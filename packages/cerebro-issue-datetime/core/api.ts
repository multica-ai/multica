import { api, parseWithFallback } from "@multica/core/api";

import {
  issueDateTimesSchema,
  workspaceDateTimeSettingsSchema,
} from "./api-schemas";
import {
  DEFAULT_WORKSPACE_DATE_TIME_SETTINGS,
  EMPTY_ISSUE_DATE_TIMES,
  type AgentStartKind,
  type IssueDateTimes,
  type WorkspaceDateTimeSettings,
} from "./types";

const path = (issueId: string) => `/api/cerebro/issues/${issueId}/date-times`;
const settingsPath = (wsId: string) =>
  `/api/workspaces/${wsId}/issue-start-settings`;

export async function fetchIssueDateTimes(issueId: string): Promise<IssueDateTimes> {
  const raw = await api.cerebroRequest<unknown>(path(issueId));
  return parseWithFallback(raw, issueDateTimesSchema, EMPTY_ISSUE_DATE_TIMES, {
    endpoint: "fetchIssueDateTimes",
  });
}

export async function saveIssueDateTimes(
  issueId: string,
  payload: IssueDateTimes,
): Promise<IssueDateTimes> {
  const raw = await api.cerebroRequest<unknown>(path(issueId), {
    method: "PUT",
    body: JSON.stringify(payload),
  });
  return parseWithFallback(raw, issueDateTimesSchema, payload, {
    endpoint: "saveIssueDateTimes",
  });
}

export async function fetchWorkspaceDateTimeSettings(
  wsId: string,
): Promise<WorkspaceDateTimeSettings> {
  const raw = await api.cerebroRequest<unknown>(settingsPath(wsId));
  return parseWithFallback(
    raw,
    workspaceDateTimeSettingsSchema,
    DEFAULT_WORKSPACE_DATE_TIME_SETTINGS,
    { endpoint: "fetchWorkspaceDateTimeSettings" },
  );
}

export async function saveWorkspaceDateTimeSettings(
  wsId: string,
  defaultAgentStartKind: AgentStartKind,
): Promise<WorkspaceDateTimeSettings> {
  const raw = await api.cerebroRequest<unknown>(settingsPath(wsId), {
    method: "PUT",
    body: JSON.stringify({ default_agent_start_kind: defaultAgentStartKind }),
  });
  return parseWithFallback(
    raw,
    workspaceDateTimeSettingsSchema,
    { default_agent_start_kind: defaultAgentStartKind },
    { endpoint: "saveWorkspaceDateTimeSettings" },
  );
}
