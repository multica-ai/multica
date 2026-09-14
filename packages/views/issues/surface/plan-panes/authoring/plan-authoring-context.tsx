"use client";

import { createContext, useContext, useMemo, useState, type ReactNode } from "react";
import type { ProjectPlanOverview, ProjectPlanPart, ProjectPlanPhase } from "@multica/core/types";

/**
 * Which authoring dialog is open, and what it is acting on. One discriminated
 * union rather than a boolean per dialog: two dialogs can never be open at
 * once, and a target (phase / part) can never be missing for the dialog that
 * needs it.
 */
export type PlanAuthoringDialog =
  | { kind: "create-plan" }
  | { kind: "edit-plan" }
  | { kind: "supersede-plan" }
  // Stage 2 of supersede: the fields have been collected, nothing has been
  // sent, and this is the confirmation that actually authorises the write.
  // Supersede is the one authoring action that both collects input AND
  // rotates the active plan version, so it needs a form stage and a confirm
  // stage rather than one dialog that does both.
  | { kind: "supersede-plan-confirm"; patch: { title: string; description: string } }
  | { kind: "delete-plan" }
  | { kind: "add-phase" }
  | { kind: "edit-phase"; phase: ProjectPlanPhase }
  | { kind: "delete-phase"; phase: ProjectPlanPhase }
  | { kind: "add-part"; phase: ProjectPlanPhase }
  | { kind: "edit-part"; phase: ProjectPlanPhase; part: ProjectPlanPart }
  | { kind: "delete-part"; phase: ProjectPlanPhase; part: ProjectPlanPart }
  | { kind: "link-issues"; phase: ProjectPlanPhase; part: ProjectPlanPart };

interface PlanAuthoringValue {
  /**
   * False whenever authoring must not be reachable — the `project_plans` flag
   * being off, or a superseded (read-only) plan version being displayed. Every
   * affordance checks this; nothing renders an edit control it would then have
   * to refuse.
   */
  enabled: boolean;
  projectId: string;
  /** The plan being authored, or null on a project that has none yet. */
  overview: ProjectPlanOverview | null;
  dialog: PlanAuthoringDialog | null;
  open: (dialog: PlanAuthoringDialog) => void;
  close: () => void;
}

/**
 * No default value on purpose. A no-op default let a consumer rendered outside
 * the provider swallow every authoring action silently — the button would
 * click and nothing would happen, with no error anywhere. `null` plus the
 * throw in `usePlanAuthoring` turns that into a loud, named failure at the
 * first render instead.
 */
const PlanAuthoringContext = createContext<PlanAuthoringValue | null>(null);

/** Thrown when an authoring component is rendered outside PlanAuthoringProvider. */
export class PlanAuthoringProviderMissingError extends Error {
  constructor() {
    super(
      "Plan authoring components must render inside <PlanAuthoringProvider>. " +
        "PlanModePane installs it; a pane rendered directly (in a test, say) has to wrap itself.",
    );
    this.name = "PlanAuthoringProviderMissingError";
  }
}

export function PlanAuthoringProvider({
  enabled,
  projectId,
  overview,
  children,
}: {
  enabled: boolean;
  projectId: string;
  overview: ProjectPlanOverview | null;
  children: ReactNode;
}) {
  const [dialog, setDialog] = useState<PlanAuthoringDialog | null>(null);
  const value = useMemo<PlanAuthoringValue>(
    () => ({
      enabled,
      projectId,
      overview,
      // A disabled provider reports no open dialog and ignores `open`, so a
      // stale affordance rendered by mistake still cannot start a write.
      // Unlike the removed context default, this no-op is deliberate and
      // scoped: authoring is genuinely off, and silently doing nothing is the
      // correct behaviour rather than a swallowed bug.
      dialog: enabled ? dialog : null,
      open: enabled ? setDialog : () => {},
      close: () => setDialog(null),
    }),
    [enabled, projectId, overview, dialog],
  );
  return <PlanAuthoringContext.Provider value={value}>{children}</PlanAuthoringContext.Provider>;
}

export function usePlanAuthoring(): PlanAuthoringValue {
  const value = useContext(PlanAuthoringContext);
  if (!value) throw new PlanAuthoringProviderMissingError();
  return value;
}

/**
 * The plan id every write needs. Null when there is no plan to write to —
 * callers that reach a mutation without one have a bug, not a state to render.
 */
export function usePlanAuthoringTarget(): { planId: string; projectId: string } | null {
  const { overview, projectId, enabled } = usePlanAuthoring();
  return useMemo(
    () => (enabled && overview ? { planId: overview.plan.id, projectId } : null),
    [enabled, overview, projectId],
  );
}
