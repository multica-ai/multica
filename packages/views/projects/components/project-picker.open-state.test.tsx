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
import { PillButton } from "../../common/pill-button";

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

/** Mirrors the create-issue dialog wiring from packages/views/modals/create-issue.tsx. */
function CreateDialogHarness({ onUpdate }: { onUpdate: (u: object) => void }) {
  const [fieldPickerOpen, setFieldPickerOpen] = useState<"project" | null>(null);
  const [projectId, setProjectId] = useState<string | null>(null);
  return withI18n(
    <ProjectPicker
      projectId={projectId}
      onUpdate={(u) => {
        onUpdate(u);
        setProjectId((u as { project_id?: string | null }).project_id ?? null);
      }}
      triggerRender={<PillButton />}
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
