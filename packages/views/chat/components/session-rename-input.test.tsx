import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enChat from "../../locales/en/chat.json";
import { SessionRenameInput } from "./session-rename-input";

const TEST_RESOURCES = { en: { chat: enChat } };
const RENAME_LABEL = enChat.session_history.row_rename_aria;
const onSubmit = vi.fn();
const onCancel = vi.fn();

function renderInput(): HTMLInputElement {
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <SessionRenameInput
        initialValue="Original title"
        onSubmit={onSubmit}
        onCancel={onCancel}
      />
    </I18nProvider>,
  );
  return screen.getByRole("textbox", { name: RENAME_LABEL });
}

describe("SessionRenameInput", () => {
  beforeEach(() => {
    onSubmit.mockReset();
    onCancel.mockReset();
  });

  it.each([
    ["standard composition signal", { isComposing: true, keyCode: 13 }],
    ["Safari composition signal", { isComposing: false, keyCode: 229 }],
  ])("does not submit Enter with the %s", (_name, eventInit) => {
    const input = renderInput();
    fireEvent.change(input, { target: { value: "yanjiu" } });

    fireEvent.keyDown(input, { key: "Enter", ...eventInit });

    expect(onSubmit).not.toHaveBeenCalled();
    expect(input).toHaveValue("yanjiu");
  });

  it("submits the latest value on outside pointerdown", () => {
    const input = renderInput();
    fireEvent.change(input, { target: { value: "研究" } });
    fireEvent.pointerDown(document.body);

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledWith("研究");
  });

  it.each([
    ["standard composition signal", { isComposing: true, keyCode: 27 }],
    ["Safari composition signal", { isComposing: false, keyCode: 229 }],
  ])("does not cancel Escape with the %s", (_name, eventInit) => {
    const input = renderInput();
    fireEvent.change(input, { target: { value: "yanjiu" } });

    fireEvent.keyDown(input, { key: "Escape", ...eventInit });

    expect(onSubmit).not.toHaveBeenCalled();
    expect(onCancel).not.toHaveBeenCalled();
    expect(input).toHaveValue("yanjiu");
  });

  it("keeps normal Enter and Escape behavior", () => {
    const input = renderInput();
    fireEvent.change(input, { target: { value: "Renamed chat" } });

    fireEvent.keyDown(input, { key: "Enter", isComposing: false, keyCode: 13 });
    fireEvent.keyDown(input, { key: "Escape" });

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledWith("Renamed chat");
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});
