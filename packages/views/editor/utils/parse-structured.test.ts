// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseStructured } from "./parse-structured";

describe("parseStructured", () => {
  it("parses JSON", () => {
    expect(parseStructured('{"a":[1,true,null]}', "json")).toEqual({
      ok: true,
      value: { a: [1, true, null] },
    });
  });

  it("reports why JSON failed", () => {
    const result = parseStructured('{"a":', "json");
    expect(result.ok).toBe(false);
    expect(!result.ok && result.message).toBeTruthy();
  });

  it("parses JSON Lines into a list, skipping blank lines", () => {
    expect(parseStructured('{"n":1}\n\n{"n":2}\r\n', "jsonl")).toEqual({
      ok: true,
      value: [{ n: 1 }, { n: 2 }],
    });
  });

  it("names the JSON Lines line that failed", () => {
    const result = parseStructured('{"n":1}\n{oops}\n', "jsonl");
    expect(result.ok).toBe(false);
    expect(!result.ok && result.message).toMatch(/^Line 2: /);
  });

  it("parses YAML", () => {
    expect(parseStructured("name: web\nports:\n  - 80\n  - 443\n", "yaml")).toEqual({
      ok: true,
      value: { name: "web", ports: [80, 443] },
    });
  });

  it("shows a multi-document YAML stream as a list of documents", () => {
    expect(parseStructured("kind: A\n---\nkind: B\n", "yaml")).toEqual({
      ok: true,
      value: [{ kind: "A" }, { kind: "B" }],
    });
  });

  it("treats an empty YAML file as null", () => {
    expect(parseStructured("", "yaml")).toEqual({ ok: true, value: null });
  });

  it("reports YAML syntax errors", () => {
    const result = parseStructured("a: [1, 2\nb: 3", "yaml");
    expect(result.ok).toBe(false);
  });

  it("refuses runaway alias expansion instead of hanging", () => {
    const bomb = [
      "a: &a [x, x, x, x, x, x, x, x, x]",
      "b: &b [*a, *a, *a, *a, *a, *a, *a, *a, *a]",
      "c: &c [*b, *b, *b, *b, *b, *b, *b, *b, *b]",
      "d: &d [*c, *c, *c, *c, *c, *c, *c, *c, *c]",
      "e: [*d, *d, *d, *d, *d, *d, *d, *d, *d]",
    ].join("\n");
    expect(parseStructured(bomb, "yaml").ok).toBe(false);
  });
});
