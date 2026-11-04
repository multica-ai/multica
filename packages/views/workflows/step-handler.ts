"use client";

import { useCallback } from "react";
import { useActorName } from "@multica/core/workspace/hooks";
import type { IssueWorkflowStep, Project } from "@multica/core/types";
import { useT } from "../i18n";

/** The concrete actor a step hands off to, when one can be named up front. */
export function stepHandlerActor(
  step: IssueWorkflowStep | undefined,
  project: Pick<Project, "lead_type" | "lead_id"> | null | undefined,
): { type: "agent" | "squad" | "member"; id: string } | null {
  if (!step) return null;
  const { type, id } = step.handler;
  if ((type === "agent" || type === "squad" || type === "member") && id) return { type, id };
  if (type === "project_lead" && project?.lead_type && project.lead_id) {
    return { type: project.lead_type, id: project.lead_id };
  }
  return null;
}

/**
 * Names who a workflow step hands an issue to (MUL-7420): the agent, squad or
 * member it names, the project lead, or the issue creator. Returns null for a
 * step that keeps the assignee.
 */
export function useStepHandlerLabel(
  project: Pick<Project, "lead_type" | "lead_id"> | null | undefined,
) {
  const { getActorName } = useActorName();
  const { t } = useT("issues");
  return useCallback(
    (step: IssueWorkflowStep | undefined): string | null => {
      if (!step) return null;
      const { type, id } = step.handler;
      switch (type) {
        case "agent":
        case "squad":
        case "member":
          return id ? getActorName(type, id) : t(($) => $.workflows.handler[type]);
        case "project_lead":
          return project?.lead_type && project.lead_id
            ? t(($) => $.workflows.handler.project_lead_named, {
                name: getActorName(project.lead_type, project.lead_id),
              })
            : t(($) => $.workflows.handler.project_lead);
        case "creator":
          return t(($) => $.workflows.handler.creator);
        default:
          return null;
      }
    },
    [getActorName, project?.lead_id, project?.lead_type, t],
  );
}
