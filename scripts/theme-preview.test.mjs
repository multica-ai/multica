import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import test from "node:test";

const root = resolve(import.meta.dirname, "..");
const read = (path) => readFileSync(join(root, path), "utf8");

/**
 * The radius and colour names a stylesheet gives Tailwind (`--radius-*` and
 * `--color-*`), as the utilities use them: `rounded-nav-item`, and
 * `nav-item-active` for bg-, text-, border- and ring-.
 */
function themeNames(path) {
  const radius = new Set();
  const color = new Set();
  for (const [, kind, name] of read(path).matchAll(/--(radius|color)-([a-z0-9-]+):\s*var\(--/g)) {
    (kind === "radius" ? radius : color).add(name);
  }
  return { radius, color };
}

/** The component aliases: what components read and themes set (docs/theming.md). */
const componentAliases = () => themeNames("packages/ui/styles/theme-variables.css");

const VOID = new Set(["input", "br", "img", "meta", "hr"]);

/** The fixture's elements, each with its tag, attributes, classes, own text and parent. */
function parseFixture() {
  const html = read("scripts/theme-preview/fixture.html");
  const elements = [];
  const stack = [];
  const tag = /<!--[\s\S]*?-->|<(\/?)([a-zA-Z][\w-]*)((?:\s+[^\s=>/]+(?:\s*=\s*(?:"[^"]*"|'[^']*'))?)*)\s*(\/?)>/g;
  let last = 0;
  for (const match of html.matchAll(tag)) {
    const [token, closing, name, rawAttrs, selfClosing] = match;
    if (stack.length > 0) stack.at(-1).text += html.slice(last, match.index).trim();
    last = match.index + token.length;
    if (token.startsWith("<!--")) continue;
    if (closing) {
      const at = stack.findLastIndex((el) => el.tag === name);
      assert.notEqual(at, -1, `fixture.html closes <${name}> that is not open`);
      stack.length = at;
      continue;
    }
    const attrs = {};
    for (const [, key, , dq, sq] of rawAttrs.matchAll(/([^\s=]+)(\s*=\s*(?:"([^"]*)"|'([^']*)'))?/g)) {
      attrs[key] = dq ?? sq ?? "";
    }
    const el = { tag: name, attrs, text: "", classes: new Set((attrs.class ?? "").split(/\s+/).filter(Boolean)), parent: stack.at(-1) ?? null };
    elements.push(el);
    if (!selfClosing && !VOID.has(name)) stack.push(el);
  }
  assert.deepEqual(stack.map((el) => el.tag), [], "fixture.html leaves elements open");
  return elements;
}

const ancestors = function* (el) {
  for (let at = el.parent; at; at = at.parent) yield at;
};
const inside = (el, anchor) => [...ancestors(el)].some((a) => a.attrs["data-mc"] === anchor);
const descendants = (elements, el) => elements.filter((other) => [...ancestors(other)].includes(el));
const anchors = (elements, name, extra = {}) =>
  elements.filter((el) => el.attrs["data-mc"] === name && Object.entries(extra).every(([k, v]) => el.attrs[k] === v));

/**
 * What each anchor and slot of the fixture draws, the classes the component
 * it stands for puts on it, and the component's source, which must still
 * carry those classes. A theme's variables reach the page through these
 * classes only, so a fixture class that differs from the component's makes
 * the store screenshot differ from the installed theme.
 */
const EXPECTED = [
  {
    what: "the current rail item",
    find: (els) => els.filter((el) => el.tag === "a" && el.attrs["aria-current"] === "page" && inside(el, "app-rail")),
    classes: ["rounded-rail-item", "aria-[current=page]:bg-rail-item-active", "aria-[current=page]:text-rail-item-active-foreground"],
    source: "packages/views/layout/app-rail.tsx",
  },
  {
    what: "the other rail items",
    find: (els) => els.filter((el) => el.tag === "a" && el.attrs["data-space"] && !el.attrs["aria-current"] && inside(el, "app-rail")),
    classes: ["rounded-rail-item", "text-rail-item"],
    source: "packages/views/layout/app-rail.tsx",
  },
  {
    what: "the active nav item",
    find: (els) => anchors(els, "nav-item", { "data-active": "true" }),
    classes: ["rounded-nav-item", "bg-nav-item-active", "text-nav-item-active-foreground"],
    source: "packages/views/layout/panel-nav.tsx",
  },
  {
    what: "the other nav items",
    find: (els) => anchors(els, "nav-item", { "data-active": "false" }),
    classes: ["rounded-nav-item", "text-nav-item"],
    source: "packages/views/layout/panel-nav.tsx",
  },
  {
    what: "the page",
    find: (els) => anchors(els, "page"),
    classes: ["rounded-page", "bg-page-canvas", "ring-page-border"],
    source: "packages/views/layout/dashboard-layout.tsx",
  },
  {
    what: "the issue group headers",
    find: (els) => anchors(els, "issue-group-header"),
    classes: ["rounded-issue-group-header", "bg-issue-group-header"],
    source: "packages/views/issues/components/list-view.tsx",
  },
  {
    what: "the selected issue row",
    find: (els) => anchors(els, "issue-row").filter((row) => descendants(els, row).some((el) => el.tag === "input" && "checked" in el.attrs)),
    classes: ["bg-issue-row-selected"],
    source: "packages/views/issues/components/list-row.tsx",
  },
  {
    what: "the issue row identifiers",
    find: (els) => els.filter((el) => /^ACM-\d+$/.test(el.text) && inside(el, "issue-row")),
    classes: ["text-issue-row-identifier"],
    source: "packages/views/issues/components/list-row.tsx",
  },
  {
    what: "the label chips",
    find: (els) => anchors(els, "label-chip"),
    classes: ["rounded-label"],
    source: "packages/views/labels/label-chip.tsx",
  },
  {
    what: "the comment card",
    find: (els) => anchors(els, "comment"),
    classes: ["rounded-card", "border-surface-border", "bg-surface", "text-surface-foreground"],
    source: "packages/ui/components/ui/card.tsx",
  },
  {
    what: "the issue peek panel",
    find: (els) => els.filter((el) => "data-issue-peek" in el.attrs),
    classes: ["ring-floating-border", "shadow-[var(--floating-shadow)]"],
    source: "packages/views/issues/components/issue-peek.tsx",
  },
  {
    what: "the switches",
    find: (els) => els.filter((el) => el.attrs["data-slot"] === "switch"),
    classes: ["border-switch-border", "data-checked:bg-switch-checked", "data-unchecked:bg-switch-unchecked", "dark:data-unchecked:bg-switch-unchecked-dark"],
    source: "packages/ui/components/ui/switch.tsx",
  },
  {
    what: "the switch thumbs",
    find: (els) => els.filter((el) => el.attrs["data-slot"] === "switch-thumb"),
    classes: [
      "data-checked:bg-switch-checked-thumb",
      "dark:data-checked:bg-switch-checked-thumb-dark",
      "data-unchecked:bg-switch-unchecked-thumb",
      "dark:data-unchecked:bg-switch-unchecked-thumb-dark",
    ],
    source: "packages/ui/components/ui/switch.tsx",
  },
  ...[
    ["open", "git-pull-request-arrow"],
    ["merged", "git-merge"],
    ["closed", "git-pull-request-closed"],
  ].map(([state, icon]) => ({
    what: `the ${state} pull request chip icons`,
    find: (els) => els.filter((el) => el.tag === "svg" && el.classes.has(`icon-${icon}`)),
    classes: [`text-pr-${state}`, `dark:text-pr-${state}-dark`],
    source: "packages/views/issues/components/pull-request-chips.tsx",
  })),
  {
    what: "the small buttons",
    find: (els) => els.filter((el) => "data-variant" in el.attrs && ["sm", "icon-sm", "icon-xs"].includes(el.attrs["data-size"])),
    classes: ["rounded-button-sm"],
    source: "packages/ui/components/ui/button.tsx",
  },
];

/**
 * Aliases the fixture does not draw: states that need a pointer or focus,
 * and surfaces that are not on the page it shows. A new alias has to be
 * drawn or listed here.
 */
const NOT_DRAWN = {
  radius: {
    menu: "no menu is open",
    "menu-item": "no menu is open",
    popover: "no popover is open",
    dialog: "no dialog is open",
    tooltip: "no tooltip is open",
    input: "the page has no text input",
    button: "only small buttons are on the page",
    avatar: "actor avatars are round (rounded-full) in the app",
    "board-column": "the page shows the list, not the board",
    "board-card": "the page shows the list, not the board",
  },
  color: {
    "menu-item-hover": "no menu is open",
    "menu-item-hover-foreground": "no menu is open",
    "nav-item-hover": "hover",
    "nav-item-hover-foreground": "hover",
    "rail-item-hover": "hover",
    "tooltip-foreground": "no tooltip is open",
    "display-chip-pressed": "no display popover is open",
    "display-chip-pressed-foreground": "no display popover is open",
    "issue-row-hover": "hover",
    "issue-group-header-hover": "hover",
    "board-column": "the page shows the list, not the board",
    "board-card": "the page shows the list, not the board",
    "board-card-hover": "the page shows the list, not the board",
    "board-card-border": "the page shows the list, not the board",
    "status-backlog": "no backlog issue is listed",
    "status-blocked": "no blocked issue is listed",
    "status-cancelled": "no cancelled issue is listed",
    "priority-none": "every listed issue has a priority",
    "pr-merged-text": "the issue's pull request list is not shown",
  },
};

test("every anchor uses the classes of the component it stands for", () => {
  const elements = parseFixture();
  const aliases = componentAliases();
  const tokens = themeNames("packages/ui/styles/tokens.css");
  for (const { what, find, classes, source } of EXPECTED) {
    const found = find(elements);
    assert.ok(found.length > 0, `fixture.html draws no ${what}`);
    const code = read(source);
    for (const cls of classes) {
      const [, utility, name] = cls.replace(/^.*:/, "").match(/^(rounded|bg|text|border|ring)-(.+)$/) ?? [];
      if (utility === "rounded") assert.ok(aliases.radius.has(name), `${cls}: theme-variables.css has no --radius-${name}`);
      else if (utility) assert.ok(aliases.color.has(name) || tokens.color.has(name), `${cls}: no stylesheet defines --color-${name}`);
      assert.ok(code.includes(cls), `${source} no longer uses ${cls}; update ${what} in fixture.html to match`);
      for (const el of found) assert.ok(el.classes.has(cls), `${what} in fixture.html lacks ${cls} (${source} has it)`);
    }
  }
});

test("status and priority glyphs use the colour of their status and priority", () => {
  const elements = parseFixture();
  const statusSource = read("packages/views/issues/components/status-icon.tsx");
  const prioritySource = read("packages/core/issues/config/priority.ts");
  for (const header of anchors(elements, "issue-group-header")) {
    const cls = `text-status-${header.attrs["data-status"].replace(/_/g, "-")}`;
    assert.ok(statusSource.includes(`"${cls}"`), `status-icon.tsx does not use ${cls}`);
    const icons = descendants(elements, header).filter((el) => el.attrs["data-mc"] === "status-icon");
    assert.equal(icons.length, 1, `the ${header.attrs["data-status"]} group header has no status icon`);
    assert.ok(icons[0].classes.has(cls), `the ${header.attrs["data-status"]} status icon lacks ${cls}`);
  }
  for (const row of anchors(elements, "issue-row")) {
    const icons = descendants(elements, row).filter((el) => el.attrs["data-mc"] === "priority-icon");
    if (icons.length === 0) continue;
    const cls = `text-priority-${row.attrs["data-priority"]}`;
    assert.ok(prioritySource.includes(`"${cls}"`), `priority.ts does not use ${cls}`);
    assert.ok(icons[0].classes.has(cls), `the ${row.attrs["data-priority"]} priority icon lacks ${cls}`);
  }
});

test("switches are drawn on and off, with thumbs in the same state", () => {
  const elements = parseFixture();
  const switches = elements.filter((el) => el.attrs["data-slot"] === "switch");
  for (const state of ["data-checked", "data-unchecked"]) {
    assert.ok(switches.some((el) => state in el.attrs), `fixture.html draws no switch with ${state}`);
  }
  for (const el of switches) {
    const state = "data-checked" in el.attrs ? "data-checked" : "data-unchecked";
    assert.equal(el.attrs["aria-checked"], String(state === "data-checked"), `a switch with ${state} has the wrong aria-checked`);
    const thumbs = descendants(elements, el).filter((child) => child.attrs["data-slot"] === "switch-thumb");
    assert.equal(thumbs.length, 1, "each switch has one thumb");
    assert.ok(state in thumbs[0].attrs, `the thumb of a switch with ${state} lacks it`);
  }
});

test("every component alias is drawn or listed as not drawn", () => {
  const elements = parseFixture();
  const { radius, color } = componentAliases();
  const used = new Set(
    elements.flatMap((el) => [...el.classes].map((cls) => cls.replace(/^.*:/, ""))),
  );
  const drawn = {
    radius: (name) => used.has(`rounded-${name}`),
    color: (name) => ["bg", "text", "border", "ring"].some((p) => used.has(`${p}-${name}`)),
  };
  for (const [kind, names] of Object.entries({ radius, color })) {
    for (const name of names) {
      const reason = NOT_DRAWN[kind][name];
      if (drawn[kind](name)) {
        assert.ok(!reason, `--${kind}-${name} is drawn now; remove it from NOT_DRAWN`);
      } else {
        assert.ok(reason, `fixture.html never uses --${kind}-${name}; draw it the way its component does, or list it in NOT_DRAWN`);
      }
    }
    for (const name of Object.keys(NOT_DRAWN[kind])) {
      assert.ok(names.has(name), `NOT_DRAWN lists --${kind}-${name}, which theme-variables.css no longer has`);
    }
  }
});

test("anchors nest as they do in the app", () => {
  const elements = parseFixture();
  const [page] = anchors(elements, "page");
  const [sidebar] = anchors(elements, "sidebar");
  const [rail] = anchors(elements, "app-rail");
  assert.ok(inside(sidebar, "page"), "the sidebar sits inside the page canvas");
  assert.ok(!inside(rail, "page"), "the rail sits outside the page canvas");
  for (const el of anchors(elements, "nav-item")) assert.ok(inside(el, "sidebar"), "nav items sit inside the sidebar");
  for (const el of anchors(elements, "panel-section")) assert.ok(inside(el, "sidebar"), "panel sections sit inside the sidebar");
  for (const name of ["page-header", "issue-group-header", "issue-row", "comment", "comment-composer"]) {
    for (const el of anchors(elements, name)) assert.ok(inside(el, "page"), `${name} sits inside the page`);
  }
  for (const el of anchors(elements, "comment-reply")) assert.ok(inside(el, "comment"), "replies sit inside their comment");
  assert.ok(page && anchors(elements, "bottom-bar").every((el) => !inside(el, "page")), "the bottom bar sits under the page");
});
