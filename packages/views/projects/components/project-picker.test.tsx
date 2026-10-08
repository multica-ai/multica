import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enProjects from "../../locales/en/projects.json";
import enIssues from "../../locales/en/issues.json";
import { ProjectPicker } from "./project-picker";
import { PillButton } from "../../common/pill-button";

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: [
      { id: "project-1", title: "Launch Command Center", icon: null },
      { id: "project-2", title: "Mobile Web", icon: null },
    ],
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

function withI18n(children: React.ReactNode) {
  return (
    <I18nProvider locale="en" resources={{ en: { projects: enProjects, issues: enIssues } }}>
      {children}
    </I18nProvider>
  );
}

// Real PropertyPicker (Popover) — do not mock it: clearing now lives inside
// the popover, so the tests have to actually open it.
function renderPicker(props: Partial<React.ComponentProps<typeof ProjectPicker>> = {}) {
  return render(
    withI18n(
      <ProjectPicker
        projectId="project-1"
        onUpdate={props.onUpdate ?? vi.fn()}
        triggerRender={<PillButton />}
        {...props}
      />,
    ),
  );
}

const SEARCH_PLACEHOLDER = "Search projects...";

describe("ProjectPicker", () => {
  it("renders the trigger alone — the pill carries no inline clear control", () => {
    // Regression: the clear × used to be an absolutely-positioned sibling
    // overlaying the trigger, and only the picker's own default trigger
    // reserved right padding for it. Every caller passing `triggerRender`
    // (create dialog pill, table cell, autopilot card) had the × painted on
    // top of the project name. Clearing belongs in the popover instead, which
    // is also where the other property pickers put it.
    renderPicker();

    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.getByRole("button", { name: /launch command center/i })).toBeInTheDocument();
  });

  it("clears the selection from the No project row", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    renderPicker({ onUpdate });

    await user.click(screen.getByRole("button", { name: /launch command center/i }));
    await user.click(await screen.findByRole("button", { name: /no project/i }));

    expect(onUpdate).toHaveBeenCalledWith({ project_id: null });
  });

  it("keeps the empty row first while searching", async () => {
    // The empty value is the fixed first row of every picker in the app — a
    // typed query must not move or hide it, or "clear this field" lands
    // somewhere different depending on what the user typed.
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    renderPicker({ onUpdate });

    await user.click(screen.getByRole("button", { name: /launch command center/i }));
    await user.type(await screen.findByPlaceholderText(SEARCH_PLACEHOLDER), "zzzznomatch");

    await user.click(screen.getByRole("button", { name: /no project/i }));
    expect(onUpdate).toHaveBeenCalledWith({ project_id: null });
  });

  it("keeps the empty row ahead of the filtered matches", async () => {
    const user = userEvent.setup();

    renderPicker({ projectId: null });

    // Unset, the trigger adopts the "No project" label too, so both it and
    // the row match — before and after typing.
    await user.click(screen.getByRole("button", { name: /no project/i }));
    expect(screen.getAllByRole("button", { name: /no project/i })).toHaveLength(2);

    await user.type(await screen.findByPlaceholderText(SEARCH_PLACEHOLDER), "mobile");
    expect(screen.getAllByRole("button", { name: /no project/i })).toHaveLength(2);
    expect(screen.getByRole("button", { name: /mobile web/i })).toBeInTheDocument();
  });

  it("locks every mutation path when disabled", async () => {
    // Regression (MUL-5150): a keyboard user could Tab to the inline clear
    // button and detach the project while a chat send was in flight,
    // retargeting the lazily-created session. With clearing moved into the
    // popover, `disabled` has a single path to close — the popover.
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    const { unmount } = render(
      withI18n(<ProjectPicker projectId="project-1" onUpdate={onUpdate} disabled />),
    );

    const trigger = screen.getByRole("button", { name: /launch command center/i });
    expect(trigger).toBeDisabled();

    await user.click(trigger);
    expect(screen.queryByPlaceholderText(SEARCH_PLACEHOLDER)).not.toBeInTheDocument();
    expect(onUpdate).not.toHaveBeenCalled();

    // The lock also wins over a controlled `open` — a caller that freezes the
    // picker mid-send cannot leave it hanging open with a live clear row.
    unmount();
    render(withI18n(<ProjectPicker projectId="project-1" onUpdate={onUpdate} disabled open />));
    expect(screen.queryByPlaceholderText(SEARCH_PLACEHOLDER)).not.toBeInTheDocument();
    expect(onUpdate).not.toHaveBeenCalled();
  });
});
