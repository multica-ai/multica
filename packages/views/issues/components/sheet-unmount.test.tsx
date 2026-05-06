import { useState } from "react";
import { render, act } from "@testing-library/react";
import { describe, it, expect, beforeEach } from "vitest";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";

/**
 * Repro for JEH-601: iOS swipe-back leaves the page locked.
 *
 * When a Sheet is open and the route changes (e.g. iOS swipe-back), the
 * page that owned the Sheet unmounts. The Sheet's portal lives on
 * document.body via FloatingPortal — if it (or any backdrop / aria-hidden /
 * scroll-lock side effect) survives unmount, the user is left on the next
 * page with stale overlays that swallow scroll + clicks.
 *
 * These tests pin the contract: unmounting the Sheet's parent must leave
 * document.body in the same shape it had before mounting.
 */
function PageWithSheet({ open }: { open: boolean }) {
  const [internalOpen, setOpen] = useState(open);
  return (
    <Sheet open={internalOpen} onOpenChange={setOpen}>
      <SheetContent>
        <div>sheet content</div>
      </SheetContent>
    </Sheet>
  );
}

describe("Sheet unmount cleanup (JEH-601)", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    document.body.removeAttribute("style");
    document.documentElement.removeAttribute("data-base-ui-scroll-locked");
    document.documentElement.removeAttribute("style");
  });

  it("removes its portal from document.body when the parent unmounts", async () => {
    const { unmount } = render(<PageWithSheet open={true} />);
    // Portal lives in body, not under the test container.
    const portalNodes = document.body.querySelectorAll("[data-slot='sheet-content']");
    expect(portalNodes.length).toBeGreaterThan(0);
    await act(async () => {
      unmount();
    });
    // Wait one tick to flush any scheduled cleanups.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    expect(
      document.body.querySelectorAll("[data-slot='sheet-content']").length,
    ).toBe(0);
    expect(
      document.body.querySelectorAll("[data-slot='sheet-overlay']").length,
    ).toBe(0);
  });

  it("clears body inline overflow styles after unmount", async () => {
    const { unmount } = render(<PageWithSheet open={true} />);
    await act(async () => {
      unmount();
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    // Base UI scroll-lock should have restored the original (empty) inline
    // styles. If anything is left, body is in a half-locked state.
    expect(document.body.style.overflowY).toBe("");
    expect(document.body.style.overflowX).toBe("");
    expect(document.body.style.position).toBe("");
    expect(
      document.documentElement.hasAttribute("data-base-ui-scroll-locked"),
    ).toBe(false);
  });

  it("removes aria-hidden / data-base-ui-inert markers after unmount", async () => {
    // Add a sibling that markOthers will hide while the dialog is open.
    const sibling = document.createElement("div");
    sibling.id = "siblings";
    document.body.appendChild(sibling);

    const { unmount } = render(<PageWithSheet open={true} />);
    await act(async () => {
      unmount();
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    expect(sibling.hasAttribute("aria-hidden")).toBe(false);
    expect(sibling.hasAttribute("data-base-ui-inert")).toBe(false);
  });
});
