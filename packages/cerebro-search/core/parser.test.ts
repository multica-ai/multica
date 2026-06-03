import { describe, expect, it } from "vitest";
import { parseSearchQuery } from "./parser";

describe("parseSearchQuery", () => {
  it("returns plain text when no filters", () => {
    const r = parseSearchQuery("login bug");
    expect(r.text).toBe("login bug");
    expect(r.filters).toEqual([]);
  });

  it("extracts a single from: filter", () => {
    const r = parseSearchQuery("login bug from:jesper");
    expect(r.text).toBe("login bug");
    expect(r.filters).toEqual([{ key: "from", value: "jesper" }]);
  });

  it("extracts multiple filters", () => {
    const r = parseSearchQuery(
      "deploy from:jesper status:todo project:lager has:attachment",
    );
    expect(r.text).toBe("deploy");
    expect(r.filters).toEqual([
      { key: "from", value: "jesper" },
      { key: "status", value: "todo" },
      { key: "project", value: "lager" },
      { key: "has", value: "attachment" },
    ]);
  });

  it("handles quoted values with spaces", () => {
    const r = parseSearchQuery(`deploy project:"my project" status:todo`);
    expect(r.text).toBe("deploy");
    expect(r.filters).toEqual([
      { key: "project", value: "my project" },
      { key: "status", value: "todo" },
    ]);
  });

  it("keeps an unknown prefix as free text", () => {
    const r = parseSearchQuery("https://example.com bug from:jesper");
    expect(r.text).toBe("https://example.com bug");
    expect(r.filters).toEqual([{ key: "from", value: "jesper" }]);
  });

  it("drops bare filter keys without value", () => {
    const r = parseSearchQuery("from: bug");
    expect(r.text).toBe("bug");
    expect(r.filters).toEqual([]);
  });

  it("is case-insensitive on the key", () => {
    const r = parseSearchQuery("FROM:jesper STATUS:Todo");
    expect(r.filters).toEqual([
      { key: "from", value: "jesper" },
      { key: "status", value: "Todo" },
    ]);
  });

  it("accumulates repeated keys", () => {
    const r = parseSearchQuery("bug from:jesper from:mia");
    expect(r.filters).toEqual([
      { key: "from", value: "jesper" },
      { key: "from", value: "mia" },
    ]);
  });
});
