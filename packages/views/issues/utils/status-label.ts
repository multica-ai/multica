"use client";

import { useCallback } from "react";
import { isIssueStatusCategory } from "@multica/core/issue-statuses";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type { IssueStatusCategory } from "@multica/core/types";
import { useT } from "../../i18n";

/**
 * Resolves a status KEY to the label the UI should show (MUL-6243).
 *
 * The catalog carries a `name` for every status including the 7 built-ins, but
 * those built-in names are seeded in English by the server. Rendering them
 * verbatim would turn "进行中" into "In Progress" for every non-English
 * workspace, so built-ins always resolve through i18n and only custom statuses
 * — whose names the workspace authored and which are not translatable — use
 * the catalog's `name`.
 *
 * A key the catalog has never heard of (created seconds ago elsewhere, or a
 * fetch still in flight) falls back to the raw key rather than rendering blank.
 */
export function useStatusLabel(wsId: string) {
  const { t } = useT("issues");
  const { entryOf } = useIssueStatuses(wsId);

  return useCallback(
    (statusKey: string): string => {
      if (isIssueStatusCategory(statusKey)) {
        return t(($) => $.status[statusKey as IssueStatusCategory]);
      }
      return entryOf(statusKey)?.name ?? statusKey;
    },
    [t, entryOf],
  );
}
