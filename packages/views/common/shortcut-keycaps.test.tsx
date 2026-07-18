import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { createShortcutChord } from "@multica/core/shortcuts";
import { ShortcutKeycaps } from "./shortcut-keycaps";

describe("ShortcutKeycaps", () => {
  it("uses Lucide icons for macOS Command and Enter", () => {
    const { container } = render(
      <ShortcutKeycaps
        shortcut={createShortcutChord("Enter", { primary: true })}
        platform="macos"
      />,
    );

    expect(screen.getByRole("img", { name: "⌘↵" })).toBeInTheDocument();
    expect(container.querySelector(".lucide-command")).toBeInTheDocument();
    expect(container.querySelector(".lucide-corner-down-left")).toBeInTheDocument();
    expect(container.querySelector("[title='Enter']")?.textContent).toBe("");
  });

  it("keeps platform-specific modifier labels while iconizing function keys", () => {
    const { container } = render(
      <ShortcutKeycaps
        shortcut={createShortcutChord("Enter", { primary: true })}
        platform="windows"
      />,
    );

    expect(screen.getByRole("img", { name: "Ctrl+Enter" })).toBeInTheDocument();
    expect(container.querySelector("[title='Ctrl']")).toHaveTextContent("Ctrl");
    expect(container.querySelector(".lucide-corner-down-left")).toBeInTheDocument();
    expect(container.querySelector(".lucide-command")).not.toBeInTheDocument();
  });

  it("uses the canonical macOS Control glyph instead of an unrelated icon", () => {
    const { container } = render(
      <ShortcutKeycaps
        shortcut={createShortcutChord("F", { control: true })}
        platform="macos"
      />,
    );

    expect(screen.getByRole("img", { name: "⌃F" })).toBeInTheDocument();
    expect(container.querySelector("[title='Control']")).toHaveTextContent("⌃");
    expect(container.querySelector(".lucide-command")).not.toBeInTheDocument();
  });

  it("falls back to compact text when Lucide has no exact semantic icon", () => {
    const { container } = render(
      <ShortcutKeycaps shortcut={createShortcutChord("Escape")} platform="linux" />,
    );

    expect(screen.getByRole("img", { name: "Esc" })).toBeInTheDocument();
    expect(container.querySelector("[title='Esc']")).toHaveTextContent("Esc");
  });
});
