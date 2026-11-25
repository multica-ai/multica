import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { DEFAULT_APPEARANCE, useAppearanceStore } from "@multica/core/appearance";
import { CollapsibleSlot } from "./collapsible-slot";

afterEach(() => {
  cleanup();
  useAppearanceStore.setState({ ...DEFAULT_APPEARANCE });
});

describe("CollapsibleSlot", () => {
  it("marks the exiting slot inert and pointer-events-none until it unmounts", async () => {
    const { container, rerender } = render(
      <CollapsibleSlot slotKey="chat-status-banner" visible>
        <button type="button">Retry</button>
      </CollapsibleSlot>,
    );

    const slot = container.querySelector('[data-slot="chat-status-banner"]')!;
    expect(slot).not.toHaveAttribute("inert");
    expect(slot.className).not.toContain("pointer-events-none");
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();

    rerender(
      <CollapsibleSlot slotKey="chat-status-banner" visible={false}>
        <button type="button">Retry</button>
      </CollapsibleSlot>,
    );

    const exiting = container.querySelector('[data-slot="chat-status-banner"]');
    if (exiting) {
      expect(exiting).toHaveAttribute("inert");
      expect(exiting.className).toContain("pointer-events-none");
    }

    await waitFor(() => {
      expect(container.querySelector('[data-slot="chat-status-banner"]')).toBeNull();
    });
  });

  it("mounts and unmounts in the same commit under reduced motion", () => {
    useAppearanceStore.getState().set({ reduceMotion: true });

    const { container, rerender } = render(
      <CollapsibleSlot slotKey="chat-status-banner" visible={false}>
        <span>Banner</span>
      </CollapsibleSlot>,
    );
    expect(container.querySelector('[data-slot="chat-status-banner"]')).toBeNull();

    rerender(
      <CollapsibleSlot slotKey="chat-status-banner" visible>
        <span>Banner</span>
      </CollapsibleSlot>,
    );
    const slot = container.querySelector<HTMLElement>('[data-slot="chat-status-banner"]')!;
    expect(slot).toBeInTheDocument();
    expect(slot.style.height).toBe("");
    expect(slot.style.opacity).toBe("");

    rerender(
      <CollapsibleSlot slotKey="chat-status-banner" visible={false}>
        <span>Banner</span>
      </CollapsibleSlot>,
    );
    expect(container.querySelector('[data-slot="chat-status-banner"]')).toBeNull();
  });
});
