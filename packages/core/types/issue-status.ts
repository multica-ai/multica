import type { IssueStatusCategory } from "./issue";

/**
 * A workspace's issue status catalog (MUL-6243).
 *
 * Seven concrete built-in statuses are grouped into four lifecycle categories.
 * Category is presentation and workflow phase; the server keeps the legacy
 * status behavior projection separate so collapsing `in_review` and `blocked`
 * into `started` does not change existing automation behavior.
 */

// IssueStatusCategory is defined in ./issue, next to IssueStatus, because the
// two only make sense read together. Re-exported here so catalog consumers can
// import both from one place. (MUL-6243, MUL-7240)
export type { IssueStatusCategory } from "./issue";

/** Visual geometry only; never use these values to infer workflow behavior. */
export const ISSUE_STATUS_ICONS = ["dotted", "circle", "half", "three_quarters", "check", "slash", "cross"] as const;
export type IssueStatusIcon = (typeof ISSUE_STATUS_ICONS)[number];

export interface IssueStatusEntry {
  id: string;
  workspace_id: string;
  /**
   * Stable machine handle, immutable after creation. This is the value stored
   * in `issue.status`, accepted by `multica issue status`, and referenced in
   * agent instructions — so it does NOT track renames of `name`.
   */
  key: string;
  /** Human-facing label. Editable for custom statuses; locked for built-ins. */
  name: string;
  description: string;
  category: IssueStatusCategory;
  /** "#rrggbb". */
  color: string;
  /** Empty/absent means category default. Unknown future values render a fallback. */
  icon?: string | null;
  /**
   * True for the 7 built-ins. They cannot be renamed, recolored, archived, or
   * have their category changed.
   */
  is_system: boolean;
  /** Ordering within the category. */
  position: number;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface ListIssueStatusesResponse {
  statuses: IssueStatusEntry[];
  /** The four lifecycle categories, in display order. */
  categories: IssueStatusCategory[];
  total: number;
}

export interface CreateIssueStatusRequest {
  /** Optional; derived from `name` when omitted. Immutable once created. */
  key?: string;
  name: string;
  description?: string;
  category: IssueStatusCategory;
  color: string;
  icon?: IssueStatusIcon | "";
}

/**
 * `key` and `category` are absent by design — both are immutable. Changing a
 * category would regroup every issue already on that status; changing a key
 * would strand them.
 */
export interface UpdateIssueStatusRequest {
  name?: string;
  description?: string;
  color?: string;
  /** Omit to preserve; empty string resets to the default. */
  icon?: IssueStatusIcon | "";
  position?: number;
}
