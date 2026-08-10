// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import {
  act,
  cleanup,
  createEvent,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import { EditorFormattingToolbar } from "./editor-formatting-toolbar";

// Preferences are mutable so a test can simulate the value the server sends
// back on the next load — that is what proves the last-used list type survives
// a reload rather than living in component state.
const preferences = vi.hoisted(() => ({
  current: { cerebro_editor_toolbar_order: ["italic", "bold"] } as Record<
    string,
    unknown
  >,
}));
const setUser = vi.hoisted(() => vi.fn());
const updateMyPreferences = vi.hoisted(() =>
  vi.fn(async (patch: Record<string, unknown>) => ({
    preferences: { ...preferences.current, ...patch },
  })),
);

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({
      user: { preferences: preferences.current },
      setUser,
    }),
}));

vi.mock("@multica/core/api", () => ({
  api: { updateMyPreferences },
}));

const run = vi.fn(() => true);
const focus = vi.fn();
const toggleBold = vi.fn();
const toggleItalic = vi.fn();
const toggleBulletList = vi.fn();
const toggleOrderedList = vi.fn();
const toggleTaskList = vi.fn();
const sinkListItem = vi.fn();
const liftListItem = vi.fn();

function fakeEditor(
  active: string | null = null,
  headingLevel?: number,
  isFocused = false,
) {
  const handlers: Record<string, Array<() => void>> = {};
  const chain = {
    focus: () => {
      focus();
      return chain;
    },
    toggleBold: () => {
      toggleBold();
      return chain;
    },
    toggleItalic: () => {
      toggleItalic();
      return chain;
    },
    toggleStrike: () => chain,
    toggleHighlight: () => chain,
    toggleTaskList: () => {
      toggleTaskList();
      return chain;
    },
    toggleBulletList: () => {
      toggleBulletList();
      return chain;
    },
    toggleOrderedList: () => {
      toggleOrderedList();
      return chain;
    },
    toggleBlockquote: () => chain,
    toggleCode: () => chain,
    sinkListItem: () => {
      sinkListItem();
      return chain;
    },
    liftListItem: () => {
      liftListItem();
      return chain;
    },
    setParagraph: () => chain,
    toggleHeading: () => chain,
    extendMarkRange: () => chain,
    setLink: () => chain,
    unsetLink: () => chain,
    run,
  };

  return {
    isEditable: true,
    isFocused,
    isActive: (mark: string, attrs?: { level?: number }) =>
      mark === "heading"
        ? attrs?.level === headingLevel
        : mark === active,
    chain: () => chain,
    can: () => ({
      sinkListItem: () => true,
      liftListItem: () => true,
    }),
    // A real registry, not a spy: the only way the row ever appears on a phone
    // is the focus handler this hook subscribes with, so a test has to be able
    // to fire it.
    on: (event: string, handler: () => void) => {
      (handlers[event] ??= []).push(handler);
    },
    off: (event: string, handler: () => void) => {
      handlers[event] = (handlers[event] ?? []).filter((h) => h !== handler);
    },
    emit: (event: string) => handlers[event]?.forEach((handler) => handler()),
    getAttributes: () => ({}),
    state: {
      selection: { empty: true, from: 1, to: 1 },
      doc: { textBetween: () => "" },
    },
  };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  preferences.current = {
    cerebro_editor_toolbar_order: ["italic", "bold"],
  };
});

