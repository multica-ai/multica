// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import type { IssueStatusEntry, IssueWorkflow } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";
import { StatusPicker } from "./status-picker";

// The catalog is server state; this suite is about what the picker PAINTS with
// it, so the entries are fed in directly. The color matrix behind `colorOf`
// lives in packages/core/issue-statuses/queries.test.ts — this file only proves
// the trigger and the list read it from the same place.
let catalogEntries: IssueStatusEntry[] | undefined;

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog(catalogEntries),
}));

// Project workflows (MUL-7420): the picker resolves the issue's project and its
// workflow through one hook, fed directly here like the catalog.
let projectWorkflow: IssueWorkflow | null = null;
let workspaceDefault: IssueWorkflow | null = null;

vi.mock("@multica/core/issue-workflows", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issue-workflows")>()),
  useProjectWithWorkflow: () => ({ project: null, workflow: projectWorkflow, defaultWorkflow: workspaceDefault }),
}));

vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid={`avatar-${actorId}`} />,
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => (id === "agent-1" ? "Sentinel" : "Unknown") }),
}));

function entry(overrides: Partial<IssueStatusEntry>): IssueStatusEntry {
  return {
    id: overrides.key ?? "id",
    workspace_id: "workspace-1",
    key: "custom",
    name: "Custom",
    description: "",
    category: "started",
    // Seeded per status by the server — including for the built-ins, which
    // are not recolorable and must ignore it.
    color: "#22c55e",
    is_system: false,
    position: 1,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

const IN_REVIEW = entry({
  id: "in_review",
  key: "in_review",
  name: "In Review",
  is_system: true,
  position: 0,
});

const QA = entry({ id: "qa", key: "qa", name: "QA", color: "#ec7a2d" });

/** StatusIcon's own glyph, told apart from the lucide check mark beside it. */
const STATUS_ICON = 'svg[viewBox="0 0 14 14"]';

function iconOf(row: Element | null): SVGElement | null {
  return row?.querySelector<SVGElement>(STATUS_ICON) ?? null;
}

/** The list row for one status — the trigger is a button carrying the same
 *  label, so the picker-item marker is what tells the two apart. */
function optionRow(label: string): HTMLElement {
  const row = Array.from(
    document.querySelectorAll<HTMLElement>("button[data-picker-item]"),
  ).find((el) => el.textContent?.trim() === label);
  if (!row) throw new Error(`no picker row for "${label}"`);
  return row;
}

afterEach(() => {
  cleanup();
  catalogEntries = undefined;
  projectWorkflow = null;
  workspaceDefault = null;
});

describe("StatusPicker trigger color", () => {
  // The bug: the trigger read the catalog entry's raw color while the list read
  // the resolved one, so a built-in rendered as the server's seeded #22c55e in
  // one and as the `text-success` token in the other — the same status in two
  // visibly different greens, side by side. (MUL-6440)
  it("paints a built-in from the token, exactly like its row in the list", () => {
    catalogEntries = [IN_REVIEW, { ...QA, icon: "slash" }];
    const { container } = renderWithI18n(
      <StatusPicker status="in_review" onUpdate={() => {}} open onOpenChange={() => {}} />,
    );

    const trigger = iconOf(container);
    const row = iconOf(optionRow("In Review"));

    // No inline color on either: an inline color is precisely what overrides
    // the token and produces the two-greens mismatch.
    expect(trigger?.getAttribute("style")).toBeNull();
    expect(row?.getAttribute("style")).toBeNull();
    expect(trigger?.getAttribute("class")).toContain("text-success");
    expect(row?.getAttribute("class")).toContain("text-success");
  });

  // The other half of the same rule: a CUSTOM status has no token to fall back
  // on, so its own color has to reach both controls.
  it("paints a custom status from its own color in both places", () => {
    catalogEntries = [IN_REVIEW, { ...QA, icon: "slash" }];
    const { container } = renderWithI18n(
      <StatusPicker status="qa" onUpdate={() => {}} open onOpenChange={() => {}} />,
    );

    const trigger = iconOf(container);
    const row = iconOf(optionRow("QA"));

    expect(trigger?.style.color).toBe("rgb(236, 122, 45)");
    expect(row?.style.color).toBe(trigger?.style.color);
    expect(trigger?.querySelector("line")).not.toBeNull();
    expect(row?.innerHTML).toBe(trigger?.innerHTML);
  });
});

describe("StatusPicker mark-as-duplicate action", () => {
  // Create and batch surfaces must never offer it: an issue that does not
  // exist yet cannot duplicate anything.
  it("is absent unless the caller opts in", () => {
    renderWithI18n(
      <StatusPicker status="todo" onUpdate={() => {}} open onOpenChange={() => {}} />,
    );
    expect(screen.queryByRole("button", { name: "Mark as duplicate" })).toBeNull();
  });

  it("is an action beside the options, not another status", () => {
    const onMarkDuplicate = vi.fn();
    const onUpdate = vi.fn();
    const onOpenChange = vi.fn();
    renderWithI18n(
      <StatusPicker
        status="todo"
        onUpdate={onUpdate}
        open
        onOpenChange={onOpenChange}
        onMarkDuplicate={onMarkDuplicate}
      />,
    );

    const action = screen.getByRole("button", { name: "Mark as duplicate" });
    // Outside the arrow-key listbox, so keyboard nav and search skip it.
    expect(action.hasAttribute("data-picker-item")).toBe(false);

    fireEvent.click(action);
    expect(onMarkDuplicate).toHaveBeenCalledTimes(1);
    expect(onUpdate).not.toHaveBeenCalled();
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});

describe("StatusPicker on an issue that already is a duplicate", () => {
  it("relabels the action to re-point the mark", () => {
    renderWithI18n(
      <StatusPicker
        status="cancelled"
        onUpdate={() => {}}
        open
        onOpenChange={() => {}}
        onMarkDuplicate={() => {}}
        isDuplicate
      />,
    );
    expect(screen.getByRole("button", { name: "Change original" })).toBeTruthy();
  });
});

describe("StatusPicker in a project that uses a workflow", () => {
  const TODO = entry({ id: "todo", key: "todo", name: "Todo", category: "unstarted", is_system: true, position: 0 });
  const DONE = entry({ id: "done", key: "done", name: "Done", category: "done", is_system: true, position: 0 });

  it("offers only the workflow's steps, in workflow order, naming each handoff", () => {
    catalogEntries = [TODO, IN_REVIEW, QA, DONE];
    projectWorkflow = {
      id: "wf-1",
      workspace_id: "workspace-1",
      name: "Delivery",
      description: "",
      is_default: false,
      owner_id: null,
      steps: [
        { status_key: "todo", handler: { type: "none" }, instructions: "" },
        { status_key: "qa", handler: { type: "agent", id: "agent-1" }, instructions: "" },
        { status_key: "done", handler: { type: "none" }, instructions: "" },
      ],
      project_ids: ["project-1"],
      created_at: "",
      updated_at: "",
    };
    renderWithI18n(
      <StatusPicker status="todo" projectId="project-1" onUpdate={() => {}} open onOpenChange={() => {}} />,
    );

    const rows = Array.from(document.querySelectorAll("button[data-picker-item]")).map(
      (el) => el.textContent?.trim(),
    );
    // Only a column with a handoff carries its handler's avatar.
    expect(rows).toEqual(["Todo", "QAHands off to Sentinel", "Done"]);
    expect(screen.getByText("Hands off to Sentinel").className).toContain("sr-only");
    expect(screen.getByTestId("avatar-agent-1")).toBeTruthy();
    expect(screen.getByText("Delivery")).toBeTruthy();
    expect(screen.queryByText("In Review")).toBeNull();
  });

  it("offers the whole catalog when the project uses the Default workflow", () => {
    catalogEntries = [TODO, IN_REVIEW, QA, DONE];
    renderWithI18n(
      <StatusPicker status="todo" projectId="project-1" onUpdate={() => {}} open onOpenChange={() => {}} />,
    );
    expect(optionRow("In Review")).toBeTruthy();
    expect(screen.queryByText(/Hands off to/)).toBeNull();
  });
});

describe("StatusPicker on an issue outside any project (MUL-8121)", () => {
  const TODO = entry({ id: "todo", key: "todo", name: "Todo", category: "unstarted", is_system: true, position: 0 });
  const DONE = entry({ id: "done", key: "done", name: "Done", category: "done", is_system: true, position: 0 });
  const MERGED = entry({ id: "merged", key: "merged", name: "Merged", position: 2 });

  const DEFAULT: IssueWorkflow = {
    id: "wf-default",
    workspace_id: "workspace-1",
    name: "Default",
    description: "",
    is_default: true,
    owner_id: null,
    steps: [
      { status_key: "todo", handler: { type: "none" }, instructions: "" },
      // A handoff on Default never fires outside a project, so it is not hinted.
      { status_key: "in_review", handler: { type: "agent", id: "agent-1" }, instructions: "" },
      { status_key: "done", handler: { type: "none" }, instructions: "" },
    ],
    project_ids: [],
    created_at: "",
    updated_at: "",
  };

  function rows(): (string | undefined)[] {
    return Array.from(document.querySelectorAll("button[data-picker-item]")).map((el) => el.textContent?.trim());
  }

  it("offers only the Default workflow's statuses, headed as such, without handoff hints", () => {
    catalogEntries = [TODO, IN_REVIEW, QA, MERGED, DONE];
    workspaceDefault = DEFAULT;
    renderWithI18n(
      <StatusPicker status="todo" projectId={null} onUpdate={() => {}} open onOpenChange={() => {}} />,
    );
    expect(rows()).toEqual(["Todo", "In Review", "Done"]);
    expect(screen.getByText("Default statuses · no project")).toBeTruthy();
    expect(screen.queryByText(/Hands off to/)).toBeNull();
  });

  it("keeps a current status Default leaves out, checked, in its category's place", () => {
    catalogEntries = [TODO, IN_REVIEW, QA, MERGED, DONE];
    workspaceDefault = DEFAULT;
    renderWithI18n(
      <StatusPicker status="merged" projectId={null} onUpdate={() => {}} open onOpenChange={() => {}} />,
    );
    expect(rows()).toEqual(["Todo", "In Review", "Merged", "Done"]);
    // The check mark is the last svg in a row; it is hidden on the others.
    const checkOf = (label: string) => Array.from(optionRow(label).querySelectorAll("svg")).at(-1);
    expect(checkOf("Merged")?.getAttribute("class")).not.toContain("invisible");
    expect(checkOf("Todo")?.getAttribute("class")).toContain("invisible");
  });

  it("still reaches every status through search", () => {
    catalogEntries = [TODO, IN_REVIEW, QA, MERGED, DONE];
    workspaceDefault = DEFAULT;
    const onUpdate = vi.fn();
    renderWithI18n(
      <StatusPicker status="todo" projectId={null} onUpdate={onUpdate} open onOpenChange={() => {}} />,
    );
    fireEvent.change(screen.getByPlaceholderText("Search status..."), { target: { value: "merg" } });
    expect(rows()).toEqual(["Merged"]);
    fireEvent.click(optionRow("Merged"));
    expect(onUpdate).toHaveBeenCalledWith({ status: "merged" });
  });

  it("offers the whole catalog when the surface spans projects", () => {
    catalogEntries = [TODO, IN_REVIEW, QA, MERGED, DONE];
    workspaceDefault = DEFAULT;
    renderWithI18n(<StatusPicker status="todo" onUpdate={() => {}} open onOpenChange={() => {}} />);
    // Cancelled is the built-in stand-in for a category the fixture omits.
    expect(rows()).toEqual(["Todo", "In Review", "QA", "Merged", "Done", "Cancelled"]);
  });

  it("offers the whole catalog while Default is unknown", () => {
    catalogEntries = [TODO, IN_REVIEW, QA, MERGED, DONE];
    renderWithI18n(
      <StatusPicker status="todo" projectId={null} onUpdate={() => {}} open onOpenChange={() => {}} />,
    );
    // Cancelled is the built-in stand-in for a category the fixture omits.
    expect(rows()).toEqual(["Todo", "In Review", "QA", "Merged", "Done", "Cancelled"]);
  });
});
