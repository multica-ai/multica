import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../../test/i18n";
import { LabelPicker } from "./label-picker";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ settings: () => "/settings" }) }));
vi.mock("../../../navigation", () => ({ useNavigation: () => ({ push: vi.fn() }) }));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: [{ id: "bug", name: "Bug", color: "#ff0000" }] }) }));
vi.mock("@multica/core/labels", () => ({
  labelListOptions: () => ({}),
  issueLabelsOptions: () => ({}),
  useAttachLabel: () => ({ mutate: vi.fn() }),
  useDetachLabel: () => ({ mutate: vi.fn() }),
  useCreateLabel: () => ({ mutate: vi.fn() }),
}));
afterEach(() => vi.restoreAllMocks());

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
