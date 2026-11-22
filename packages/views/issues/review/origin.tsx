"use client";

import { useContext, useEffect, useLayoutEffect, useMemo, type ReactNode } from "react";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import type { IssueTableQuerySpec } from "@multica/core/types";
import { currentPath } from "../../navigation/current-path";
import { useNavigation } from "../../navigation/context";
import { ReviewOriginContext, ReviewSessionContext, reviewHref } from "./session";

export function ReviewOriginProvider({ query, enabled, sourceId, children }: {
  query: IssueTableQuerySpec; enabled: boolean; sourceId?: string; children: ReactNode;
}) {
  const session = useContext(ReviewSessionContext);
  const navigation = useNavigation();
  const workspaceId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const listPath = currentPath(navigation);
  // Projection membership changes are data changes, not a new saved view or
  // filter meaning. Refresh them for each operation instead of expiring Undo.
  const { working_issue_ids, unread_issue_ids, today, ...staticFilters } = query.filters;
  const sourceKey = JSON.stringify([workspaceId, userId, listPath, sourceId, {
    ...query, filters: {
      ...staticFilters, working: working_issue_ids !== undefined,
      unread: unread_issue_ids !== undefined, relativeDay: today !== undefined,
    },
  }]);

  useEffect(() => {
    // Returning from the final item keeps its receipt. Changing the source
    // meaning or identity expires old callbacks and Undo.
    if (session?.origin && session.origin.sourceKey !== sourceKey) session.invalidate();
  }, [session, sourceKey]);

  useLayoutEffect(() => {
    const origin = session?.origin;
    if (!origin || origin.sourceKey !== sourceKey || listPath !== origin.listPath) return;
    const frame = requestAnimationFrame(() => {
      const elements = document.querySelectorAll<HTMLElement>("main, [data-scroll-restoration], [data-slot=scroll-area-viewport]");
      origin.scroll.forEach((position, index) => {
        const element = elements[index];
        if (element) { element.scrollTop = position.top; element.scrollLeft = position.left; }
      });
    });
    return () => cancelAnimationFrame(frame);
  }, [session, sourceKey, listPath]);

  const open = useMemo(() => enabled && userId && session ? (href: string) => {
    session.invalidate();
    const token = crypto.randomUUID();
    session.origin = {
      token, workspaceId, userId, listPath, sourceKey,
      query: structuredClone(query),
      scroll: [...document.querySelectorAll<HTMLElement>("main, [data-scroll-restoration], [data-slot=scroll-area-viewport]")].map((element) => ({
        top: element.scrollTop, left: element.scrollLeft,
      })),
    };
    return reviewHref(href, token);
  } : null, [enabled, userId, session, workspaceId, listPath, sourceKey, query]);

  return <ReviewOriginContext.Provider value={open}>{children}</ReviewOriginContext.Provider>;
}
