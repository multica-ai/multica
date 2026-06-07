import { useEffect, useState } from "react";
import { api } from "@multica/core/api";
import { useWorkspaceSlug } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

export interface IssueTerminalLink {
  /**
   * Whether the "Open terminal" affordance should be rendered for this
   * task's runtime. True only when:
   *   - the `cerebro_interactive_terminal` feature flag is on
   *   - we know the workspace slug (from the URL)
   *   - the runtime's presentation_mode resolves to "interactive"
   *
   * Lookup failures (e.g. 404 runtime) silently keep this false so the
   * upstream call-site can guard with a single `link.shouldShow` check
   * without dealing with API errors.
   */
  shouldShow: boolean;
  /** Pre-built popout URL when shouldShow is true; null otherwise. */
  popoutUrl: string | null;
}

/**
 * Derives whether the active-agent banner on an issue should expose an
 * "Open terminal" link for the given runtime.
 *
 * The hook keeps the upstream issue-detail surface small: callers render
 *   `link.shouldShow ? <a href={link.popoutUrl} ...>Open terminal</a> : null`
 * which is one inline JSX line guarded by a single boolean — well under
 * the 5-line CEREBRO-PATCH budget for upstream-zone files.
 *
 * Behaviour:
 *   - `runtimeId` empty / missing → never shows (placeholder for tasks
 *     that don't carry runtime info, e.g. very old payloads).
 *   - Feature flag off → never shows.
 *   - presentation_mode === "headless" → never shows.
 *   - Network error fetching mode → never shows (silent fallback).
 */
export function useIssueTerminalLink(
  runtimeId: string | null | undefined,
): IssueTerminalLink {
  const flagOn = useFeatureFlag("cerebro_interactive_terminal");
  const slug = useWorkspaceSlug();
  const [mode, setMode] = useState<"headless" | "interactive" | null>(null);

  useEffect(() => {
    if (!flagOn || !runtimeId) {
      setMode(null);
      return;
    }
    let cancelled = false;
    api
      .getRuntimePresentationMode(runtimeId)
      .then((r) => {
        if (!cancelled) setMode(r.presentation_mode);
      })
      .catch(() => {
        // Silent: the link is purely additive; a lookup failure should
        // never produce an error toast on the issue page.
        if (!cancelled) setMode(null);
      });
    return () => {
      cancelled = true;
    };
  }, [flagOn, runtimeId]);

  const shouldShow =
    flagOn && !!slug && !!runtimeId && mode === "interactive";

  return {
    shouldShow,
    popoutUrl: shouldShow ? `/${slug}/terminal/${runtimeId}` : null,
  };
}

/**
 * Opens the terminal for a runtime in a new browser tab/window. Uses a
 * stable per-runtime target name so re-clicking from a different surface
 * focuses the existing tab instead of spawning duplicates. No window
 * features are passed — modern browsers treat that as a regular tab,
 * which is what the user expects (no chromeless popup quirks).
 */
export function openTerminalPopout(popoutUrl: string, runtimeId: string) {
  window.open(popoutUrl, `terminal-${runtimeId}`);
}
