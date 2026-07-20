// Regression test: selecting a project in the create-issue
// dialog left the dropdown stuck open. The dialog wires the picker with
// `open={cond ? true : undefined}`; Base UI's Menu latches a controlled
// `open={true}` and does NOT treat a later `undefined` as "close", so the
// picker must normalize to an always-boolean controlled value. This test
// uses the REAL dropdown-menu (Base UI) — do not mock it here.
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enProjects from "../../locales/en/projects.json";
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

/** Mirrors the create-issue dialog wiring from packages/views/modals/create-issue.tsx. */
function CreateDialogHarness({ onUpdate }: { onUpdate: (u: object) => void }) {
  const [fieldPickerOpen, setFieldPickerOpen] = useState<"project" | null>(null);
  const [projectId, setProjectId] = useState<string | null>(null);
  return (
    <I18nProvider locale="en" resources={{ en: { projects: enProjects } }}>
      <ProjectPicker
        projectId={projectId}
        onUpdate={(u) => {
          onUpdate(u);
          setProjectId(u.project_id ?? null);
        }}
        triggerRender={<PillButton />}
        align="start"
        open={fieldPickerOpen === "project" ? true : undefined}
        onOpenChange={(open) => setFieldPickerOpen(open ? "project" : null)}
      />
    </I18nProvider>
  );
}

describe("ProjectPicker open state under create-dialog wiring", () => {
  it("closes the dropdown after selecting a project", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    render(<CreateDialogHarness onUpdate={onUpdate} />);

    // Open the picker via its trigger.
    await user.click(screen.getByRole("button", { name: /no project/i }));
    const item = await screen.findByRole("menuitem", { name: /mobile web/i });

    // Select a project — the selection must register AND the popup must close.
    await user.click(item);
    expect(onUpdate).toHaveBeenCalledWith({ project_id: "project-2" });
    await waitFor(() => {
      expect(screen.queryByRole("menuitem", { name: /mobile web/i })).not.toBeInTheDocument();
    });
  });

  it("can be reopened and closed again after a selection", async () => {
    const user = userEvent.setup();

    render(<CreateDialogHarness onUpdate={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: /no project/i }));
    await user.click(await screen.findByRole("menuitem", { name: /launch command center/i }));
    await waitFor(() => {
      expect(screen.queryByRole("menuitem", { name: /launch command center/i })).not.toBeInTheDocument();
    });

    // Reopen from the (now selected) trigger and close by selecting again.
    await user.click(screen.getByRole("button", { name: /launch command center/i }));
    await user.click(await screen.findByRole("menuitem", { name: /mobile web/i }));
    await waitFor(() => {
      expect(screen.queryByRole("menuitem", { name: /mobile web/i })).not.toBeInTheDocument();
    });
  });
});
