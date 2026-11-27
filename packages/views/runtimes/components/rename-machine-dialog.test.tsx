// @vitest-environment jsdom

import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { RenameMachineDialog } from "./rename-machine-dialog";

const mutate = vi.fn();

vi.mock("@multica/core/runtimes/mutations", () => ({
  useUpdateRuntime: () => ({ mutate, isPending: false }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

function renderDialog() {
  renderWithI18n(
    <RenameMachineDialog
      open
      onOpenChange={vi.fn()}
      wsId="ws-1"
      runtimeId="rt-1"
      currentName=""
    />,
  );
  return screen.getByPlaceholderText("e.g. Work MacBook") as HTMLInputElement;
}

describe("RenameMachineDialog IME composition", () => {
  beforeEach(() => {
    mutate.mockReset();
  });

  it.each([
    ["standard composition signal", { isComposing: true, keyCode: 13 }],
    ["Safari composition signal", { isComposing: false, keyCode: 229 }],
  ])("does not save with a composing Enter (%s)", (_name, eventInit) => {
    const input = renderDialog();
    fireEvent.change(input, { target: { value: "xiangmu" } });
    fireEvent.keyDown(input, { key: "Enter", ...eventInit });
    expect(mutate).not.toHaveBeenCalled();
    expect(input).toHaveValue("xiangmu");
  });

  it("saves once after composition ends", () => {
    const input = renderDialog();
    fireEvent.change(input, { target: { value: "项目" } });
    fireEvent.keyDown(input, { key: "Enter", isComposing: false, keyCode: 13 });
    expect(mutate).toHaveBeenCalledTimes(1);
    expect(mutate.mock.calls[0]![0]).toEqual({
      runtimeId: "rt-1",
      patch: { custom_name: "项目", apply_to_machine: true },
    });
  });
});
