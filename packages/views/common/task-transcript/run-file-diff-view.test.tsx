// @vitest-environment jsdom
import {
  act,
  fireEvent,
  screen,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { copyText } from "@multica/ui/lib/clipboard";
import { api, ApiError } from "@multica/core/api";
import {
  type RunFileChange,
  type RunFileChanges,
  type RunFileChangeBody,
} from "@multica/core/agents/file-changes";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { RunFileDiffView } from "./run-file-diff-view";
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
// Partial mock: file-changes.ts pulls ApiError/errorCode from this module to
// classify relay failures, so the real exports must survive alongside the
// mocked client.
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: {
    listRunFileChanges: vi.fn(),
    getRunFileChangeBody: vi.fn(),
    listRuntimes: vi.fn(),
  },
}));
vi.mock("@multica/ui/lib/clipboard", () => ({
  copyText: vi.fn().mockResolvedValue(true),
}));
const file: RunFileChange = {
  id: "f1",
  rootId: "r1",
  path: "same.ts",
  change: "modified",
  beforeBytes: 4,
  afterBytes: 4,
  beforeMode: "100644",
  afterMode: "100644",
  binary: false,
  bodyStatus: "available",
  reason: "",
};
const page: RunFileChanges = {
  collectionId: "c",
  status: "ready",
  reason: "",
  scope: "registered_workdir_roots",
  roots: [
    {
      id: "r1",
      label: "repo-one",
      kind: "git",
      coverage: "complete",
      reason: "",
    },
    {
      id: "r2",
      label: "repo-two",
      kind: "git",
      coverage: "complete",
      reason: "",
    },
  ],
  files: [file],
  filesTotal: 2,
  totalsComplete: true,
  nextCursor: "100",
};
function renderView(
  taskId = "task",
  view?: { workdir?: string; runtime?: { id: string; runtime_mode: "local" | "cloud"; daemon_id: string | null } | null },
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  const render = renderWithI18n(
    <QueryClientProvider client={client}>
      <RunFileDiffView
        taskId={taskId}
        workdir={view?.workdir}
        runtime={view?.runtime}
        onBack={() => {}}
      />
    </QueryClientProvider>,
  );
  return { client, ...render };
}
function runtimeFixture(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt1",
    workspace_id: "ws",
    daemon_id: "daemon-1",
    name: "Claude (dev.local)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "dev.local",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: "2026-07-15T00:00:00Z",
    created_at: "2026-07-15T00:00:00Z",
    updated_at: "2026-07-15T00:00:00Z",
    ...overrides,
  };
}
function relayError(status: number, code: string): ApiError {
  return new ApiError(code, status, String(status), { code });
}
function fileTrigger(rootLabel: string, path: string) {
  const node = screen.getByTitle(`${rootLabel} / ${path}`);
  const button = node.closest("button");
  if (!button) throw new Error(`missing file trigger for ${rootLabel} / ${path}`);
  return button;
}
async function findFileTrigger(rootLabel: string, path: string) {
  await screen.findByTitle(`${rootLabel} / ${path}`);
  return fileTrigger(rootLabel, path);
}
beforeEach(() => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue(page);
  vi.mocked(api.getRunFileChangeBody).mockResolvedValue({
    ...file,
    before: "old\n",
    after: "new\n",
  });
  vi.mocked(api.listRuntimes).mockResolvedValue([runtimeFixture()]);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
describe("persisted run diff", () => {
  it("shows persisted per-file counts before expansion without loading bodies", async () => {
    vi.mocked(api.listRunFileChanges).mockResolvedValue({
      ...page,
      files: [{ ...file, insertions: 9, deletions: 0 }],
    });
    renderView();
    await findFileTrigger("repo-one", "same.ts");
    expect(screen.getAllByText("+9").length).toBeGreaterThan(0);
    expect(screen.getAllByText("−0").length).toBeGreaterThan(0);
    const trigger = fileTrigger("repo-one", "same.ts");
    fireEvent.click(screen.getByRole("button", { name: "Copy file path" }));
    expect(copyText).toHaveBeenCalledWith("repo-one/same.ts");
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(api.getRunFileChangeBody).not.toHaveBeenCalled();
  });
  it("uses a pointer cursor on file-diff action buttons", async () => {
    renderView();
    await findFileTrigger("repo-one", "same.ts");
    expect(screen.getByRole("button", { name: "Back to steps" })).toHaveClass(
      "cursor-pointer",
    );
    expect(screen.getByRole("button", { name: "Expand all" })).toHaveClass(
      "cursor-pointer",
    );
    expect(screen.getByRole("button", { name: "Collapse all" })).toHaveClass(
      "cursor-pointer",
    );
    expect(screen.getByRole("button", { name: "Copy file path" })).toHaveClass(
      "cursor-pointer",
    );
    expect(fileTrigger("repo-one", "same.ts")).toHaveClass("cursor-pointer");
  });
  it("toggles from empty header space besides action buttons", async () => {
    vi.mocked(api.listRunFileChanges).mockResolvedValue({
      ...page,
      files: [{ ...file, insertions: 9, deletions: 0 }],
    });
    renderView();
    const trigger = await findFileTrigger("repo-one", "same.ts");
    fireEvent.click(screen.getByTestId("run-file-diff-row-rest"));
    await waitFor(() =>
      expect(trigger).toHaveAttribute("aria-expanded", "true"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Copy file path" }));
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(screen.getByTestId("run-file-diff-row-rest"));
    await waitFor(() =>
      expect(trigger).toHaveAttribute("aria-expanded", "false"),
    );
  });
  it("does not invent zero counts for snapshots without line statistics", async () => {
    renderView();
    await findFileTrigger("repo-one", "same.ts");
    expect(screen.queryByText("+0")).toBeNull();
    expect(screen.queryByText("−0")).toBeNull();
  });

  it("paginates same paths in different roots and loads bodies only on expansion", async () => {
    vi.mocked(api.listRunFileChanges).mockImplementation(async (_id, cursor) =>
      cursor === "0"
        ? page
        : {
            ...page,
            files: [{ ...file, id: "f2", rootId: "r2" }],
            nextCursor: null,
          },
    );
    renderView();
    const trigger = await findFileTrigger("repo-one", "same.ts");
    expect(api.getRunFileChangeBody).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Load more files" }));
    await findFileTrigger("repo-two", "same.ts");
    expect(api.listRunFileChanges).toHaveBeenCalledWith("task", "100");
    expect(
      screen.getAllByTestId("run-file-diff-group").map((el) => el.getAttribute("data-root-id")),
    ).toEqual(["r1", "r2"]);
    fireEvent.click(trigger);
    await waitFor(() =>
      expect(api.getRunFileChangeBody).toHaveBeenCalledWith("task", "f1"),
    );
    await waitFor(() =>
      expect(document.querySelector("[data-github-split-diff]")).not.toBeNull(),
    );
    fireEvent.click(
      screen.getAllByRole("button", { name: "Copy file path" })[0]!,
    );
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(trigger);
    await waitFor(() =>
      expect(document.querySelector("[data-github-split-diff]")).toBeNull(),
    );
  });
  it.each(["collecting", "unavailable", "future"])(
    "does not label %s with zero files as no modifications",
    async (status) => {
      vi.mocked(api.listRunFileChanges).mockResolvedValue({
        ...page,
        status,
        files: [],
        filesTotal: 0,
        nextCursor: null,
      });
      renderView();
      await waitFor(() => expect(api.listRunFileChanges).toHaveBeenCalled());
      await waitFor(() =>
        expect(screen.queryByText("Loading file changes...")).toBeNull(),
      );
      expect(screen.queryByText("No file edits in this run.")).toBeNull();
      expect(screen.getByRole("status")).toBeInTheDocument();
    },
  );
  it("allows retry after a failed body request", async () => {
    vi.mocked(api.getRunFileChangeBody).mockRejectedValueOnce(
      new Error("offline"),
    );
    renderView();
    fireEvent.click(
      await findFileTrigger("repo-one", "same.ts"),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
    await waitFor(() =>
      expect(document.querySelector("[data-github-split-diff]")).not.toBeNull(),
    );
  });
  it.each(["binary", "omitted", "unavailable", "future"])(
    "keeps %s metadata without fetching unavailable bodies",
    async (bodyStatus) => {
      vi.mocked(api.listRunFileChanges).mockResolvedValue({
        ...page,
        files: [{ ...file, bodyStatus }],
      });
      renderView();
      fireEvent.click(
        await findFileTrigger("repo-one", "same.ts"),
      );
      expect(api.getRunFileChangeBody).not.toHaveBeenCalled();
    },
  );
  it("shows no changes only for a complete ready snapshot", async () => {
    vi.mocked(api.listRunFileChanges).mockResolvedValue({
      ...page,
      files: [],
      filesTotal: 0,
      nextCursor: null,
    });
    renderView();
    expect(
      await screen.findByText("No file edits in this run."),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Only part of the changes could be collected."),
    ).toBeNull();
    expect(screen.queryByText(/0 known files/)).toBeNull();
    expect(screen.queryByTestId("run-file-diff-expand")).toBeNull();
  });
  it("shows empty copy and a list-level partial warning when a partial snapshot has no files", async () => {
    vi.mocked(api.listRunFileChanges).mockResolvedValue({
      ...page,
      status: "partial",
      files: [],
      filesTotal: 0,
      nextCursor: null,
    });
    renderView();
    expect(
      await screen.findByText("No file edits in this run."),
    ).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent(
      "Only part of the changes could be collected.",
    );
    expect(screen.queryByTestId("run-file-diff-group")).toBeNull();
    expect(screen.queryByText(/0 known files/)).toBeNull();
    expect(screen.queryByTestId("run-file-diff-expand")).toBeNull();
  });
  it("identifies redacted bodies instead of implying exact original text", async () => {
    vi.mocked(api.getRunFileChangeBody).mockResolvedValue({
      ...file,
      bodyStatus: "redacted",
      before: "[REDACTED]",
      after: "safe",
    });
    renderView();
    fireEvent.click(
      await findFileTrigger("repo-one", "same.ts"),
    );
    expect(
      await screen.findByText("Sensitive content has been redacted."),
    ).toBeInTheDocument();
  });
  it("asks before fetching a large body", async () => {
    vi.mocked(api.listRunFileChanges).mockResolvedValue({
      ...page,
      files: [{ ...file, afterBytes: 330000 }],
    });
    renderView();
    fireEvent.click(
      await findFileTrigger("repo-one", "same.ts"),
    );
    expect(api.getRunFileChangeBody).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "View anyway" }));
    await waitFor(() => expect(api.getRunFileChangeBody).toHaveBeenCalled());
  });
});

// Converted from the independent review probes: assert recovery, not the defect.
it.each(["invalid_response", "legacy_no_baseline", "different_collection"])(
  "retains the failed second-page cursor for %s and recovers on retry",
  async (reason) => {
    let failed = false;
    vi.mocked(api.listRunFileChanges).mockImplementation(
      async (_id, cursor) => {
        if (cursor === "0") return page;
        if (!failed) {
          failed = true;
          return reason === "different_collection"
            ? { ...page, collectionId: "other", nextCursor: null }
            : {
                ...page,
                collectionId: null,
                status: "unavailable",
                reason,
                files: [],
                filesTotal: 0,
                totalsComplete: false,
                nextCursor: null,
              };
        }
        return {
          ...page,
          files: [{ ...file, id: "f2", rootId: "r2" }],
          nextCursor: null,
        };
      },
    );
    renderView();
    await findFileTrigger("repo-one", "same.ts");
    fireEvent.click(screen.getByRole("button", { name: "Load more files" }));
    const retry = await screen.findByRole("button", { name: "Retry" });
    expect(screen.getByRole("status")).toHaveTextContent(
      "Unable to load file changes.",
    );
    expect(
      fileTrigger("repo-one", "same.ts"),
    ).toBeInTheDocument();
    fireEvent.click(retry);
    await findFileTrigger("repo-two", "same.ts");
    expect(
      vi.mocked(api.listRunFileChanges).mock.calls.map((call) => call[1]),
    ).toEqual(["0", "100", "100"]);
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  },
);
it.each(["future", "partial", "unavailable"])(
  "shows empty copy plus a list-level partial warning for %s root coverage with no groups",
  async (coverage) => {
    vi.mocked(api.listRunFileChanges).mockResolvedValue({
      ...page,
      files: [],
      filesTotal: 0,
      nextCursor: null,
      roots: [{ ...page.roots[0]!, coverage }],
    });
    renderView();
    expect(
      await screen.findByText("No file edits in this run."),
    ).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent(
      "Only part of the changes could be collected.",
    );
    expect(screen.queryByTestId("run-file-diff-group")).toBeNull();
    expect(screen.queryByText(/0 known files/)).toBeNull();
  },
);
it.each([1, 2])("uses English file-count agreement for %i", async (count) => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    filesTotal: count,
  });
  renderView();
  expect(
    await screen.findByText(
      new RegExp(`${count} known file${count === 1 ? "" : "s"}$`),
    ),
  ).toBeInTheDocument();
});
it("disambiguates identical root labels", async () => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    roots: page.roots.map((root) => ({ ...root, label: "repo" })),
    files: [file, { ...file, id: "f2", rootId: "r2" }],
    nextCursor: null,
  });
  renderView();
  expect(
    await screen.findByRole("button", { name: /repo \(r1\)/ }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: /repo \(r2\)/ }),
  ).toBeInTheDocument();
  expect(fileTrigger("repo (r1)", "same.ts")).toHaveTextContent("same.ts");
  expect(fileTrigger("repo (r2)", "same.ts")).toHaveTextContent("same.ts");
  expect(fileTrigger("repo (r1)", "same.ts")).not.toHaveTextContent("repo");
});
it.each(["added", "deleted", "mode_changed"])(
  "renders empty-file %s metadata and loads the corresponding body",
  async (change) => {
    const changed = {
      ...file,
      change,
      beforeBytes: 0,
      afterBytes: 0,
      beforeMode: change === "added" ? "" : "100644",
      afterMode: change === "deleted" ? "" : "100755",
    };
    vi.mocked(api.listRunFileChanges).mockResolvedValue({
      ...page,
      files: [changed],
      nextCursor: null,
    });
    vi.mocked(api.getRunFileChangeBody).mockResolvedValue({
      ...changed,
      before: change === "added" ? null : "",
      after: change === "deleted" ? null : "",
    });
    renderView();
    const trigger = await findFileTrigger("repo-one", "same.ts");
    expect(trigger).toHaveTextContent(
      change === "added"
        ? "New file"
        : change === "deleted"
          ? "Deleted"
          : "Mode changed",
    );
    fireEvent.click(trigger);
    await waitFor(() =>
      expect(document.querySelector("[data-github-split-diff]")).not.toBeNull(),
    );
    expect(
      screen.getByText(
        `${changed.beforeMode || "—"} → ${changed.afterMode || "—"}`,
      ),
    ).toBeInTheDocument();
  },
);
it("restores global layout switching, collapse all, sticky headers and syntax highlighting", async () => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    files: [
      { ...file, path: "a.go" },
      { ...file, id: "f2", path: "b.go" },
    ],
    nextCursor: null,
  });
  vi.mocked(api.getRunFileChangeBody).mockImplementation(async (_task, id) => ({
    ...file,
    id,
    path: id === "f1" ? "a.go" : "b.go",
    before: "func old() {}\n",
    after: "func next() {}\n",
  }));
  renderView();
  fireEvent.click(await screen.findByRole("button", { name: /a.go/ }));
  fireEvent.click(screen.getByRole("button", { name: /b.go/ }));
  await waitFor(() =>
    expect(document.querySelectorAll("[data-github-split-diff]")).toHaveLength(
      2,
    ),
  );
  expect(document.querySelector(".hljs-keyword")).toHaveTextContent("func");
  expect(
    screen.getByRole("button", { name: /a.go/ }).parentElement,
  ).toHaveClass("sticky", "top-8");
  const collapse = screen.getByRole("button", { name: "Collapse all" });
  const expand = screen.getByRole("button", { name: "Expand all" });
  const layout = screen.getByTestId("run-file-diff-layout");
  expect(expand.compareDocumentPosition(collapse)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  expect(collapse.compareDocumentPosition(layout)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  for (const mode of ["unified", "split"]) {
    fireEvent.click(
      screen.getByRole("radio", {
        name: mode === "unified" ? "Unified" : "Split",
      }),
    );
    for (const diff of document.querySelectorAll("[data-github-split-diff]"))
      expect(diff).toHaveAttribute("data-github-split-mode", mode);
  }
  expect(
    document.querySelector(
      '[data-change-diff-kind="remove"] [data-change-diff-line]',
    ),
  ).toHaveClass("bg-destructive/20");
  expect(
    document.querySelector(
      '[data-change-diff-kind="add"] [data-change-diff-line]',
    ),
  ).toHaveClass("bg-success/20");
  expect(document.querySelector("[data-change-diff-sign]")).toBeNull();
  fireEvent.click(collapse);
  await waitFor(() =>
    expect(document.querySelectorAll("[data-github-split-diff]")).toHaveLength(
      0,
    ),
  );
  expect(
    screen.getByRole("button", { name: /repo-one/ }),
  ).toHaveAttribute("aria-expanded", "true");
  expect(fileTrigger("repo-one", "a.go")).toBeVisible();
  fireEvent.click(expand);
  await waitFor(() =>
    expect(document.querySelectorAll("[data-github-split-diff]")).toHaveLength(
      2,
    ),
  );
});
it("forces Unified and hides the layout switch in a narrow container", async () => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
    new DOMRect(0, 0, 499, 400),
  );
  try {
    renderView();
    fireEvent.click(
      await findFileTrigger("repo-one", "same.ts"),
    );
    await waitFor(() =>
      expect(
        document.querySelector("[data-github-split-diff]"),
      ).toHaveAttribute("data-github-split-mode", "unified"),
    );
    expect(screen.getByTestId("run-file-diff-view")).toHaveAttribute(
      "data-file-diff-container-width",
      "499",
    );
    expect(screen.queryByTestId("run-file-diff-layout")).toBeNull();
  } finally {
    vi.restoreAllMocks();
  }
});

