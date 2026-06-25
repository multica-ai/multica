// CEREBRO-PATCH(stacked-data-tables): FIR-1727 — rehype plugin that tags every
// markdown DATA table (a table that has a real header row) with the `data-table`
// class and copies each column's header text onto its body cells as a
// `data-label` attribute. CSS (cerebro-overrides.css / markdown.css) then renders
// these as wrapping tables on desktop and one labelled card per row on mobile,
// instead of crushing four columns onto a phone and shredding words. Layout tables
// (email/message bodies, no header row) are left untouched. Shared by both the
// ReadonlyContent and Markdown renderers. New fork file living in an upstream path.

import type { Root, Element, ElementContent } from "hast";

function textOf(node: ElementContent | Element): string {
  if (node.type === "text") return node.value;
  const children = (node as Element).children;
  return Array.isArray(children) ? children.map(textOf).join("") : "";
}

function childElements(node: Element, tagName: string): Element[] {
  return (node.children ?? []).filter(
    (c): c is Element => c.type === "element" && c.tagName === tagName,
  );
}

function firstChild(node: Element, tagName: string): Element | undefined {
  return childElements(node, tagName)[0];
}

function addClass(node: Element, cls: string): void {
  node.properties = node.properties ?? {};
  const existing = node.properties.className;
  if (Array.isArray(existing)) node.properties.className = [...existing, cls];
  else if (typeof existing === "string" && existing.length > 0)
    node.properties.className = [existing, cls];
  else node.properties.className = [cls];
}

function enhanceTable(table: Element): void {
  const thead = firstChild(table, "thead");
  const headerRow = thead ? childElements(thead, "tr")[0] : undefined;
  // No <thead><tr> → this is a layout/email table, not a data table. Leave it.
  if (!headerRow) return;
  const headers = childElements(headerRow, "th").map((th) => textOf(th).trim());
  if (headers.length === 0) return;

  addClass(table, "data-table");

  const body = firstChild(table, "tbody") ?? table;
  for (const tr of childElements(body, "tr")) {
    childElements(tr, "td").forEach((td, i) => {
      const label = headers[i];
      if (!label) return;
      td.properties = td.properties ?? {};
      // hast camelCase → renders as the `data-label` DOM attribute.
      if (td.properties.dataLabel == null) td.properties.dataLabel = label;
    });
  }
}

function walk(node: Root | Element): void {
  const children = node.children;
  if (!Array.isArray(children)) return;
  for (const child of children) {
    if (child.type === "element") {
      if (child.tagName === "table") enhanceTable(child);
      walk(child);
    }
  }
}

/**
 * rehype plugin. Runs after rehype-raw (so inline-HTML tables are in the tree)
 * and before rehype-sanitize (which whitelists the `data-table` class and
 * `data-label` attribute so they survive sanitization).
 */
export function rehypeStackedDataTables() {
  return (tree: Root): void => {
    walk(tree);
  };
}
