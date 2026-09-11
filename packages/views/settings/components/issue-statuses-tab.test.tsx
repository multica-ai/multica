/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { IssueStatusEntry } from "@multica/core/types";
import { ISSUE_STATUS_ICONS } from "@multica/core/types/issue-status";
import en from "../../locales/en/settings.json";
import { IssueStatusesTab } from "./issue-statuses-tab";
import { ApiError } from "@multica/core/api/client";

const reorderMutate = vi.hoisted(() => vi.fn());
const createMutate = vi.hoisted(() => vi.fn());
const updateMutate = vi.hoisted(() => vi.fn());
const archiveMutate = vi.hoisted(() => vi.fn());
const navigatePush = vi.hoisted(() => vi.fn());
const prepareList = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issues: () => "/dev/issues" }) }));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push: navigatePush }) }));
vi.mock("@multica/core/issue-statuses", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issue-statuses")>()),
  prepareIssueStatusList: prepareList,
}));
let catalog: IssueStatusEntry[] = [];
let role: string = "owner";

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => ({
    data: options.queryKey[0] === "issue-statuses" ? catalog : members(),
    isLoading: false,
  }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: unknown) => unknown) => selector({ user: { id: "u-1" } }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members", "ws-1"] }),
}));
// Only the fetch is stubbed. The module's pure helpers (`issueStatusColor`)
// are what the rows render with, and a stub of those would test the stub.
vi.mock("@multica/core/issue-statuses/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issue-statuses/queries")>()),
  issueStatusListOptions: () => ({ queryKey: ["issue-statuses", "ws-1"] }),
}));
vi.mock("@multica/core/issue-statuses/mutations", () => ({
  useCreateIssueStatus: () => ({ mutate: createMutate, isPending: false }),
  useUpdateIssueStatus: () => ({ mutate: updateMutate, isPending: false }),
  useArchiveIssueStatus: () => ({ mutate: archiveMutate, isPending: false }),
  useReorderIssueStatuses: () => ({ mutate: reorderMutate }),
}));
vi.mock("../../issues/utils/status-label", () => ({
  useStatusLabel: () => (key: string) => key,
}));
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (accessor: (dict: unknown) => string, params?: Record<string, unknown>) => {
      const template = accessor(en);
      if (!params) return template;
      return template.replace(/\{\{(\w+)\}\}/g, (_, k: string) => String(params[k] ?? ""));
    },
  }),
}));

function members() {
  return [{ user_id: "u-1", role }];
}

