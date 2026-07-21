// @vitest-environment jsdom

import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import {
  ActorMentionChip,
  type ActorMentionType,
} from "@multica/ui/components/common/actor-mention-chip";

// ActorMentionChip delegates avatar rendering to the real ActorAvatar (no
// mock) so the type -> shape wiring (isAgent / isSquad) is exercised
// end-to-end. avatarUrl stays null, matching the readonly data path (KTD2:
// the chip makes no data request).

afterEach(cleanup);

function pillFor(type: ActorMentionType, label = "张三") {
  const { container } = render(
    <ActorMentionChip type={type} label={label} initials={label.charAt(0)} />,
  );
  return container.querySelector(".actor-mention-chip") as HTMLElement;
}

describe("ActorMentionChip", () => {
  it("renders a member chip with muted styling and the @label", () => {
    const { container, getByText } = render(
      <ActorMentionChip type="member" label="张三" initials="张" />,
    );

    const pill = container.querySelector(".actor-mention-chip")!;
    expect(pill.className).toContain("bg-muted");
    expect(pill.className).toContain("border-border");
    // Member avatars are round (not square).
    const avatar = container.querySelector('[data-slot="avatar"]')!;
    expect(avatar.className).toContain("rounded-full");
    expect(avatar.className).not.toContain("rounded-md");
    expect(getByText("@张三")).toBeInTheDocument();
  });

  it("renders an agent chip with brand tint", () => {
    const pill = pillFor("agent", "ReviewerBot");
    expect(pill.className).toContain("bg-brand/10");
    expect(pill.className).toContain("border-brand/20");
  });

  it("renders a squad chip with info tint and a round avatar", () => {
    const { container } = render(
      <ActorMentionChip type="squad" label="设计组" initials="设" />,
    );
    const pill = container.querySelector(".actor-mention-chip")!;
    expect(pill.className).toContain("bg-info/10");
    expect(pill.className).toContain("border-info/20");
    // Upstream unified all avatars to circles (MUL-4277); squads are round too.
    const avatar = container.querySelector('[data-slot="avatar"]')!;
    expect(avatar.className).toContain("rounded-full");
  });

  it("renders an @all chip with warning tint and a dedicated avatar", () => {
    const { container } = render(
      <ActorMentionChip type="all" label="all" initials="A" />,
    );
    const pill = container.querySelector(".actor-mention-chip")!;
    expect(pill.className).toContain("bg-warning/10");
    expect(pill.className).toContain("border-warning/20");
    // @all does not route through ActorAvatar (no "all" mode); it renders its
    // own avatar slot so the chip still has the avatar + label anatomy.
    expect(container.querySelector('[data-slot="avatar"]')).not.toBeNull();
    expect(pill.getAttribute("aria-label")).toBe(
      "Mention: all workspace members",
    );
  });

  it("announces type context in the aria-label for member/agent/squad", () => {
    expect(pillFor("member", "张三").getAttribute("aria-label")).toBe(
      "Mention: 张三, member",
    );
    expect(pillFor("agent", "ReviewerBot").getAttribute("aria-label")).toBe(
      "Mention: ReviewerBot, agent",
    );
    expect(pillFor("squad", "设计组").getAttribute("aria-label")).toBe(
      "Mention: 设计组, squad",
    );
  });

  it("caps and truncates long labels while keeping the @ prefix visible", () => {
    const { container } = render(
      <ActorMentionChip
        type="member"
        label="Alexander Christopherson"
        initials="A"
      />,
    );
    // The label span owns the truncation; the avatar + @ prefix stay outside
    // it so they remain visible when the name clips. (jsdom cannot render the
    // actual ellipsis; visual ellipsis is verified manually in U4.)
    const label = container.querySelector('[data-slot="label"]')!;
    expect(label.className).toContain("truncate");
    expect(label.className).toContain("max-w-[8rem]");
    expect(container.querySelector(".actor-mention-chip")!.textContent).toContain(
      "@Alexander Christopherson",
    );
  });

  it("merges extra className from callers (hover / focus hints)", () => {
    const { container } = render(
      <ActorMentionChip
        type="member"
        label="张三"
        initials="张"
        className="hover:bg-accent transition-colors"
      />,
    );
    expect(
      container.querySelector(".actor-mention-chip")!.className,
    ).toContain("hover:bg-accent");
  });

  it("is non-focusable by default (readonly) and focusable on demand (editor)", () => {
    const readonlyPill = render(
      <ActorMentionChip type="member" label="张三" initials="张" />,
    ).container.querySelector(".actor-mention-chip")!;
    expect(readonlyPill.getAttribute("tabindex")).toBeNull();
    expect(readonlyPill.className).not.toContain("focus-visible");

    const editorPill = render(
      <ActorMentionChip
        type="member"
        label="张三"
        initials="张"
        focusable
      />,
    ).container.querySelector(".actor-mention-chip")!;
    expect(editorPill.getAttribute("tabindex")).toBe("0");
    expect(editorPill.className).toContain("focus-visible:border-ring");
  });
});
