// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_EDITOR_TOOLBAR_ORDER,
  EDITOR_TOOLBAR_ORDER_KEY,
} from "./editor-toolbar-preferences";
import { EditorToolbarSettings } from "./editor-toolbar-settings";

const preferences = vi.hoisted(() => ({
  current: { cerebro_editor_toolbar_order: ["bold", "italic"] } as Record<
    string,
    unknown
  >,
}));
const updateMyPreferences = vi.hoisted(() =>
  vi.fn(async (patch: Record<string, unknown>) => ({
    id: "user-1",
    preferences: { ...preferences.current, ...patch },
  })),
);
const setUser = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { updateMyPreferences },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({
      user: { id: "user-1", preferences: preferences.current },
      setUser,
    }),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  preferences.current = { cerebro_editor_toolbar_order: ["bold", "italic"] };
});

/** Drives the native drag-and-drop sequence the list listens for. */
function dragRowOnto(from: HTMLElement, to: HTMLElement) {
  const dataTransfer = {
    data: {} as Record<string, string>,
    effectAllowed: "",
    dropEffect: "",
    setData(key: string, value: string) {
      this.data[key] = value;
    },
    getData(key: string) {
      return this.data[key] ?? "";
    },
  };
  fireEvent.dragStart(from, { dataTransfer });
  fireEvent.dragOver(to, { dataTransfer });
  fireEvent.drop(to, { dataTransfer });
}

describe("EditorToolbarSettings", () => {
  it("reorders by dragging a row and saves once when the row is dropped", async () => {
    render(<EditorToolbarSettings />);

    const boldRow = screen.getByTestId("toolbar-setting-bold");
    const italicRow = screen.getByTestId("toolbar-setting-italic");
    expect(boldRow).toHaveAttribute("draggable", "true");

    dragRowOnto(italicRow, boldRow);

    await waitFor(() => expect(updateMyPreferences).toHaveBeenCalledTimes(1));
    const saved = updateMyPreferences.mock.calls[0]?.[0] as Record<
      string,
      { order: string[] }
    >;
    expect(saved[EDITOR_TOOLBAR_ORDER_KEY]?.order.slice(0, 2)).toEqual([
      "italic",
      "bold",
    ]);
    expect(setUser).toHaveBeenCalledTimes(1);
  });

  it("no longer offers the one-step arrow buttons", () => {
    render(<EditorToolbarSettings />);

    expect(
      screen.queryByRole("button", { name: "Move Italic up" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Move Bold down" }),
    ).not.toBeInTheDocument();
  });

  it("shows each control's icon so the list can be matched to the row", () => {
    render(<EditorToolbarSettings />);

    const boldRow = screen.getByTestId("toolbar-setting-bold");
    expect(boldRow.querySelector("svg.lucide-bold")).toBeInTheDocument();
  });

  it("previews the user's own toolbar above the list", () => {
    render(<EditorToolbarSettings />);

    const preview = screen.getByTestId("toolbar-settings-preview");
    expect(preview).toBeVisible();
    expect(preview.querySelector("svg.lucide-bold")).toBeInTheDocument();
  });

  it("hides an action into the overflow menu instead of removing it", async () => {
    render(<EditorToolbarSettings />);

    fireEvent.click(screen.getByRole("button", { name: "Hide Bold" }));

    await waitFor(() => expect(updateMyPreferences).toHaveBeenCalledTimes(1));
    const saved = updateMyPreferences.mock.calls[0]?.[0] as Record<
      string,
      { hidden: string[] }
    >;
    expect(saved[EDITOR_TOOLBAR_ORDER_KEY]?.hidden).toContain("bold");
  });

  it("shows a hidden action as hidden and lets it be shown again", async () => {
    preferences.current = {
      cerebro_editor_toolbar_order: { order: ["bold", "italic"], hidden: ["bold"] },
    };

    render(<EditorToolbarSettings />);

    const boldRow = screen.getByTestId("toolbar-setting-bold");
    expect(boldRow).toHaveTextContent(/In the ⋯ menu/i);

    fireEvent.click(screen.getByRole("button", { name: "Show Bold" }));

    await waitFor(() => expect(updateMyPreferences).toHaveBeenCalledTimes(1));
    const saved = updateMyPreferences.mock.calls[0]?.[0] as Record<
      string,
      { hidden: string[] }
    >;
    expect(saved[EDITOR_TOOLBAR_ORDER_KEY]?.hidden).not.toContain("bold");
  });

  it("resets to the default order and clears everything hidden", async () => {
    preferences.current = {
      cerebro_editor_toolbar_order: { order: ["italic", "bold"], hidden: ["bold"] },
    };

    render(<EditorToolbarSettings />);
    fireEvent.click(screen.getByRole("button", { name: /Reset/ }));

    await waitFor(() => expect(updateMyPreferences).toHaveBeenCalledTimes(1));
    const saved = updateMyPreferences.mock.calls[0]?.[0] as Record<
      string,
      { order: string[]; hidden: string[] }
    >;
    expect(saved[EDITOR_TOOLBAR_ORDER_KEY]?.hidden).toEqual([]);
    expect(saved[EDITOR_TOOLBAR_ORDER_KEY]?.order).toEqual([
      ...DEFAULT_EDITOR_TOOLBAR_ORDER,
    ]);
  });
});
