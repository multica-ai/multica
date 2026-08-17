import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const autopilotKeys = {
  all: (wsId: string) => ["autopilots", wsId] as const,
  list: (wsId: string) => [...autopilotKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...autopilotKeys.all(wsId), "detail", id] as const,
  runs: (wsId: string, id: string) =>
    [...autopilotKeys.all(wsId), "runs", id] as const,
  run: (wsId: string, autopilotId: string, runId: string) =>
    [...autopilotKeys.all(wsId), "runs", autopilotId, runId] as const,
  deliveries: (wsId: string, id: string) =>
    [...autopilotKeys.all(wsId), "deliveries", id] as const,
  delivery: (wsId: string, autopilotId: string, deliveryId: string) =>
    [...autopilotKeys.all(wsId), "deliveries", autopilotId, deliveryId] as const,
  cronPreview: (wsId: string, expr: string, tz: string) =>
    [...autopilotKeys.all(wsId), "cron-preview", expr, tz] as const,
};

export function autopilotListOptions(wsId: string) {
  return queryOptions({
    queryKey: autopilotKeys.list(wsId),
    queryFn: () => api.listAutopilots(),
    select: (data) => data.autopilots,
  });
}

export function autopilotDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: autopilotKeys.detail(wsId, id),
    queryFn: () => api.getAutopilot(id),
  });
}

export function autopilotRunsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: autopilotKeys.runs(wsId, id),
    queryFn: () => api.listAutopilotRuns(id),
    select: (data) => data.runs,
  });
}

// autopilotRunOptions fetches a single run with its full trigger_payload.
// The list endpoint (autopilotRunsOptions) omits trigger_payload to keep
// list responses small; callers (e.g. the run-detail dialog) use this
// query on demand when the user opens a run.
export function autopilotRunOptions(
  wsId: string,
  autopilotId: string,
  runId: string,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.run(wsId, autopilotId, runId),
    queryFn: () => api.getAutopilotRun(autopilotId, runId),
    enabled: options?.enabled ?? true,
  });
}

// autopilotDeliveriesOptions powers the Deliveries section in the autopilot
// detail page. The list is slim — raw_body / selected_headers / response_body
// are omitted server-side. Detail rows are fetched on-demand when the user
// expands a row (see autopilotDeliveryOptions).
export function autopilotDeliveriesOptions(
  wsId: string,
  autopilotId: string,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.deliveries(wsId, autopilotId),
    queryFn: () => api.listAutopilotDeliveries(autopilotId),
    select: (data) => data.deliveries,
    enabled: options?.enabled ?? true,
  });
}

// autopilotDeliveryOptions fetches the full delivery row including raw_body
// and headers subset. Used by the detail dialog opened from a list row.
export function autopilotDeliveryOptions(
  wsId: string,
  autopilotId: string,
  deliveryId: string,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.delivery(wsId, autopilotId, deliveryId),
    queryFn: () => api.getAutopilotDelivery(autopilotId, deliveryId),
    enabled: options?.enabled ?? true,
  });
}

// cronPreviewOptions backs the schedule editor's next-run preview. The server
// owns cron/timezone evaluation, so the editor never approximates it locally.
export function cronPreviewOptions(
  wsId: string,
  expr: string,
  tz: string,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.cronPreview(wsId, expr, tz),
    queryFn: () => api.cronPreview({ expr, tz }),
    enabled: options?.enabled ?? true,
    staleTime: 30_000,
    // A 400 (invalid expression/timezone) is a stable answer for this input,
    // not a transient failure — retrying would only delay the inline error.
    retry: false,
  });
}
