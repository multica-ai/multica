"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, ApiError } from "@multica/core/api";
import type { CommandBarContext } from "@multica/core/command-bar";
import { useWorkspaceId } from "@multica/core/hooks";
import { useQuickCreateStore } from "@multica/core/issues/stores/quick-create-store";
import { projectListOptions } from "@multica/core/projects/queries";
import { useIssueLimitUpgradePrompt } from "../../modals/use-issue-limit-upgrade-prompt";
import { useT } from "../../i18n";
import type { DelegateActor } from "../types";
import { useActorDirectory } from "../use-actor-directory";

export interface DelegateAttachments {
  project: { id: string; title: string } | null;
  parent: { id: string; identifier: string } | null;
}

/**
 * What a delegated issue is created with, read from the page: the open issue
 * becomes its parent and lends its project; a project page lends itself.
 */
export function useDelegateAttachments(contexts: readonly CommandBarContext[]): DelegateAttachments {
  const wsId = useWorkspaceId();
  const issueContext = contexts.find((c) => c.kind === "issue");
  const projectContext = contexts.find((c) => c.kind === "project");
  const issue = issueContext?.kind === "issue" ? issueContext.issue : null;
  const pageProject = projectContext?.kind === "project" ? projectContext.project : null;
  const { data: projects } = useQuery({
    ...projectListOptions(wsId),
    enabled: !!issue?.project_id && !pageProject,
  });

  return useMemo(() => {
    let project: DelegateAttachments["project"] = null;
    if (pageProject) project = { id: pageProject.id, title: pageProject.title };
    else if (issue?.project_id) {
      const found = projects?.find((p) => p.id === issue.project_id);
      if (found) project = { id: found.id, title: found.title };
    }
    return {
      project,
      parent: issue ? { id: issue.id, identifier: issue.identifier } : null,
    };
  }, [issue, pageProject, projects]);
}

/**
 * Who "hand to agent" goes to when the input names nobody: the agent or squad
 * the user last delegated to through quick create, else the first assignable
 * agent that is online.
 */
export function useDelegateCandidates(enabled: boolean) {
  const { agents, squads } = useActorDirectory(enabled);
  const lastActorType = useQuickCreateStore((s) => s.lastActorType);
  const lastActorId = useQuickCreateStore((s) => s.lastActorId);

  return useMemo(() => {
    const assignableAgents = agents.filter((entry) => entry.assignable);
    const runnableSquads = squads.filter((entry) => entry.runnable);
    const actors: DelegateActor[] = [
      ...assignableAgents.map(
        (entry): DelegateActor => ({ type: "agent", id: entry.agent.id, name: entry.agent.name, agent: entry.agent }),
      ),
      ...runnableSquads.map(
        (entry): DelegateActor => ({ type: "squad", id: entry.squad.id, name: entry.squad.name, squad: entry.squad }),
      ),
    ];
    const last = actors.find((actor) => actor.type === lastActorType && actor.id === lastActorId);
    const firstOnline = assignableAgents.find((entry) => entry.availability === "online");
    const fallback =
      last ??
      (firstOnline ? actors.find((actor) => actor.id === firstOnline.agent.id) : undefined) ??
      actors[0] ??
      null;
    return { actors, fallback, agentEntries: agents };
  }, [agents, lastActorId, lastActorType, squads]);
}

/** Create an issue from `prompt` and hand it to `actor` via quick create. */
export function useDelegateSubmit() {
  const { t } = useT("modals");
  const showIssueLimitUpgradePrompt = useIssueLimitUpgradePrompt();
  const setLastActor = useQuickCreateStore((s) => s.setLastActor);

  return useCallback(
    async (actor: DelegateActor, prompt: string, attachments: DelegateAttachments) => {
      try {
        await api.quickCreateIssue({
          ...(actor.type === "agent" ? { agent_id: actor.id } : { squad_id: actor.id }),
          prompt,
          project_id: attachments.project?.id,
          parent_issue_id: attachments.parent?.id,
        });
        setLastActor(actor.type, actor.id);
        toast.success(t(($) => $.create_issue.agent.toast_sent), { duration: 4000 });
      } catch (e) {
        if (e instanceof ApiError && e.body && typeof e.body === "object") {
          const body = e.body as { code?: string; reason?: string; current_version?: string; min_version?: string };
          if (body.code === "issue_limit_reached") {
            showIssueLimitUpgradePrompt();
            return;
          }
          if (body.code === "agent_unavailable") {
            toast.error(body.reason || t(($) => $.create_issue.agent.error_agent_unavailable_fallback));
            return;
          }
          if (body.code === "daemon_version_unsupported") {
            toast.error(
              t(($) => $.create_issue.agent.error_daemon_version, {
                current: body.current_version || "unknown",
                min: body.min_version || "",
              }),
            );
            return;
          }
        }
        toast.error(
          e instanceof Error && e.message ? e.message : t(($) => $.create_issue.agent.error_unknown),
        );
      }
    },
    [setLastActor, showIssueLimitUpgradePrompt, t],
  );
}
