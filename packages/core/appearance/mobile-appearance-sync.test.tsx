// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
vi.mock("../platform/storage", () => ({
  defaultStorage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
}));

import { MobileAppearanceSync } from "./mobile-appearance-sync";
import { useMobileAppearanceStore } from "./mobile-appearance-store";

const root = () => document.documentElement;

beforeEach(() => {
  useMobileAppearanceStore.setState({ fontSize: "default", contentWidth: "standard" });
  root().removeAttribute("data-mobile-font-size");
  root().removeAttribute("data-mobile-content-width");
});
afterEach(cleanup);

describe("MobileAppearanceSync", () => {
  it("projects the defaults onto the root element", () => {
    render(<MobileAppearanceSync />);
    expect(root().getAttribute("data-mobile-font-size")).toBe("default");
    expect(root().getAttribute("data-mobile-content-width")).toBe("standard");
  });

  it("updates the root attributes as soon as the store changes", () => {
    render(<MobileAppearanceSync />);
    act(() => {
      useMobileAppearanceStore.getState().setFontSize("large");
      useMobileAppearanceStore.getState().setContentWidth("full");
    });
    expect(root().getAttribute("data-mobile-font-size")).toBe("large");
    expect(root().getAttribute("data-mobile-content-width")).toBe("full");
    act(() => useMobileAppearanceStore.getState().setFontSize("small"));
    expect(root().getAttribute("data-mobile-font-size")).toBe("small");
  });

  it("renders nothing", () => {
    const { container } = render(<MobileAppearanceSync />);
    expect(container.innerHTML).toBe("");
  });
});
