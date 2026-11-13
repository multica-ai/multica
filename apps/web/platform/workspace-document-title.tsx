"use client";

import { useEffect, useMemo, useRef } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { parseTabSubject } from "@multica/core/paths";
import { useTabPresentation } from "@multica/views/layout";
import { SITE_TITLE, formatDocumentTitle } from "./document-title";

/**
 * Names the browser tab after whatever the workspace route has open, e.g.
 * `MUL-123: Fix login | Multica` (MUL-6222). Without it every open dashboard
 * tab renders the root metadata title, so several issues side by side are
 * indistinguishable until you click into each one.
 *
 * The URL → title resolution is not ours: it is the same pure subject parser
 * plus cache-backed presentation hook that names desktop's tabs, so a browser
 * tab and a desktop tab for one URL always read identically, in the user's
 * locale, and a renamed project or a changed issue title re-titles the tab
 * live. Every query behind it is cache-only, so this costs no extra request.
 *
 * Mounted once per dashboard layout — one writer for the whole tree. A page
 * that wants a different title should teach `resolveTabPresentation` about its
 * subject rather than setting `document.title` behind this component's back.
 */
export function WorkspaceDocumentTitle() {
  const pathname = usePathname();
  const searchParams = useSearchParams();

  // Inbox and Chat carry their selection in the query string, and the subject
  // parser reads it, so the title has to be recomputed from the full URL.
  const search = searchParams.toString();
  const url = search ? `${pathname}?${search}` : pathname;

  const subject = useMemo(() => parseTabSubject(url), [url]);
  const { title } = useTabPresentation(url);

  // An unrecognized URL gets the site title, never the "Unknown page" label a
  // tab strip needs: a browser tab has no other place to show the product name.
  const documentTitle =
    subject.kind === "unknown" ? SITE_TITLE : formatDocumentTitle(title);
  const documentTitleRef = useRef(documentTitle);
  documentTitleRef.current = documentTitle;

  // Keep one observer for the dashboard lifetime and have it read the latest
  // intended title from a ref. Recreating an observer per route leaves a window
  // where a queued callback from the previous issue can restore its stale title.
  useEffect(() => {
    const applyLatestTitle = () => {
      const latest = documentTitleRef.current;
      if (document.title !== latest) document.title = latest;
    };

    const observer = new MutationObserver(applyLatestTitle);
    observer.observe(document.head, { childList: true, subtree: true });

    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (document.title !== documentTitle) document.title = documentTitle;
  }, [documentTitle, url]);

  // Leaving the dashboard entirely (logout, workspace switcher, landing) drops
  // back to the site title so the stale issue name never outlives its page.
  useEffect(() => {
    return () => {
      document.title = SITE_TITLE;
    };
  }, []);

  return null;
}
