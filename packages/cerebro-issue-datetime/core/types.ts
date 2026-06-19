// Optional time-of-day attached to an issue's start_date / due_date. The dates
// themselves stay pure calendar days on the issue; this is the companion time.
// Times are wall-clock "HH:MM" (24h) strings, or null when unset.

export type IssueTimeKind = "start" | "due";

export interface IssueDateTimes {
  start_time: string | null;
  due_time: string | null;
}

export const EMPTY_ISSUE_DATE_TIMES: IssueDateTimes = {
  start_time: null,
  due_time: null,
};
