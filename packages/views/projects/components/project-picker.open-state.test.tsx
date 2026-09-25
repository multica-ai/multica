// Behavioural tests for the Popover-backed ProjectPicker. These use the REAL
// PropertyPicker / Base UI Popover — do not mock them here.
//
// Open-state regression: selecting a project in the create-issue dialog left
// the dropdown stuck open. The dialog wires the picker with
// `open={cond ? true : undefined}`; Base UI latches a controlled `open={true}`
// and does NOT treat a later `undefined` as "close", so the picker normalizes
// to an always-boolean controlled value.
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enProjects from "../../locales/en/projects.json";
import enIssues from "../../locales/en/issues.json";
import { ProjectPicker } from "./project-picker";
import { ClearablePillButton, PillButton } from "../../common/pill-button";

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: [
      { id: "project-1", title: "Launch Command Center", icon: null },
      { id: "project-2", title: "Mobile Web", icon: null },
      { id: "project-3", title: "数据透明化", icon: null },
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

/** Mirrors the create-issue dialog wiring from packages/views/modals/create-issue.tsx,
 *  including the clearable pill both create panels use. */
function CreateDialogHarness({ onUpdate }: { onUpdate: (u: object) => void }) {
  const [fieldPickerOpen, setFieldPickerOpen] = useState<"project" | null>(null);
  const [projectId, setProjectId] = useState<string | null>(null);
  const commit = (next: string | null) => {
    onUpdate({ project_id: next });
    setProjectId(next);
  };
  return withI18n(
    <ProjectPicker
      projectId={projectId}
      onUpdate={(u) => commit((u as { project_id?: string | null }).project_id ?? null)}
      triggerRender={
        <ClearablePillButton
          onClear={projectId !== null ? () => commit(null) : undefined}
          clearLabel="Clear project"
        />
      }
      align="start"
      open={fieldPickerOpen === "project" ? true : undefined}
      onOpenChange={(open) => setFieldPickerOpen(open ? "project" : null)}
    />,
  );
}

// The picker is closed iff its search input is unmounted. A closed selection
// can't be detected by the item's name because the trigger adopts the selected
// project's title, so a name query would keep matching the trigger.
function expectClosed() {
  return waitFor(() => {
    expect(screen.queryByPlaceholderText("Search projects...")).not.toBeInTheDocument();
  });
}

describe("ProjectPicker open state under create-dialog wiring", () => {
  it("closes the dropdown after selecting a project", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    render(<CreateDialogHarness onUpdate={onUpdate} />);

    // Open the picker via its trigger (unselected → trigger reads "No project").
    await user.click(screen.getByRole("button", { name: /no project/i }));
    const item = await screen.findByRole("button", { name: /mobile web/i });

    // Select a project — the selection must register AND the popup must close.
    await user.click(item);
    expect(onUpdate).toHaveBeenCalledWith({ project_id: "project-2" });
    await expectClosed();
  });

  it("can be reopened and closed again after a selection", async () => {
    const user = userEvent.setup();

    render(<CreateDialogHarness onUpdate={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: /no project/i }));
    await user.click(await screen.findByRole("button", { name: /launch command center/i }));
    await expectClosed();

    // Reopen from the (now selected) trigger and close by selecting again.
    await user.click(screen.getByRole("button", { name: /launch command center/i }));
    await user.click(await screen.findByRole("button", { name: /mobile web/i }));
    await expectClosed();
  });
});

