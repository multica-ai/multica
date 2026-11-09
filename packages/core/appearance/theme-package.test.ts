// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { INSTALLED_THEMES_STORAGE_KEY, findTheme, useInstalledThemesStore } from "./installed-themes";
import { readZip } from "./theme-archive";
import { parseStoredTheme, parseTheme, themeCss, themeFromFiles } from "./theme-package";

const FONT = "data:font/woff2;base64,d09GMgABAAAAAA==";
const FONT_BYTES = Uint8Array.from(atob("d09GMgABAAAAAA=="), (c) => c.charCodeAt(0));

const manifest = {
  apiVersion: 1,
  id: "paper",
  name: "Paper",
  author: "Someone",
  version: "1.2.0",
  options: [
    { id: "header", type: "select", label: { en: "Headers", "zh-Hans": "分组标题" }, default: "block", choices: [
      { value: "flat", label: "Flat" },
      { value: "block", label: "Block", swatch: "oklch(0.8 0.1 90)" },
    ] },
    { id: "hyphens", type: "toggle", label: "Hyphenate", default: true },
  ],
  extra: "ignored",
};
const css = [
  '@font-face { font-family: "Paper Serif"; src: url(fonts/serif.woff2); }',
  ".theme-light { --page-canvas: #f6efdf; --font-content-latin: \"Paper Serif\", serif; }",
  ".theme-dark { --page-canvas: oklch(0.2 0.01 60); }",
  ':root[data-theme-option-hyphens] [data-mc="content"] p { hyphens: auto; }',
].join("\n");
const paper = { manifest, css, assets: { "fonts/serif.woff2": FONT } };

const encode = (text: string) => new TextEncoder().encode(text);

describe("parseTheme", () => {
  it("rebuilds a valid package from what passes validation", () => {
    const result = parseTheme(paper);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.theme).toMatchObject({ id: "paper", name: "Paper", author: "Someone", version: "1.2.0", css });
    expect(result.theme).not.toHaveProperty("extra");
    expect(result.theme.options).toHaveLength(2);
    expect(themeCss(result.theme)).toContain(`url("${FONT}")`);
  });

  it("lists every problem instead of stopping at the first", () => {
    const result = parseTheme({
      manifest: {
        apiVersion: 2,
        id: "Bad Id",
        options: [{ id: "x", type: "select", label: "X", choices: [{ value: "a", label: "A" }] }],
      },
      css: "body { background: url(https://evil.test); }",
      assets: { "../x.woff2": FONT, "y.png": "https://evil.test/y.png" },
    });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    const errors = result.errors.join("\n");
    expect(errors).toMatch(/apiVersion/);
    expect(errors).toMatch(/^id must/m);
    expect(errors).toMatch(/name is required/);
    expect(errors).toMatch(/theme\.css line 1/);
    expect(errors).toMatch(/"\.\.\/x\.woff2" must be/);
    expect(errors).toMatch(/"y\.png" must be/);
    expect(errors).toMatch(/at least two/);
  });

  it("reads every option type and rejects ones it cannot render", () => {
    const options = [
      { id: "look", type: "heading", label: "Look" },
      { id: "paper-color", type: "color", label: "Paper", default: { light: "#f6efdf", dark: "#1d1b16" } },
      { id: "ink", type: "color", label: "Ink", default: "#222" },
      { id: "page-width", type: "slider", label: "Width", default: 46, min: 30, max: 60, step: 2, unit: "rem" },
      { id: "line-height", type: "number", label: "Leading", default: 1.6, step: 0.05 },
      { id: "h1-style", type: "text", label: "H1 style", default: "italic" },
      { id: "h1-font", type: "font", label: "H1 font", default: '"Paper Serif", serif' },
      { id: "rule", type: "variable-select", label: "Rule", default: "2px", choices: [
        { value: "1px", label: "Thin" },
        { value: "2px", label: "Thick" },
      ] },
      { id: "bad-color", type: "color", label: "Bad", default: "red" },
      { id: "bad-slider", type: "slider", label: "Bad", default: 1 },
      { id: "bad-unit", type: "number", label: "Bad", default: 1, unit: "px;" },
      { id: "bad-text", type: "text", label: "Bad", default: "x; } body { display: none" },
    ];
    const result = parseTheme({ ...paper, manifest: { ...manifest, options } });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors.filter((e) => /^Option "bad-/.test(e))).toHaveLength(4);
    const good = parseTheme({ ...paper, manifest: { ...manifest, options: options.slice(0, 8) } });
    expect(good.ok && good.theme.options?.map((o) => o.type)).toEqual([
      "heading", "color", "color", "slider", "number", "text", "font", "variable-select",
    ]);
    expect(good.ok && good.theme.options?.[2]).toMatchObject({ default: { light: "#222", dark: "#222" } });
  });

  it("needs a theme.css and refuses to shadow a built-in theme", () => {
    expect(parseTheme({ manifest, css: undefined }).ok).toBe(false);
    expect(parseTheme({ ...paper, manifest: { ...manifest, id: "pop" } }).ok).toBe(false);
  });

  it("rejects packages over the size limit", () => {
    expect(parseTheme({ ...paper, css: `/* ${"x".repeat(1024 * 1024)} */` }).ok).toBe(false);
  });
});

