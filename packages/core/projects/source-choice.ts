import { create } from "zustand";
import type { Project } from "../types/project";

type Choice = "once" | "saved" | "cancel";
type Target = Pick<Project, "id" | "workspace_id" | "title" | "resource_count" | "source_intent">;
type Request = { project: Target; finish: (choice: Choice) => void };
type Lookup = (
  path: string,
  body: object,
  decisions: Record<string, string>,
) => Project | undefined;

export const useProjectSourceChoice = create<{ current: Request | null }>(
  () => ({ current: null }),
);
const waiting: Request[] = [];
const lookups = new Set<Lookup>();
let hosts = 0;

// A host contributes only synchronous cache reads. Normal assignment never
// waits for a project lookup or trigger-preview request.
export function cachedProjectSourceChoice(
  path: string,
  body: object,
  decisions: Record<string, string>,
) {
  for (const lookup of lookups) {
    const project = lookup(path, body, decisions);
    if (project) return project;
  }
  return undefined;
}

export function registerProjectSourceHost(lookup?: Lookup) {
  hosts++;
  if (lookup) lookups.add(lookup);
  return () => {
    hosts--;
    if (lookup) lookups.delete(lookup);
    if (!hosts) {
      // Unmounting or switching workspace must never leave a queued write
      // waiting for a dialog that no longer exists.
      const requests = [
        useProjectSourceChoice.getState().current,
        ...waiting.splice(0),
      ];
      requests.forEach((request) => request?.finish("cancel"));
    }
  };
}

export function hasProjectSourceHost() {
  return hosts > 0;
}

export function requestProjectSourceChoice(project: Target): Promise<Choice> {
  if (!hosts) return Promise.resolve("cancel");
  return new Promise((resolve) => {
    const request: Request = {
      project: {
        id: project.id,
        workspace_id: project.workspace_id,
        title: project.title,
        resource_count: project.resource_count,
        source_intent: project.source_intent,
      },
      finish: (choice) => {
        if (useProjectSourceChoice.getState().current !== request) {
          const index = waiting.indexOf(request);
          if (index >= 0) waiting.splice(index, 1);
        } else {
          useProjectSourceChoice.setState({ current: waiting.shift() ?? null });
        }
        resolve(choice);
      },
    };
    if (useProjectSourceChoice.getState().current) waiting.push(request);
    else useProjectSourceChoice.setState({ current: request });
  });
}
