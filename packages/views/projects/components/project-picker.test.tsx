import { cloneElement, isValidElement, type ReactElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
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
    expect(clear!.className).toContain("size-3.5");
    expect(clear!.className).toContain("hover:bg-muted-foreground/20");
    expect(clear!.className).not.toContain("bg-background/95");
    expect(clear!.className).not.toContain("inset-y-0");
    expect(clear!.className).not.toContain("w-7");

    await user.click(clear!);
    expect(onUpdate).toHaveBeenCalledWith({ project_id: null });
  });

  it("clears via keyboard activation when enabled", async () => {
    // Other callers (issue/create/autopilot) rely on the clear control staying
    // reachable by keyboard, not just hover. Enabling must not regress that.
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
    expect(clear).not.toBeDisabled();

    clear!.focus();
    expect(clear).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(onUpdate).toHaveBeenCalledWith({ project_id: null });
  });

  it("locks the inline clear control against pointer and keyboard when disabled", () => {
    // Regression (MUL-5150): the outer wrapper's `pointer-events-none` only
    // blocks the mouse. The inline clear button stayed in the tab order, so a
    // keyboard user could Tab to it and press Enter to detach the project while
    // a chat send was in flight — retargeting the lazily-created session. The
    // explicit `disabled` capability must remove it from the tab order and make
    // both pointer and keyboard activation inert.
    const onUpdate = vi.fn();

    render(
      <I18nProvider locale="en" resources={{ en: { projects: enProjects } }}>
        <ProjectPicker
          projectId="project-1"
          onUpdate={onUpdate}
          disabled
          triggerRender={<PillButton />}
        />
      </I18nProvider>,
    );

    const clear = screen
      .getAllByRole("button", { name: "Remove from project" })
      .find((button) => button.className.includes("group-hover/project:opacity-100"));
    expect(clear).toBeDefined();
    expect(clear).toBeDisabled();

    // A disabled control cannot receive focus, so it is not tabbable.
    clear!.focus();
    expect(clear).not.toHaveFocus();

    // Neither keyboard activation nor a direct click may mutate the selection.
    fireEvent.keyDown(clear!, { key: "Enter" });
    fireEvent.keyDown(clear!, { key: " " });
    fireEvent.click(clear!);
    expect(onUpdate).not.toHaveBeenCalled();
  });
});
