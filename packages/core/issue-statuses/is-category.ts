import type { IssueStatusCategory } from "../types";
import { STATUS_ORDER } from "../issues/config/status";

const BUILT_IN = new Set<string>(STATUS_ORDER);

/** True when `value` is one of the 7 built-in status categories. */
export function isIssueStatusCategory(value: string): value is IssueStatusCategory {
  return BUILT_IN.has(value);
}