it("copies the unshortened relative path when the run workdir is missing", async () => {
  const path = "packages/views/docs/workspaces.zh.mdx";
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    files: [{ ...file, path }],
  });
  renderView();
  const label = await screen.findByTitle(`repo-one / ${path}`);
  expect(label).toHaveTextContent(".../docs/workspaces.zh.mdx");
  expect(label).not.toHaveTextContent("packages/views");
  expect(label).not.toHaveTextContent("repo-one /");
  fireEvent.click(screen.getByRole("button", { name: "Copy file path" }));
  expect(copyText).toHaveBeenCalledWith(`repo-one/${path}`);
});

it("copies absolute paths for same-named files in different roots without a local opener", async () => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    files: [file, { ...file, id: "f2", rootId: "r2", change: "deleted" }],
    nextCursor: null,
  });
  renderView("task", { workdir: "/run" });
    await findFileTrigger("repo-two", "same.ts");
  const copies = screen.getAllByRole("button", { name: "Copy file path" });
  fireEvent.click(copies[0]!);
  expect(copyText).toHaveBeenLastCalledWith("/run/repo-one/same.ts");
  fireEvent.click(copies[1]!);
  expect(copyText).toHaveBeenLastCalledWith("/run/repo-two/same.ts");
  expect(api.getRunFileChangeBody).not.toHaveBeenCalled();
  expect(screen.queryByTestId("run-file-diff-open")).toBeNull();
});

