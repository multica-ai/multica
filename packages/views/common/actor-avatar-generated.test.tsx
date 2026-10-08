/**
 * Rendering precedence for generated avatars (MAKE-291).
 *
 * The server resolves precedence into a single value per response (image >
 * gen:<seed> > emoji:<x> > empty), so what matters client-side is that each
 * value class renders the right branch and that a `gen:` marker can never
 * reach the <img> path (it is not a URL — a broken img would fall through to
 * the placeholder and silently lose the identity).
 *
 * Exercised through the shared base renderer in packages/ui (ui has no test
 * runner of its own; views is where ui consumers are tested).
 */
import { afterEach, describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";

afterEach(() => {
  document.body.innerHTML = "";
});

function renderAvatar(props: Partial<Parameters<typeof ActorAvatar>[0]> = {}) {
  return render(
    <ActorAvatar
      name="Test Agent"
      initials="TA"
      isAgent
      {...props}
    />,
  );
}

describe("ActorAvatar generated-avatar precedence", () => {
  it("renders the generated avatar from a gen: marker", () => {
    renderAvatar({
      avatarUrl: "gen:11111111-1111-1111-1111-111111111111",
    });
    const svg = document.querySelector('[data-slot="generated-avatar"]');
    expect(svg).not.toBeNull();
    expect(svg?.getAttribute("data-avatar-seed")).toBe(
      "11111111-1111-1111-1111-111111111111",
    );
    expect(svg?.getAttribute("aria-label")).toBe("Test Agent");
    // Marker must never leak into the image path.
    expect(screen.queryByRole("img", { name: "" })).toBeNull();
    expect(document.querySelector("img")).toBeNull();
  });

  it("renders different seeds as different generated avatars", () => {
    const { unmount } = renderAvatar({
      avatarUrl: "gen:11111111-1111-1111-1111-111111111111",
    });
    const first = document.querySelector('[data-slot="generated-avatar"]')?.outerHTML;
    unmount();

    renderAvatar({ avatarUrl: "gen:22222222-2222-2222-2222-222222222222" });
    const second = document.querySelector('[data-slot="generated-avatar"]')?.outerHTML;
    expect(first).toBeDefined();
    expect(second).toBeDefined();
    expect(first).not.toBe(second);
  });

  it("renders the legacy emoji when the value is an emoji marker (seedless fallback)", () => {
    renderAvatar({ avatarUrl: "emoji:🐙" });
    expect(
      screen.getByText("🐙", { selector: "span" }),
    ).toBeInTheDocument();
    expect(document.querySelector('[data-slot="generated-avatar"]')).toBeNull();
    expect(document.querySelector("img")).toBeNull();
  });

  it("renders an explicit image when the value is a URL (image wins server-side)", () => {
    renderAvatar({ avatarUrl: "https://cdn.example.com/avatars/agent.png" });
    const img = document.querySelector("img");
    expect(img).not.toBeNull();
    expect(img?.getAttribute("src")).toBe(
      "https://cdn.example.com/avatars/agent.png",
    );
    expect(document.querySelector('[data-slot="generated-avatar"]')).toBeNull();
  });

  it("falls back to the agent placeholder when neither marker nor image exists", () => {
    renderAvatar({ avatarUrl: null });
    expect(document.querySelector('[data-slot="generated-avatar"]')).toBeNull();
    expect(document.querySelector("img")).toBeNull();
    // Bot icon fallback: lucide renders an svg, not an emoji span or img.
    expect(document.querySelector("svg")).not.toBeNull();
    expect(screen.queryByText("🐙")).toBeNull();
  });

  it("keeps the legacy empty-value placeholder for seedless empty string", () => {
    renderAvatar({ avatarUrl: "" });
    expect(document.querySelector('[data-slot="generated-avatar"]')).toBeNull();
    expect(document.querySelector("svg")).not.toBeNull();
  });
});
