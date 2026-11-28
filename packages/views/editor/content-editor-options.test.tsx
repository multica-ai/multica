import { describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { Editor } from "@tiptap/core";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider } from "../navigation/context";
import type { NavigationAdapter } from "../navigation/types";
import { ContentEditor } from "./content-editor";

function adapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => `https://app.example${path}`,
  };
}

// MUL-9751: Tiptap calls `setOptions` after any render whose options differ
// from the editor's, and that re-runs every plugin view — for a live doc, a
// diff of the whole document against the shared one. A host re-render that
// changes nothing must not reach it.
describe("ContentEditor options", () => {
  it.each([
    { mode: "without commit links", repository: undefined },
    { mode: "with commit links", repository: "https://github.com/example/project" },
  ])("leaves the editor alone on an unchanged host re-render $mode", async ({ repository }) => {
    const queryClient = new QueryClient();
    const setOptions = vi.spyOn(Editor.prototype, "setOptions");
    const onReady = vi.fn();
    const ui = (className: string) => (
      <NavigationProvider value={adapter()}>
        <QueryClientProvider client={queryClient}>
          <ContentEditor
            defaultValue="See commit 716c854"
            commitRepository={repository}
            className={className}
            showBubbleMenu={false}
            // A fresh callback each render, as hosts pass them.
            onReady={() => onReady()}
          />
        </QueryClientProvider>
      </NavigationProvider>
    );
    const { rerender } = renderWithI18n(ui("a"));
    await waitFor(() => expect(onReady).toHaveBeenCalled());
    setOptions.mockClear();

    rerender(ui("a"));
    rerender(ui("a"));
    expect(setOptions).not.toHaveBeenCalled();
    if (repository) {
      expect(screen.getByRole("link", { name: "716c854" })).toHaveAttribute(
        "href",
        `${repository}/commit/716c854`,
      );
    }

    // A prop the editor does read still reaches it.
    rerender(ui("b"));
    expect(setOptions).toHaveBeenCalledTimes(1);
    setOptions.mockRestore();
  });
});
