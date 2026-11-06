"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";
import { motion } from "motion/react";
import type { Layout, PanelSize } from "react-resizable-panels";
import {
  isEditableShortcutTarget,
  shortcutMatchesEvent,
  useShortcut,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import { cn } from "@multica/ui/lib/utils";

export const rightSidebarPanelMotionProps = {
  "data-right-sidebar-panel": "true",
  style: { overflowX: "hidden" },
} as const;

const RIGHT_SIDEBAR_PANEL_TRANSITION_MS = 220;
const RIGHT_SIDEBAR_PANEL_SETTLE_MS = RIGHT_SIDEBAR_PANEL_TRANSITION_MS + 80;

const rightSidebarTransition = {
  type: "spring",
  stiffness: 420,
  damping: 38,
  mass: 0.8,
} as const;

export function getAnimatedRightSidebarInitialOpen(
  defaultOpen: boolean,
  defaultLayout: Layout | undefined,
  panelId = "sidebar",
) {
  const restoredSize = defaultLayout?.[panelId];
  return typeof restoredSize === "number" ? restoredSize > 0 : defaultOpen;
}

function isRightSidebarPanelOpen(size: PanelSize) {
  return size.asPercentage > 0 || size.inPixels > 0;
}

export function useAnimatedRightSidebarState(initialOpen: boolean) {
  const [open, setOpen] = useState(initialOpen);
  const [visualOpen, setVisualOpen] = useState(initialOpen);
  const [motionEnabled, setMotionEnabled] = useState(false);
  const toggleTargetRef = useRef<boolean | null>(null);
  const settleTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const beginToggle = useCallback((nextOpen: boolean) => {
    toggleTargetRef.current = nextOpen;
    setMotionEnabled(true);
    setOpen(nextOpen);
    setVisualOpen(nextOpen);

    if (settleTimeoutRef.current) {
      clearTimeout(settleTimeoutRef.current);
    }

    settleTimeoutRef.current = setTimeout(() => {
      toggleTargetRef.current = null;
      settleTimeoutRef.current = null;
      setMotionEnabled(false);
    }, RIGHT_SIDEBAR_PANEL_SETTLE_MS);
  }, []);

  const handleResize = useCallback((size: PanelSize) => {
    const nextOpen = isRightSidebarPanelOpen(size);
    const toggleTarget = toggleTargetRef.current;

    if (toggleTarget === null) {
      setOpen(nextOpen);
      setVisualOpen(nextOpen);
      return;
    }

    setOpen(toggleTarget);
  }, []);

  useEffect(() => {
    return () => {
      if (settleTimeoutRef.current) {
        clearTimeout(settleTimeoutRef.current);
      }
    };
  }, []);

  return { open, visualOpen, motionEnabled, beginToggle, handleResize };
}

/** Toggle the right sidebar only for the visible detail page that owns it. */
export function useRightSidebarShortcut(
  targetRef: RefObject<HTMLElement | null>,
  onToggle: () => void,
) {
  const shortcut = useShortcut("toggleRightSidebar");

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.repeat || isImeComposing(event)) return;
      if (isEditableShortcutTarget(event.target)) return;
      if (!shortcutMatchesEvent(shortcut, event)) return;

      const target = targetRef.current;
      if (!target || target.getClientRects().length === 0) return;

      event.preventDefault();
      onToggle();
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [onToggle, shortcut, targetRef]);
}

export function AnimatedRightSidebar({
  open,
  motionEnabled,
  children,
  className,
  contentClassName = "p-4",
}: {
  open: boolean;
  motionEnabled?: boolean;
  children: ReactNode;
  className?: string;
  contentClassName?: string;
}) {
  return (
    <motion.div
      animate={{ opacity: open ? 1 : 0, x: open ? 0 : 12 }}
      // `relative` makes this scroller the containing block of absolutely
      // positioned descendants (`sr-only` labels in the execution log, etc.).
      // Without it they resolve against an ancestor outside the scroller:
      // they escape its clip and stretch the enclosing panel's scroll height,
      // so wheeling past the end of the comments scrolled the whole issue
      // detail up and left a blank band at the bottom (MUL-8100).
      className={cn(
        "relative h-full overflow-x-hidden overflow-y-auto [scrollbar-gutter:stable] border-l",
        !open && "pointer-events-none",
        className,
      )}
      initial={false}
      transition={motionEnabled ? rightSidebarTransition : { duration: 0 }}
    >
      <div className={contentClassName}>{children}</div>
    </motion.div>
  );
}
