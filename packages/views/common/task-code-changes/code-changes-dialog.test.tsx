// @vitest-environment jsdom

import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import type { TaskCodeChanges } from "@multica/core/types/agent";
import { renderWithI18n } from "../../test/i18n";
import { CodeChangesButton } from "./code-changes-dialog";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/api", () => ({
  api: { getTaskCodeChanges: vi.fn() },
}));

afterEach(() => {
  cleanup();
  vi.mocked(api.getTaskCodeChanges).mockReset();
});

const PATCH = [
  "diff --git a/src/app.ts b/src/app.ts",
  "--- a/src/app.ts",
  "+++ b/src/app.ts",
  "@@ -1 +1 @@",
  "-const greeting = 1;",
  "+const greeting = 2;",
  "diff --git a/README.md b/README.md",
  "new file mode 100644",
  "--- /dev/null",
  "+++ b/README.md",
  "@@ -0,0 +1 @@",
  "+# Hello",
  "",
].join("\n");

function snapshot(overrides: Partial<TaskCodeChanges> = {}): TaskCodeChanges {
  return {
    task_id: "task-1",
    updated_at: new Date().toISOString(),
    repos_truncated: false,
    repos: [
      {
        path: "app",
        branch: "agent/fix-login",
        base: "abc",
        files: [
          { path: "src/app.ts", status: "modified", additions: 1, deletions: 1 },
          { path: "README.md", status: "added", additions: 1, deletions: 0 },
        ],
        patch: PATCH,
      },
    ],
    ...overrides,
  };
}

async function openDialog() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <CodeChangesButton task={{ id: "task-1" }} />
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "View code changes" }));
  return { user, dialog: await screen.findByRole("dialog") };
}

const diffText = () => document.querySelector("pre.transcript-code")?.textContent ?? "";

describe("CodeChangesButton", () => {
  it("fetches only when opened, then lists the files and shows the selected file's diff", async () => {
    vi.mocked(api.getTaskCodeChanges).mockResolvedValue(snapshot());
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <CodeChangesButton task={{ id: "task-1" }} />
      </QueryClientProvider>,
    );
    expect(api.getTaskCodeChanges).not.toHaveBeenCalled();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "View code changes" }));
    const dialog = await screen.findByRole("dialog", { name: "Code changes" });
    expect(api.getTaskCodeChanges).toHaveBeenCalledWith("task-1");
    expect(await within(dialog).findByText("2 files")).toBeInTheDocument();
    expect(within(dialog).getByText("agent/fix-login")).toBeInTheDocument();
    // The first file is selected by default. Syntax highlighting splits the
    // code into spans, so read the diff body as text.
    expect(diffText()).toContain("const greeting = 2;");

    const readme = within(dialog).getByRole("button", { name: /README\.md/ });
    await user.click(readme);
    expect(readme).toHaveAttribute("aria-current", "true");
    expect(diffText()).toContain("# Hello");
    expect(diffText()).not.toContain("greeting");

    await user.click(within(dialog).getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("says when nothing was recorded for the run", async () => {
    vi.mocked(api.getTaskCodeChanges).mockResolvedValue(snapshot({ updated_at: null, repos: [] }));
    const { dialog } = await openDialog();
    expect(await within(dialog).findByText(/No code changes recorded for this run/)).toBeInTheDocument();
  });

  it("says when the run changed nothing", async () => {
    vi.mocked(api.getTaskCodeChanges).mockResolvedValue(snapshot({ repos: [] }));
    const { dialog } = await openDialog();
    expect(await within(dialog).findByText("This run didn't change any code.")).toBeInTheDocument();
  });

  it("marks truncated repositories, file lists and patches", async () => {
    vi.mocked(api.getTaskCodeChanges).mockResolvedValue(
      snapshot({
        repos_truncated: true,
        repos: [
          {
            path: "big",
            base: "abc",
            files: [{ path: "generated.json", status: "added", additions: 90000, deletions: 0 }],
            files_truncated: true,
            patch: "",
            patch_truncated: true,
          },
        ],
      }),
    );
    const { dialog } = await openDialog();
    expect(await within(dialog).findByRole("note")).toHaveTextContent("some aren't listed");
    expect(within(dialog).getByText("More files changed than can be listed.")).toBeInTheDocument();
    expect(within(dialog).getByText("This file's diff was too large to record.")).toBeInTheDocument();
  });

  it("shows a load error instead of an empty state", async () => {
    vi.mocked(api.getTaskCodeChanges).mockRejectedValue(new Error("boom"));
    const { dialog } = await openDialog();
    expect(await within(dialog).findByText("Couldn't load code changes.")).toBeInTheDocument();
  });
});
