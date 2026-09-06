import type { SecretaryItem, SecretaryResponse } from "./contract";

export type ArrangedItem = SecretaryItem & {
  scheduled_on?: string | null; estimate_minutes?: number | null;
  reported?: "completed" | "cancelled"; instruction_note?: string; pending_reconciliation?: boolean;
};

/** User instructions remain durable across source refreshes. A completion report
 * leaves the personal queue immediately but is not a fabricated business receipt. */
export function arrangeSecretaryItems(data: SecretaryResponse): ArrangedItem[] {
  const result = new Map<string, ArrangedItem>(data.projection?.items.map(item => [item.key, { ...item, reported: item.reported_status ?? undefined }]) ?? []);
  if (data.resolved) return [...result.values()];
  const latestDisposition = new Map<string, number>();
  for (const instruction of data.instructions) {
    if (instruction.kind !== "plan" && instruction.kind !== "capacity") latestDisposition.set(instruction.item_key, instruction.sequence);
  }
  for (const instruction of data.instructions) {
    const item = result.get(instruction.item_key);
    if (!item) continue;
    const payload = instruction.payload;
    if (instruction.kind === "plan") {
      item.scheduled_on = payload.scheduled_on;
      item.estimate_minutes = payload.estimate_minutes;
      continue;
    }
    if (latestDisposition.get(instruction.item_key) !== instruction.sequence) continue;
    // Once reconciled, canonical facts decide status. Retain the user's original note.
    item.instruction_note = payload.note;
    item.pending_reconciliation = !instruction.canonical_receipt;
    if (instruction.canonical_receipt || item.stage === "history") continue;
    if (["prepare", "wait", "later"].includes(instruction.kind)) item.next_step = payload.note;
    switch (instruction.kind) {
      case "complete": item.stage = item.closure_mode === "self_report" ? "history" : "preparing"; item.owner = "secretary"; item.reported = "completed"; item.reported_at = instruction.created_at; break;
      case "cancel": item.stage = "history"; item.reported = "cancelled"; break;
      case "prepare": case "reopen": item.stage = "preparing"; item.owner = "secretary"; item.reported = undefined; break;
      case "wait": item.stage = "waiting"; item.owner = "external"; item.follow_up_on = payload.scheduled_on; item.reported = undefined; break;
      case "later": item.stage = "later"; item.follow_up_on = payload.scheduled_on; item.reported = undefined; break;
    }
  }
  return [...result.values()];
}

export function secretaryDay(items: ArrangedItem[], data: SecretaryResponse, day: string) {
  const active = items.filter(i => i.kind === "action" && i.stage !== "history");
  const completionReports = active.filter(i => i.reported === "completed");
  const completedToday = items.filter(i => i.stage === "history" && i.reported === "completed" && i.reported_at &&
    new Intl.DateTimeFormat("sv-SE", { timeZone: "Asia/Shanghai" }).format(new Date(i.reported_at)) === day);
  const priorityFollowups = active.filter(i => i.stage !== "ready" && i.watch_reason && !i.reported);
  const ready = active.filter(i => i.stage === "ready");
  const today = ready.filter(i => i.scheduled_on === day);
  const overduePlans = ready.filter(i => i.scheduled_on && i.scheduled_on < day);
  const nextDay = new Date(`${day}T12:00:00Z`);
  nextDay.setUTCDate(nextDay.getUTCDate() + 1);
  const tomorrow = nextDay.toISOString().slice(0, 10);
  const deadlines = ready.filter(i => i.due_date && i.due_date <= tomorrow && !i.reported);
  const followups = active.filter(i => i.stage !== "ready" && i.follow_up_on && i.follow_up_on <= day && !i.reported);
  const unplanned = ready.filter(i => !i.scheduled_on);
  const capacity = data.instructions.filter(i => i.kind === "capacity" && i.payload.scheduled_on === day).at(-1)?.payload.capacity_minutes;
  const minutes = today.reduce((sum, i) => sum + (i.estimate_minutes ?? 0), 0);
  const unestimated = today.filter(i => !i.estimate_minutes).length;
  const highlighted = new Set(deadlines.map(i => i.key));
  const todayDetails = today.filter(i => !highlighted.has(i.key));
  const overdueDetails = overduePlans.filter(i => !highlighted.has(i.key));
  const unplannedDetails = unplanned.filter(i => !highlighted.has(i.key));
  return { completedToday, priorityFollowups, completionReports, today, todayDetails, ready, overduePlans, overdueDetails, deadlines, followups, unplanned, unplannedDetails, capacity, minutes, unestimated,
    overCapacity: capacity != null && minutes > capacity };
}
