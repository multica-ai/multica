import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enChat from "../../locales/en/chat.json";
import { contextMenuOriginRef } from "@multica/ui/lib/context-menu-origin";
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

  it("keeps editing while its own context menu is used", () => {
    const input = renderInput();
    const menu = document.createElement("div");
    menu.setAttribute("role", "menu");
    const cut = menu.appendChild(document.createElement("div"));
    cut.setAttribute("role", "menuitem");
    document.body.appendChild(menu);
    const release = contextMenuOriginRef(input)(menu);

    fireEvent.pointerDown(cut);
    fireEvent.click(cut);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(input).toBeInTheDocument();

    fireEvent.pointerDown(document.body);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    release?.();
    menu.remove();
  });

  it("submits on a menu it did not open", () => {
    renderInput();
    const menu = document.body.appendChild(document.createElement("div"));
    menu.setAttribute("role", "menu");

    fireEvent.pointerDown(menu);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    menu.remove();
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