// The pill's quick-clear (MUL-5862). It is a sibling button inside the pill
// shell, not an overlay on the trigger — the overlay version this replaces
// needed every caller to reserve right padding and three of five didn't
// (MUL-5666). The trigger stays the popover's anchor, so pressing × must
// clear the field WITHOUT opening the list.
describe("ProjectPicker clearable pill", () => {
  it("offers no × until a project is selected", async () => {
    render(<CreateDialogHarness onUpdate={vi.fn()} />);

    expect(screen.queryByRole("button", { name: "Clear project" })).not.toBeInTheDocument();
  });

  it("clears the selection in one click without opening the popover", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    render(<CreateDialogHarness onUpdate={onUpdate} />);

    await user.click(screen.getByRole("button", { name: /no project/i }));
    await user.click(await screen.findByRole("button", { name: /mobile web/i }));
    await expectClosed();

    await user.click(screen.getByRole("button", { name: "Clear project" }));

    expect(onUpdate).toHaveBeenLastCalledWith({ project_id: null });
    // Trigger is back to the empty label, the × is gone, and the click never
    // reached the trigger underneath it.
    expect(screen.getByRole("button", { name: /no project/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Clear project" })).not.toBeInTheDocument();
    await expectClosed();
  });
});

describe("ProjectPicker search", () => {
  it("filters the project list by title substring", async () => {
    const user = userEvent.setup();

    render(withI18n(<ProjectPicker projectId={null} onUpdate={vi.fn()} triggerRender={<PillButton />} />));

    await user.click(screen.getByRole("button", { name: /no project/i }));
    const search = await screen.findByPlaceholderText("Search projects...");

    await user.type(search, "mobile");
    expect(screen.getByRole("button", { name: /mobile web/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /launch command center/i })).not.toBeInTheDocument();
  });

  it("matches Chinese project names by pinyin", async () => {
    const user = userEvent.setup();

    render(withI18n(<ProjectPicker projectId={null} onUpdate={vi.fn()} triggerRender={<PillButton />} />));

    await user.click(screen.getByRole("button", { name: /no project/i }));
    const search = await screen.findByPlaceholderText("Search projects...");

    // "数据透明化" → full pinyin "shujutouminghua"; a prefix must match.
    await user.type(search, "shuju");
    expect(screen.getByText("数据透明化")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /mobile web/i })).not.toBeInTheDocument();
  });

  it("shows an empty state when no project matches", async () => {
    const user = userEvent.setup();

    render(withI18n(<ProjectPicker projectId={null} onUpdate={vi.fn()} triggerRender={<PillButton />} />));

    await user.click(screen.getByRole("button", { name: /no project/i }));
    const search = await screen.findByPlaceholderText("Search projects...");

    await user.type(search, "zzzznomatch");
    expect(screen.getByText("No results")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /mobile web/i })).not.toBeInTheDocument();
  });

  // Regression: the empty value is pinned as the first row, which also made it
  // the first `data-picker-item`. PropertyPicker used to reset the highlight to
  // index 0 on every keystroke, so typing a query and pressing Enter committed
  // the empty row — clearing the field the user was searching in instead of
  // picking the match they were looking at.
  it("commits the first match on Enter, not the empty row", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    render(<CreateDialogHarness onUpdate={onUpdate} />);

    await user.click(screen.getByRole("button", { name: /no project/i }));
    await user.type(await screen.findByPlaceholderText("Search projects..."), "mobile");
    await user.keyboard("{Enter}");

    expect(onUpdate).toHaveBeenCalledWith({ project_id: "project-2" });
    expect(onUpdate).not.toHaveBeenCalledWith({ project_id: null });
  });

  it("leaves Enter inert when the query matches nothing", async () => {
    // The empty row survives every filter, so a no-match query leaves it as
    // the sole item. The single-item auto-select must not fire on it.
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    render(<CreateDialogHarness onUpdate={onUpdate} />);

    await user.click(screen.getByRole("button", { name: /no project/i }));
    await user.type(await screen.findByPlaceholderText("Search projects..."), "zzzznomatch");
    await user.keyboard("{Enter}");

    expect(onUpdate).not.toHaveBeenCalled();
  });

  it("still reaches the empty row by arrow key while searching", async () => {
    // Skipping it as the Enter default must not make it unreachable.
    // jsdom has no layout, so scrollIntoView isn't defined at all — assign it
    // rather than spy on it (vi.spyOn requires an existing property).
    HTMLElement.prototype.scrollIntoView = vi.fn();
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    render(<CreateDialogHarness onUpdate={onUpdate} />);

    await user.click(screen.getByRole("button", { name: /no project/i }));
    await user.type(await screen.findByPlaceholderText("Search projects..."), "mobile");
    await user.keyboard("{ArrowUp}{Enter}");

    expect(onUpdate).toHaveBeenCalledWith({ project_id: null });
  });

  // Regression: selecting a row closes the popover by calling `setOpen(false)`
  // directly, which never routes through PropertyPicker's own open-change
  // handler — the only place that used to reset the query. The stale search
  // term survived into the next open and kept the rest of the list hidden.
  it("resets the search term after selecting a match and reopening", async () => {
    const user = userEvent.setup();

    render(<CreateDialogHarness onUpdate={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: /no project/i }));
    await user.type(await screen.findByPlaceholderText("Search projects..."), "mobile");
    await user.click(await screen.findByRole("button", { name: /mobile web/i }));
    await expectClosed();

    // Reopen: the input must be empty and the full list restored.
    await user.click(screen.getByRole("button", { name: /mobile web/i }));
    const reopened = await screen.findByPlaceholderText("Search projects...");
    expect(reopened).toHaveValue("");
    expect(screen.getByRole("button", { name: /launch command center/i })).toBeInTheDocument();
    expect(screen.getByText("数据透明化")).toBeInTheDocument();
  });
});