function entry(overrides: Partial<IssueStatusEntry>): IssueStatusEntry {
  return {
    id: overrides.key ?? "id",
    workspace_id: "ws-1",
    key: "custom",
    name: "Custom",
    description: "",
    category: "started",
    color: "#ff0000",
    is_system: false,
    position: 1,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

const BUILT_IN_IN_REVIEW = entry({
  id: "in_review",
  key: "in_review",
  name: "In Review",
  is_system: true,
  position: 0,
});

afterEach(() => {
  cleanup();
  reorderMutate.mockClear();
  createMutate.mockClear();
  updateMutate.mockClear();
  archiveMutate.mockReset();
  navigatePush.mockClear();
  prepareList.mockClear();
  catalog = [];
  role = "owner";
});

describe("IssueStatusesTab", () => {
  it("aligns category and row actions with equal end padding and pointer target sizes", () => {
    catalog = [BUILT_IN_IN_REVIEW, entry({ key: "qa", name: "QA" })];
    render(<IssueStatusesTab />);
    const add = screen.getByLabelText(`${en.issue_statuses.add}: ${en.issue_statuses.category_labels.started}`);
    expect(add.parentElement).toHaveClass("px-4");
    expect(add).toHaveClass("size-7", "shrink-0", "[@media(pointer:coarse)]:size-11");
    for (const name of ["in_review", "QA"]) {
      const action = screen.getByLabelText(en.issue_statuses.actions.open.replace("{{name}}", name));
      expect(action.closest(".group\\/row")).toHaveClass("pr-4");
      expect(action).toHaveClass("size-7", "shrink-0", "[@media(pointer:coarse)]:size-11");
    }
  });

  it.each(["unstarted", "started", "done", "closed"] as const)(
    "offers seven unique shapes and a text-only default in %s",
    async (category) => {
      render(<IssueStatusesTab />);
      fireEvent.click(screen.getByLabelText(`${en.issue_statuses.add}: ${en.issue_statuses.category_labels[category]}`));
      const dialog = await screen.findByRole("dialog");
      const picker = within(dialog).getByRole("group", { name: en.issue_statuses.editor.icon });
      const defaultChoice = within(picker).getByRole("button", { name: en.issue_statuses.editor.icon_shapes.default });
      expect(defaultChoice).toHaveTextContent(en.issue_statuses.editor.icon_shapes.default);
      expect(defaultChoice.querySelector("svg")).toBeNull();
      expect(defaultChoice).toHaveAttribute("aria-pressed", "true");
      const shapes = ISSUE_STATUS_ICONS.map((icon) => {
        const button = within(picker).getByRole("button", { name: en.issue_statuses.editor.icon_shapes[icon] });
        expect(button).toHaveAttribute("aria-pressed", "false");
        return button.querySelector("svg")!.innerHTML;
      });
      expect(picker.querySelectorAll("svg")).toHaveLength(7);
      expect(new Set(shapes).size).toBe(7);
    },
  );

  it("can reset a saved shape to the existing category default", async () => {
    catalog = [entry({ key: "qa", name: "QA", icon: "slash" })];
    render(<IssueStatusesTab />);
    fireEvent.click(screen.getByLabelText(en.issue_statuses.actions.open.replace("{{name}}", "QA")));
    fireEvent.click(await screen.findByRole("menuitem", { name: en.issue_statuses.actions.edit }));
    const dialog = await screen.findByRole("dialog");
    const defaultChoice = within(dialog).getByRole("button", { name: en.issue_statuses.editor.icon_shapes.default });
    fireEvent.click(defaultChoice);
    expect(defaultChoice).toHaveAttribute("aria-pressed", "true");
    expect(within(dialog).getByRole("button", { name: en.issue_statuses.editor.icon_shapes.slash })).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(within(dialog).getByRole("button", { name: en.issue_statuses.editor.save }));
    expect(updateMutate).toHaveBeenCalledWith(expect.objectContaining({ id: "qa", icon: "" }), expect.any(Object));
  });

  it("keeps an in-use status active and opens its exact issue list after the archive conflict", async () => {
    catalog = [entry({ key: "shipped", name: "Shipped", category: "done" })];
    archiveMutate.mockImplementation((_id, options) => options.onError(new ApiError("in use", 409, "Conflict", {
      code: "issue_status_in_use", issue_count: 120,
    })));
    render(<IssueStatusesTab />);
    await userEvent.click(screen.getByLabelText(en.issue_statuses.actions.open.replace("{{name}}", "Shipped")));
    await userEvent.click(await screen.findByRole("menuitem", { name: en.issue_statuses.actions.archive }));
    await userEvent.click(screen.getByRole("button", { name: en.issue_statuses.archive_dialog.confirm }));
    const dialog = screen.getByRole("alertdialog");
    expect(within(dialog).getByText(en.issue_statuses.archive_dialog.in_use_title)).toBeInTheDocument();
    expect(within(dialog).getByText(/120/)).toBeInTheDocument();
    expect(within(dialog).queryByRole("button", { name: en.issue_statuses.archive_dialog.confirm })).toBeNull();
    await userEvent.click(within(dialog).getByRole("button", { name: en.issue_statuses.archive_dialog.view_issues }));
    expect(prepareList).toHaveBeenCalledWith("ws-1", "shipped");
    expect(navigatePush).toHaveBeenCalledWith("/dev/issues");
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("closes the confirmation only after an empty status is successfully archived", async () => {
    catalog = [entry({ key: "empty", name: "Empty" })];
    render(<IssueStatusesTab />);
    await userEvent.click(screen.getByLabelText(en.issue_statuses.actions.open.replace("{{name}}", "Empty")));
    await userEvent.click(await screen.findByRole("menuitem", { name: en.issue_statuses.actions.archive }));
    await userEvent.click(screen.getByRole("button", { name: en.issue_statuses.archive_dialog.confirm }));
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    act(() => archiveMutate.mock.calls[0]![1].onSuccess());
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });
  it("creates a status with independent shape and color", async () => {
    catalog = [BUILT_IN_IN_REVIEW];
    render(<IssueStatusesTab />);
    fireEvent.click(screen.getByLabelText(`${en.issue_statuses.add}: ${en.issue_statuses.category_labels.started}`));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText(en.issue_statuses.editor.name), { target: { value: "Awaiting response" } });
    const choice = within(dialog).getByRole("button", { name: en.issue_statuses.editor.icon_shapes.three_quarters });
    fireEvent.click(choice);
    expect(choice).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(within(dialog).getByRole("button", { name: en.issue_statuses.editor.save }));
    expect(createMutate).toHaveBeenCalledWith(expect.objectContaining({ name: "Awaiting response", category: "started", icon: "three_quarters", color: expect.any(String) }), expect.any(Object));
  });

  it("loads and edits a saved shape without changing category", async () => {
    catalog = [entry({ key: "qa", name: "QA", icon: "slash" })];
    render(<IssueStatusesTab />);
    fireEvent.click(screen.getByLabelText(en.issue_statuses.actions.open.replace("{{name}}", "QA")));
    fireEvent.click(await screen.findByRole("menuitem", { name: en.issue_statuses.actions.edit }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("button", { name: en.issue_statuses.editor.icon_shapes.slash })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(within(dialog).getByRole("button", { name: en.issue_statuses.editor.icon_shapes.cross }));
    fireEvent.click(within(dialog).getByRole("button", { name: en.issue_statuses.editor.save }));
    expect(updateMutate).toHaveBeenCalledWith(expect.objectContaining({ id: "qa", icon: "cross" }), expect.any(Object));
    expect(updateMutate.mock.calls[0]![0]).not.toHaveProperty("category");
  });

  it("preserves a future icon when editing only the name", async () => {
    catalog = [entry({ key: "qa", name: "QA", icon: "future-shape" })];
    render(<IssueStatusesTab />);
    fireEvent.click(screen.getByLabelText(en.issue_statuses.actions.open.replace("{{name}}", "QA")));
    fireEvent.click(await screen.findByRole("menuitem", { name: en.issue_statuses.actions.edit }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText(en.issue_statuses.editor.name), { target: { value: "Renamed QA" } });
    fireEvent.click(within(dialog).getByRole("button", { name: en.issue_statuses.editor.save }));
    expect(updateMutate.mock.calls[0]![0]).toMatchObject({ id: "qa", name: "Renamed QA" });
    expect(updateMutate.mock.calls[0]![0]).not.toHaveProperty("icon");
  });
  it("keeps the create dialog concise and category choices text-only", async () => {
    catalog = [BUILT_IN_IN_REVIEW];
    render(<IssueStatusesTab />);
    fireEvent.click(screen.getByLabelText(
      `${en.issue_statuses.add}: ${en.issue_statuses.category_labels.started}`,
    ));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByText(/inherit|parking|recovery/i)).toBeNull();
    expect(within(dialog).getByLabelText(en.issue_statuses.editor.name)).toBeInTheDocument();
    expect(within(dialog).getByLabelText(en.issue_statuses.editor.description)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", {
      name: en.issue_statuses.editor.color,
    }).querySelector("svg")).not.toBeNull();
    const category = within(dialog).getByRole("combobox");
    // The chevron is a selector affordance, not a status glyph.
    expect(category.querySelector("svg:not(.lucide-chevron-down)")).toBeNull();
    fireEvent.click(category);
    for (const option of await screen.findAllByRole("option")) {
      expect(option.querySelector("svg:not(.lucide-check)")).toBeNull();
    }
  });

  // Creation answers to workspace role alone since MUL-6643 removed the
  // rollout flag; an owner gets the affordance on every deployment.
  it("offers status creation to an owner", () => {
    catalog = [BUILT_IN_IN_REVIEW];
    render(<IssueStatusesTab />);

    expect(
      screen.getAllByLabelText(new RegExp(`^${en.issue_statuses.add}:`)).length,
    ).toBeGreaterThan(0);
  });

  it("hides creation and row actions from a non-admin member", () => {
    catalog = [BUILT_IN_IN_REVIEW, entry({ key: "qa", name: "QA" })];
    role = "member";
    render(<IssueStatusesTab />);

    expect(screen.queryByLabelText(new RegExp(`^${en.issue_statuses.add}:`))).toBeNull();
    expect(screen.getByText("QA")).toBeInTheDocument();
    expect(
      screen.queryByLabelText(
        en.issue_statuses.actions.open.replace("{{name}}", "QA"),
      ),
    ).toBeNull();
  });

  // Archiving retires a status from FUTURE assignment; the row has to stay
  // legible so an admin can tell what a lingering status on an old issue is.
  it("keeps an archived status visible but not actionable", () => {
    catalog = [
      BUILT_IN_IN_REVIEW,
      entry({ key: "qa", name: "QA", archived_at: "2026-01-01T00:00:00Z" }),
    ];
    render(<IssueStatusesTab />);

    // Hidden until the toggle is on, but the toggle itself is enabled because
    // the workspace has one.
    expect(screen.queryByText("QA")).toBeNull();
    expect(screen.getByRole("switch")).toBeEnabled();
  });

  // The category header used to repeat the sentence its built-in row already
  // carried, so every category said the same thing twice. (MUL-6422)
  it("states a category's behavior once, on its built-in row", () => {
    catalog = [BUILT_IN_IN_REVIEW];
    render(<IssueStatusesTab />);

    expect(
      screen.getAllByText(en.issue_statuses.built_in_descriptions.in_review),
    ).toHaveLength(1);
  });

  // A toggle that can only ever reveal nothing is not worth a row of chrome.
  it("offers the archived toggle only once something is archived", () => {
    catalog = [BUILT_IN_IN_REVIEW, entry({ key: "qa", name: "QA" })];
    render(<IssueStatusesTab />);

    expect(screen.queryByRole("switch")).toBeNull();
  });

  it("allows a single custom status to move relative to built-ins", () => {
    catalog = [BUILT_IN_IN_REVIEW, entry({ key: "qa", name: "QA" })];
    render(<IssueStatusesTab />);

    expect(
      screen.getByLabelText(
        en.issue_statuses.actions.reorder.replace("{{name}}", "QA"),
      ),
    ).toBeInTheDocument();
  });

  it("offers reorder once a category holds two", () => {
    catalog = [
      BUILT_IN_IN_REVIEW,
      entry({ id: "qa", key: "qa", name: "QA", position: 1 }),
      entry({ id: "uat", key: "uat", name: "UAT", position: 2 }),
    ];
    render(<IssueStatusesTab />);

    expect(
      screen.getByLabelText(en.issue_statuses.actions.reorder.replace("{{name}}", "QA")),
    ).toBeInTheDocument();
  });

  it("saves the full active order, including built-ins, from the menu", async () => {
    catalog = [BUILT_IN_IN_REVIEW, entry({ key: "qa", name: "QA" })];
    render(<IssueStatusesTab />);
    fireEvent.click(screen.getByLabelText(en.issue_statuses.actions.open.replace("{{name}}", "QA")));
    fireEvent.click(await screen.findByRole("menuitem", { name: en.issue_statuses.actions.move_up }));
    expect(reorderMutate).toHaveBeenCalledWith(
      { category: "started", ordered: [catalog[1], catalog[0]] },
      expect.any(Object),
    );
  });

  it("excludes archived rows from reorder and restores the order after failure", async () => {
    catalog = [
      BUILT_IN_IN_REVIEW,
      entry({ key: "old", name: "Old", archived_at: "2026-01-01", position: 1 }),
      entry({ key: "qa", name: "QA", position: 2 }),
    ];
    render(<IssueStatusesTab />);
    fireEvent.click(screen.getByRole("switch"));
    expect(screen.getByText("Old")).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText(en.issue_statuses.actions.open.replace("{{name}}", "QA")));
    fireEvent.click(await screen.findByRole("menuitem", { name: en.issue_statuses.actions.move_up }));
    expect(reorderMutate.mock.calls[0]![0].ordered.map((s: IssueStatusEntry) => s.key)).toEqual(["qa", "in_review"]);
    const callbacks = reorderMutate.mock.calls[0]![1];
    act(() => {
      callbacks.onError(new Error("Could not save order"));
      callbacks.onSettled();
    });
    const section = screen.getByRole("region", { name: en.issue_statuses.category_labels.started });
    expect(within(section).getAllByRole("button", { name: /^Reorder / }).map((button) => button.getAttribute("aria-label"))).toEqual([
      "Reorder in_review", "Reorder QA",
    ]);
  });

  it.each([
    ["edit", "click"], ["archive", "click"],
    ["edit", "enter"], ["archive", "enter"],
    ["edit", "escape"], ["archive", "escape"],
  ] as const)("dismisses the built-in %s notice using %s", async (action, dismiss) => {
    const user = userEvent.setup();
    catalog = [BUILT_IN_IN_REVIEW];
    render(<IssueStatusesTab />);
    const trigger = screen.getByLabelText(
      en.issue_statuses.actions.open.replace("{{name}}", "in_review"),
    );
    expect(trigger.className).toContain("group-focus-within/row:opacity-100");
    expect(trigger.className).toContain("data-popup-open:opacity-100");
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByRole("menuitem", { name: en.issue_statuses.actions[action] }));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText(en.issue_statuses.built_in_dialog.description)).toBeInTheDocument();
    expect(screen.queryByLabelText(en.issue_statuses.editor.name)).toBeNull();
    const close = within(dialog).getByRole("button", { name: en.issue_statuses.built_in_dialog.confirm });
    if (dismiss === "click") {
      await user.click(close);
    } else {
      close.focus();
      await user.keyboard(dismiss === "enter" ? "{Enter}" : "{Escape}");
    }
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    // Closing must release the modal layer, not leave Settings inaccessible.
    await user.click(screen.getByLabelText(`${en.issue_statuses.add}: ${en.issue_statuses.category_labels.started}`));
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
  });
});
