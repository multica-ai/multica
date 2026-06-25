import { describe, it, expect } from "vitest";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import type { Root, Element } from "hast";
import { rehypeStackedDataTables } from "@multica/ui/markdown/cerebro-stacked-tables";

// Minimal hast builders (dependency-free).
const text = (value: string) => ({ type: "text", value }) as never;
const el = (
  tagName: string,
  children: unknown[] = [],
  properties: Record<string, unknown> = {},
): Element =>
  ({ type: "element", tagName, properties, children }) as unknown as Element;

function dataTableTree(): Root {
  return {
    type: "root",
    children: [
      el("table", [
        el("thead", [
          el("tr", [el("th", [text("Tema")]), el("th", [text("Testet?")])]),
        ]),
        el("tbody", [
          el("tr", [el("td", [text("Opus for dyr")]), el("td", [text("Ja")])]),
        ]),
      ]),
    ],
  } as Root;
}

function layoutTableTree(): Root {
  return {
    type: "root",
    children: [
      el("table", [
        el("tbody", [
          el("tr", [el("td", [text("left")]), el("td", [text("right")])]),
        ]),
      ]),
    ],
  } as Root;
}

function firstTable(tree: Root): Element {
  return tree.children[0] as Element;
}

function bodyCells(table: Element): Element[] {
  const tbody = table.children.find(
    (c) => c.type === "element" && (c as Element).tagName === "tbody",
  ) as Element;
  const tr = tbody.children.find(
    (c) => c.type === "element" && (c as Element).tagName === "tr",
  ) as Element;
  return tr.children.filter(
    (c) => c.type === "element" && (c as Element).tagName === "td",
  ) as Element[];
}

describe("rehypeStackedDataTables", () => {
  it("tags data tables and injects data-label from the column headers", () => {
    const tree = dataTableTree();
    rehypeStackedDataTables()(tree);

    const table = firstTable(tree);
    expect(table.properties?.className).toContain("data-table");

    const tds = bodyCells(table);
    expect(tds[0]!.properties?.dataLabel).toBe("Tema");
    expect(tds[1]!.properties?.dataLabel).toBe("Testet?");
  });

  it("leaves layout tables (no header row) untouched", () => {
    const tree = layoutTableTree();
    rehypeStackedDataTables()(tree);

    const table = firstTable(tree);
    const className = (table.properties?.className ?? []) as unknown[];
    expect(className).not.toContain("data-table");
    expect(bodyCells(table)[0]!.properties?.dataLabel).toBeUndefined();
  });

  it("survives sanitize when the schema whitelists the class + data-label", () => {
    const schema = {
      ...defaultSchema,
      attributes: {
        ...defaultSchema.attributes,
        table: [
          ...(defaultSchema.attributes?.table ?? []),
          ["className", "data-table"],
        ],
        td: [...(defaultSchema.attributes?.td ?? []), "dataLabel"],
      },
    };

    const tree = dataTableTree();
    rehypeStackedDataTables()(tree);
    const sanitized = rehypeSanitize(
      schema as Parameters<typeof rehypeSanitize>[0],
    )(tree) as unknown as Root;

    const table = firstTable(sanitized);
    expect(table.properties?.className).toContain("data-table");
    expect(bodyCells(table)[0]!.properties?.dataLabel).toBe("Tema");
  });
});
