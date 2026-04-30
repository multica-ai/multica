import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const workCalendarKeys = {
  all: (wsId: string) => ["work-calendars", wsId] as const,
  detail: (wsId: string, calendarId: string) =>
    ["work-calendars", wsId, calendarId] as const,
};

export function workCalendarListOptions(wsId: string) {
  return queryOptions({
    queryKey: workCalendarKeys.all(wsId),
    queryFn: () => api.listWorkCalendars(),
    enabled: !!wsId,
  });
}

export function workCalendarDetailOptions(wsId: string, calendarId: string) {
  return queryOptions({
    queryKey: workCalendarKeys.detail(wsId, calendarId),
    queryFn: () => api.getWorkCalendar(calendarId),
    enabled: !!wsId && !!calendarId,
  });
}
