"use client";

import { forwardRef } from "react";
import { useNavigation } from "./context";

interface AppLinkProps extends React.AnchorHTMLAttributes<HTMLAnchorElement> {
  href: string;
  /**
   * Desktop only: label for the tab created when the click opens a new tab
   * (modifier-click or `target="_blank"`). Falls back to the path.
   */
  newTabTitle?: string;
}

export const AppLink = forwardRef<HTMLAnchorElement, AppLinkProps>(
  function AppLink(
    { href, children, onClick, onMouseEnter, onFocus, target, newTabTitle, ...props },
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
        // silently overridden by a caller passing onClick/onMouseEnter/onFocus
        // through {...rest}. AppLink owns these three events.
        {...props}
        onClick={handleClick}
        onMouseEnter={handleMouseEnter}
        onFocus={handleFocus}
      >
        {children}
      </a>
    );
  },
);
