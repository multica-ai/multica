// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { EDITOR_TOOLBAR_ORDER_KEY } from "./editor-toolbar-preferences";
import { EditorToolbarSettings } from "./editor-toolbar-settings";

const updateMyPreferences = vi.hoisted(() =>
  vi.fn().mockResolvedValue({
    id: "user-1",
    preferences: {
      cerebro_editor_toolbar_order: ["italic", "bold"],
    },
  }),
);
const setUser = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { updateMyPreferences },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({
      user: {
        id: "user-1",
        preferences: {
          [EDITOR_TOOLBAR_ORDER_KEY]: ["bold", "italic"],
        },
      },
      setUser,
    }),
}));

afterEach(() => {
  cleanup();
  updateMyPreferences.mockClear();
  setUser.mockClear();
});

describe("EditorToolbarSettings", () => {
  it("lets the user reorder controls and saves the order to their account", async () => {
    render(<EditorToolbarSettings />);

    const boldRow = screen.getByTestId("toolbar-setting-bold");
    const italicRow = screen.getByTestId("toolbar-setting-italic");
    expect(
      boldRow.compareDocumentPosition(italicRow) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Move Italic up" }),
    );

    await waitFor(() =>
      expect(updateMyPreferences).toHaveBeenCalledWith({
        [EDITOR_TOOLBAR_ORDER_KEY]: [
          "italic",
          "bold",
          "link",
          "heading",
          "highlight",
          "taskList",
          "comment",
          "strike",
          "bulletList",
          "orderedList",
          "blockquote",
          "code",
          "indent",
          "outdent",
        ],
      }),
    );
    expect(setUser).toHaveBeenCalled();
  });
});
