// @vitest-environment jsdom

import { useState } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { FindBar } from "./find-bar";

function renderFindBar() {
  const goNext = vi.fn();
  const goPrev = vi.fn();
  const closeFind = vi.fn();

  function Harness() {
    const [query, setQuery] = useState("");
    return (
      <FindBar
        find={{
          open: true,
          query,
          matchCount: 2,
          activeIndex: 0,
          supported: false,
          inputRef: { current: null },
          setQuery,
          openFind: vi.fn(),
          closeFind,
          goNext,
          goPrev,
        }}
      />
    );
  }

  renderWithI18n(<Harness />);
  return { goNext, goPrev, closeFind };
}

describe("FindBar", () => {
  it.each([
    ["standard Enter", { key: "Enter", isComposing: true, keyCode: 13 }],
    ["Safari Enter", { key: "Enter", isComposing: false, keyCode: 229 }],
    ["standard Shift+Enter", { key: "Enter", shiftKey: true, isComposing: true, keyCode: 13 }],
    ["Safari Shift+Enter", { key: "Enter", shiftKey: true, isComposing: false, keyCode: 229 }],
    ["standard Escape", { key: "Escape", isComposing: true, keyCode: 27 }],
    ["Safari Escape", { key: "Escape", isComposing: false, keyCode: 229 }],
  ])("leaves %s to the IME without navigating or closing", (_label, event) => {
    const { goNext, goPrev, closeFind } = renderFindBar();
    const input = screen.getByRole("textbox");
    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "zhong" } });

    expect(fireEvent.keyDown(input, event)).toBe(true);
    expect(goNext).not.toHaveBeenCalled();
    expect(goPrev).not.toHaveBeenCalled();
    expect(closeFind).not.toHaveBeenCalled();
    expect(input).toHaveValue("zhong");
    expect(input).toHaveFocus();

    fireEvent.compositionEnd(input, { data: "中" });
    fireEvent.change(input, { target: { value: "中" } });
    expect(input).toHaveValue("中");
    expect(fireEvent.keyDown(input, { key: "Enter" })).toBe(false);
    expect(goNext).toHaveBeenCalledExactlyOnceWith();
  });

  it("keeps ordinary Enter, Shift+Enter and Escape actions", () => {
    const { goNext, goPrev, closeFind } = renderFindBar();
    const input = screen.getByRole("textbox");

    expect(fireEvent.keyDown(input, { key: "Enter" })).toBe(false);
    expect(goNext).toHaveBeenCalledExactlyOnceWith();
    expect(goPrev).not.toHaveBeenCalled();
    expect(closeFind).not.toHaveBeenCalled();

    expect(fireEvent.keyDown(input, { key: "Enter", shiftKey: true })).toBe(false);
    expect(goPrev).toHaveBeenCalledExactlyOnceWith();

    expect(fireEvent.keyDown(input, { key: "Escape" })).toBe(false);
    expect(closeFind).toHaveBeenCalledExactlyOnceWith();
  });
});
