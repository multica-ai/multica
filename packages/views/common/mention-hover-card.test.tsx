// @vitest-environment jsdom

import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { MentionHoverCard } from "@multica/ui/components/common/mention-hover-card";

// MentionHoverCard opens its popup on hover OR keyboard focus (Base UI
// PreviewCard fires on triggerFocus). In jsdom we drive it with focus, then
// assert the portaled popup content.

afterEach(cleanup);

describe("MentionHoverCard", () => {
  it("renders the @all branch with warning colors", async () => {
    const { container } = render(
      <MentionHoverCard type="all" id="all" name="all" initials="A">
        <span>trigger</span>
      </MentionHoverCard>,
    );
    fireEvent.focus(
      container.querySelector('[data-slot="hover-card-trigger"]')!,
    );

    // @all branch copy — not the non-all "name" label.
    await waitFor(() => {
      expect(document.body.textContent).toContain("All members");
    });
    // Warning-tinted avatar tile + icon (recolored from bg-primary/10).
    expect(document.body.querySelector('[class*="bg-warning"]')).not.toBeNull();
    expect(document.body.querySelector('[class*="text-warning"]')).not.toBeNull();
  });

  it("passes isSquad to the avatar for squad type (square tile)", async () => {
    const { container } = render(
      <MentionHoverCard type="squad" id="s1" name="设计组" initials="设">
        <span>trigger</span>
      </MentionHoverCard>,
    );
    fireEvent.focus(
      container.querySelector('[data-slot="hover-card-trigger"]')!,
    );

    await waitFor(() => {
      expect(document.body.textContent).toContain("设计组");
    });
    // Squad avatars are a square tile (rounded-md), not round — the
    // isSquad pass-through added in this change.
    const avatar = document.body.querySelector('[data-slot="avatar"]')!;
    expect(avatar.className).toContain("rounded-md");
    expect(avatar.className).not.toContain("rounded-full");
  });

  it("renders the member branch avatar as round", async () => {
    const { container } = render(
      <MentionHoverCard type="member" id="u1" name="张三" initials="张">
        <span>trigger</span>
      </MentionHoverCard>,
    );
    fireEvent.focus(
      container.querySelector('[data-slot="hover-card-trigger"]')!,
    );

    await waitFor(() => {
      expect(document.body.textContent).toContain("张三");
    });
    const avatar = document.body.querySelector('[data-slot="avatar"]')!;
    expect(avatar.className).toContain("rounded-full");
  });
});
