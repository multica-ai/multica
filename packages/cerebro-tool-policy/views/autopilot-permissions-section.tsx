"use client";

// FIR-1692: System is a first-class ACTOR, not a stacked layer. An autopilot run
// is a human-less ("system") run — there is no human at the top of the chain, so
// the autopilot itself supplies the mandate that governs what it may do. This
// section gives that mandate its OWN permissions page, on the autopilot's detail
// page, exactly the way an agent or a member has its own permissions page. It
// replaces the old "System layer" picker bar that hung confusingly on top of the
// agent/member tables (Jesper: "Systems skal være actor på niveau med agenter og
// members").
//
// It authors layer="system" keyed on the autopilot id. The server caps a System
// rule at the autopilot owner's own ceiling (a system run can never be looser
// than its owner), so the table just surfaces that error if a rule is too loose.

import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { ToolPolicyTabs } from "./tool-policy-table";

export function AutopilotPermissionsSection({
  autopilotId,
  heading,
  description,
}: {
  autopilotId: string;
  /** Localised section heading, supplied by the host page. */
  heading: string;
  /** Localised one-line explanation, supplied by the host page. */
  description: string;
}) {
  const wsId = useWorkspaceId();
  // Same gate as every other tool-policy surface — when the unified engine is off
  // the autopilot page shows no permissions section (fork rule 4).
  const enabled = useFeatureFlag("cerebro_tool_policy");
  if (!enabled || !wsId) return null;

  return (
    <section className="space-y-3">
      <div className="space-y-1">
        <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground">
          {heading}
        </h2>
        <p className="max-w-2xl text-xs text-muted-foreground">{description}</p>
      </div>
      <ToolPolicyTabs wsId={wsId} view="system" subjectId={autopilotId} />
    </section>
  );
}
