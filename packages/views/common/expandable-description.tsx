"use client";

import { useCallback, useLayoutEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

/**
 * The description line under a detail page's title. Clamped to two lines, with
 * a toggle that appears only when there is something clamped to reveal.
 *
 * Left unclamped, a long description pushes the meta row and the tab strip
 * down the page, so how far the content starts depends on how much prose
 * someone wrote. Two lines is where the agent and skill headers already sit
 * visually; the toggle is what makes clamping safe, since without it the rest
 * of the text has nowhere to be — this IS the detail page, so expanding in
 * place is the only move that does not send the reader somewhere else to read
 * one paragraph.
 *
 * The toggle is deliberately quiet — muted until hovered, matching the
 * "show more" control on the agent activity tab. Brand colour is reserved for
 * primary actions and live state; this page's Save and Add-to-agent buttons
 * are what should be drawing the eye, not a text expander.
 */
export function ExpandableDescription({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  const { t } = useT("common");
  const [expanded, setExpanded] = useState(false);
  const [clamped, setClamped] = useState(false);
  const textRef = useRef<HTMLParagraphElement | null>(null);

  // Measured rather than guessed from character count: whether two lines are
  // enough depends on the container's width and the script being rendered.
  const measure = useCallback(() => {
    const element = textRef.current;
    if (!element) return;
    // Read while clamped, or an already-expanded element reports no overflow
    // and the toggle would vanish mid-read.
    if (expanded) return;
    setClamped(element.scrollHeight > element.clientHeight + 1);
  }, [expanded]);

  useLayoutEffect(() => {
    measure();
    const element = textRef.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [measure, children]);

  return (
    <>
      <p
        ref={textRef}
        className={cn(
          "mt-1 max-w-2xl text-pretty text-body leading-6 text-muted-foreground",
          !expanded && "line-clamp-2",
          className,
        )}
      >
        {children}
      </p>
      {(clamped || expanded) && (
        <button
          type="button"
          onClick={() => setExpanded((value) => !value)}
          className="mt-0.5 rounded text-caption text-muted-foreground transition-colors hover:text-foreground focus-visible:text-foreground focus-visible:outline-none"
        >
          {expanded
            ? t(($) => $.expandable_description.collapse)
            : t(($) => $.expandable_description.expand)}
        </button>
      )}
    </>
  );
}
