import { cloneElement, isValidElement, type ReactElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enProjects from "../../locales/en/projects.json";
import { ProjectPicker } from "./project-picker";
import { PillButton } from "../../common/pill-button";

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: [{ id: "project-1", title: "Launch Command Center", icon: null }],
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects"] }),
}));

vi.mock("./project-icon", () => ({
  ProjectIcon: () => <span data-testid="project-icon" />,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render: trigger, children }: { render?: ReactElement; children: ReactNode }) =>
    isValidElement(trigger)
      ? cloneElement(trigger, {}, children)
      : <button type="button">{children}</button>,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>{children}</button>
  ),
  DropdownMenuSeparator: () => null,
}));

describe("ProjectPicker", () => {
  it("shows a hover clear action for the selected project", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    render(
      <I18nProvider locale="en" resources={{ en: { projects: enProjects } }}>
        <ProjectPicker
          projectId="project-1"
          onUpdate={onUpdate}
          triggerRender={<PillButton />}
        />
      </I18nProvider>,
    );

    const clear = screen
      .getAllByRole("button", { name: "Remove from project" })
      .find((button) => button.className.includes("group-hover/project:opacity-100"));
    expect(clear).toBeDefined();
    expect(clear!.className).toContain("group-hover/project:opacity-100");

    await user.click(clear!);
    expect(onUpdate).toHaveBeenCalledWith({ project_id: null });
  });
});