it("groups project roots above workdir and omits empty groups", async () => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    roots: [
      {
        id: "wd",
        label: "workdir",
        kind: "directory",
        coverage: "complete",
        reason: "",
      },
      ...page.roots,
    ],
    files: [
      { ...file, id: "w1", rootId: "wd", path: "notes.md", change: "added" },
      file,
    ],
    nextCursor: null,
  });
  renderView();
  await findFileTrigger("repo-one", "same.ts");
  expect(
    screen.getAllByTestId("run-file-diff-group").map((el) => el.getAttribute("data-root-id")),
  ).toEqual(["r1", "wd"]);
  expect(screen.queryByRole("button", { name: /repo-two/ })).toBeNull();
  expect(fileTrigger("repo-one", "same.ts")).toHaveTextContent("same.ts");
  expect(fileTrigger("repo-one", "same.ts")).not.toHaveTextContent("repo-one /");
  expect(fileTrigger("workdir", "notes.md")).not.toHaveTextContent("workdir /");
  expect(screen.getByRole("button", { name: /repo-one/ })).toHaveTextContent(
    "Project",
  );
  expect(screen.getByRole("button", { name: /^workdir/ })).toHaveTextContent(
    "Run working directory",
  );
});

it("moves per-root partial warnings onto the matching group header", async () => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    roots: [{ ...page.roots[0]!, coverage: "partial" }, page.roots[1]!],
    nextCursor: null,
  });
  renderView();
  await findFileTrigger("repo-one", "same.ts");
  expect(screen.getByTestId("run-file-diff-group")).toHaveTextContent(
    "Only part of the changes could be collected.",
  );
  expect(screen.queryByText(/repo-one ·/)).toBeNull();
  expect(screen.getByTestId("run-file-diff-banner")).toHaveTextContent(
    "Only part of the changes could be collected.",
  );
});

