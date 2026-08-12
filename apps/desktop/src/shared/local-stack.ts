// src/shared/local-stack.ts
// Shared between main (produces state), preload (bridges it), and renderer
// (renders it). Kept free of node/electron imports for that reason.

export type LocalStackStep =
  | "config"
  | "probe"
  | "engine"
  | "containers"
  | "backend";

/** Bring-up order; also the display order in the startup overlay. */
export const LOCAL_STACK_STEPS: readonly LocalStackStep[] = [
  "config",
  "probe",
  "engine",
  "containers",
  "backend",
] as const;

export type LocalStackState =
  | { phase: "idle" }
  | { phase: "running"; step: LocalStackStep }
  | { phase: "ready" }
  | { phase: "failed"; step: LocalStackStep; message: string };