describe("EditorFormattingToolbar", () => {
  it("stays visible without a text selection, follows the saved order, and formats the editor", () => {
    render(
      <EditorFormattingToolbar
        editor={fakeEditor() as never}
      />,
    );

    const toolbar = screen.getByRole("toolbar", {
      name: "Formatting toolbar",
    });
    const italic = screen.getByRole("button", { name: "Italic" });
    const bold = screen.getByRole("button", { name: "Bold" });

    expect(toolbar).toBeVisible();
    expect(
      italic.compareDocumentPosition(bold) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Code" })).toBeVisible();

    fireEvent.click(bold);
    expect(toggleBold).toHaveBeenCalled();
    expect(run).toHaveBeenCalled();
  });

  it.each(["Notes", "Documents"])(
    "renders Bold as an active Toggle with a shortcut tooltip and separator in %s",
    async () => {
      render(<EditorFormattingToolbar editor={fakeEditor("bold") as never} />);

      const bold = screen.getByRole("button", { name: "Bold" });
      expect(bold).toHaveAttribute("aria-pressed", "true");
      expect(bold).toHaveAttribute("data-pressed");
      expect(bold).toHaveClass("data-pressed:bg-[var(--toolbar-active)]");
      expect(bold).toHaveClass("data-pressed:border-[var(--toolbar-active-border)]");
      // One divider per group boundary now, not the single hardcoded one.
    expect(screen.getAllByRole("separator")[0]).toBeVisible();

      fireEvent.focus(bold);
      expect(await screen.findByText("⌘B")).toBeVisible();
    },
  );

  it.each([
    ["Italic", "italic", "⌘I"],
    ["Strikethrough", "strike", "⌘⇧X"],
    ["Highlight", "highlight", "⌘⇧H"],
    ["Code", "code", "⌘E"],
    ["Link", "link", "⌘K"],
    ["Quote", "blockquote", "⌘⇧B"],
  ])(
    "renders %s as an active Toggle with its shortcut",
    async (label, mark, shortcut) => {
      render(<EditorFormattingToolbar editor={fakeEditor(mark) as never} />);

      const control = screen.getByRole("button", { name: label });
      expect(control).toHaveAttribute("aria-pressed", "true");
      expect(control).toHaveAttribute("data-pressed");
      expect(control).toHaveClass("data-pressed:bg-[var(--toolbar-active)]");

      fireEvent.focus(control);
      expect(await screen.findByText(shortcut)).toBeVisible();
    },
  );

  it("shows the current worded text style and heading shortcuts", async () => {
    render(<EditorFormattingToolbar editor={fakeEditor(null, 2) as never} />);

    const control = screen.getByRole("button", { name: "Heading 2" });
    expect(control).toHaveAttribute("aria-pressed", "true");
    expect(control).toHaveTextContent("Heading 2");

    fireEvent.click(control);
    expect(await screen.findByText("Body text")).toBeVisible();
    expect(screen.getByText("⌘⌥1")).toBeVisible();
    expect(screen.getByText("⌘⌥2")).toBeVisible();
    expect(screen.getByText("⌘⌥3")).toBeVisible();
  });

  it("collapses the three list buttons and the two indent buttons into one split control", () => {
    preferences.current = {
      cerebro_editor_toolbar_order: ["bulletList", "orderedList", "taskList"],
    };

    render(<EditorFormattingToolbar editor={fakeEditor() as never} />);

    // The left half toggles the last-used type; the default is Bullet list.
    const primary = screen.getByRole("button", { name: "Bullet list" });
    fireEvent.click(primary);
    expect(toggleBulletList).toHaveBeenCalled();

    // The other two types and both indent buttons leave the row.
    expect(
      screen.queryByRole("button", { name: "Ordered list" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Task list" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Increase indent" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Decrease indent" }),
    ).not.toBeInTheDocument();

    expect(screen.getByRole("button", { name: "List options" })).toBeVisible();
  });

  it("offers both list types and Indent and Outdent with shortcuts behind the chevron", async () => {
    preferences.current = {
      cerebro_editor_toolbar_order: ["bulletList"],
    };

    render(<EditorFormattingToolbar editor={fakeEditor() as never} />);

    fireEvent.click(screen.getByRole("button", { name: "List options" }));

    expect(await screen.findByRole("menuitem", { name: /Ordered list/ })).toBeVisible();
    expect(screen.getByRole("menuitem", { name: /Task list/ })).toBeVisible();
    expect(screen.getByText("⌘⇧7")).toBeVisible();
    expect(screen.getByText("⌘⇧8")).toBeVisible();
    expect(screen.getByText("⌘⇧9")).toBeVisible();

    fireEvent.click(screen.getByRole("menuitem", { name: /Increase indent/ }));
    expect(sinkListItem).toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "List options" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Decrease indent/ }));
    expect(liftListItem).toHaveBeenCalled();
  });

  it("saves the chosen list type and shows it as the primary half on the next load", async () => {
    preferences.current = {
      cerebro_editor_toolbar_order: ["bulletList"],
    };

    const { unmount } = render(
      <EditorFormattingToolbar editor={fakeEditor() as never} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "List options" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Task list/ }));

    expect(toggleTaskList).toHaveBeenCalled();
    expect(updateMyPreferences).toHaveBeenCalledWith({
      cerebro_editor_list_type: "taskList",
    });

    // Next load: the server hands back the saved preference, so the primary
    // half must come from the stored value, not from component state.
    unmount();
    preferences.current = {
      cerebro_editor_toolbar_order: ["bulletList"],
      cerebro_editor_list_type: "taskList",
    };

    render(<EditorFormattingToolbar editor={fakeEditor() as never} />);
    expect(screen.getByRole("button", { name: "Task list" })).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Bullet list" }),
    ).not.toBeInTheDocument();
  });

  it("pins Comment last however the user ordered the row", () => {
    preferences.current = {
      cerebro_editor_toolbar_order: ["comment", "bold", "italic"],
    };

    render(
      <EditorFormattingToolbar
        editor={fakeEditor() as never}
        onCommentOnSelection={vi.fn()}
      />,
    );

    const comment = screen.getByRole("button", { name: "Comment" });
    const italic = screen.getByRole("button", { name: "Italic" });

    // Comment must follow Italic in the document even though the saved order
    // puts it first — it lives next to the comments rail, always last.
    expect(
      italic.compareDocumentPosition(comment) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("is one tab stop and moves focus between controls with the arrow keys", () => {
    preferences.current = {
      cerebro_editor_toolbar_order: ["bold", "italic", "strike"],
    };

    render(<EditorFormattingToolbar editor={fakeEditor() as never} />);

    const toolbar = screen.getByRole("toolbar", { name: "Formatting toolbar" });
    const controls = Array.from(
      toolbar.querySelectorAll<HTMLElement>("[data-toolbar-item]"),
    );
    expect(controls.length).toBeGreaterThan(1);

    // Exactly one control is reachable by Tab; the rest are reached by arrows.
    expect(controls.filter((c) => c.tabIndex === 0)).toHaveLength(1);
    expect(controls[0]?.tabIndex).toBe(0);

    controls[0]?.focus();
    fireEvent.keyDown(toolbar, { key: "ArrowRight" });
    expect(document.activeElement).toBe(controls[1]);

    fireEvent.keyDown(toolbar, { key: "ArrowLeft" });
    expect(document.activeElement).toBe(controls[0]);

    // Wraps, so the row never dead-ends.
    fireEvent.keyDown(toolbar, { key: "ArrowLeft" });
    expect(document.activeElement).toBe(controls[controls.length - 1]);
  });

  it("collapses to the primary controls plus an overflow menu when the pane is narrow", async () => {
    preferences.current = {
      cerebro_editor_toolbar_order: [
        "heading",
        "bold",
        "italic",
        "strike",
        "highlight",
        "code",
        "link",
        "bulletList",
        "blockquote",
        "comment",
      ],
    };

    render(
      <EditorFormattingToolbar
        editor={fakeEditor() as never}
        onCommentOnSelection={vi.fn()}
      />,
    );

    const toolbar = screen.getByRole("toolbar", { name: "Formatting toolbar" });

    // The pane decides, not the screen: the toolbar is its own query container.
    expect(toolbar.className).toContain("@container");

    // The six primary controls never leave the row.
    for (const name of ["Body text", "Bold", "Italic", "Link", "Bullet list", "Comment"]) {
      const control = screen.getByRole("button", { name });
      expect(control.closest("[data-toolbar-slot]")?.className ?? "").not.toContain(
        "@max-[520px]:hidden",
      );
    }

    // The rest step aside below the breakpoint rather than sliding past the edge.
    for (const name of ["Strikethrough", "Highlight", "Code", "Quote"]) {
      const control = screen.getByRole("button", { name });
      expect(control.closest("[data-toolbar-slot]")?.className ?? "").toContain(
        "@max-[520px]:hidden",
      );
    }

    // And they are all reachable from the overflow menu, which only appears
    // when the row has collapsed.
    // 521, not 520: the slots collapse at `@max-[520px]`, so hiding the trigger
    // from 520 up would leave the collapsed actions in neither place at exactly
    // that width.
    const overflow = screen.getByRole("button", { name: "More formatting" });
    expect(overflow.closest("[data-toolbar-slot]")?.className ?? "").toContain(
      "@[521px]:hidden",
    );

    fireEvent.click(overflow);
    for (const name of ["Strikethrough", "Highlight", "Code", "Quote"]) {
      expect(
        await screen.findByRole("menuitem", { name: new RegExp(name) }),
      ).toBeVisible();
    }
  });

  it("never hides a control without putting it in the overflow menu", async () => {
    preferences.current = {
      cerebro_editor_toolbar_order: [
        "heading",
        "bold",
        "italic",
        "strike",
        "highlight",
        "code",
        "link",
        "bulletList",
        "blockquote",
        "comment",
      ],
    };

    render(
      <EditorFormattingToolbar
        editor={fakeEditor() as never}
        onCommentOnSelection={vi.fn()}
      />,
    );

    const toolbar = screen.getByRole("toolbar", { name: "Formatting toolbar" });
    const hidden = Array.from(
      toolbar.querySelectorAll<HTMLElement>("[data-toolbar-slot]"),
    )
      .filter((slot) => slot.className.includes("@max-[520px]:hidden"))
      .map((slot) => slot.dataset.toolbarSlot);

    expect(hidden.length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: "More formatting" }));
    const menu = await screen.findByRole("menu");
    const inMenu = Array.from(
      menu.querySelectorAll<HTMLElement>("[data-overflow-action]"),
    ).map((item) => item.dataset.overflowAction);

    expect(inMenu.sort()).toEqual(hidden.sort());
  });

  it("moves an action the user hid into the overflow menu at every width", async () => {
    preferences.current = {
      cerebro_editor_toolbar_order: {
        order: ["bold", "italic", "link"],
        hidden: ["italic"],
      },
    };

    render(<EditorFormattingToolbar editor={fakeEditor() as never} />);

    // Gone from the row...
    expect(
      screen.queryByRole("button", { name: "Italic" }),
    ).not.toBeInTheDocument();

    // ...but the trigger is always there while something is hidden, not only
    // when the pane is narrow, and the action is inside it.
    const overflow = screen.getByRole("button", { name: "More formatting" });
    expect(overflow.closest("[data-toolbar-slot]")?.className ?? "").not.toContain(
      "@[521px]:hidden",
    );

    fireEvent.click(overflow);
    expect(
      await screen.findByRole("menuitem", { name: /Italic/ }),
    ).toBeVisible();
  });

  it("shows skeleton blocks instead of a wall of disabled buttons before the editor is ready", () => {
    const { container } = render(<EditorFormattingToolbar editor={null} />);

    expect(
      container.querySelectorAll('[data-slot="skeleton"]').length,
    ).toBeGreaterThan(0);
    expect(screen.queryAllByRole("button")).toHaveLength(0);
    expect(
      screen.getByRole("toolbar", { name: "Formatting toolbar" }),
    ).toHaveAttribute("aria-busy", "true");
  });
});

