"use client";

import type { ReactNode } from "react";
import { MotionConfig } from "motion/react";

/**
 * Root Framer Motion configuration shared by Web and Desktop.
 * `reducedMotion="user"` honors OS `prefers-reduced-motion: reduce` globally
 * across every `motion.*` element, complementing per-component reduced-motion
 * overrides.
 */
export function MotionProvider({ children }: { children: ReactNode }) {
  return <MotionConfig reducedMotion="user">{children}</MotionConfig>;
}