describe("themeFromFiles", () => {
  it("reads a theme folder, wrapped in a top-level folder or not", () => {
    const files = [
      { path: "paper/manifest.json", data: encode(JSON.stringify(manifest)) },
      { path: "paper/theme.css", data: encode(css) },
      { path: "paper/fonts/serif.woff2", data: FONT_BYTES },
      { path: "paper/README.md", data: encode("# Paper") },
      { path: "__MACOSX/paper/._theme.css", data: encode("junk") },
    ];
    const result = themeFromFiles(files);
    expect(result.ok && result.theme.assets).toEqual({ "fonts/serif.woff2": FONT });
    const flat = themeFromFiles(files.map((f) => ({ ...f, path: f.path.replace(/^paper\//, "") })));
    expect(flat.ok).toBe(true);
  });

  it("explains what is missing", () => {
    expect(themeFromFiles([{ path: "theme.css", data: encode(css) }])).toEqual({
      ok: false,
      errors: ["manifest.json is missing"],
    });
    expect(themeFromFiles([{ path: "manifest.json", data: encode("{") }])).toEqual({
      ok: false,
      errors: ["manifest.json is not valid JSON"],
    });
  });
});

/** A minimal zip writer for the reader to read. */
async function zip(entries: { path: string; data: Uint8Array; deflate: boolean }[]): Promise<Uint8Array> {
  const chunks: Uint8Array[] = [];
  const central: Uint8Array[] = [];
  let offset = 0;
  for (const entry of entries) {
    const body = entry.deflate
      ? new Uint8Array(
          await new Response(
            new ReadableStream<Uint8Array>({
              start(controller) {
                controller.enqueue(entry.data);
                controller.close();
              },
            }).pipeThrough(new CompressionStream("deflate-raw") as ReadableWritablePair<Uint8Array, Uint8Array>),
          ).arrayBuffer(),
        )
      : entry.data;
    const name = encode(entry.path);
    const local = new DataView(new ArrayBuffer(30));
    local.setUint32(0, 0x04034b50, true);
    local.setUint16(8, entry.deflate ? 8 : 0, true);
    local.setUint32(18, body.length, true);
    local.setUint32(22, entry.data.length, true);
    local.setUint16(26, name.length, true);
    const header = new DataView(new ArrayBuffer(46));
    header.setUint32(0, 0x02014b50, true);
    header.setUint16(10, entry.deflate ? 8 : 0, true);
    header.setUint32(20, body.length, true);
    header.setUint32(24, entry.data.length, true);
    header.setUint16(28, name.length, true);
    header.setUint32(42, offset, true);
    chunks.push(new Uint8Array(local.buffer), name, body);
    central.push(new Uint8Array(header.buffer), name);
    offset += 30 + name.length + body.length;
  }
  const size = central.reduce((n, c) => n + c.length, 0);
  const end = new DataView(new ArrayBuffer(22));
  end.setUint32(0, 0x06054b50, true);
  end.setUint16(10, entries.length, true);
  end.setUint32(12, size, true);
  end.setUint32(16, offset, true);
  const all = [...chunks, ...central, new Uint8Array(end.buffer)];
  const out = new Uint8Array(all.reduce((n, c) => n + c.length, 0));
  let at = 0;
  for (const chunk of all) {
    out.set(chunk, at);
    at += chunk.length;
  }
  return out;
}

describe("readZip", () => {
  it("reads stored and deflated entries", async () => {
    const bytes = await zip([
      { path: "paper/manifest.json", data: encode(JSON.stringify(manifest)), deflate: true },
      { path: "paper/theme.css", data: encode(css), deflate: false },
      { path: "paper/fonts/serif.woff2", data: FONT_BYTES, deflate: true },
    ]);
    const result = await readZip(bytes, 1024 * 1024);
    expect(result).toMatchObject({ ok: true });
    if (!result.ok) return;
    expect(themeFromFiles(result.files).ok).toBe(true);
  });

  it("stops at the size limit and rejects what is not a zip", async () => {
    const bytes = await zip([{ path: "big.css", data: encode("x".repeat(4096)), deflate: true }]);
    expect(await readZip(bytes, 1024)).toMatchObject({ ok: false });
    expect(await readZip(encode("not a zip at all, just some text"), 1024)).toEqual({
      ok: false,
      error: "Not a zip file",
    });
  });
});

describe("installed themes", () => {
  beforeEach(() => {
    localStorage.clear();
    useInstalledThemesStore.setState({ themes: [] });
  });

  it("installs, replaces by id and uninstalls", () => {
    const parsed = parseTheme(paper);
    if (!parsed.ok) throw new Error("fixture");
    const { install, uninstall } = useInstalledThemesStore.getState();
    install(parsed.theme);
    install({ ...parsed.theme, version: "1.3.0" });
    expect(useInstalledThemesStore.getState().themes).toHaveLength(1);
    expect(findTheme("paper")?.version).toBe("1.3.0");
    uninstall("paper");
    expect(findTheme("paper")).toBeUndefined();
  });

  it("checks stored packages again when loading them", async () => {
    const stored = { ...manifest, css, assets: paper.assets };
    expect(parseStoredTheme(stored).ok).toBe(true);
    localStorage.setItem(
      INSTALLED_THEMES_STORAGE_KEY,
      JSON.stringify({
        state: { themes: [stored, { ...stored, id: "tampered", css: "body { background: url(https://evil.test); }" }] },
        version: 0,
      }),
    );
    await useInstalledThemesStore.persist.rehydrate();
    expect(useInstalledThemesStore.getState().themes.map((t) => t.id)).toEqual(["paper"]);
  });
});
