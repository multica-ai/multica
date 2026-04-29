import type { Issue } from "@/shared/types";

const TERMINAL_STATUSES = new Set(["done", "cancelled"]);

function parseDate(value: string | null): Date | null {
  if (!value) return null;
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? null : parsed;
}

function startOfDay(value: Date): Date {
  const next = new Date(value);
  next.setHours(0, 0, 0, 0);
  return next;
}

function endOfDay(value: Date): Date {
  const next = new Date(value);
  next.setHours(23, 59, 59, 999);
  return next;
}

function isScheduledForToday(issue: Issue, now: Date): boolean {
  const dayStart = startOfDay(now);
  const dayEnd = endOfDay(now);

  const dueDate = parseDate(issue.due_date);
  const startDate = parseDate(issue.start_date);
  const endDate = parseDate(issue.end_date);

  if (dueDate && dueDate >= dayStart && dueDate <= dayEnd) return true;
  if (startDate && startDate >= dayStart && startDate <= dayEnd) return true;
  if (endDate && endDate >= dayStart && endDate <= dayEnd) return true;

  return !!(startDate && endDate && startDate <= dayEnd && endDate >= dayStart);
}

function isScheduledAfterToday(issue: Issue, now: Date): boolean {
  const dayEnd = endOfDay(now);

  const dueDate = parseDate(issue.due_date);
  const startDate = parseDate(issue.start_date);
  const endDate = parseDate(issue.end_date);

  return !!(
    (dueDate && dueDate > dayEnd) ||
    (startDate && startDate > dayEnd) ||
    (endDate && endDate > dayEnd)
  );
}

export function deriveBacklogIssues(issues: Issue[]): Issue[] {
  return issues.filter((issue) => issue.status === "backlog");
}

export function deriveTodayIssues(issues: Issue[], now = new Date()): Issue[] {
  return issues.filter(
    (issue) =>
      !TERMINAL_STATUSES.has(issue.status) &&
      isScheduledForToday(issue, now),
  );
}

export function deriveUpcomingIssues(issues: Issue[], now = new Date()): Issue[] {
  return issues.filter(
    (issue) =>
      !TERMINAL_STATUSES.has(issue.status) &&
      !isScheduledForToday(issue, now) &&
      isScheduledAfterToday(issue, now),
  );
}

function formatShortDate(value: Date): string {
  return value.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}

export function formatIssueSchedule(issue: Issue): string | null {
  const dueDate = parseDate(issue.due_date);
  const startDate = parseDate(issue.start_date);
  const endDate = parseDate(issue.end_date);

  if (startDate && endDate) {
    const startLabel = formatShortDate(startDate);
    const endLabel = formatShortDate(endDate);
    return startLabel === endLabel ? startLabel : `${startLabel} - ${endLabel}`;
  }

  if (startDate) return `Starts ${formatShortDate(startDate)}`;
  if (endDate) return `Ends ${formatShortDate(endDate)}`;
  if (dueDate) return `Due ${formatShortDate(dueDate)}`;

  return null;
}

export function isIssueScheduleOverdue(issue: Issue, now = new Date()): boolean {
  const dayStart = startOfDay(now);
  const dueDate = parseDate(issue.due_date);
  const endDate = parseDate(issue.end_date);

  if (issue.status === "done" || issue.status === "cancelled") return false;
  if (dueDate) return dueDate < dayStart;
  if (endDate) return endDate < dayStart;
  return false;
}