import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ActorAvatar } from "../actor-avatar";

afterEach(cleanup);

function avatarOf(ui: React.ReactElement) {
  const { container } = render(ui);
  return container.querySelector<HTMLElement>('[data-slot="avatar"]')!;
}

describe("ActorAvatar shape", () => {
  it("takes the theme's avatar radius", () => {
    const avatar = avatarOf(<ActorAvatar name="Ada" initials="A" />);
    expect(avatar.classList).toContain("rounded-avatar");
    expect(avatar.classList).not.toContain("rounded-full");
  });

  it("ignores a call-site radius", () => {
    const avatar = avatarOf(
      <ActorAvatar name="Ada" initials="A" className="rounded-md rounded-full" />,
    );
    expect(avatar.classList).toContain("rounded-avatar");
    expect(avatar.classList).not.toContain("rounded-md");
    expect(avatar.classList).not.toContain("rounded-full");
  });

  it("fills a system avatar with the mark's tile colour", () => {
    const avatar = avatarOf(<ActorAvatar name="Multica" initials="M" isSystem />);
    expect(avatar.classList).toContain("bg-multica-ink");
    expect(avatar.classList).not.toContain("bg-muted");
  });
});
