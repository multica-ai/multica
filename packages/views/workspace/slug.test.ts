import { describe, it, expect } from "vitest";
import {
  nameToWorkspaceSlug,
  randomCelestialWorkspaceIdentity,
} from "./slug";
import { CELESTIAL_WORKSPACE_NAMES } from "./celestial-workspace-names";

describe("nameToWorkspaceSlug", () => {
  it("lowercases ASCII names and joins words with hyphens", () => {
    expect(nameToWorkspaceSlug("My Team")).toBe("my-team");
    expect(nameToWorkspaceSlug("Acme Inc")).toBe("acme-inc");
    expect(nameToWorkspaceSlug("Project X-1")).toBe("project-x-1");
  });

  it("strips leading and trailing hyphens", () => {
    expect(nameToWorkspaceSlug("---test---")).toBe("test");
    expect(nameToWorkspaceSlug("  Hello  ")).toBe("hello");
  });

  it("collapses multiple non-alphanumeric runs into a single hyphen", () => {
    expect(nameToWorkspaceSlug("foo!!!bar")).toBe("foo-bar");
    expect(nameToWorkspaceSlug("a.b.c")).toBe("a-b-c");
  });

  // MUL-6050: a Chinese name used to produce no slug at all, which left the
  // create form with an empty URL *and* an empty issue prefix — the two
  // fields that make a workspace identifiable.
  it("romanizes Chinese names into a slug", () => {
    expect(nameToWorkspaceSlug("蜘蛛侠")).toBe("zhizhuxia");
    expect(nameToWorkspaceSlug("前端团队")).toBe("qianduantuandui");
    expect(nameToWorkspaceSlug("测试")).toBe("ceshi");
  });

  // Each Han run is romanized whole so the phrase dictionary can pick the
  // right reading for 多音字 — per-character conversion would give
  // "zhangsha" / "zhongqing" / "yinxing" here.
  it("resolves polyphonic characters from surrounding context", () => {
    expect(nameToWorkspaceSlug("长沙组")).toBe("changshazu");
    expect(nameToWorkspaceSlug("重庆团队")).toBe("chongqingtuandui");
    expect(nameToWorkspaceSlug("银行")).toBe("yinhang");
  });

  // Han characters are shared but their readings are not, so a Japanese or
  // Korean name must not be read as Mandarin: 東京 is "tokyo", not
  // "dongjing". An empty field the user fills beats a wrong default they
  // have to notice and undo.
  it("does not romanize names that are not Chinese", () => {
    // Kana is a certain signal, in any UI language.
    expect(nameToWorkspaceSlug("東京チーム")).toBe("");
    expect(nameToWorkspaceSlug("ひらがな会社")).toBe("");
    // All-kanji names carry no signal, so the reader's locale decides.
    expect(nameToWorkspaceSlug("東京支社", "ja")).toBe("");
    expect(nameToWorkspaceSlug("大韓民国", "ko")).toBe("");
    // …and the same name still romanizes for a Chinese-reading audience.
    expect(nameToWorkspaceSlug("東京支社", "zh-Hans")).toBe("dongjingzhishe");
    expect(nameToWorkspaceSlug("蜘蛛侠", "en")).toBe("zhizhuxia");
  });

  // Regression: previously fell back to literal "workspace" — caused two
  // separate non-ASCII-named workspaces on the same instance to 409 (slug
  // taken) and silently surfaced a confusing "/workspace/issues" URL.
  // Romanization covers Han only; everything else still asks the user.
  it("returns empty string for names that romanize to nothing", () => {
    expect(nameToWorkspaceSlug("こんにちは")).toBe("");
    expect(nameToWorkspaceSlug("안녕하세요")).toBe("");
    expect(nameToWorkspaceSlug("🚀")).toBe("");
    expect(nameToWorkspaceSlug("مرحبا")).toBe("");
  });

  it("returns empty string for symbol-only names", () => {
    expect(nameToWorkspaceSlug("---")).toBe("");
    expect(nameToWorkspaceSlug("!!!")).toBe("");
    expect(nameToWorkspaceSlug("   ")).toBe("");
  });

  it("keeps Latin and romanized segments apart when a name mixes them", () => {
    expect(nameToWorkspaceSlug("测试 Team")).toBe("ceshi-team");
    expect(nameToWorkspaceSlug("Project 测试 1")).toBe("project-ceshi-1");
    // No separator in the source: the romanized run must not glue onto it.
    expect(nameToWorkspaceSlug("Acme蜘蛛侠")).toBe("acme-zhizhuxia");
  });
});

describe("randomCelestialWorkspaceIdentity", () => {
  it("picks a celestial name and appends a four-character slug suffix", () => {
    const values = [0, 0, 0.5, 0.999, 0.25];
    let index = 0;

    const identity = randomCelestialWorkspaceIdentity(
      "en",
      () => values[index++] ?? 0,
    );

    expect(identity.name).toBe(CELESTIAL_WORKSPACE_NAMES[0]?.names.en);
    expect(identity.slug).toMatch(/^alpha-centauri-[a-z0-9]{4}$/);
    expect(identity.slug).toBe("alpha-centauri-as9j");
  });

  it("keeps the source list unique", () => {
    expect(
      new Set(CELESTIAL_WORKSPACE_NAMES.map(({ slugBase }) => slugBase)).size,
    ).toBe(
      CELESTIAL_WORKSPACE_NAMES.length,
    );
  });
});
