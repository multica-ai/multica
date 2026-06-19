import { api, parseWithFallback } from "@multica/core/api";

import { issueDateTimesSchema } from "./api-schemas";
import { EMPTY_ISSUE_DATE_TIMES, type IssueDateTimes } from "./types";

const path = (issueId: string) => `/api/cerebro/issues/${issueId}/date-times`;

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
