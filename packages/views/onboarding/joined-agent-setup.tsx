"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { createPersonalAgent, type CodingToolsBridge } from "@multica/core/onboarding";
import { paths } from "@multica/core/paths";
import type { Workspace } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "../navigation";
import { DragStrip } from "../platform";
import { STEP_COLUMN, STEP_GUTTER } from "./components/step-shell";
import { StepAgentSetup } from "./steps/step-agent-setup";

/**
 * Step 3 on its own, for a member who just joined a workspace through an
 * invitation. They skipped onboarding, so without this they would land in a
 * team workspace with no agent of their own. Creating one opens its chat;
 * skipping goes to the workspace's issues.
 */
export function JoinedAgentSetup({
  workspace,
  codingTools,
  onRuntimeRefresh,
  runtimesPending,
}: {
  workspace: Workspace;
  codingTools?: CodingToolsBridge;
  onRuntimeRefresh?: () => void | Promise<void>;
  runtimesPending?: boolean;
}) {
  const user = useAuthStore((s) => s.user);
  const qc = useQueryClient();
  const { push } = useNavigation();
  if (!user) return null;

  return (
    <div className="animate-onboarding-enter flex h-full min-h-svh flex-col bg-background">
      <DragStrip />
      <main className={cn("min-h-0 flex-1 overflow-y-auto", STEP_GUTTER)}>
        <div className={STEP_COLUMN}>
          <StepAgentSetup
            wsId={workspace.id}
            wsSlug={workspace.slug}
            userName={user.name}
            workspaceName={workspace.name}
            codingTools={codingTools}
            onRuntimeRefresh={onRuntimeRefresh}
            runtimesPending={runtimesPending}
            skipHint={false}
            onCreate={async ({ runtime, name, description }) => {
              const { chatSession } = await createPersonalAgent({
                workspaceSlug: workspace.slug,
                workspaceId: workspace.id,
                queryClient: qc,
                runtimeId: runtime.id,
                name,
                description,
              });
              push(paths.workspace(workspace.slug).chatSession(chatSession.id));
            }}
            onSkip={async () => push(paths.workspace(workspace.slug).issues())}
          />
        </div>
      </main>
    </div>
  );
}