it("collapses only one group's files from the group header", async () => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    files: [file, { ...file, id: "f2", rootId: "r2" }],
    nextCursor: null,
  });
  renderView();
  await findFileTrigger("repo-two", "same.ts");
  const groupToggle = screen.getByRole("button", { name: /repo-one/ });
  fireEvent.click(groupToggle);
  expect(groupToggle).toHaveAttribute("aria-expanded", "false");
  expect(screen.getByTitle("repo-one / same.ts")).not.toBeVisible();
  expect(fileTrigger("repo-two", "same.ts")).toBeVisible();
});

it("keeps a list-level partial warning with empty copy for an unknown root kind", async () => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    files: [],
    filesTotal: 0,
    nextCursor: null,
    roots: [{ ...page.roots[0]!, kind: "future" }],
  });
  renderView();
  expect(
    await screen.findByText("No file edits in this run."),
  ).toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent(
    "Only part of the changes could be collected.",
  );
});
it("does not claim registered-workdir coverage for an unknown scope", async () => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    scope: "future",
    files: [],
    filesTotal: 0,
    nextCursor: null,
  });
  renderView();
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent(
      "Final file changes are unavailable.",
    ),
  );
  // The scope/count line is ready/partial-only; a degraded snapshot must not
  // carry it in any form.
  expect(screen.queryByText(/Registered workdir roots only/)).toBeNull();
  expect(screen.queryByText(/Collection scope/)).toBeNull();
  expect(screen.queryByText("No file edits in this run.")).toBeNull();
});
it("offers retry for an interrupted snapshot and names the interruption", async () => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    status: "unavailable",
    reason: "interrupted",
    files: [],
    filesTotal: 0,
    nextCursor: null,
  });
  renderView();
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent(
      "The run was interrupted; final file changes are temporarily unavailable.",
    ),
  );
  expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  expect(screen.queryByText(/Registered workdir roots only/)).toBeNull();
});
it("keeps a legacy snapshot final with no retry affordance", async () => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    status: "unavailable",
    reason: "legacy_no_baseline",
    files: [],
    filesTotal: 0,
    nextCursor: null,
  });
  renderView();
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent(
      "This run has no baseline. Final file changes are unavailable.",
    ),
  );
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  expect(screen.queryByText(/Registered workdir roots only/)).toBeNull();
});
it("retains unknown file metadata without requesting or interpreting its body", async () => {
  vi.mocked(api.listRunFileChanges).mockResolvedValue({
    ...page,
    files: [{ ...file, change: "future" }],
    nextCursor: null,
  });
  renderView();
  fireEvent.click(
    await findFileTrigger("repo-one", "same.ts"),
  );
  expect(api.getRunFileChangeBody).not.toHaveBeenCalled();
  expect(document.querySelector("[data-github-split-diff]")).toBeNull();
});

