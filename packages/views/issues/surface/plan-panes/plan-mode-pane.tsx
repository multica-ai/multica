"use client";

import { useFeatureEnabled } from "@multica/core/config";
import { PROJECT_PLANS_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { usePlanOverview } from "@multica/core/project-plan/hooks";
import type { PlanViewMode } from "../types";
import { PlanErrorState, PlanLoadingState, PlanNoPlanState } from "./plan-states";
import { PlanDocumentPane } from "./plan-document-pane";
import { PlanPipelinePane } from "./plan-pipeline-pane";
import { PlanCoveragePane } from "./plan-coverage-pane";
import { PlanAuthoringProvider } from "./authoring/plan-authoring-context";
import { PlanAuthoringDialogs } from "./authoring/plan-authoring-dialogs";
import { CreatePlanButton } from "./authoring/plan-authoring-controls";

/**
 * Resolves one of the three Plan view modes into exactly one of four states —
 * loading, error, no-plan, or the pane itself wired to live data. Collapsing
 * error into no-plan (or vice versa) is the bug this component exists to
 * prevent: a project with a plan must never render "no plan" just because
 * the request hasn't resolved yet or failed.
 *
 * It also owns the authoring boundary. Reaching a plan_* mode
 * already requires `project_plans` — the flag drives `issueSurfaceModes` in
 * project-detail, so a plan pane cannot render with it off — but authoring is
 * gated on the same flag directly rather than inheriting that guarantee. One
 * flag, checked where the writes are: acceptance criteria #3 asks for no
 * authoring affordance to be visible OR reachable, and a second-hand guarantee
 * is easy to break from a distance.
 *
 * Authoring is also off for a superseded version. Those are retained history,
 * and `activePlanForWrite` refuses every write against them
 * (`ErrorNotActive`) — offering an edit control that the service is certain to
 * reject would be a lie about what the UI can do.
 */
export function PlanModePane({ mode, projectId }: { mode: PlanViewMode; projectId: string }) {
  const wsId = useWorkspaceId();
  const state = usePlanOverview(wsId, projectId);
  const planFlagEnabled = useFeatureEnabled(PROJECT_PLANS_FLAG, false);

  const overview = state.status === "present" ? state.data : null;
  const authoringEnabled = planFlagEnabled && !overview?.plan.superseded;

  return (
    <PlanAuthoringProvider enabled={authoringEnabled} projectId={projectId} overview={overview}>
      {(() => {
        switch (state.status) {
          case "loading":
            return <PlanLoadingState />;
          case "error":
            return <PlanErrorState onRetry={state.retry} />;
          case "no-plan":
            return <PlanNoPlanState action={<CreatePlanButton />} />;
          case "present":
            switch (mode) {
              case "plan_document":
                return <PlanDocumentPane overview={state.data} />;
              case "plan_pipeline":
                return <PlanPipelinePane overview={state.data} />;
              case "plan_coverage":
                return <PlanCoveragePane overview={state.data} />;
            }
        }
      })()}
      <PlanAuthoringDialogs />
    </PlanAuthoringProvider>
  );
}
