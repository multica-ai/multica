"use client";

import type { ReactNode } from "react";
import { AnimatePresence, motion, useIsPresent } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";

function CollapsibleSlotInner({
  slotKey,
  children,
}: {
  slotKey: string;
  children: ReactNode;
}) {
  const isPresent = useIsPresent();
  return (
    <motion.div
      data-slot={slotKey}
      inert={!isPresent}
      initial={{ opacity: 0, height: 0, overflow: "hidden" }}
      animate={{
        opacity: 1,
        height: "auto",
        transitionEnd: { overflow: "visible" },
      }}
      exit={{ opacity: 0, height: 0, overflow: "hidden" }}
      transition={{ duration: UI_MOTION_DURATION.fast, ease: UI_EASE_OUT }}
      className={cn("shrink-0", !isPresent && "pointer-events-none")}
    >
      {children}
    </motion.div>
  );
}

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

  if (reduceMotion) {
    return visible ? (
      <div data-slot={slotKey} className="shrink-0">
        {children}
      </div>
    ) : null;
  }

  return (
    <AnimatePresence initial={false}>
      {visible && (
        <CollapsibleSlotInner key={slotKey} slotKey={slotKey}>
          {children}
        </CollapsibleSlotInner>
      )}
    </AnimatePresence>
  );
}
