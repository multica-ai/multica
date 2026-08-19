import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const navigation = vi.hoisted(() => ({
  pathname: "/studio/issues",
  searchParams: new URLSearchParams(),
  replace: vi.fn(),
}));
const autopilotsPage = vi.hoisted(() => vi.fn());

vi.mock("../../navigation", () => ({
  useNavigation: () => navigation,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: (resource: { task_center: Record<string, string> }) => string) =>
      selector({
        task_center: {
          tasks: "Tasks",
          projects: "Projects",
          mine: "My Tasks",
          activity: "Activity",
          automations: "Automations",
        },
      }),
  }),
}));

vi.mock("./issues-page", () => ({ IssuesPage: () => <div>tasks-panel</div> }));
vi.mock("../../projects/components/projects-page", () => ({
  ProjectsPage: () => <div>projects-panel</div>,
}));
vi.mock("../../my-issues/components/my-issues-page", () => ({
  MyIssuesPage: () => <div>mine-panel</div>,
}));
vi.mock("../../inbox/components/inbox-page", () => ({
  InboxPage: () => <div>activity-panel</div>,
}));
vi.mock("../../autopilots/components/autopilots-page", () => ({
  AutopilotsPage: (props: unknown) => {
    autopilotsPage(props);
    return <div>automations-panel</div>;
  },
}));

import { TaskCenterPage } from "./task-center-page";

describe("TaskCenterPage", () => {
  it("opens Projects as a workspace inside Tasks", () => {
    navigation.searchParams = new URLSearchParams("tab=projects");
    navigation.replace.mockClear();

    render(<TaskCenterPage workspaceSlug="studio" />);

    expect(screen.getByRole("tab", { name: "Projects" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByText("projects-panel")).toBeTruthy();

    fireEvent.click(screen.getByRole("tab", { name: "Tasks" }));
    expect(navigation.replace).toHaveBeenCalledWith("/studio/issues?tab=tasks");
  });

  it("keeps My Tasks, Activity, and Automations under the one Tasks route", () => {
    navigation.searchParams = new URLSearchParams("tab=activity");
    navigation.replace.mockClear();

    render(<TaskCenterPage workspaceSlug="studio" />);

    expect(screen.getByRole("tab", { name: "Activity" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByText("activity-panel")).toBeTruthy();

    fireEvent.click(screen.getByRole("tab", { name: "Automations" }));
    expect(navigation.replace).toHaveBeenCalledWith(
      "/studio/issues?tab=automations",
    );
  });

  it("passes the host-owned Automation detail path into the reused list", () => {
    navigation.searchParams = new URLSearchParams("tab=automations");
    const automationDetailHref = (id: string) =>
      `/studio/issues/automations/${id}`;

    render(
      <TaskCenterPage
        workspaceSlug="studio"
        automationDetailHref={automationDetailHref}
      />,
    );

    expect(screen.getByText("automations-panel")).toBeTruthy();
    expect(autopilotsPage).toHaveBeenLastCalledWith(
      expect.objectContaining({ detailHref: automationDetailHref }),
    );
  });
});
