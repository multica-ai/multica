"use client";

import { useRef, type ReactNode } from "react";
import { AnimatePresence, motion } from "motion/react";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useNavigation } from "../navigation";

// Mobile-only route transition. Wraps DashboardLayout's main content area
// in AnimatePresence keyed by pathname so that route changes produce an
// iOS-style slide rather than a hard cut. Desktop renders children directly.
//
// Two transition shapes:
// - Sibling tab swap (e.g. Inbox -> Issues): cross-fade with a small vertical
//   nudge — no horizontal direction is meaningful between unrelated tops.
// - Drill / pop (entering or leaving a detail page): slide horizontally,
//   right-to-left for push, left-to-right for back. We detect drill by
//   comparing current depth to the previous pathname's depth: deeper means
//   push, shallower means pop, equal means tab swap.
//
// pathname can be "" before NavigationAdapter mounts; skip animating the
// empty key so the very first render doesn't flash.
const PATH_DEPTH_RE = /\//g;
function pathDepth(p: string) {
  // count slashes — sufficient to distinguish /a/b vs /a/b/c without brittle
  // segment-by-segment compares (workspace slug, ids, etc. all just bump depth).
  return (p.match(PATH_DEPTH_RE) ?? []).length;
}

export function MobilePageTransition({ children }: { children: ReactNode }) {
  const isMobile = useIsMobile();
  const { pathname } = useNavigation();
  const prevRef = useRef(pathname);

  if (!isMobile) return <>{children}</>;

  const prev = prevRef.current;
  prevRef.current = pathname;

  const prevDepth = pathDepth(prev);
  const curDepth = pathDepth(pathname);
  const direction =
    curDepth > prevDepth ? "push" : curDepth < prevDepth ? "pop" : "swap";

  // iOS uses ~50% screen translate; we use 24px because cross-fade does most
  // of the lift and a full slide on a single mounted element would feel
  // heavier than the hardware can keep up with under a route transition.
  const initial =
    direction === "push"
      ? { opacity: 0, x: 24, y: 0 }
      : direction === "pop"
        ? { opacity: 0, x: -24, y: 0 }
        : { opacity: 0, x: 0, y: 8 };
  const exit =
    direction === "push"
      ? { opacity: 0, x: -16, y: 0 }
      : direction === "pop"
        ? { opacity: 0, x: 16, y: 0 }
        : { opacity: 0, x: 0, y: -6 };

  return (
    <AnimatePresence mode="wait" initial={false}>
      <motion.div
        key={pathname || "_init"}
        initial={initial}
        animate={{ opacity: 1, x: 0, y: 0 }}
        exit={exit}
        // 260ms with iOS-style decel curve — enough to feel intentional
        // without slowing tab swaps to a crawl. cubic-bezier(0.32, 0.72, 0, 1)
        // is the same easing the system uses for sheet present/dismiss.
        transition={{ duration: 0.26, ease: [0.32, 0.72, 0, 1] }}
        className="flex flex-1 min-h-0 flex-col will-change-transform"
      >
        {children}
      </motion.div>
    </AnimatePresence>
  );
}
