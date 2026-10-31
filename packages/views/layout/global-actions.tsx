"use client";

import type { ComponentProps } from "react";
import { Search, SquarePen } from "@multica/ui/icons";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { openCreateIssueWithPreference } from "@multica/core/issues/stores";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { useShortcut } from "@multica/core/shortcuts";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { useSearchStore } from "../search/search-store";

type TooltipSide = ComponentProps<typeof TooltipHint>["side"];

/**
 * Opens the create-issue modal the way the `C` shortcut does: a project
 * page files the issue into that project, anywhere else starts it empty.
 */
export function openCreateIssueForPath(pathname: string) {
  const projectMatch = pathname.match(/^\/[^/]+\/projects\/([^/]+)$/);
  openCreateIssueWithPreference(projectMatch ? { project_id: projectMatch[1] } : undefined);
}

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => s.hasDraft());
  if (!hasDraft) return null;
  return <span aria-hidden className="absolute -top-0.5 -right-0.5 size-1.5 rounded-full bg-brand" />;
}

/** The app-wide New issue action, reachable from every space. */
export function NewIssueButton({
  className,
  tooltipSide,
}: {
  className: string;
  tooltipSide: TooltipSide;
}) {
  const { t } = useT("layout");
  const { pathname } = useNavigation();
  const shortcut = useShortcut("createIssue");
  const label = t(($) => $.sidebar.new_issue);
  return (
    <TooltipHint
      label={label}
      shortcut={shortcut ? <ShortcutKeycaps shortcut={shortcut} decorative className="ml-1.5" /> : null}
      side={tooltipSide}
    >
      <button
        type="button"
        aria-label={label}
        data-action="new-issue"
        onClick={() => openCreateIssueForPath(pathname)}
        className={className}
      >
        <span className="relative flex">
          <SquarePen />
          <DraftDot />
        </span>
      </button>
    </TooltipHint>
  );
}

export function SearchButton({
  className,
  tooltipSide,
}: {
  className: string;
  tooltipSide: TooltipSide;
}) {
  const { t } = useT("search");
  const shortcut = useShortcut("openSearch");
  const label = t(($) => $.trigger.label);
  return (
    <TooltipHint
      label={label}
      shortcut={shortcut ? <ShortcutKeycaps shortcut={shortcut} decorative className="ml-1.5" /> : null}
      side={tooltipSide}
    >
      <button
        type="button"
        aria-label={label}
        data-action="search"
        onClick={() => useSearchStore.getState().setOpen(true)}
        className={className}
      >
        <Search />
      </button>
    </TooltipHint>
  );
}
