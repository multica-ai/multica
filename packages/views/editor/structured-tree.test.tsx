import { describe, expect, it } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";
import { previewOf, StructuredTree } from "./structured-tree";

function disclosure(name: RegExp): HTMLElement {
  return screen.getByRole("button", { name });
}

describe("StructuredTree", () => {
  it("opens an object root one level deep and colours values by type", () => {
    renderWithI18n(
      <StructuredTree
        value={{ name: "web", replicas: 3, debug: false, owner: null, env: { PORT: "80" } }}
      />,
    );

    expect(screen.getByText('"web"')).toHaveClass("hljs-string");
    expect(screen.getByText("3")).toHaveClass("hljs-number");
    expect(screen.getByText("false")).toHaveClass("hljs-literal");
    expect(screen.getByText("null")).toHaveClass("hljs-literal");
    // A small second-level object is already open.
    expect(disclosure(/^env/)).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText('"80"')).toBeTruthy();
  });

  it("keeps the records of an array root collapsed, each with a preview", () => {
    renderWithI18n(
      <StructuredTree value={[{ id: 1, name: "a" }, { id: 2, name: "b" }]} />,
    );

    const first = disclosure(/^0/);
    expect(first).toHaveAttribute("aria-expanded", "false");
    expect(first).toHaveTextContent('{ id: 1, name: "a" }');

    fireEvent.click(first);
    expect(first).toHaveAttribute("aria-expanded", "true");
    expect(first).toHaveTextContent("2 keys");
    expect(screen.getByText('"a"')).toBeTruthy();

    fireEvent.click(first);
    expect(screen.queryByText('"a"')).toBeNull();
  });

  it("pages through a long list instead of rendering it all", () => {
    const value = Array.from({ length: 250 }, (_, i) => `item-${i}`);
    renderWithI18n(<StructuredTree value={value} />);

    expect(screen.getByText('"item-99"')).toBeTruthy();
    expect(screen.queryByText('"item-100"')).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Show more (150 left)" }));
    expect(screen.getByText('"item-199"')).toBeTruthy();
    expect(screen.queryByText('"item-200"')).toBeNull();
    expect(screen.getByRole("button", { name: "Show more (50 left)" })).toBeTruthy();
  });

  it("shows empty containers inline, not as disclosures", () => {
    renderWithI18n(<StructuredTree value={{ tags: [], meta: {} }} />);
    expect(screen.getByText(": []")).toBeTruthy();
    expect(screen.getByText(": {}")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("quotes keys that would read ambiguously bare", () => {
    renderWithI18n(<StructuredTree value={{ "two words": 1, "": 2, plain_key: 3 }} />);
    expect(screen.getByText('"two words"')).toHaveClass("hljs-attr");
    expect(screen.getByText('""')).toHaveClass("hljs-attr");
    expect(screen.getByText("plain_key")).toHaveClass("hljs-attr");
  });

  it("renders a bare value", () => {
    renderWithI18n(<StructuredTree value="just text" />);
    expect(screen.getByText('"just text"')).toHaveClass("hljs-string");
  });
});

describe("previewOf", () => {
  it("shrinks nested containers and stops at the budget", () => {
    expect(previewOf({ a: 1, b: [1, 2], c: { d: 1 } })).toBe("{ a: 1, b: […], c: {…} }");
    const long = previewOf(Array.from({ length: 40 }, (_, i) => i * 1000));
    expect(long.endsWith(", …]")).toBe(true);
    expect(long.length).toBeLessThan(100);
  });
});
