import React from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

vi.mock("@tanstack/react-query", () => ({
  queryOptions: (options: unknown) => options,
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey?.[0];
    if (key === "projects") {
      return {
        data: [
          {
            id: "project-1",
            title: "Alpha Project",
            status: "planned",
            priority: "medium",
            issue_count: 12,
            done_count: 7,
            lead_type: null,
            lead_id: null,
            created_at: "2026-06-01T00:00:00.000Z",
          },
        ],
        isLoading: false,
      };
    }
    if (key === "workspaces") {
      return { data: [] };
    }
    return { data: [] };
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    projectDetail: (id: string) => `/workspaces/workspace-1/projects/${id}`,
  }),
}));

vi.mock("@multica/core/projects/mutations", () => ({
  useUpdateProject: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["workspaces", "workspace-1", "members"] }),
  agentListOptions: () => ({ queryKey: ["workspaces", "workspace-1", "agents"] }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: vi.fn() }),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ href, className, children }: { href: string; className?: string; children: React.ReactNode }) => (
    <a href={href} className={className}>
      {children}
    </a>
  ),
}));

vi.mock("../../layout/page-header", () => ({
  PageHeader: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
}));

vi.mock("../../issues/components/priority-icon", () => ({
  PriorityIcon: () => <span data-testid="priority-icon" />,
}));

vi.mock("./project-icon", () => ({
  ProjectIcon: () => <span data-testid="project-icon" />,
}));

vi.mock("./labels", () => ({
  useProjectStatusLabels: () => ({
    planned: "Planned",
    in_progress: "In progress",
    paused: "Paused",
    completed: "Completed",
    cancelled: "Cancelled",
  }),
  useProjectPriorityLabels: () => ({
    urgent: "Urgent",
    high: "High",
    medium: "Medium",
    low: "Low",
    none: "None",
  }),
  useFormatRelativeDate: () => () => "2d ago",
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/skeleton", () => ({
  Skeleton: ({ className }: { className?: string }) => <div className={className} />,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({
    render,
    children,
  }: {
    render?: React.ReactNode;
    children?: React.ReactNode;
  }) => <>{render ?? children}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button">{children}</button>
  ),
}));

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({
    render,
    children,
  }: {
    render?: React.ReactNode;
    children?: React.ReactNode;
  }) => <>{render ?? children}</>,
  PopoverContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render, children }: { render?: React.ReactNode; children?: React.ReactNode }) => <>{render ?? children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@multica/ui/lib/utils", () => ({
  cn: (...values: Array<string | false | null | undefined>) => values.filter(Boolean).join(" "),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: (dict: Record<string, unknown>) => string) =>
      selector({
        page: { title: "Projects", new_project: "New project", empty: "No projects", create_first: "Create first project" },
        table: { name: "Name", priority: "Priority", status: "Status", progress: "Progress", lead: "Lead", created: "Created" },
        lead: {
          assign_placeholder: "Assign a lead",
          no_lead: "No lead",
          members_group: "Members",
          agents_group: "Agents",
          no_results: "No matches",
        },
      }),
  }),
}));

import { ProjectsPage } from "./projects-page";

describe("ProjectsPage", () => {
  it("stacks project rows on small screens so the project title gets the first line", () => {
    render(<ProjectsPage />);

    const titleLink = screen.getByRole("link", { name: "Alpha Project" });
    const row = titleLink.parentElement;

    expect(row).toHaveClass("flex", "flex-col", "sm:flex-row");
    expect(row).toHaveClass("sm:items-center");
    expect(screen.getByText("Name").parentElement).toHaveClass("hidden", "sm:flex");
    expect(titleLink).toHaveClass("sm:flex-1");
  });
});
