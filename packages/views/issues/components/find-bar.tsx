"use client";

import type { KeyboardEvent } from "react";
import { motion } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { isImeComposing } from "@multica/core/utils";
import { ChevronDown, ChevronUp, Search, X } from "@multica/ui/icons";
import { Input } from "@multica/ui/components/ui/input";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { UI_EASE_IN, UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { UseInPageFindResult } from "../hooks/use-in-page-find";

// Floating find-in-page bar for the issue detail page. Presentational — all
// search state, highlighting, and scrolling live in `useInPageFind`. Rendered
// only while `find.open`, so the input's autoFocus fires on every open.
//
// `data-find-ignore` keeps the bar's own text out of the match walk.
export function FindBar({
  find,
  className,
}: {
  find: UseInPageFindResult;
  className?: string;
}) {
  const { t } = useT("issues");
  const reduceMotion = useReducedMotion();
  const {
    query,
    matchCount,
    activeIndex,
    setQuery,
    closeFind,
    goNext,
    goPrev,
    inputRef,
  } = find;

  const hasQuery = query.trim().length > 0;
  const countLabel = !hasQuery
    ? ""
    : matchCount === 0
      ? t(($) => $.detail.find.no_matches)
      : t(($) => $.detail.find.count, {
          current: activeIndex + 1,
          total: matchCount,
        });
  const noMatches = matchCount === 0;

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (isImeComposing(e)) return;
    if (e.key === "Enter") {
      e.preventDefault();
      if (e.shiftKey) goPrev();
      else goNext();
    } else if (e.key === "Escape") {
      e.preventDefault();
      closeFind();
    }
  };

  return (
    <motion.div
      data-find-ignore
      role="search"
      initial={reduceMotion ? false : { opacity: 0, y: -6, scale: 0.98 }}
      animate={{
        opacity: 1,
        y: 0,
        scale: 1,
        transition: { duration: reduceMotion ? 0 : UI_MOTION_DURATION.fast, ease: UI_EASE_OUT },
      }}
      exit={
        reduceMotion
          ? { opacity: 0, transition: { duration: 0 } }
          : {
              opacity: 0,
              y: -4,
              scale: 0.98,
              transition: { duration: UI_MOTION_DURATION.micro, ease: UI_EASE_IN },
            }
      }
      className={cn(
        "flex items-center gap-1 rounded-lg border bg-popover/95 p-1 pl-2 shadow-md backdrop-blur supports-[backdrop-filter]:bg-popover/80",
        className,
      )}
    >
      <Search className="size-3.5 shrink-0 text-muted-foreground" />
      <Input
        ref={inputRef}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={handleKeyDown}
        autoFocus
        placeholder={t(($) => $.detail.find.placeholder)}
        aria-label={t(($) => $.detail.find.placeholder)}
        className="h-7 w-44 border-0 bg-transparent px-1 shadow-none focus-visible:ring-0"
      />
      <span className="min-w-[3.5rem] shrink-0 whitespace-nowrap text-right text-caption tabular-nums text-muted-foreground">
        {countLabel}
      </span>
      <div className="flex items-center">
        <IconButton
          type="button"
          variant="ghost"
          size="icon-xs"
          disabled={noMatches}
          onClick={goPrev}
          label={t(($) => $.detail.find.previous)}
          tooltipSide="bottom"
        >
          <ChevronUp />
        </IconButton>
        <IconButton
          type="button"
          variant="ghost"
          size="icon-xs"
          disabled={noMatches}
          onClick={goNext}
          label={t(($) => $.detail.find.next)}
          tooltipSide="bottom"
        >
          <ChevronDown />
        </IconButton>
        <IconButton
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={closeFind}
          label={t(($) => $.detail.find.close)}
          tooltipSide="bottom"
        >
          <X />
        </IconButton>
      </div>
    </motion.div>
  );
}
