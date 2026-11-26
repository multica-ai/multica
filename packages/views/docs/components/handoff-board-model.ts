import { issueRouteRef, paths } from "@multica/core/paths";
import type { ExcalidrawInitialDataState } from "@excalidraw/excalidraw/types";

const prefix = "```multica-board-v1\n";
const suffix = "\n```";

export function boardMarkdown(sceneJSON: string): string {
  return prefix + sceneJSON + suffix;
}

export const EMPTY_HANDOFF_BOARD = boardMarkdown('{"elements":[]}');

export function isHandoffBoard(markdown: string): boolean {
  const content = markdown.trimEnd();
  return (
    content.startsWith(prefix) &&
    content.endsWith(suffix) &&
    content.indexOf(suffix, prefix.length) === content.length - suffix.length
  );
}

export function readBoard(markdown: string): ExcalidrawInitialDataState | null {
  const content = markdown.trimEnd();
  if (!isHandoffBoard(content)) return null;
  try {
    const scene = JSON.parse(content.slice(prefix.length, -suffix.length));
    if (
      !scene ||
      !Array.isArray(scene.elements) ||
      !scene.elements.every(
        (element: unknown) =>
          typeof element === "object" &&
          element !== null &&
          "type" in element &&
          typeof element.type === "string" &&
          "id" in element &&
          typeof element.id === "string",
      )
    )
      return null;
    return {
      elements: scene.elements,
      files: scene.files,
      appState: scene.appState,
      scrollToContent: true,
    };
  } catch {
    return null;
  }
}

export interface BoardIssue {
  id: string;
  identifier: string;
  workspace_id: string;
}

export function linkedIssue(
  customData: Record<string, unknown> | undefined,
  workspaceId: string,
): BoardIssue | null {
  const issue = customData?.multicaIssue;
  if (
    !issue ||
    typeof issue !== "object" ||
    !("workspace_id" in issue) ||
    issue.workspace_id !== workspaceId ||
    !("id" in issue) ||
    typeof issue.id !== "string" ||
    !issue.id ||
    !("identifier" in issue) ||
    typeof issue.identifier !== "string" ||
    !issue.identifier
  )
    return null;
  return { workspace_id: workspaceId, id: issue.id, identifier: issue.identifier };
}

export function boardIssuePath(workspaceSlug: string, issue: BoardIssue): string {
  return paths.workspace(workspaceSlug).issueDetail(issueRouteRef(issue));
}
