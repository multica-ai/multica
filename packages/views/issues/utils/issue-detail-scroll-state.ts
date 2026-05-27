"use client";

const SCROLL_ROOT_SELECTOR = "[data-issue-detail-scroll-root]";
const positions = new Map<string, number>();

function keyFor(workspaceId: string, issueId: string) {
  return `${workspaceId}:${issueId}`;
}

export function saveIssueDetailScrollPosition(
  workspaceId: string,
  issueId: string,
  scrollTop: number,
) {
  positions.set(keyFor(workspaceId, issueId), scrollTop);
}

export function getIssueDetailScrollPosition(
  workspaceId: string,
  issueId: string,
) {
  return positions.get(keyFor(workspaceId, issueId));
}

export function saveIssueDetailScrollPositionFromTarget(target: EventTarget | null) {
  if (!(target instanceof Element)) return;
  const root = target.closest<HTMLElement>(SCROLL_ROOT_SELECTOR);
  const workspaceId = root?.dataset.workspaceId;
  const issueId = root?.dataset.issueId;
  if (!root || !workspaceId || !issueId) return;
  saveIssueDetailScrollPosition(workspaceId, issueId, root.scrollTop);
}

export function resetIssueDetailScrollPositionsForTest() {
  positions.clear();
}
