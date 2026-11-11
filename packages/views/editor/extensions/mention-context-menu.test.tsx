import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { NavigationProvider } from "../../navigation";
import { IssueMentionCard } from "../../issues/components/issue-mention-card";
import { ProjectMentionCard } from "../../projects/components/project-mention-card";

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}`, projectDetail: (id: string) => `/acme/projects/${id}` }),
}));
vi.mock("../../issues/components/issue-chip", () => ({
  useChipIssue: () => ({ id: "issue-1", title: "Mentioned issue" }),
  IssueChip: () => <span data-testid="mention">Mentioned issue</span>,
}));
vi.mock("../../projects/components/project-chip", () => ({
  useChipProject: () => ({ id: "project-1", title: "Mentioned project" }),
  ProjectChip: () => <span data-testid="mention">Mentioned project</span>,
}));
vi.mock("../../issues/components/issue-hover-card", () => ({
  IssueHoverCard: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("../../issues/actions/issue-actions-context-menu", () => ({
  IssueContextMenuProvider: ({ children }: { children: ReactNode }) => children,
  useOpenIssueContextMenu: () => (_issue: unknown, event: React.MouseEvent) => event.preventDefault(),
}));
vi.mock("../../projects/components/project-row-actions", () => ({ ProjectMenu: () => null }));

afterEach(() => window.getSelection()?.removeAllRanges());

const variants = [
  ["issue", <IssueMentionCard key="issue" issueId="issue-1" />],
  ["project", <ProjectMentionCard key="project" projectId="project-1" />],
] as const;

describe.each(variants)("%s mention context menu", (_name, card) => {
  const show = (editable = false) => render(
    <NavigationProvider value={{ push: vi.fn(), replace: vi.fn(), back: vi.fn(), pathname: "/", searchParams: new URLSearchParams(), hash: "", getShareableUrl: (p) => p }}>
      <div data-testid="paragraph" contentEditable={editable} suppressContentEditableWarning>
        Selected text before {card} and after
      </div>
    </NavigationProvider>,
  );
  it("opens the object menu on a plain read-only mention", () => {
    show();
    expect(fireEvent.contextMenu(screen.getByTestId("mention"))).toBe(false);
  });
  it("lets a selection spanning the mention keep its text context menu", () => {
    show();
    const range = document.createRange();
    range.selectNodeContents(screen.getByTestId("paragraph"));
    window.getSelection()!.addRange(range);
    expect(fireEvent.contextMenu(screen.getByTestId("mention"))).toBe(true);
  });
  it("leaves an editor mention to the edit context menu", () => {
    show(true);
    expect(fireEvent.contextMenu(screen.getByTestId("mention"))).toBe(true);
  });
});