// FIR-4028 slice 10 — the phone. A row of formatting controls that sits above
// the text costs writing space no phone has to give. So on a touch device it
// exists only while you are typing, and then it sits on the keyboard's top
// edge rather than in the document.
describe("EditorFormattingToolbar on a phone", () => {
  const realVisualViewport = window.visualViewport;
  const realInnerHeight = window.innerHeight;
  const realInnerWidth = window.innerWidth;

  function pointerCoarse(coarse: boolean, width = 390) {
    window.innerWidth = width;
    window.matchMedia = vi.fn().mockImplementation((query: string) => ({
      // Mirrors the real query: a coarse pointer AND a phone-width viewport.
      matches:
        coarse && query.includes("pointer: coarse") && window.innerWidth < 768,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })) as unknown as typeof window.matchMedia;
  }

  function keyboardOpen(keyboardHeight: number) {
    window.innerHeight = 800;
    Object.defineProperty(window, "visualViewport", {
      configurable: true,
      value: {
        height: 800 - keyboardHeight,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
    });
  }

  afterEach(() => {
    // @ts-expect-error — restoring jsdom's own (absent) matchMedia.
    delete window.matchMedia;
    window.innerHeight = realInnerHeight;
    window.innerWidth = realInnerWidth;
    Object.defineProperty(window, "visualViewport", {
      configurable: true,
      value: realVisualViewport,
    });
  });

  it("is not on screen at all while the editor does not have focus", () => {
    pointerCoarse(true);
    keyboardOpen(0);

    render(<EditorFormattingToolbar editor={fakeEditor(null, undefined, false) as never} />);

    expect(
      screen.queryByRole("toolbar", { name: "Formatting toolbar" }),
    ).toBeNull();
  });

  it("sits on the keyboard's top edge once you start typing", () => {
    pointerCoarse(true);
    keyboardOpen(300);

    render(<EditorFormattingToolbar editor={fakeEditor(null, undefined, true) as never} />);

    const toolbar = screen.getByRole("toolbar", { name: "Formatting toolbar" });
    expect(toolbar).toHaveStyle({ position: "fixed", bottom: "300px" });
    // 44px — `min-h-11` — is the row height the design fixes for touch.
    expect(toolbar.className).toContain("min-h-11");
  });

  it("appears when you tap into the note and leaves again when you tap out", () => {
    pointerCoarse(true);
    keyboardOpen(300);
    // A note opens with the editor unfocused, so this transition — not the
    // mount-time state — is the only path that ever shows the row on a phone.
    const editor = fakeEditor(null, undefined, false);

    render(<EditorFormattingToolbar editor={editor as never} />);
    expect(
      screen.queryByRole("toolbar", { name: "Formatting toolbar" }),
    ).toBeNull();

    act(() => editor.emit("focus"));
    expect(
      screen.getByRole("toolbar", { name: "Formatting toolbar" }),
    ).toHaveStyle({ position: "fixed" });

    act(() => editor.emit("blur"));
    expect(
      screen.queryByRole("toolbar", { name: "Formatting toolbar" }),
    ).toBeNull();
  });

  it("leaves a tablet's row where it is — a coarse pointer is not a phone", () => {
    // fff2d8687 already had to put the tools panel back on a tablet; an iPad
    // reports a coarse pointer and has the width to keep the row in the page.
    pointerCoarse(true, 1024);
    keyboardOpen(0);

    render(<EditorFormattingToolbar editor={fakeEditor(null, undefined, false) as never} />);

    const toolbar = screen.getByRole("toolbar", { name: "Formatting toolbar" });
    expect(toolbar).not.toHaveStyle({ position: "fixed" });
  });

  it("keeps its place in the document on a mouse-driven screen", () => {
    pointerCoarse(false);

    render(<EditorFormattingToolbar editor={fakeEditor() as never} />);

    const toolbar = screen.getByRole("toolbar", { name: "Formatting toolbar" });
    expect(toolbar).not.toHaveStyle({ position: "fixed" });
  });

  it("leaves nothing unreachable at 390 px", () => {
    pointerCoarse(true);
    keyboardOpen(300);
    window.innerWidth = 390;
    preferences.current = {
      cerebro_editor_toolbar_order: {
        order: [
          "heading",
          "bold",
          "italic",
          "strike",
          "highlight",
          "code",
          "link",
          "lists",
          "blockquote",
          "comment",
        ],
        hidden: [],
      },
    };

    render(<EditorFormattingToolbar editor={fakeEditor(null, undefined, true) as never} />);

    const toolbar = screen.getByRole("toolbar", { name: "Formatting toolbar" });
    const offRow = Array.from(
      toolbar.querySelectorAll<HTMLElement>("[data-toolbar-slot]"),
    )
      .filter((slot) => slot.className.includes("@max-[520px]:hidden"))
      .map((slot) => slot.dataset.toolbarSlot);

    expect(offRow.length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: "More formatting" }));
    const inSheet = Array.from(
      screen
        .getByRole("menu", { name: "More formatting" })
        .querySelectorAll<HTMLElement>("[data-overflow-action]"),
    ).map((item) => item.dataset.overflowAction);

    expect(inSheet.sort()).toEqual(offRow.sort());
  });

  it("opens the format sheet without letting the keyboard drop", () => {
    pointerCoarse(true);
    keyboardOpen(300);
    preferences.current = {
      cerebro_editor_toolbar_order: {
        order: ["heading", "bold", "italic", "strike", "code", "link", "lists", "comment"],
        hidden: [],
      },
    };

    render(<EditorFormattingToolbar editor={fakeEditor(null, undefined, true) as never} />);

    const trigger = screen.getByRole("button", { name: "More formatting" });
    // Every touch target in the sheet's path must refuse the focus change that
    // would dismiss the keyboard — the sheet is useless if opening it closes
    // the thing it formats.
    const triggerDown = createEvent.mouseDown(trigger);
    fireEvent(trigger, triggerDown);
    expect(triggerDown.defaultPrevented).toBe(true);

    fireEvent.click(trigger);
    const item = screen.getByRole("menuitem", { name: /Strikethrough/ });
    expect(item.className).toContain("min-h-[38px]");

    const itemDown = createEvent.mouseDown(item);
    fireEvent(item, itemDown);
    expect(itemDown.defaultPrevented).toBe(true);

    fireEvent.click(item);
    expect(screen.queryByRole("menuitem", { name: /Strikethrough/ })).toBeNull();
  });
});

