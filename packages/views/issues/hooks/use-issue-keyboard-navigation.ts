"use client";

import { useCallback, useEffect, useMemo, useState } from "react";

const ISSUE_KEYBOARD_ATTR = "data-issue-keyboard-id";

export function isIssueKeyboardShortcutTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;

  const tagName = target.tagName.toLowerCase();
  if (tagName === "input" || tagName === "textarea" || tagName === "select") {
    return true;
  }

  return !!target.closest(
    [
      "[contenteditable='true']",
      "[role='dialog']",
      "[role='menu']",
      "[role='listbox']",
      "[role='combobox']",
      "[data-radix-popper-content-wrapper]",
    ].join(","),
  );
}

export function nextIssueKeyboardIndex(
  currentId: string | null,
  issueIds: readonly string[],
  direction: 1 | -1,
): number {
  if (issueIds.length === 0) return -1;

  const currentIndex = currentId ? issueIds.indexOf(currentId) : -1;
  if (currentIndex === -1) return direction === 1 ? 0 : issueIds.length - 1;

  return Math.min(issueIds.length - 1, Math.max(0, currentIndex + direction));
}

function scrollIssueIntoView(issueId: string) {
  document
    .querySelector<HTMLElement>(`[${ISSUE_KEYBOARD_ATTR}="${issueId}"]`)
    ?.scrollIntoView({ block: "nearest", inline: "nearest" });
}

export function useIssueKeyboardNavigation({
  issueIds,
  onOpenIssue,
  disabled = false,
}: {
  issueIds: readonly string[];
  onOpenIssue: (issueId: string) => void;
  disabled?: boolean;
}) {
  const [activeIssueId, setActiveIssueId] = useState<string | null>(null);
  const stableIssueIds = useMemo(() => issueIds.filter(Boolean), [issueIds]);

  useEffect(() => {
    if (!activeIssueId) return;
    if (!stableIssueIds.includes(activeIssueId)) {
      setActiveIssueId(stableIssueIds[0] ?? null);
    }
  }, [activeIssueId, stableIssueIds]);

  useEffect(() => {
    if (disabled || stableIssueIds.length === 0) return;

    const handler = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return;
      if (event.isComposing || isIssueKeyboardShortcutTarget(event.target)) return;

      if (event.key === "j" || event.key === "k") {
        event.preventDefault();
        const direction = event.key === "j" ? 1 : -1;
        setActiveIssueId((currentId) => {
          const nextIndex = nextIssueKeyboardIndex(currentId, stableIssueIds, direction);
          const nextId = nextIndex >= 0 ? stableIssueIds[nextIndex] ?? null : null;
          if (nextId) requestAnimationFrame(() => scrollIssueIntoView(nextId));
          return nextId;
        });
        return;
      }

      if (event.key === "Enter" && activeIssueId) {
        event.preventDefault();
        onOpenIssue(activeIssueId);
      }
    };

    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [activeIssueId, disabled, onOpenIssue, stableIssueIds]);

  const issueKeyboardProps = useCallback(
    (issueId: string) => ({
      [ISSUE_KEYBOARD_ATTR]: issueId,
      onMouseEnter: () => setActiveIssueId(issueId),
      onFocus: () => setActiveIssueId(issueId),
    }),
    [],
  );

  return {
    activeIssueId,
    issueKeyboardProps,
    setActiveIssueId,
  };
}
