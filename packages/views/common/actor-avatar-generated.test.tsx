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
import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";


// Lets tests simulate an unsupported archetype / missing illustrated asset so
// the generic procedural fallback keeps meaningful coverage now that all five
// archetypes ship illustrated art.
const catalogGate = vi.hoisted(() => ({ forceMissing: false }));
vi.mock("@multica/ui/lib/avatar-catalog", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@multica/ui/lib/avatar-catalog")>();
  return {
    ...actual,
    hasIllustratedAvatarAsset: (archetype: string, variant: string) =>
      catalogGate.forceMissing
        ? false
        : actual.hasIllustratedAvatarAsset(archetype, variant),
  };
});

afterEach(() => {
  catalogGate.forceMissing = false;
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
    // Simulate a missing illustrated asset: the generic procedural renderer
    // must still render the identity (and never leak the marker into <img>).
    catalogGate.forceMissing = true;
    renderAvatar({
      avatarUrl: "gen:android-0",
    });
    const svg = document.querySelector('[data-slot="generated-avatar"]');
    expect(svg).not.toBeNull();
    expect(svg?.getAttribute("data-avatar-seed")).toBe("android-0");
    expect(svg?.getAttribute("aria-label")).toBe("Test Agent");
    // Marker must never leak into the image path.
    expect(screen.queryByRole("img", { name: "" })).toBeNull();
    expect(document.querySelector("img")).toBeNull();
  });

  it("renders an approved illustrated asset for the Fox archetype", () => {
    renderAvatar({ avatarUrl: "gen:fox-26" });
    const img = document.querySelector(
      'img[data-slot="generated-avatar"][data-avatar-archetype="fox"]',
    );
    expect(img).not.toBeNull();
    expect(img?.getAttribute("data-avatar-variant")).toBe("general");
    expect(img?.getAttribute("src")).toMatch(/^data:image\/webp;base64,/);
    expect(img?.getAttribute("srcset")).toContain("32w");
    expect(img?.getAttribute("alt")).toBe("Test Agent");
  });

  it("renders an approved illustrated asset for the Bear archetype", () => {
    renderAvatar({ avatarUrl: "gen:bear-2" });
    const img = document.querySelector(
      'img[data-slot="generated-avatar"][data-avatar-archetype="bear"]',
    );
    expect(img).not.toBeNull();
    expect(img?.getAttribute("data-avatar-variant")).toBe("general");
    expect(img?.getAttribute("src")).toMatch(/^data:image\/webp;base64,/);
    expect(img?.getAttribute("srcset")).toContain("96w");
    expect(img?.getAttribute("alt")).toBe("Test Agent");
  });

  it("renders an approved illustrated asset for the Owl archetype", () => {
    renderAvatar({ avatarUrl: "gen:owl-30" });
    const img = document.querySelector(
      'img[data-slot="generated-avatar"][data-avatar-archetype="owl"]',
    );
    expect(img).not.toBeNull();
    expect(img?.getAttribute("data-avatar-variant")).toBe("general");
    expect(img?.getAttribute("src")).toMatch(/^data:image\/webp;base64,/);
    expect(img?.getAttribute("srcset")).toContain("96w");
    expect(img?.getAttribute("alt")).toBe("Test Agent");
  });

  it("renders an approved illustrated asset for the Dragon archetype", () => {
    renderAvatar({ avatarUrl: "gen:dragon-2" });
    const img = document.querySelector(
      'img[data-slot="generated-avatar"][data-avatar-archetype="dragon"]',
    );
    expect(img).not.toBeNull();
    expect(img?.getAttribute("data-avatar-variant")).toBe("developer");
    expect(img?.getAttribute("src")).toMatch(/^data:image\/webp;base64,/);
    expect(img?.getAttribute("srcset")).toContain("96w");
    expect(img?.getAttribute("alt")).toBe("Test Agent");
  });

  it("renders approved illustrated Android variants for every lane", () => {
    const expected: Record<string, string> = {
      "android-0": "general",
      "android-27": "developer",
      "android-15": "auditor",
    };
    for (const [seed, variant] of Object.entries(expected)) {
      const { unmount } = renderAvatar({ avatarUrl: `gen:${seed}` });
      const img = document.querySelector(
        'img[data-slot="generated-avatar"][data-avatar-archetype="android"]',
      );
      expect(img, seed).not.toBeNull();
      expect(img?.getAttribute("data-avatar-variant")).toBe(variant);
      expect(img?.getAttribute("src")).toMatch(/^data:image\/webp;base64,/);
      expect(img?.getAttribute("srcset")).toContain("96w");
      unmount();
    }
  });

  it("renders every one of the five archetypes through the illustrated path", () => {
    const seeds = {
      fox: "fox-26",
      bear: "bear-2",
      owl: "owl-30",
      dragon: "dragon-8",
      android: "android-0",
    };
    for (const [archetype, seed] of Object.entries(seeds)) {
      const { unmount } = renderAvatar({ avatarUrl: `gen:${seed}` });
      const img = document.querySelector('img[data-slot="generated-avatar"]');
      expect(img?.getAttribute("data-avatar-archetype"), seed).toBe(archetype);
      unmount();
    }
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
