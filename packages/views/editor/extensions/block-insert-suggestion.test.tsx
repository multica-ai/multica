import { act, render, screen } from "@testing-library/react";
import { createRef, type ReactNode } from "react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enEditor from "../../locales/en/editor.json";
import zhEditor from "../../locales/zh-Hans/editor.json";
import {
  BLOCK_MENU_ITEMS,
  BlockInsertList,
  type BlockInsertListRef,
  createBlockInsertSuggestion,
  filterBlockMenuItems,
} from "./block-insert-suggestion";
import type { InsertBlockKind } from "./block-commands";

beforeAll(() => {
  Element.prototype.scrollIntoView = vi.fn();
});

function wrapper(locale: "en" | "zh-Hans") {
  return function I18nWrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider
        locale={locale}
        resources={{ en: { editor: enEditor }, "zh-Hans": { editor: zhEditor } }}
      >
        {children}
      </I18nProvider>
    );
  };
}

const enLabel = (kind: InsertBlockKind) => enEditor.block_menu.blocks[kind];
const zhLabel = (kind: InsertBlockKind) => zhEditor.block_menu.blocks[kind];
const kinds = (items: { kind: InsertBlockKind }[]) => items.map((item) => item.kind);

describe("filterBlockMenuItems", () => {
  it("returns every block for an empty query", () => {
    expect(filterBlockMenuItems(BLOCK_MENU_ITEMS, "", enLabel)).toHaveLength(
      BLOCK_MENU_ITEMS.length,
    );
  });

  it("matches Markdown-style aliases by prefix", () => {
    expect(kinds(filterBlockMenuItems(BLOCK_MENU_ITEMS, "h2", enLabel))).toEqual([
      "heading2",
    ]);
    expect(kinds(filterBlockMenuItems(BLOCK_MENU_ITEMS, "todo", enLabel))).toEqual([
      "taskList",
    ]);
  });

  it("matches localized labels, including by pinyin", () => {
    expect(kinds(filterBlockMenuItems(BLOCK_MENU_ITEMS, "表格", zhLabel))).toEqual([
      "table",
    ]);
    expect(kinds(filterBlockMenuItems(BLOCK_MENU_ITEMS, "dmk", zhLabel))).toEqual([
      "codeBlock",
    ]);
  });
});

describe("BlockInsertList", () => {
  const items = [...BLOCK_MENU_ITEMS];

  it("filters by the typed query and accepts the highlight on Enter", () => {
    const command = vi.fn();
    const ref = createRef<BlockInsertListRef>();
    render(<BlockInsertList ref={ref} items={items} query="list" command={command} />, {
      wrapper: wrapper("en"),
    });
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual([
      "Bulleted list-",
      "Numbered list1.",
      "Task list[ ]",
    ]);
    act(() => {
      ref.current!.onKeyDown({ event: new KeyboardEvent("keydown", { key: "ArrowDown" }) });
    });
    act(() => {
      ref.current!.onKeyDown({ event: new KeyboardEvent("keydown", { key: "Enter" }) });
    });
    expect(command).toHaveBeenCalledWith(expect.objectContaining({ kind: "orderedList" }));
  });

  it("picks from what was typed when Enter beats the re-render", () => {
    const command = vi.fn();
    const ref = createRef<BlockInsertListRef>();
    render(<BlockInsertList ref={ref} items={items} query="" command={command} />, {
      wrapper: wrapper("en"),
    });
    act(() => {
      ref.current!.onKeyDown({
        event: new KeyboardEvent("keydown", { key: "Enter" }),
        query: "image",
      });
    });
    expect(command).toHaveBeenCalledWith(expect.objectContaining({ kind: "image" }));
  });

  it("renders nothing and lets Enter through when nothing matches", () => {
    const ref = createRef<BlockInsertListRef>();
    const { container } = render(
      <BlockInsertList ref={ref} items={items} query="usr/local" command={vi.fn()} />,
      { wrapper: wrapper("en") },
    );
    expect(container).toBeEmptyDOMElement();
    expect(
      ref.current!.onKeyDown({ event: new KeyboardEvent("keydown", { key: "Enter" }) }),
    ).toBe(false);
  });
});

describe("createBlockInsertSuggestion", () => {
  it("offers image and file only when the host can upload", () => {
    const items = (upload: unknown) =>
      kinds(
        (createBlockInsertSuggestion({ current: upload as never }).items as unknown as (p: {
          query: string;
        }) => { kind: InsertBlockKind }[])({ query: "" }),
      );
    expect(items(undefined)).not.toContain("image");
    expect(items(vi.fn())).toEqual(expect.arrayContaining(["image", "file"]));
  });
});
