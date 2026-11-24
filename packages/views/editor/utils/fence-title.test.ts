// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseFenceTitle, withFenceTitle } from "./fence-title";

describe("parseFenceTitle", () => {
  it("accepts double, single and bare values", () => {
    expect(parseFenceTitle('title="Weekly p95" theme=dark')).toBe("Weekly p95");
    expect(parseFenceTitle("title='Sign-up funnel'")).toBe("Sign-up funnel");
    expect(parseFenceTitle("title=Funnel")).toBe("Funnel");
    expect(parseFenceTitle('subtitle="nope"')).toBeNull();
    expect(parseFenceTitle('title="  "')).toBeNull();
    expect(parseFenceTitle(null)).toBeNull();
  });
});

describe("withFenceTitle", () => {
  it.each([
    ["adds a title to a fence that has none", null, "Weekly p95", 'title="Weekly p95"'],
    ["appends after other attributes", "theme=dark", "Funnel", 'theme=dark title="Funnel"'],
    ["replaces the title in place", 'a=1 title="Old" b=2', "New", 'a=1 title="New" b=2'],
    ["replaces a leading single-quoted title", "title='Old' b=2", "New", 'title="New" b=2'],
    ["replaces a bare title", "title=Old", "New", 'title="New"'],
    ["removes the title when blank", 'a=1 title="Old" b=2', "  ", "a=1 b=2"],
    ["drops meta that held only the title", 'title="Old"', "", null],
    ["leaves subtitle alone", 'subtitle="Keep"', "New", 'subtitle="Keep" title="New"'],
  ])("%s", (_name, meta, title, expected) => {
    expect(withFenceTitle(meta, title)).toBe(expected);
  });

  it("quotes so the title reads back unchanged", () => {
    for (const title of ['Say "hi"', "It's p95", "Plain"]) {
      expect(parseFenceTitle(withFenceTitle(null, title))).toBe(title);
    }
    expect(parseFenceTitle(withFenceTitle(null, `Both "kinds" it's`))).toBe("Both 'kinds' it's");
  });

  it("keeps the info string on one line and free of backticks", () => {
    expect(withFenceTitle(null, "Weekly\n`p95`")).toBe('title="Weekly p95"');
  });
});