// jsdom has no layout: give the real TanStack virtualizer a finite viewport.
function mockDiffViewport() {
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(240);
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(800);
}
it.each([79, 80])(
  "renders %i visible items at the virtualization boundary",
  async (count) => {
    mockDiffViewport();
    try {
      const changed = {
        ...file,
        change: "added",
        beforeMode: "",
        afterMode: "100644",
      };
      vi.mocked(api.listRunFileChanges).mockResolvedValue({
        ...page,
        files: [changed],
        nextCursor: null,
      });
      vi.mocked(api.getRunFileChangeBody).mockResolvedValue({
        ...changed,
        before: null,
        after: Array.from({ length: count }, (_, i) => `line-${i}`).join("\n"),
      });
      renderView();
      fireEvent.click(
        await findFileTrigger("repo-one", "same.ts"),
      );
      await waitFor(() =>
        expect(
          document.querySelector("[data-github-split-diff]"),
        ).not.toBeNull(),
      );
      const diff = document.querySelector("[data-github-split-diff]")!;
      if (count === 79) {
        expect(diff.querySelectorAll("[data-index]")).toHaveLength(0);
        expect(diff).toHaveTextContent("line-78");
        expect(diff.children).toHaveLength(79);
      } else {
        await waitFor(() =>
          expect(diff.querySelectorAll("[data-index]").length).toBeGreaterThan(
            0,
          ),
        );
        expect(diff.querySelectorAll("[data-index]").length).toBeLessThan(80);
        expect(diff).toHaveTextContent("line-0");
        expect(diff.querySelector('[data-index="79"]')).toBeNull();
      }
    } finally {
      cleanup();
      vi.restoreAllMocks();
    }
  },
);
it("switches to virtualization when expanding hidden unchanged rows crosses 80 items", async () => {
  mockDiffViewport();
  try {
    const unchanged = Array.from({ length: 100 }, (_, i) => `keep-${i}`).join(
      "\n",
    );
    vi.mocked(api.getRunFileChangeBody).mockResolvedValue({
      ...file,
      before: `old\n${unchanged}`,
      after: `new\n${unchanged}`,
    });
    renderView();
    fireEvent.click(
      await findFileTrigger("repo-one", "same.ts"),
    );
    const expand = await screen.findByRole("button", {
      name: /Show .* unchanged lines/,
    });
    const diff = document.querySelector("[data-github-split-diff]")!;
    expect(diff.querySelectorAll("[data-index]")).toHaveLength(0);
    expect(diff).toHaveTextContent("new");
    fireEvent.click(expand);
    await waitFor(() =>
      expect(
        document.querySelectorAll("[data-github-split-diff] [data-index]")
          .length,
      ).toBeGreaterThan(0),
    );
    expect(
      document.querySelectorAll("[data-github-split-diff] [data-index]").length,
    ).toBeLessThan(101);
    expect(
      document.querySelector("[data-github-split-diff]"),
    ).toHaveTextContent("keep-0");
    expect(
      screen.queryByRole("button", { name: /Show .* unchanged lines/ }),
    ).toBeNull();
  } finally {
    cleanup();
    vi.restoreAllMocks();
  }
});

