import type { Approval } from "../../core/types";

// Human-readable display helpers for the approval inbox (TECH-3498). The UI must
// NEVER render a raw UUID — these resolve a name first, then fall back to a
// humanized type label, never an id.

const REQUESTER_TYPE_LABELS: Record<string, string> = {
  agent: "Agent",
  member: "Member",
};

// requesterLabel returns the requester's display name, falling back to a
// humanized requester type. It never returns a UUID.
export function requesterLabel(a: Approval): string {
  if (a.requester_name && a.requester_name.trim()) return a.requester_name;
  const type = a.requester_type || "";
  return REQUESTER_TYPE_LABELS[type] ?? "Unknown";
}

// issueLabel returns "IDENTIFIER — title" (or just one of them) for the issue an
// approval was requested on, or "" when no issue is linked.
export function issueLabel(a: Approval): string {
  const id = a.issue_identifier?.trim() ?? "";
  const title = a.issue_title?.trim() ?? "";
  if (id && title) return `${id} — ${title}`;
  return id || title;
}

// hasIssue reports whether an approval carries any issue context to show.
export function hasIssue(a: Approval): boolean {
  return Boolean(a.issue_identifier?.trim() || a.issue_title?.trim());
}
