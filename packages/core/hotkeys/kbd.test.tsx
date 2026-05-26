/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";

// The Kbd component lives in packages/ui but has zero business-logic deps —
// just string splitting + class names. We test it here because packages/core
// already has the jsdom + @testing-library test infra.

// We need to test platform-specific rendering. Since Kbd reads navigator at
// module load time, we stub it before importing.

describe("Kbd component", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.resetModules();
  });

  it("renders Mod+K as ⌘ K on Mac", async () => {
    vi.stubGlobal("navigator", { platform: "MacIntel" });
    const { Kbd } = await import("@multica/ui/components/ui/kbd");
    const { container } = render(<Kbd keys="Mod+K" />);
    const kbds = container.querySelectorAll("kbd");
    expect(kbds).toHaveLength(2);
    expect(kbds[0]!.textContent).toBe("⌘");
    expect(kbds[1]!.textContent).toBe("K");
  });

  it("renders Mod+K as Ctrl K on Windows", async () => {
    vi.stubGlobal("navigator", { platform: "Win32" });
    const { Kbd } = await import("@multica/ui/components/ui/kbd");
    const { container } = render(<Kbd keys="Mod+K" />);
    const kbds = container.querySelectorAll("kbd");
    expect(kbds).toHaveLength(2);
    expect(kbds[0]!.textContent).toBe("Ctrl");
    expect(kbds[1]!.textContent).toBe("K");
  });

  it("renders Shift+Enter with correct symbols on Mac", async () => {
    vi.stubGlobal("navigator", { platform: "MacIntel" });
    const { Kbd } = await import("@multica/ui/components/ui/kbd");
    const { container } = render(<Kbd keys="Shift+Enter" />);
    const kbds = container.querySelectorAll("kbd");
    expect(kbds).toHaveLength(2);
    expect(kbds[0]!.textContent).toBe("⇧");
    expect(kbds[1]!.textContent).toBe("↵");
  });

  it("renders a single key without splitting", async () => {
    vi.stubGlobal("navigator", { platform: "Win32" });
    const { Kbd } = await import("@multica/ui/components/ui/kbd");
    const { container } = render(<Kbd keys="Escape" />);
    const kbds = container.querySelectorAll("kbd");
    expect(kbds).toHaveLength(1);
    expect(kbds[0]!.textContent).toBe("Esc");
  });
});
