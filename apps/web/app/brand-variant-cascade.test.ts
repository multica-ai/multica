import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import postcss from "postcss";
import tailwind from "@tailwindcss/postcss";
import { cn } from "@multica/ui/lib/utils";
import { buttonVariants } from "@multica/ui/components/ui/button";
import { beforeAll, describe, expect, it } from "vitest";

/**
 * Does the brand chip actually render blue?
 *
 * This lives in apps/web because it tests the COMPILED STYLESHEET, not React
 * behaviour: `globals.css` is the app's artifact, and the `dark` variant is
 * defined there (`@custom-variant dark (&:is(.dark *))`) — that definition is
 * what decides the outcome below. It follows the same pattern as
 * font-fallback-order.test.ts, which also asserts on the app's CSS.
 *
 * Why simulate a cascade instead of asserting class names: a class name in
 * the DOM proves nothing about the pixel. Twice on MUL-4884 the brand colour
 * was present as a class and still lost:
 *
 *   1. Brand classes layered over the `outline` variant. tailwind-merge keeps
 *      `dark:bg-input/30` (different modifier group, no conflict to resolve),
 *      and `dark:` compiles to `&:is(.dark *)` — specificity (0,2,0) against
 *      a bare `.bg-brand`'s (0,1,0). The chip was grey in dark mode while
 *      every string assertion passed.
 *   2. A colour class appended via `className` beat the variant that was
 *      supposed to own the colour (see the merged-class case below).
 *
 * Both are invisible to a `toContain("bg-brand")` test and both are caught by
 * resolving what a browser would actually paint: filter the declarations that
 * match the element's classes and state, then take the winner by specificity,
 * then source order.
 */

interface FlatRule {
  sel: string;
  prop: string;
  order: number;
}

interface ElementState {
  dark?: boolean;
  hover?: boolean;
  active?: boolean;
  expanded?: boolean;
}

const repoRoot = resolve(process.cwd(), "../..");

async function compileStylesheet(entry: string): Promise<FlatRule[]> {
  const css = readFileSync(entry, "utf8");
  const built = await postcss([tailwind({ base: dirname(entry) })]).process(css, {
    from: entry,
  });

  // Flatten Tailwind's nested output. Declarations sit under a rule OR inside
  // an at-rule (`hover:` is wrapped in `@media (hover:hover)`, opacity
  // modifiers in `@supports`), so collect at both levels and keep document
  // order — it is the tie-breaker when specificity ties.
  const rules: FlatRule[] = [];
  let order = 0;
  const walk = (container: postcss.Container, prefix: string) => {
    container.each((node) => {
      if (node.type === "decl") {
        if (prefix) rules.push({ sel: prefix, prop: node.prop, order: order++ });
      } else if (node.type === "rule") {
        const sel = node.selector.includes("&")
          ? node.selector.replace(/&/g, prefix)
          : prefix
            ? prefix + node.selector
            : node.selector;
        walk(node, sel);
      } else if (node.type === "atrule") {
        walk(node, prefix);
      }
    });
  };
  walk(built.root, "");
  return rules;
}

/** Selector specificity's `b` column — classes, attributes, pseudo-classes.
 *  `:is()` contributes its most specific argument. Nothing here reaches for
 *  ids or elements, so the other two columns are always 0. */
