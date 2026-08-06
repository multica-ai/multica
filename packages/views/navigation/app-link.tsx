"use client";

import { forwardRef } from "react";
import { useNavigation } from "./context";

interface AppLinkProps extends React.AnchorHTMLAttributes<HTMLAnchorElement> {
  href: string;
  /**
   * Desktop only: label for the tab created when the click opens a new tab
   * (modifier-click, middle click, or `target="_blank"`). Falls back to the
   * path.
   */
  newTabTitle?: string;
}

export const AppLink = forwardRef<HTMLAnchorElement, AppLinkProps>(
  function AppLink(
    {
      href,
      children,
      onClick,
      onAuxClick,
      onMouseEnter,
      onFocus,
      target,
      newTabTitle,
      ...props
    },
    ref,
  ) {
    const { push, openInNewTab, prefetch } = useNavigation();

    const handleClick = (e: React.MouseEvent<HTMLAnchorElement>) => {
      if (e.metaKey || e.ctrlKey || e.shiftKey) {
        if (openInNewTab) {
          e.preventDefault();
          openInNewTab(href, newTabTitle);
        }
        return;
      }
      if (target === "_blank") {
        // Caller's onClick runs first — same contract as the push path below.
        onClick?.(e);
        if (openInNewTab) {
          // Desktop: foreground app tab. target="_blank" carries "take me
          // there" intent, matching the browser's foreground-tab behavior.
          e.preventDefault();
          openInNewTab(href, newTabTitle, { activate: true });
        }
        // Web: no adapter — leave the event alone so the browser's native
        // target="_blank" handling opens a real browser tab.
        return;
      }
      e.preventDefault();
      // Caller's onClick runs BEFORE push so any synchronous side effect
      // (close popover, clear selection, blur the trigger) lands in the
      // same tick rather than getting deferred behind the transition.
      onClick?.(e);
      push(href);
    };

    // A middle click never produces a `click` event, so handleClick above
    // never sees one — it has to be handled on its own event.
    //
    // Desktop needs this because the native path is a dead end: Chromium
    // raises a window-open request that the shell's setWindowOpenHandler
    // denies and hands to openExternalSafely. In a packaged build the
    // renderer is `file://`, so this href resolves to `file:///<path>` and
    // the http/https allowlist drops it — the click does nothing at all. In
    // dev the renderer is `http://localhost:<port>`, which clears the
    // allowlist and throws the click out into the system browser instead.
    // Both were reproduced against Electron 39 before this handler existed.
    const handleAuxClick = (e: React.MouseEvent<HTMLAnchorElement>) => {
      // The caller sees every aux click, and can opt out of the new tab by
      // calling preventDefault — the same escape hatch a child element inside
      // the link has.
      onAuxClick?.(e);
      if (e.defaultPrevented || e.button !== 1) return; // middle click only
      // Web: no adapter, and this is a real anchor, so the browser already
      // opens a background tab — exactly what a middle click should do.
      // Preventing the default here would break it.
      if (!openInNewTab) return;
      e.preventDefault();
      // Background tab, never activated: a middle click means "keep me here",
      // matching the cmd/ctrl branch in handleClick. That holds for
      // target="_blank" links too — browsers background a middle click
      // regardless of target.
      openInNewTab(href, newTabTitle);
    };

    const handleMouseEnter = (e: React.MouseEvent<HTMLAnchorElement>) => {
      prefetch?.(href);
      onMouseEnter?.(e);
    };

    const handleFocus = (e: React.FocusEvent<HTMLAnchorElement>) => {
      prefetch?.(href);
      onFocus?.(e);
    };

    return (
      <a
        ref={ref}
        href={href}
        target={target}
        // Referrer is same-origin noise here and noopener hygiene applies
        // even though the destination is our own app.
        rel={target === "_blank" ? "noopener noreferrer" : undefined}
        // Spread props first so that the navigation handlers below cannot be
        // silently overridden by a caller passing
        // onClick/onAuxClick/onMouseEnter/onFocus through {...rest}. AppLink
        // owns these four events.
        {...props}
        onClick={handleClick}
        onAuxClick={handleAuxClick}
        onMouseEnter={handleMouseEnter}
        onFocus={handleFocus}
      >
        {children}
      </a>
    );
  },
);