it("uses the originating page's root semantics for later-page bodies", async () => {
  vi.mocked(api.listRunFileChanges).mockImplementation(async (_task, cursor) => cursor === "0" ? page : {
    ...page,
    roots: [{ ...page.roots[1]!, kind: "future" }],
    files: [{ ...file, id: "f2", rootId: "r2" }],
    nextCursor: null,
  });
  renderView();
  fireEvent.click(await screen.findByRole("button", { name: "Load more files" }));
  fireEvent.click(await findFileTrigger("repo-two", "same.ts"));
  expect(
    screen.getAllByTestId("run-file-diff-group").find(
      (el) => el.getAttribute("data-root-id") === "r2",
    ),
  ).toHaveTextContent("Only part of the changes could be collected.");
  expect(screen.getByTestId("run-file-diff-banner")).toBeInTheDocument();
  expect(api.getRunFileChangeBody).not.toHaveBeenCalled();
});

describe("RunFileDiffView open-local-file", () => {
  const listLocalFileOpeners = vi.fn();
  const resolveLocalFiles = vi.fn();
  const openLocalFile = vi.fn();

  afterEach(() => {
    delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;
    delete (window as unknown as { daemonAPI?: unknown }).daemonAPI;
    listLocalFileOpeners.mockReset();
    resolveLocalFiles.mockReset();
    openLocalFile.mockReset();
  });

  const localRuntime = {
    id: "runtime-local",
    runtime_mode: "local" as const,
    daemon_id: "daemon-local",
  };

  function stubDesktop() {
    (window as unknown as { daemonAPI: unknown }).daemonAPI = {
      getStatus: async () => ({ state: "running", daemonId: "daemon-local" }),
    };
    listLocalFileOpeners.mockResolvedValue({
      openers: [
        { id: "cursor", kind: "editor", name: "Cursor" },
        { id: "file-manager", kind: "file_manager", name: "File manager" },
      ],
      platform: "darwin",
    });
    resolveLocalFiles.mockImplementation(async (paths: string[]) => paths);
    openLocalFile.mockResolvedValue({ ok: true });
    (window as unknown as { desktopAPI: unknown }).desktopAPI = {
      listLocalFileOpeners,
      resolveLocalFiles,
      openLocalFile,
    };
  }

  it("puts the open icon after copy for an existing local file", async () => {
    stubDesktop();
    renderView("task", { workdir: "/Users/dev/proj", runtime: localRuntime });

    const open = await screen.findByTestId("run-file-diff-open");
    const copy = screen.getByRole("button", { name: "Copy file path" });
    expect(copy.compareDocumentPosition(open)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
    fireEvent.click(copy);
    expect(copyText).toHaveBeenCalledWith("/Users/dev/proj/repo-one/same.ts");
    expect(open).toHaveAttribute("aria-label", "Open with Cursor");
    fireEvent.click(open);
    await waitFor(() => {
      expect(openLocalFile).toHaveBeenCalledWith(
        "/Users/dev/proj/repo-one/same.ts",
        "cursor",
      );
    });
  });

  it("joins the run-workdir sentinel root onto the workdir itself", async () => {
    stubDesktop();
    vi.mocked(api.listRunFileChanges).mockResolvedValue({
      ...page,
      roots: [{ ...page.roots[0]!, label: "workdir" }],
    });
    renderView("task", { workdir: "/Users/dev/proj", runtime: localRuntime });

    const open = await screen.findByTestId("run-file-diff-open");
    fireEvent.click(screen.getByRole("button", { name: "Copy file path" }));
    expect(copyText).toHaveBeenCalledWith("/Users/dev/proj/same.ts");
    fireEvent.click(open);
    await waitFor(() => {
      expect(openLocalFile).toHaveBeenCalledWith(
        "/Users/dev/proj/same.ts",
        "cursor",
      );
    });
  });

  it("does not probe or render the button without a desktop API", async () => {
    renderView("task", { workdir: "/Users/dev/proj", runtime: localRuntime });
    await findFileTrigger("repo-one", "same.ts");
    await waitFor(() =>
      expect(listLocalFileOpeners).not.toHaveBeenCalled(),
    );
    expect(resolveLocalFiles).not.toHaveBeenCalled();
    expect(
      screen.queryByTestId("run-file-diff-open"),
    ).not.toBeInTheDocument();
  });

  it.each([
    null,
    { id: "cloud", runtime_mode: "cloud" as const, daemon_id: "daemon-local" },
    { id: "unknown", runtime_mode: "local" as const, daemon_id: null },
  ])(
    "does not probe paths without confirmed local runtime ownership: %j",
    async (runtime) => {
      stubDesktop();
      renderView("task", { workdir: "/Users/dev/proj", runtime });
      await findFileTrigger("repo-one", "same.ts");
      await waitFor(() =>
        expect(listLocalFileOpeners).toHaveBeenCalled(),
      );
      expect(resolveLocalFiles).not.toHaveBeenCalled();
      expect(
        screen.queryByTestId("run-file-diff-open"),
      ).not.toBeInTheDocument();
    },
  );

  it("hides the button when the workdir cannot anchor the path", async () => {
    stubDesktop();
    renderView("task", { runtime: localRuntime });
    await findFileTrigger("repo-one", "same.ts");
    await waitFor(() =>
      expect(listLocalFileOpeners).toHaveBeenCalled(),
    );
    expect(resolveLocalFiles).not.toHaveBeenCalled();
    expect(
      screen.queryByTestId("run-file-diff-open"),
    ).not.toBeInTheDocument();
  });
});

describe("relay states", () => {
  const localRuntime = {
    id: "rt1",
    runtime_mode: "local" as const,
    daemon_id: "daemon-1",
  };
  const offlineRuntime = () =>
    runtimeFixture({
      id: "rt1",
      status: "offline",
      last_seen_at: new Date(Date.now() - 10 * 60 * 1000).toISOString(),
    });

  it.each([
    [503, "runtime_offline"],
    [504, "runtime_timeout"],
    [429, "relay_busy"],
  ])(
    "renders the recoverable offline card for HTTP %i (%s) without polling",
    async (status, code) => {
      vi.mocked(api.getRunFileChangeBody).mockRejectedValueOnce(
        relayError(status, code),
      );
      renderView("task", { runtime: localRuntime });
      fireEvent.click(await findFileTrigger("repo-one", "same.ts"));
      expect(await screen.findByTestId("run-file-diff-body-offline"))
        .toHaveTextContent("Runtime offline");
      expect(
        screen.getByText(
          "File contents are kept on the runtime machine and can't be read until it's back online.",
        ),
      ).toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: "Retry" }),
      ).toBeInTheDocument();
      // The offline state is expected and recoverable — no automatic polling.
      await act(() => new Promise((resolve) => setTimeout(resolve, 50)));
      expect(api.getRunFileChangeBody).toHaveBeenCalledTimes(1);
    },
  );

  it("renders the terminal purged card for 410 without a retry action", async () => {
    vi.mocked(api.getRunFileChangeBody).mockRejectedValueOnce(
      relayError(410, "record_purged"),
    );
    renderView("task", { runtime: localRuntime });
    fireEvent.click(await findFileTrigger("repo-one", "same.ts"));
    expect(await screen.findByTestId("run-file-diff-body-purged"))
      .toHaveTextContent("Local records cleaned up");
    expect(
      screen.queryByRole("button", { name: "Retry" }),
    ).toBeNull();
  });

  it("marks the group header while the runtime is offline and hides it online", async () => {
    vi.mocked(api.listRuntimes).mockResolvedValue([offlineRuntime()]);
    const { client } = renderView("task", { runtime: localRuntime });
    await findFileTrigger("repo-one", "same.ts");
    expect(
      await screen.findByTestId("run-file-diff-group-offline"),
    ).toHaveTextContent("Runtime offline");
    await act(async () => {
      client.setQueryData(runtimeKeys.list("ws"), [runtimeFixture({ id: "rt1" })]);
    });
    await waitFor(() =>
      expect(
        screen.queryByTestId("run-file-diff-group-offline"),
      ).toBeNull(),
    );
  });

  it("auto-refetches an expanded offline file once the runtime is back online", async () => {
    let resolveReload!: (value: RunFileChangeBody) => void;
    vi.mocked(api.listRuntimes).mockResolvedValue([offlineRuntime()]);
    vi.mocked(api.getRunFileChangeBody)
      .mockRejectedValueOnce(relayError(503, "runtime_offline"))
      .mockImplementationOnce(
        () =>
          new Promise<RunFileChangeBody>((resolve) => {
            resolveReload = resolve;
          }),
      );
    const { client } = renderView("task", { runtime: localRuntime });
    fireEvent.click(await findFileTrigger("repo-one", "same.ts"));
    await screen.findByTestId("run-file-diff-body-offline");
    expect(api.getRunFileChangeBody).toHaveBeenCalledTimes(1);

    await act(async () => {
      client.setQueryData(runtimeKeys.list("ws"), [runtimeFixture({ id: "rt1" })]);
    });
    // The auto-reload shows the dedicated reconnecting line, not the card.
    expect(
      await screen.findByTestId("run-file-diff-body-reconnecting"),
    ).toHaveTextContent("Runtime is back online. Reloading changes...");
    await act(async () => {
      resolveReload({ ...file, before: "old\n", after: "new\n" });
    });
    await waitFor(() =>
      expect(
        document.querySelector("[data-github-split-diff]"),
      ).not.toBeNull(),
    );
    // Exactly one automatic refetch — a follow-up failure stays manual.
    expect(api.getRunFileChangeBody).toHaveBeenCalledTimes(2);
    await act(() => new Promise((resolve) => setTimeout(resolve, 50)));
    expect(api.getRunFileChangeBody).toHaveBeenCalledTimes(2);
  });

  it("keeps the offline card disabled-busy during a manual retry", async () => {
    let resolveRetry!: (value: RunFileChangeBody) => void;
    vi.mocked(api.getRunFileChangeBody)
      .mockRejectedValueOnce(relayError(503, "runtime_offline"))
      .mockImplementationOnce(
        () =>
          new Promise<RunFileChangeBody>((resolve) => {
            resolveRetry = resolve;
          }),
      );
    renderView("task", { runtime: localRuntime });
    fireEvent.click(await findFileTrigger("repo-one", "same.ts"));
    fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
    // The in-flight flip lands asynchronously on a replaced node.
    await waitFor(() => {
      const busyRetry = screen.getByRole("button", { name: "Retry" });
      expect(busyRetry).toBeDisabled();
      expect(busyRetry).toHaveAttribute("aria-busy", "true");
    });
    // The card stays put while the retry is in flight — only the button spins.
    expect(screen.getByTestId("run-file-diff-body-offline")).toBeInTheDocument();
    await act(async () => {
      resolveRetry({ ...file, before: "old\n", after: "new\n" });
    });
    await waitFor(() =>
      expect(
        document.querySelector("[data-github-split-diff]"),
      ).not.toBeNull(),
    );
  });
});
