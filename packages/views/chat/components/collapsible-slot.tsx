"use client";

import type { ReactNode } from "react";
import { AnimatePresence, motion } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";

export function CollapsibleSlot({
  slotKey,
  visible,
  children,
}: {
  slotKey: string;
  visible: boolean;
  children: ReactNode;
}) {
  const reduceMotion = useReducedMotion();
  const duration = reduceMotion ? 0 : UI_MOTION_DURATION.fast;

  return (
    <AnimatePresence initial={false}>
      {visible && (
        <motion.div
          key={slotKey}
          data-slot={slotKey}
          initial={{ opacity: 0, height: 0, overflow: "hidden" }}
          animate={{
            opacity: 1,
            height: "auto",
            transitionEnd: { overflow: "visible" },
          }}
          exit={{ opacity: 0, height: 0, overflow: "hidden" }}
          transition={{ duration, ease: UI_EASE_OUT }}
          className="shrink-0"
        >
          {children}
        </motion.div>
      )}
    </AnimatePresence>
  );
}
