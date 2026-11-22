import { describe, expect, it } from "vitest";
import { descriptionAnchorsForSend, locateDescriptionAnchor, type DescriptionAnchor } from "./description-anchor";
const make = (source: string, quote: string, start = source.indexOf(quote)): DescriptionAnchor => ({
  version: 1, id: "a", issue_id: "issue", quote, start, source_text: source,
  prefix: source.slice(Math.max(0, start - 32), start), suffix: source.slice(start + quote.length, start + quote.length + 32),
});
describe("published description anchors", () => {
  it("uses rendered UTF-16 coordinates with Unicode, whitespace and code newlines", () => {
    const source = "Title\n中文 🐟 café\n  let x = 1;\nnext\n"; const a = make(source, "café\n  let x = 1;");
    expect(locateDescriptionAnchor(source, a)).toBe(a.start);
    expect(locateDescriptionAnchor("A new heading\n" + source, a)).toBe(a.start + 14);
    expect(locateDescriptionAnchor(source + "Unrelated paragraph\n", a)).toBe(a.start);
  });
  it("relocates an unambiguous repeated phrase by its original context", () => {
    const source = "first: repeat\n" + "spacer ".repeat(10) + "\nsecond: repeat\nend";
    const a = make(source, "repeat", source.lastIndexOf("repeat"));
    expect(locateDescriptionAnchor("intro\n" + source + "\noutro", a)).toBe(a.start + 6);
    expect(locateDescriptionAnchor(source.replace("second: repeat", "second: removed"), a)).toBeNull();
  });
  it("never jumps to a surviving indistinguishable copy after deletion", () => {
    const block = "padding ".repeat(8) + "repeat" + " suffix".repeat(8) + "\n";
    const source = block + block; const a = make(source, "repeat");
    expect(locateDescriptionAnchor(source, a)).toBe(a.start);
    expect(locateDescriptionAnchor(source + "after\n", a)).toBe(a.start);
    expect(locateDescriptionAnchor(block, a)).toBeNull();
    expect(locateDescriptionAnchor(block + block + block, a)).toBeNull();
  });
  it("retains the quote but reports missing, changed and ambiguously duplicated sources", () => {
    const a = make("before original after", "original");
    expect(locateDescriptionAnchor("before removed after", a)).toBeNull();
    expect(locateDescriptionAnchor("before original after before original after", a)).toBeNull();
    expect(a.quote).toBe("original");
  });
  it("keeps legacy draft metadata explicitly outdated when its snapshot is unavailable", () => {
    expect(locateDescriptionAnchor("quote", { ...make("quote", "quote"), source_text: "" })).toBeNull();
  });
  it("publishes all and only this issue's saved description selections", () => {
    const base = { id: "a", sourceCommentId: "description:issue", sourceActorName: "Description", note: "/note keep", quote: "quote", start: 0, prefix: "", suffix: "", sourceText: "quote" };
    expect(descriptionAnchorsForSend("issue", [base, { ...base, id: "b" }, { ...base, sourceCommentId: "another-comment" }, { ...base, sourceCommentId: "description:other" }, { ...base, note: "" }, { ...base, id: "legacy", sourceText: undefined }]).map(a => a.id)).toEqual(["a", "b", "legacy"]);
  });
});