function specificity(selector: string): number {
  let count = 0;
  const withoutIs = selector.replace(/:is\(([^()]*)\)/g, (_, inner: string) => {
    count += Math.max(
      ...inner.split(",").map((part) => (part.match(/\.[^.\s>+~:[]+/g) ?? []).length),
    );
    return "";
  });
  return count + (withoutIs.match(/\\?\.[A-Za-z0-9_\\/:.\-[\]%]+/g) ?? []).length;
}

// Tailwind escapes `/` and `:` in class selectors (`.bg-brand\/7`), so split
// on the matched (escaped) prefix and unescape only for comparison — slicing
// by the unescaped length silently mis-parses every opacity utility.
const CLASS_PREFIX = /^\.((?:[^.\s:[\\]|\\.)+)/;

function baseClassOf(selector: string): string | null {
  const m = selector.match(CLASS_PREFIX);
  return m ? m[1]!.replace(/\\/g, "") : null;
}

function matches(selector: string, classes: string[], state: ElementState) {
  const m = selector.match(CLASS_PREFIX);
  if (!m) return false;
  const cls = m[1]!.replace(/\\/g, "");
  if (!classes.includes(cls)) return false;
  const rest = selector.slice(m[0].length);
  if (rest.includes(":is(.dark *)") && !state.dark) return false;
  if (rest.includes(":hover") && !state.hover) return false;
  if (rest.includes(":active") && !state.active) return false;
  if (rest.includes('[aria-expanded="true"]') && !state.expanded) return false;
  // Ignore any rule whose selector we do not model, rather than guessing.
  const unmodelled = rest.replace(
    /:is\(\.dark \*\)|:hover|:active|\[aria-expanded="true"\]/g,
    "",
  );
  return unmodelled.trim() === "";
}

/** The class a browser would let win for `prop` on an element carrying
 *  `classes` in `state`. */
function winning(
  rules: FlatRule[],
  classes: string[],
  state: ElementState,
  prop: string,
): string | null {
  const candidates = rules.filter(
    (r) => r.prop === prop && matches(r.sel, classes, state),
  );
  if (candidates.length === 0) return null;
  candidates.sort(
    (a, b) => specificity(a.sel) - specificity(b.sel) || a.order - b.order,
  );
  return baseClassOf(candidates.at(-1)!.sel);
}

const WEB_CSS = resolve(repoRoot, "apps/web/app/globals.css");
const DESKTOP_CSS = resolve(repoRoot, "apps/desktop/src/renderer/src/globals.css");

// The chip's own composition: layout classes only, colour from the variant.
const brand = cn(buttonVariants({ variant: "brand", size: "sm" }), "h-8 px-2");
const brandSubtle = cn(
  buttonVariants({ variant: "brandSubtle", size: "sm" }),
  "h-8 px-2",
);

describe("brand Button variants resolve to brand colour in the real stylesheet", () => {
  let rules: FlatRule[];

  beforeAll(async () => {
    rules = await compileStylesheet(WEB_CSS);
  }, 60_000);

  const bg = (classes: string, state: ElementState) =>
    winning(rules, classes.split(/\s+/), state, "background-color");
  const text = (classes: string, state: ElementState) =>
    winning(rules, classes.split(/\s+/), state, "color");

  describe("brand (filter ON — the loud filled tier)", () => {
    // --brand flips per theme, so one set of rules must serve both. If a
    // `dark:` rule from another variant ever creeps in, these diverge.
    for (const dark of [false, true]) {
      const theme = dark ? "dark" : "light";

      it(`fills with brand and never with the neutral input token (${theme})`, () => {
        expect(bg(brand, { dark })).toBe("bg-brand");
      });

      it(`deepens one notch on hover, another when pressed (${theme})`, () => {
        expect(bg(brand, { dark, hover: true })).toBe("hover:bg-brand/90");
        expect(bg(brand, { dark, hover: true, active: true })).toBe(
          "active:bg-brand/85",
        );
      });

      it(`reads as hover, not as a colour change, while the popover is open (${theme})`, () => {
        expect(bg(brand, { dark, expanded: true })).toBe(
          "aria-expanded:bg-brand/90",
        );
      });

      it(`keeps brand-foreground text (${theme})`, () => {
        expect(text(brand, { dark })).toBe("text-brand-foreground");
      });
    }
  });

  describe("brandSubtle (activity, filter OFF — the tint tier)", () => {
    // Dark runs one notch hotter: the same alpha reads weaker on a near-black
    // surface than on white.
    it("uses the light notches in light mode", () => {
      expect(bg(brandSubtle, {})).toBe("bg-brand/7");
      expect(bg(brandSubtle, { hover: true })).toBe("hover:bg-brand/12");
      expect(bg(brandSubtle, { hover: true, active: true })).toBe(
        "active:bg-brand/16",
      );
      expect(bg(brandSubtle, { expanded: true })).toBe("aria-expanded:bg-brand/12");
    });

    it("uses the hotter dark notches in dark mode", () => {
      expect(bg(brandSubtle, { dark: true })).toBe("dark:bg-brand/12");
      expect(bg(brandSubtle, { dark: true, hover: true })).toBe(
        "dark:hover:bg-brand/18",
      );
      expect(bg(brandSubtle, { dark: true, hover: true, active: true })).toBe(
        "dark:active:bg-brand/24",
      );
      expect(bg(brandSubtle, { dark: true, expanded: true })).toBe(
        "dark:aria-expanded:bg-brand/18",
      );
    });
  });

  // The two ways the brand colour has actually been lost. Both are asserted
  // as the FAILURE they are, so the reason for the current shape is executable
  // rather than a comment someone can quietly undo.
  describe("the mistakes this design prevents", () => {
    it("shows why layering brand over `outline` cannot work in dark mode", () => {
      const layered = cn(
        buttonVariants({ variant: "outline", size: "sm" }),
        "border-brand bg-brand text-brand-foreground",
      );

      // tailwind-merge cannot drop outline's dark: rules — different modifier
      // group, so there is no conflict for it to resolve...
      expect(layered).toContain("dark:bg-input/30");
      // ...and `:is(.dark *)` then outranks the bare .bg-brand.
      expect(bg(layered, { dark: true })).toBe("dark:bg-input/30");
      // Light mode looks fine, which is exactly why this shipped unnoticed.
      expect(bg(layered, {})).toBe("bg-brand");
    });

    it("shows why a colour class in `className` beats the variant that owns it", () => {
      // `filter ON + 0 agents` used to land here: variant `brand`, muted text
      // appended for the empty state → grey text on a brand-blue fill.
      const overridden = cn(
        buttonVariants({ variant: "brand", size: "sm" }),
        "text-muted-foreground",
      );

      expect(text(overridden, {})).toBe("text-muted-foreground");
      expect(text(overridden, { dark: true })).toBe("text-muted-foreground");
    });
  });
});

describe("desktop ships the same brand cascade as web", () => {
  let rules: FlatRule[];

  beforeAll(async () => {
    rules = await compileStylesheet(DESKTOP_CSS);
  }, 60_000);

  // Desktop declares its own `@custom-variant dark` and its own @source globs,
  // so the guarantee has to be re-proved against its bundle rather than
  // assumed from web's.
  it("fills the brand tier with brand in both themes", () => {
    const classes = brand.split(/\s+/);
    expect(winning(rules, classes, {}, "background-color")).toBe("bg-brand");
    expect(winning(rules, classes, { dark: true }, "background-color")).toBe(
      "bg-brand",
    );
    expect(winning(rules, classes, { dark: true }, "color")).toBe(
      "text-brand-foreground",
    );
  });

  it("keeps the tint tier's dark notches", () => {
    const classes = brandSubtle.split(/\s+/);
    expect(winning(rules, classes, { dark: true }, "background-color")).toBe(
      "dark:bg-brand/12",
    );
  });
});
