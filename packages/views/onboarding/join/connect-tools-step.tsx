"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { Bot, Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { useT } from "../../i18n";
import { OnboardingPage } from "../components/onboarding-page";
import { StepFooter, StepHeading } from "../components/step-shell";
import {
  EMPTY_TOOL_SELECTION,
  ToolPicker,
  type ToolSelection,
} from "../components/tool-picker";
import { useOnboardingRuntimes } from "../components/use-onboarding-runtimes";
import { ensureToolAgent, toolDisplayName } from "../provisioning";

/**
 * After accepting an invite on desktop: the workspace's agents run on their
 * owners' computers, and this one has tools of its own. Adding one makes it
 * this member's agent — private, like any agent they create — under a name
 * that cannot collide with a teammate's ("Claude Code · Jiayuan").
 *
 * Skippable in one click. The agents teammates opened to the workspace are
 * listed so "skip" is a real choice, not a dead end.
 */
export function ConnectToolsStep({
  workspace,
  onDone,
}: {
  workspace: Workspace;
  onDone: () => void;
}) {
  const { t } = useT("onboarding");
  const user = useAuthStore((s) => s.user)!;
  const { runtimes, detecting, canRescan, rescan } = useOnboardingRuntimes(
    workspace.id,
    workspace.slug,
  );
  const [selection, setSelection] = useState<ToolSelection>(EMPTY_TOOL_SELECTION);
  const [adding, setAdding] = useState(false);
  const runtime = runtimes.find((r) => r.id === selection.runtimeId) ?? null;

  const { data: members } = useQuery(memberListOptions(workspace.id));
  const role = members?.find((m) => m.user_id === user.id)?.role ?? null;
  const { data: agents } = useQuery({
    queryKey: ["onboarding", "join-agents", workspace.id],
    queryFn: () => api.listAgents({ workspace_id: workspace.id }, workspace.slug),
  });
  const shared = (agents ?? []).filter(
    (a) =>
      !a.archived_at &&
      a.owner_id !== user.id &&
      canAssignAgentToIssue(a, { userId: user.id, role }).allowed,
  );

  const add = async () => {
    if (!runtime) return;
    setAdding(true);
    try {
      const agent = await ensureToolAgent({
        workspace,
        runtime,
        model: selection.model,
        thinkingLevel: selection.thinkingLevel,
        user,
        template: "invite_connect",
      });
      toast.success(t(($) => $.join.added, { name: agent.name }));
      onDone();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.errors.setup_failed));
    } finally {
      setAdding(false);
    }
  };

  return (
    <OnboardingPage showLogout={false}>
      <div className="flex flex-col gap-6 pt-6">
        <StepHeading
          title={t(($) => $.join.title, { workspace: workspace.name })}
          description={t(($) => $.join.description)}
        />

        {shared.length > 0 ? (
          <div className="flex flex-col gap-2">
            <span className="text-caption font-medium text-muted-foreground">
              {t(($) => $.join.shared_agents)}
            </span>
            <div className="flex flex-wrap gap-2">
              {shared.slice(0, 6).map((agent) => (
                <span
                  key={agent.id}
                  className="inline-flex items-center gap-1.5 rounded-full border bg-card px-2.5 py-1 text-caption text-foreground"
                >
                  <Bot className="size-3.5 text-muted-foreground" />
                  {agent.name}
                </span>
              ))}
            </div>
          </div>
        ) : null}

        <div className="flex flex-col gap-2">
          <span className="text-caption font-medium text-muted-foreground">
            {t(($) => $.join.your_tools)}
          </span>
          <ToolPicker
            runtimes={runtimes}
            detecting={detecting}
            canRescan={canRescan}
            onRescan={rescan}
            workspaceSlug={workspace.slug}
            value={selection}
            onChange={setSelection}
            disabled={adding}
          />
        </div>
      </div>

      <StepFooter>
        <Button className="w-full" disabled={!runtime || adding} aria-busy={adding} onClick={add}>
          {adding ? <Loader2 className="animate-spin" /> : null}
          {runtime
            ? t(($) => $.join.add, { name: toolDisplayName(runtime.provider) })
            : t(($) => $.common.continue)}
        </Button>
        <Button
          variant="ghost"
          className="w-full text-muted-foreground"
          disabled={adding}
          onClick={onDone}
        >
          {t(($) => $.join.skip)}
        </Button>
      </StepFooter>
    </OnboardingPage>
  );
}
