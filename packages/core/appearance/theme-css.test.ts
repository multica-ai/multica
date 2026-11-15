import { describe, expect, it } from "vitest";
import { THEME_CSS_MAX_LENGTH, sanitizeThemeCss, themeAssetPath } from "./theme-css";

const FONT = "data:font/woff2;base64,d09GMgABAAAAAA==";
const assets = { "fonts/paper.woff2": FONT };

const rejects = (css: string) => {
  const result = sanitizeThemeCss(css, assets);
  expect(result.ok, css).toBe(false);
  return result.ok ? [] : result.errors;
};

describe("sanitizeThemeCss", () => {
  it("keeps ordinary CSS as it is", () => {
    const css = [
      '[data-mc="issue-row"]:hover { background: color-mix(in oklch, var(--brand) 8%, transparent); }',
      "@media (prefers-reduced-motion: reduce) { * { transition: none; } }",
      '@font-face { font-family: "Local"; src: local("Songti SC"); }',
      '.x::before { content: attr(data-label) " →"; }',
      "svg { filter: url(#shadow); }",
    ].join("\n");
    expect(sanitizeThemeCss(css)).toEqual({ ok: true, css });
  });

  it("resolves url() to the package's own assets", () => {
    const result = sanitizeThemeCss(
      '@font-face { font-family: Paper; src: url(fonts/paper.woff2) format("woff2"), url("./fonts/paper.woff2"); }',
      assets,
    );
    expect(result).toEqual({
      ok: true,
      css: `@font-face { font-family: Paper; src: url("${FONT}") format("woff2"), url("${FONT}"); }`,
    });
  });

  it("rejects anything that would load from elsewhere", () => {
    for (const css of [
      "body { background: url(https://evil.test/x.png); }",
      'body { background: url("//evil.test/x"); }',
      "body { background: url(missing.png); }",
      "body { background: url(../fonts/paper.woff2); }",
      'body { background: url("data:image/png;base64,AAAA"); }',
      '@import "https://evil.test/a.css";',
      "@import url(fonts/paper.woff2);",
      'body { background: image-set("https://evil.test/a.png" 1x); }',
      'body { background: -webkit-image-set(url(x) 1x); }',
      "body { background: image(x); }",
      "body { background: cross-fade(a, b); }",
      'body { background: src("https://evil.test"); }',
      "body { width: expression(alert(1)); }",
      "body { behavior: url(x.htc); }",
      "body { -moz-binding: x; }",
      '.x::before { content: attr(href url); }',
      "body { background: url(var(--x)); }",
      "body { background: url(a b); }",
      "body { background: url(fonts/paper.woff2",
    ]) {
      rejects(css);
    }
  });

  it("sees through escapes, case and comments", () => {
    for (const css of [
      "body { background: u\\72 l(https://evil.test); }",
      "body { background: \\75\\72\\6c(https://evil.test); }",
      "body { background: URL(https://evil.test); }",
      "body { background: Url(https://evil.test); }",
      "@\\69mport 'https://evil.test';",
      "@IMPORT 'https://evil.test';",
      "body { background: url(https\\3a //evil.test); }",
      'body { background: url("https:\\2f\\2f evil.test"); }',
      "body { background: #url(https://evil.test); }",
      "body { background: 1url(https://evil.test); }",
    ]) {
      rejects(css);
    }
    expect(sanitizeThemeCss('/* url(https://evil.test) */ .a { content: "url(https://evil.test)"; }').ok).toBe(true);
  });

  it("does not let a string or comment hide a url() after it", () => {
    rejects('.a { content: "\\"" } body { background: url(https://evil.test); }');
    rejects(".a { content: 'it\\'s' } body { background: url(https://evil.test); }");
    rejects("/* a */ body { background: url(https://evil.test); } /* b */");
  });

  it("reports the line of each problem", () => {
    const errors = rejects(".a { color: red; }\n\nbody { background: url(https://evil.test); }");
    expect(errors[0]!.line).toBe(3);
  });

  it("has a size limit", () => {
    rejects(`/*${"x".repeat(THEME_CSS_MAX_LENGTH)}*/`);
  });
});

describe("themeAssetPath", () => {
  it("only allows paths inside the package", () => {
    expect(themeAssetPath("./fonts/a.woff2")).toBe("fonts/a.woff2");
    expect(themeAssetPath("a.png")).toBe("a.png");
    for (const bad of ["/a.png", "../a.png", "fonts/../../a", "https://x/a.png", "fonts//a", "a\\b"]) {
      expect(themeAssetPath(bad)).toBeNull();
    }
  });
});
