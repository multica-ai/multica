"use client";

import type { ReactNode } from "react";
import { MotionConfig } from "motion/react";
import { useAppearanceStore } from "@multica/core/appearance";

/**
 * Root motion configuration for Web and Desktop apps.
 * Ensures all `motion/react` animations honor both OS `prefers-reduced-motion`
 * and the in-app Appearance "Reduce motion" toggle.
 */
export function MotionProvider({ children }: { children: ReactNode }) {
  const reduceMotion = useAppearanceStore((s) => s.reduceMotion);
  return (
    <MotionConfig reducedMotion={reduceMotion ? "always" : "user"}>
      {children}
    </MotionConfig>
  );
}
