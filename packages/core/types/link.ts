/** Issue-to-issue link types. Mirrors backend AllowedLinkTypes in handler/link.go. */
export type LinkType = "blocks" | "depends_on" | "duplicates" | "relates_to";

/** Direction the link is being viewed from. The backend stores two mirror rows
 *  per link sharing the same pair_id; a "blocks" outgoing on issue A is
 *  exactly the same physical link as the "blocks" incoming on issue B. */
export type LinkDirection = "outgoing" | "incoming";

export const LINK_TYPES: readonly LinkType[] = [
  "blocks",
  "depends_on",
  "duplicates",
  "relates_to",
] as const;

/** Display labels for picker / chip rendering. Direction-specific so the
 *  same physical link reads naturally from either endpoint:
 *
 *    A blocks B          A depends on B
 *    A blocked by C      A required by D
 */
export const LINK_LABEL: Record<LinkType, { outgoing: string; incoming: string }> = {
  blocks:      { outgoing: "blocks",       incoming: "blocked by" },
  depends_on:  { outgoing: "depends on",   incoming: "required by" },
  duplicates:  { outgoing: "duplicates",   incoming: "duplicated by" },
  relates_to:  { outgoing: "relates to",   incoming: "relates to" },
};

/** Short form for chip badges where space is tight. */
export const LINK_LABEL_SHORT: Record<LinkType, { outgoing: string; incoming: string }> = {
  blocks:      { outgoing: "blocks",       incoming: "blocked by" },
  depends_on:  { outgoing: "depends on",   incoming: "needed by" },
  duplicates:  { outgoing: "duplicates",   incoming: "duplicated by" },
  relates_to:  { outgoing: "relates to",   incoming: "relates to" },
};

/** Issue statuses that are considered "open" — i.e. an outgoing blocks link
 *  pointing at an issue with this status is a real blocker. Mirrors the
 *  ListBlockersForIssue query's filter (NOT IN done/cancelled/duplicate). */
export const OPEN_STATUSES = new Set([
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "ready_for_dev",
  // Plus the BMAD lifecycle statuses, which are all "active":
  "draft",
  "approved",
  "story_implementation_completed",
  "story_qa_passed",
]);

/** A single direction of a link, as returned by the backend per the
 *  perspective of the issue it was loaded for. The "other side" is in the
 *  Target* fields. Matches handler.IssueLinkResponse JSON exactly. */
export interface IssueLink {
  id: string;
  pair_id: string;
  link_type: LinkType;
  direction: LinkDirection;
  creator_type: "member" | "agent" | "system";
  creator_id: string | null;
  created_at: string;

  target_issue_id: string;
  target_identifier: string;        // e.g. "TIM-7"
  target_title: string;
  target_status: string;
  target_number: number;
  target_workspace_id: string;
  target_workspace_name: string;
  target_workspace_slug: string;
}

/** A single open blocker for an issue. Matches handler.IssueBlockerResponse. */
export interface IssueBlocker {
  blocker_issue_id: string;
  blocker_identifier: string;
  blocker_title: string;
  blocker_status: string;
  blocker_number: number;
  blocker_workspace_id: string;
  blocker_workspace_name: string;
  blocker_workspace_slug: string;
}

/** Maximum links per issue. Mirrors handler/link.go maxLinksPerIssue. */
export const MAX_LINKS_PER_ISSUE = 50;