// FIR-4028 design review — the row's shape and placement, not its actions.
// Every assertion here is a line the review named as diverging from the
// approved design; each one failed before the fix.
describe("EditorFormattingToolbar — the row as a field", () => {
  function toolbarEl() {
    return screen.getByRole("toolbar", { name: "Formatting toolbar" });
  }

  it("stays put while the note scrolls", () => {
    // The row mounts inside the writing pane's scroll container, so without
    // this it leaves the screen the moment the note is longer than the pane —
    // which is the whole thing the issue asked for.
    render(<EditorFormattingToolbar editor={fakeEditor() as never} />);
    expect(toolbarEl().className).toContain("sticky");
    expect(toolbarEl().className).toContain("top-0");
  });

  it("is a standalone field, not a full-bleed bar", () => {
    render(<EditorFormattingToolbar editor={fakeEditor() as never} />);
    const { className } = toolbarEl();
    expect(className).toContain("rounded-");
    expect(className).toContain("bg-card");
    // A bottom rule and a near-white wash are what made it read as another
    // edge of the pane instead of a control surface for the text.
    expect(className).not.toContain("border-b");
    expect(className).not.toContain("bg-muted/20");
  });

  it("is 36px tall, not 44", () => {
    render(<EditorFormattingToolbar editor={fakeEditor() as never} />);
    expect(toolbarEl().className).toContain("min-h-9");
    expect(toolbarEl().className).not.toContain("min-h-11");
  });

  it("waits in the same field it ends up as", () => {
    // The loading row used to keep the old full-bleed bar at 44px, so the row
    // changed shape and height under the text the moment the editor mounted.
    render(<EditorFormattingToolbar editor={null as never} />);
    const { className } = toolbarEl();
    expect(className).toContain("min-h-9");
    expect(className).toContain("rounded-");
    expect(className).toContain("bg-card");
    expect(className).not.toContain("border-b");
    expect(className).not.toContain("bg-muted/20");
  });

  // FIR-4028 design review, findings 7 and 8 — the two active-state tokens were
  // raw hex from a different grey family, in the embedded-app stylesheet, and
  // switched on the operating system's colour scheme instead of the app's.
  it("keeps the active-state tokens on the app's own grey ramp and theme class", () => {
    // Vitest roots at this package, so the stylesheet is one relative read
    // away — import.meta.url is not a file URL under jsdom.
    const css = readFileSync("styles/editor-toolbar.css", "utf8");
    expect(css).toMatch(/--toolbar-active:\s*oklch\([^)]*28[56]/);
    expect(css).toMatch(/--toolbar-active-border:\s*oklch\([^)]*28[56]/);
    // The app's theme is a .dark class; prefers-color-scheme would follow the
    // machine and go wrong the moment the two disagree.
    expect(css).toContain(".dark");
    expect(css).not.toMatch(/@media[^{]*prefers-color-scheme/);
    expect(css).not.toMatch(/--toolbar-active[^:]*:\s*#/);
  });

  it("separates the groups, wherever the user has dragged Bold", () => {
    // The separator used to be hardcoded after `bold`, so reordering moved the
    // group divider somewhere meaningless.
    preferences.current = {
      cerebro_editor_toolbar_order: {
        order: [
          "heading",
          "italic",
          "strike",
          "highlight",
          "code",
          "bold",
          "link",
          "lists",
          "blockquote",
          "comment",
        ],
        hidden: [],
      },
    };
    const { container } = render(
      <EditorFormattingToolbar editor={fakeEditor() as never} />,
    );
    const separators = container.querySelectorAll('[data-slot="separator"]');
    // heading | marks | link | lists+quote | comment → four boundaries.
    expect(separators).toHaveLength(4);
    preferences.current = {
      cerebro_editor_toolbar_order: ["italic", "bold"],
    };
  });
});
