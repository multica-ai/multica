import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../../test/i18n";
import { LabelPicker } from "./label-picker";

const spies = vi.hoisted(() => ({
  push: vi.fn(),
  listTypes: [] as string[],
  attachIssue: vi.fn(),
  attachSkill: vi.fn(),
  create: vi.fn(),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ settings: () => "/settings" }) }));
vi.mock("../../../navigation", () => ({ useNavigation: () => ({ push: spies.push }) }));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: [{ id: "bug", name: "Bug", color: "#ff0000" }] }) }));
vi.mock("@multica/core/labels", () => ({
  labelListOptions: (_wsId: string, resourceType: string) => {
    spies.listTypes.push(resourceType);
    return {};
  },
  issueLabelsOptions: () => ({}),
  resourceLabelsOptions: () => ({}),
  useAttachLabel: () => ({ mutate: spies.attachIssue }),
  useDetachLabel: () => ({ mutate: vi.fn() }),
  useAttachResourceLabel: () => ({ mutate: spies.attachSkill }),
  useDetachResourceLabel: () => ({ mutate: vi.fn() }),
  useCreateLabel: () => ({ mutate: spies.create, isPending: false }),
}));
afterEach(() => {
  vi.restoreAllMocks();
  spies.push.mockReset();
  spies.attachIssue.mockReset();
  spies.attachSkill.mockReset();
  spies.create.mockReset();
  spies.listTypes = [];
});

describe("LabelPicker trigger semantics", () => {
  it("uses non-native semantics for the compound trigger and opens from the keyboard without warnings", async () => {
    const error = vi.spyOn(console, "error");
    const user = userEvent.setup();
    renderWithI18n(<LabelPicker selectedIds={["bug"]} onSelectedIdsChange={() => {}} />);
    const trigger = screen.getByRole("button", { name: /^Bug/ }).closest('[data-slot="popover-trigger"]')!;
    expect(trigger.tagName).toBe("DIV");
    expect(trigger).not.toHaveAttribute("type");
    (trigger as HTMLElement).focus();
    await user.keyboard("{Enter}");
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(error).not.toHaveBeenCalled();
  });
  it("keeps chip removal independent from opening the picker", async () => {
    const change = vi.fn();
    renderWithI18n(<LabelPicker selectedIds={["bug"]} onSelectedIdsChange={change} />);
    screen.getByRole("button", { name: "Remove label Bug" }).focus();
    await userEvent.keyboard("{Enter}");
    expect(change).toHaveBeenCalledWith([]);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("retains native buttons for empty and custom triggers across selection changes", () => {
    const error = vi.spyOn(console, "error");
    const { rerender } = renderWithI18n(<LabelPicker selectedIds={[]} />);
    expect(document.querySelector('[data-slot="popover-trigger"]')?.tagName).toBe("BUTTON");
    rerender(<LabelPicker selectedIds={["bug"]} />);
    expect(document.querySelector('[data-slot="popover-trigger"]')?.tagName).toBe("DIV");
    rerender(<LabelPicker selectedIds={["bug"]} triggerRender={<button type="button" />} />);
    expect(document.querySelector('[data-slot="popover-trigger"]')?.tagName).toBe("BUTTON");
    expect(screen.queryByRole("button", { name: "Remove label Bug" })).not.toBeInTheDocument();
    expect(error).not.toHaveBeenCalled();
  });
});

describe("LabelPicker on a skill", () => {
  it("creates a skill label inline and attaches it to the skill", async () => {
    const user = userEvent.setup();
    spies.create.mockImplementation((_data, opts) => opts.onSuccess({ id: "new", name: "Docs" }));
    renderWithI18n(<LabelPicker skillId="skill-1" open onOpenChange={() => {}} />);

    expect(spies.listTypes).toContain("skill");
    expect(spies.listTypes).not.toContain("issue");
    await user.type(screen.getByPlaceholderText("Find or create a label…"), "Docs{Enter}");

    expect(spies.create).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Docs", resource_type: "skill" }),
      expect.anything(),
    );
    expect(spies.attachSkill).toHaveBeenCalledWith("new");
    expect(spies.attachIssue).not.toHaveBeenCalled();
  });

  it("manages labels on the skill tab of Settings → Labels", async () => {
    const user = userEvent.setup();
    renderWithI18n(<LabelPicker skillId="skill-1" open onOpenChange={() => {}} />);

    await user.click(screen.getByRole("button", { name: "Manage labels…" }));
    expect(spies.push).toHaveBeenCalledWith("/settings?tab=labels&scope=skill");
  });
});
