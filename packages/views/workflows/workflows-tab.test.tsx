// @vitest-environment jsdom

import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import { projectListOptions } from "@multica/core/projects/queries";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { IssueWorkflow, Project } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { WorkflowsTab } from "./workflows-tab";

const { workflows } = vi.hoisted(() => ({ workflows: [] as IssueWorkflow[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: { user: { id: string } }) => unknown) => select({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ projectDetail: (id: string) => `/acme/projects/${id}` }),
}));
vi.mock("@multica/core/issue-statuses/hooks", () => ({ useIssueStatuses: () => buildIssueStatusCatalog([]) }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Lambda" }) }));
vi.mock("@multica/core/issue-workflows", () => ({ useIssueWorkflows: () => ({ workflows, isLoaded: true }) }));
vi.mock("../navigation", () => ({
  AppLink: ({ href, children, ...props }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({ searchParams: new URLSearchParams("tab=workflows"), pathname: "/acme/settings", push: vi.fn() }),
}));
vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid={`avatar-${actorId}`} />,
}));
vi.mock("../settings/components/issue-statuses-tab", () => ({ IssueStatusesTab: () => null }));
vi.mock("./workflow-editor-page", () => ({ WorkflowEditorPage: () => null }));

function project(id: string, title: string, workflowId: string | null = null): Project {
  return {
    id,
    title,
    icon: null,
    workflow_id: workflowId,
    issue_count: 3,
    updated_at: `2026-10-${String(10 + Number(id.replace(/\D/g, "")) % 20).padStart(2, "0")}T00:00:00Z`,
  } as Project;
}

function workflow(partial: Partial<IssueWorkflow>): IssueWorkflow {
  return {
    workspace_id: "ws-1",
    description: "",
    is_default: false,
    owner_id: null,
    steps: [],
    project_ids: [],
    created_at: "",
    updated_at: "",
    ...partial,
  } as IssueWorkflow;
}

function renderTab(projects: Project[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(projectListOptions("ws-1").queryKey, { projects } as never);
  qc.setQueryData(memberListOptions("ws-1").queryKey, [{ user_id: "user-1", role: "owner" }] as never);
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkflowsTab />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  workflows.length = 0;
});

describe("WorkflowsTab list (MUL-8377)", () => {
  it("summarises a long project list in one line and lists it on demand", async () => {
    const projects = Array.from({ length: 25 }, (_, i) => project(`p${i}`, i === 7 ? "数据透明化" : `Sprint ${i}`));
    workflows.push(workflow({ id: "wf-default", name: "Default", is_default: true }));
    renderTab(projects);

    // The row no longer spells out every project name.
    expect(screen.queryByText(/Sprint 3/)).toBeNull();
    const trigger = screen.getByRole("button", { name: "25 projects + no-project issues" });

    fireEvent.click(trigger);
    const filter = await screen.findByRole("textbox", { name: "Filter projects" });
    const dialog = screen.getByRole("dialog", { name: "Default" });
    expect(dialog.getAttribute("aria-describedby")).toBeTruthy();
    expect(document.getElementById(dialog.getAttribute("aria-describedby")!)?.textContent).toBe("25 projects");
    expect(screen.getAllByRole("link").filter((a) => a.getAttribute("href")?.startsWith("/acme/projects/"))).toHaveLength(25);
    expect(screen.getByText("Also applies to issues without a project")).toBeTruthy();

    fireEvent.change(filter, { target: { value: "shuju" } });
    const remaining = screen.getAllByRole("link").filter((a) => a.getAttribute("href")?.startsWith("/acme/projects/"));
    expect(remaining).toHaveLength(1);
    expect(remaining[0]!.textContent).toContain("数据透明化");
  });

  it("names a single project, flags an unused workflow, and links the row by its name", () => {
    workflows.push(
      workflow({ id: "wf-default", name: "Default", is_default: true }),
      workflow({ id: "wf-release", name: "版本发布", project_ids: ["p1"] }),
      workflow({ id: "wf-community", name: "Community" }),
    );
    renderTab([project("p1", "Multica 线上发布", "wf-release")]);

    expect(screen.getByRole("button", { name: "Multica 线上发布" })).toBeTruthy();
    expect(screen.getByText("Not used yet")).toBeTruthy();
    expect(screen.getByText("Issues without a project")).toBeTruthy();
    expect(screen.getByRole("link", { name: "版本发布" }).getAttribute("href")).toBe(
      "/acme/settings?tab=workflows&workflow=wf-release",
    );
  });

  it("names who each handoff step goes to", () => {
    workflows.push(
      workflow({ id: "wf-default", name: "Default", is_default: true }),
      workflow({
        id: "wf-release",
        name: "版本发布",
        steps: [
          { status_key: "todo", handler: { type: "agent", id: "agent-1" }, instructions: "" },
          { status_key: "in_progress", handler: { type: "project_lead" }, instructions: "" },
          { status_key: "in_review", handler: { type: "none" }, instructions: "" },
          { status_key: "done", handler: { type: "none" }, instructions: "" },
        ],
      }),
    );
    renderTab([]);

    // A step's arrow travels in a wrapper with the same text, so take the innermost match.
    const chip = (text: string) => screen.getAllByText((_, el) => el?.tagName === "SPAN" && el.textContent === text).at(-1)!;
    expect(within(chip("TodoLambda")).getByTestId("avatar-agent-1")).toBeTruthy();
    expect(chip("In ProgressProject lead")).toBeTruthy();
    expect(chip("In Review")).toBeTruthy();
  });
});
