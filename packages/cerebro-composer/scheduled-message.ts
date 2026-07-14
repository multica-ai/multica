import { api } from "@multica/core/api";

export interface ScheduledMessage {
  id: string;
  issue_id: string;
  content: string;
  parent_id: string | null;
  attachment_ids: string[];
  send_at: string;
  status: "pending" | "processing" | "sent" | "failed";
  created_at: string;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function parseScheduledMessage(value: unknown): ScheduledMessage {
  if (!isRecord(value)) throw new Error("Invalid scheduled message response");
  const status = value.status;
  if (
    typeof value.id !== "string" ||
    typeof value.issue_id !== "string" ||
    typeof value.content !== "string" ||
    !(value.parent_id === null || typeof value.parent_id === "string") ||
    !Array.isArray(value.attachment_ids) ||
    !value.attachment_ids.every((id) => typeof id === "string") ||
    typeof value.send_at !== "string" ||
    Number.isNaN(Date.parse(value.send_at)) ||
    !["pending", "processing", "sent", "failed"].includes(String(status)) ||
    typeof value.created_at !== "string"
  ) throw new Error("Invalid scheduled message response");
  return value as unknown as ScheduledMessage;
}

function parseList(value: unknown): ScheduledMessage[] {
  if (!Array.isArray(value)) throw new Error("Invalid scheduled messages response");
  return value.map(parseScheduledMessage);
}

export function tomorrowAtNine(now = new Date()): Date {
  const value = new Date(now);
  value.setDate(value.getDate() + 1);
  value.setHours(9, 0, 0, 0);
  return value;
}

export function nextMondayAtNine(now = new Date()): Date {
  const value = new Date(now);
  const days = ((8 - value.getDay()) % 7) || 7;
  value.setDate(value.getDate() + days);
  value.setHours(9, 0, 0, 0);
  return value;
}

export function toLocalInputValue(value: Date): string {
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${value.getFullYear()}-${pad(value.getMonth() + 1)}-${pad(value.getDate())}T${pad(value.getHours())}:${pad(value.getMinutes())}`;
}

export async function createScheduledMessage(issueId: string, input: {
  content: string;
  parent_id?: string;
  attachment_ids?: string[];
  send_at: string;
}): Promise<ScheduledMessage> {
  return parseScheduledMessage(await api.cerebroRequest(`/api/issues/${issueId}/scheduled-messages`, {
    method: "POST",
    body: JSON.stringify(input),
  }));
}

export async function listScheduledMessages(issueId: string): Promise<ScheduledMessage[]> {
  return parseList(await api.cerebroRequest(`/api/issues/${issueId}/scheduled-messages`));
}

export async function updateScheduledMessage(id: string, input: { content?: string; send_at?: string }): Promise<ScheduledMessage> {
  return parseScheduledMessage(await api.cerebroRequest(`/api/scheduled-messages/${id}`, {
    method: "PATCH",
    body: JSON.stringify(input),
  }));
}

export async function deleteScheduledMessage(id: string): Promise<void> {
  await api.cerebroRequest(`/api/scheduled-messages/${id}`, { method: "DELETE" });
}

export async function sendScheduledMessageNow(id: string): Promise<void> {
  await api.cerebroRequest(`/api/scheduled-messages/${id}/send`, { method: "POST" });
}
