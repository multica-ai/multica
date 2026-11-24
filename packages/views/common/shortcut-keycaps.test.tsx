import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { createShortcutChord } from "@multica/core/shortcuts";
import { ShortcutKeycaps } from "./shortcut-keycaps";

describe("ShortcutKeycaps", () => {
  it("renders a unified optical pill by default for macOS Command+Enter", () => {
    const { container } = render(
      <ShortcutKeycaps
        shortcut={createShortcutChord("Enter", { primary: true })}
        platform="macos"
      />,
    );

    expect(screen.getByRole("img", { name: "⌘↵" })).toBeInTheDocument();
    expect(container.querySelectorAll("[data-slot='kbd']")).toHaveLength(1);
    expect(container.querySelector("[data-kbd-glyph='command']")).toBeInTheDocument();
    expect(container.querySelector("[data-kbd-glyph='enter']")).toBeInTheDocument();
    expect(container.querySelector("[title='Enter']")?.textContent).toBe("");
  });

  it("renders separate keycaps when layout is split", () => {
    const { container } = render(
      <ShortcutKeycaps
        shortcut={createShortcutChord("J", { primary: true, shift: true })}
        platform="macos"
        layout="split"
      />,
    );

    expect(screen.getByRole("img", { name: "⇧⌘J" })).toBeInTheDocument();
    const keycaps = container.querySelectorAll("[data-slot='kbd']");
    expect(keycaps).toHaveLength(3);
    expect(Array.from(keycaps).map((el) => el.getAttribute("data-shortcut-key"))).toEqual([
      "shift",
      "primary",
      "key-J",
    ]);
  });

  it("orders macOS modifiers in canonical Control-Option-Shift-Command order", () => {
    const { container } = render(
      <ShortcutKeycaps
        shortcut={createShortcutChord("S", {
          primary: true,
          control: true,
          alt: true,
          shift: true,
        })}
        platform="macos"
      />,
    );

    expect(screen.getByRole("img", { name: "⌃⌥⇧⌘S" })).toBeInTheDocument();
    const keys = Array.from(
      container.querySelectorAll("[data-shortcut-key]"),
    ).map((el) => el.getAttribute("data-shortcut-key"));
    expect(keys).toEqual(["control", "alt", "shift", "primary", "key-S"]);
    expect(container.querySelector("[data-kbd-glyph='control']")).toBeInTheDocument();
    expect(container.querySelector("[data-kbd-glyph='option']")).toBeInTheDocument();
    expect(container.querySelector("[data-kbd-glyph='shift']")).toBeInTheDocument();
    expect(container.querySelector("[data-kbd-glyph='command']")).toBeInTheDocument();
  });

  it("keeps platform-specific modifier labels while using optical glyphs for action keys", () => {
    const { container } = render(
      <ShortcutKeycaps
        shortcut={createShortcutChord("Enter", { primary: true })}
        platform="windows"
      />,
    );

    expect(screen.getByRole("img", { name: "Ctrl+Enter" })).toBeInTheDocument();
    expect(container.querySelector("[title='Ctrl']")).toHaveTextContent("Ctrl");
    expect(container.querySelector("[data-kbd-glyph='enter']")).toBeInTheDocument();
    expect(container.querySelector("[data-kbd-glyph='command']")).not.toBeInTheDocument();
  });

  it("distinguishes Backspace from forward Delete and renders Space as text", () => {
    const { container: backspaceContainer } = render(
      <ShortcutKeycaps shortcut={createShortcutChord("Backspace")} platform="macos" />,
    );
    expect(backspaceContainer.querySelector("[data-kbd-glyph='backspace']")).toBeInTheDocument();

    const { container: deleteContainer } = render(
      <ShortcutKeycaps shortcut={createShortcutChord("Delete")} platform="macos" />,
    );
    expect(deleteContainer.querySelector("[data-kbd-glyph='forwardDelete']")).toBeInTheDocument();

    const { container: spaceContainer } = render(
      <ShortcutKeycaps shortcut={createShortcutChord("Space", { alt: true })} platform="macos" />,
    );
    expect(spaceContainer.querySelector("[title='Space']")).toHaveTextContent("Space");
    expect(spaceContainer.querySelector("[data-kbd-glyph='space']")).not.toBeInTheDocument();
  });

  it("falls back to compact text when there is no semantic glyph", () => {
    const { container } = render(
      <ShortcutKeycaps shortcut={createShortcutChord("Escape")} platform="linux" />,
    );

    expect(screen.getByRole("img", { name: "Esc" })).toBeInTheDocument();
    expect(container.querySelector("[title='Esc']")).toHaveTextContent("Esc");
  });
});
