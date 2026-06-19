import { api, parseWithFallback } from "@multica/core/api";

import { reminderListSchema, reminderSchema } from "./api-schemas";
import type { CreateReminderInput, Reminder } from "./types";

const BASE = "/api/cerebro/reminders";

// The member's reminders for the overview (server excludes done), oldest due
// first. On a contract drift we degrade to an empty list rather than throwing.
export async function listReminders(): Promise<Reminder[]> {
  const raw = await api.cerebroRequest<unknown>(BASE);
  return parseWithFallback(raw, reminderListSchema, [] as Reminder[], {
    endpoint: "listReminders",
  });
}

export async function getReminder(id: string): Promise<Reminder | null> {
  const raw = await api.cerebroRequest<unknown>(`${BASE}/${id}`);
  return parseWithFallback(raw, reminderSchema, null, {
    endpoint: "getReminder",
  });
}

export async function createReminder(input: CreateReminderInput): Promise<Reminder | null> {
  const raw = await api.cerebroRequest<unknown>(BASE, {
    method: "POST",
    body: JSON.stringify(input),
  });
  return parseWithFallback(raw, reminderSchema, null, {
    endpoint: "createReminder",
  });
}

// Reschedule ("Udsæt"). `until` is RFC3339; the server returns the row to
// pending and clears the fired markers so the sweeper picks it up again.
export async function snoozeReminder(id: string, until: string): Promise<Reminder | null> {
  const raw = await api.cerebroRequest<unknown>(`${BASE}/${id}/snooze`, {
    method: "POST",
    body: JSON.stringify({ until }),
  });
  return parseWithFallback(raw, reminderSchema, null, {
    endpoint: "snoozeReminder",
  });
}

// Dismiss ("Færdig").
export async function markReminderDone(id: string): Promise<Reminder | null> {
  const raw = await api.cerebroRequest<unknown>(`${BASE}/${id}/done`, {
    method: "POST",
  });
  return parseWithFallback(raw, reminderSchema, null, {
    endpoint: "markReminderDone",
  });
}

export async function deleteReminder(id: string): Promise<void> {
  await api.cerebroRequest<void>(`${BASE}/${id}`, { method: "DELETE" });
}
