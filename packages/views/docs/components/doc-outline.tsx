"use client";
import { useEffect, useState, type RefObject } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { watchDom } from "./dom-throttle";

export interface OutlineHeading {
  level: number;
  text: string;
  element: HTMLElement;
}

/** The outline shows only when a document has at least this many headings. */
export const MIN_OUTLINE_HEADINGS = 3;
/** Distance below the scroll top at which a heading counts as the current one. */
const ACTIVE_OFFSET = 96;
/** Space kept above a heading the outline scrolls to. */
const SCROLL_MARGIN = 24;

/** Top-level H1–H3 of the rendered document, in order; empty headings skipped. */
export function readOutline(root: ParentNode): OutlineHeading[] {
  return Array.from(
    root.querySelectorAll<HTMLElement>(
      ".ProseMirror > h1, .ProseMirror > h2, .ProseMirror > h3",
    ),
  )
    .map((element) => ({
      level: Number(element.tagName.slice(1)),
      text: element.textContent?.trim() ?? "",
      element,
    }))
    .filter((heading) => heading.text !== "");
}

function sameOutline(a: OutlineHeading[], b: OutlineHeading[]): boolean {
  return (
    a.length === b.length &&
    a.every(
      (heading, i) =>
        heading.level === b[i]!.level &&
        heading.text === b[i]!.text &&
        heading.element === b[i]!.element,
    )
  );
}

/**
 * Headings of the editor under `rootRef`, kept current as the user types, and
 * which one the scroll position of `scrollRef` is in.
 */
export function useDocOutline(
  rootRef: RefObject<HTMLElement | null>,
  scrollRef: RefObject<HTMLElement | null>,
) {
  const [headings, setHeadings] = useState<OutlineHeading[]>([]);
  const [activeIndex, setActiveIndex] = useState(0);

  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    // The editor mounts after first paint and re-renders headings in place, so
    // watch the DOM rather than the Markdown.
    return watchDom(root, () => {
      const next = readOutline(root);
      setHeadings((prev) => (sameOutline(prev, next) ? prev : next));
    });
  }, [rootRef]);

  useEffect(() => {
    const scroller = scrollRef.current;
    if (!scroller || headings.length === 0) return;
    const update = () => {
      const line = scroller.getBoundingClientRect().top + ACTIVE_OFFSET;
      let active = 0;
      headings.forEach((heading, i) => {
        if (heading.element.getBoundingClientRect().top <= line) active = i;
      });
      setActiveIndex(active);
    };
    update();
    scroller.addEventListener("scroll", update, { passive: true });
    return () => scroller.removeEventListener("scroll", update);
  }, [headings, scrollRef]);

  const scrollTo = (heading: OutlineHeading) => {
    const scroller = scrollRef.current;
    if (!scroller) return;
    const offset =
      heading.element.getBoundingClientRect().top -
      scroller.getBoundingClientRect().top;
    scroller.scrollTo({
      top: scroller.scrollTop + offset - SCROLL_MARGIN,
      behavior: "smooth",
    });
  };

  return { headings, activeIndex, scrollTo };
}

export function DocOutline({
  headings,
  activeIndex,
  onSelect,
}: {
  headings: OutlineHeading[];
  activeIndex: number;
  onSelect: (heading: OutlineHeading) => void;
}) {
  const { t } = useT("docs");
  const minLevel = Math.min(...headings.map((heading) => heading.level));
  return (
    <nav
      aria-label={t(($) => $.outline)}
      className="sticky top-8 max-h-[70vh] overflow-y-auto"
    >
      <ul className="border-l">
        {headings.map((heading, i) => (
          <li key={`${i}:${heading.text}`}>
            <button
              type="button"
              aria-current={i === activeIndex ? "location" : undefined}
              onClick={() => onSelect(heading)}
              className={cn(
                "-ml-px block w-full truncate border-l-2 py-1 pr-2 text-left text-caption transition-colors",
                i === activeIndex
                  ? "border-foreground text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground",
              )}
              style={{ paddingLeft: 12 + (heading.level - minLevel) * 12 }}
            >
              {heading.text}
            </button>
          </li>
        ))}
      </ul>
    </nav>
  );
}
