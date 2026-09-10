import type { IssueStatusCategory } from "./issue";

/**
 * A workspace's issue status catalog (MUL-6243).
 *
 * The 7 categories map one-to-one onto the 7 built-in statuses: a category's
 * value IS its canonical status key. A custom status names a category and
 * inherits that canonical status's platform behavior in full — a custom status
 * in the `todo` category starts an agent exactly like Todo does, one in the
 * `in_review` category finalizes an autopilot run exactly like In Review does.
 *
 * That is why there is no separate "behavior" field here: the category is the
 * behavior.
 */

// IssueStatusCategory is defined in ./issue, next to IssueStatus, because the
// two only make sense read together: a category is one of the 7 built-in keys,
// and a status key is a category or a workspace's custom key. Re-exported here
// so catalog consumers can import both from one place. (MUL-6243)
export type { IssueStatusCategory } from "./issue";

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
  /**
   * True for the 7 built-ins. They cannot be renamed, recolored, archived, or
   * have their category changed: each one is its category's canonical
   * definition, and the default workspace must look identical for every user
   * who never opens the status settings.
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
  /** The 7 categories, in display order. */
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
}

/**
 * `key` and `category` are absent by design — both are immutable. Changing a
 * category would silently rewrite the machine semantics of every issue already
 * on that status; changing a key would strand them.
 */
export interface UpdateIssueStatusRequest {
  name?: string;
  description?: string;
  color?: string;
  position?: number;
}
