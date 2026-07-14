import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";

// Tiptap's NodeView primitives need a full editor to instantiate. Stub
// NodeViewWrapper so MentionView renders as a plain React component and the
// test can assert the chip DOM shape.
vi.mock("@tiptap/react", () => {
  const NodeViewWrapper = ({ children, ...rest }: any) => (
    <span data-testid="nvw" {...rest}>
      {children}
    </span>
  );
  return { NodeViewWrapper };
});

import { MentionView } from "./mention-view";

function renderMention(type: string, label: string, id = "x") {
  const props = {
    node: { attrs: { type, id, label } },
  } as unknown as Parameters<typeof MentionView>[0];
  return render(<MentionView {...props} />);
}

afterEach(cleanup);

describe("MentionView — actor mentions render as avatar chips", () => {
  it("renders a member mention as an ActorMentionChip, not a plain .mention span", () => {
    const { container } = renderMention("member", "张三");
    const chip = container.querySelector(".actor-mention-chip");
    expect(chip).not.toBeNull();
    // The legacy plain-text mention span is gone for actor mentions.
    expect(container.querySelector("span.mention")).toBeNull();
    expect(chip!.textContent).toContain("@张三");
  });

  it("renders an agent mention with brand tint", () => {
    const chip = renderMention("agent", "ReviewerBot").container.querySelector(
      ".actor-mention-chip",
    )!;
    expect(chip.className).toContain("bg-brand/10");
  });

  it("renders a squad mention with info tint", () => {
    const chip = renderMention("squad", "设计组").container.querySelector(
      ".actor-mention-chip",
    )!;
    expect(chip.className).toContain("bg-info/10");
  });

  it("renders an @all mention with warning tint and the all-members aria-label", () => {
    const chip = renderMention("all", "all").container.querySelector(
      ".actor-mention-chip",
    )!;
    expect(chip.className).toContain("bg-warning/10");
    expect(chip.getAttribute("aria-label")).toBe(
      "Mention: all workspace members",
    );
  });

  it("makes the editor chip keyboard-focusable with a focus-visible ring", () => {
    const chip = renderMention("member", "张三").container.querySelector(
      ".actor-mention-chip",
    )!;
    expect(chip.getAttribute("tabindex")).toBe("0");
    expect(chip.className).toContain("focus-visible:border-ring");
  });

  it("wraps the chip in a MentionHoverCard trigger", () => {
    const { container } = renderMention("member", "张三");
    const trigger = container.querySelector(
      '[data-slot="hover-card-trigger"]',
    );
    expect(trigger).not.toBeNull();
    expect(trigger!.querySelector(".actor-mention-chip")).not.toBeNull();
  });

  it("layers the per-type hover tint on the chip", () => {
    const memberChip = renderMention("member", "张三").container.querySelector(
      ".actor-mention-chip",
    )!;
    expect(memberChip.className).toContain("hover:bg-accent");

    const agentChip = renderMention("agent", "ReviewerBot").container.querySelector(
      ".actor-mention-chip",
    )!;
    expect(agentChip.className).toContain("hover:bg-brand/15");
  });
});
