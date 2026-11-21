import { useEffect } from "react";
import { useLocation } from "react-router-dom";
import { useTabPresentation } from "@multica/views/layout";

/**
 * The only writer of document.title (the OS window title). It resolves the
 * current URL through the same contract as the tab strip, so the window title
 * matches the active tab, is localized, and re-resolves when the interface
 * language changes or the resource finishes loading. Must render inside a
 * WorkspaceSlugProvider so resource names can resolve.
 */
export function DocumentTitleSync() {
  const { pathname, search } = useLocation();
  const { title } = useTabPresentation(pathname + search);

  useEffect(() => {
    document.title = title;
  }, [title]);

  return null;
}
