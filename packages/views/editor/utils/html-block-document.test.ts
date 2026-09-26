// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  HTML_BLOCK_MAX_CONTENT_PX,
  HTML_BLOCK_MESSAGE_KEY,
  INITIAL_HTML_BLOCK_HEIGHT,
  __HTML_BLOCK_BRIDGE__,
  buildHtmlBlockDocument,
  nextHtmlBlockHeight,
  readHtmlBlockMessage,
  readHtmlBlockTheme,
  usesThemeTokens,
} from "./html-block-document";
import { __FRAGMENT_NAV_SHIM__ } from "./iframe-fragment-nav";

const THEME = { declarations: "--foreground:black;--chart-1:blue", colorScheme: "dark" as const };

describe("buildHtmlBlockDocument", () => {
  it("puts the theme and bridge in front of the markup and the fragment shim after it", () => {
    const doc = buildHtmlBlockDocument("<p>hi</p>", THEME);
    expect(doc.startsWith("<style>:root{--foreground:black;--chart-1:blue}</style><script>")).toBe(true);
    expect(doc).toContain(__HTML_BLOCK_BRIDGE__);
    expect(doc).toContain("</script><p>hi</p>");
    expect(doc.endsWith(__FRAGMENT_NAV_SHIM__)).toBe(true);
  });

  it("keeps a leading doctype first", () => {
    const doc = buildHtmlBlockDocument("<!DOCTYPE html>\n<html><body>x</body></html>", THEME);
    expect(doc.startsWith("<!DOCTYPE html><style>")).toBe(true);
    expect(doc).toContain("</script>\n<html>");
  });

  // Error line numbers come from the srcdoc; the prefix must not shift them.
  it("adds no line breaks in front of the author's markup", () => {
    expect(__HTML_BLOCK_BRIDGE__).not.toContain("\n");
    const doc = buildHtmlBlockDocument("line1\nline2", THEME);
    expect(doc.split("\n")[1]!.startsWith("line2")).toBe(true);
  });

  it("gives the app's color scheme only to HTML that uses a theme token", () => {
    expect(buildHtmlBlockDocument('<p style="color:var(--foreground)">x</p>', THEME)).toContain(
      ";color-scheme:dark}",
    );
    expect(buildHtmlBlockDocument('<p style="color:#333">x</p>', THEME)).not.toContain(
      "color-scheme",
    );
  });
});

describe("usesThemeTokens", () => {
  it("matches whole token names only", () => {
    expect(usesThemeTokens("fill: var(--chart-2)")).toBe(true);
    expect(usesThemeTokens("color: var( --muted-foreground )")).toBe(true);
    expect(usesThemeTokens("font-family: var(--font-sans)")).toBe(true);
    expect(usesThemeTokens("border-radius: var(--border-radius)")).toBe(false);
    expect(usesThemeTokens("--foreground: red")).toBe(false);
  });
});

describe("readHtmlBlockTheme", () => {
  const styles = (values: Record<string, string>, fontFamily = "Inter, sans-serif", colorScheme = "light") => ({
    getPropertyValue: (name: string) => values[name] ?? "",
    fontFamily,
    colorScheme,
  });

  it("copies the defined tokens and the font, skipping undefined ones", () => {
    const theme = readHtmlBlockTheme(styles({ "--foreground": " oklch(0.2 0 0) ", "--chart-1": "blue" }));
    expect(theme.declarations).toBe(
      "--foreground:oklch(0.2 0 0);--chart-1:blue;--font-sans:Inter, sans-serif",
    );
  });

  it("gives --background the block's surface color", () => {
    const theme = readHtmlBlockTheme(styles({ "--background": "gray", "--surface": "white" }));
    expect(theme.declarations).toContain("--background:white;--surface:white");
    expect(readHtmlBlockTheme(styles({ "--background": "gray" })).declarations).toContain(
      "--background:gray",
    );
  });

  it("drops a value that could break out of the style rule", () => {
    const theme = readHtmlBlockTheme(styles({ "--foreground": "red}</style><script>" }));
    expect(theme.declarations).not.toContain("script");
  });

  it("reads dark only when the host is dark", () => {
    expect(readHtmlBlockTheme(styles({}, "", "dark")).colorScheme).toBe("dark");
    expect(readHtmlBlockTheme(styles({}, "", "light dark")).colorScheme).toBe("light");
    expect(readHtmlBlockTheme(styles({}, "", "normal")).colorScheme).toBe("light");
  });
});

describe("readHtmlBlockMessage", () => {
  const msg = (data: Record<string, unknown>) => ({ [HTML_BLOCK_MESSAGE_KEY]: 1, ...data });

  it("reads height and error messages", () => {
    expect(readHtmlBlockMessage(msg({ type: "height", height: 240.2 }))).toEqual({
      type: "height",
      height: 241,
    });
    expect(readHtmlBlockMessage(msg({ type: "error", message: "boom", line: 9 }))).toEqual({
      type: "error",
      message: "boom",
      line: 9,
    });
  });

  it("clamps what the sandbox sends", () => {
    expect(readHtmlBlockMessage(msg({ type: "height", height: 1e9 }))).toEqual({
      type: "height",
      height: HTML_BLOCK_MAX_CONTENT_PX,
    });
    expect(readHtmlBlockMessage(msg({ type: "height", height: -5 }))).toEqual({ type: "height", height: 0 });
    const long = readHtmlBlockMessage(msg({ type: "error", message: "x".repeat(2000), line: "9" }));
    expect(long).toEqual({ type: "error", message: "x".repeat(500), line: 0 });
  });

  it("ignores anything else", () => {
    expect(readHtmlBlockMessage({ type: "height", height: 10 })).toBeNull();
    expect(readHtmlBlockMessage(msg({ type: "height", height: "10" }))).toBeNull();
    expect(readHtmlBlockMessage(msg({ type: "resize" }))).toBeNull();
    expect(readHtmlBlockMessage("height")).toBeNull();
    expect(readHtmlBlockMessage(null)).toBeNull();
  });
});

describe("nextHtmlBlockHeight", () => {
  const run = (reports: number[]) => reports.reduce(nextHtmlBlockHeight, INITIAL_HTML_BLOCK_HEIGHT);

  it("follows the content up and down", () => {
    expect(run([40, 320]).height).toBe(320);
    expect(run([40, 320, 180]).height).toBe(180);
  });

  // A document sized in `vh` grows by the same step each time the frame
  // grows. Three equal steps in a row stop it.
  it("stops a document whose height follows the frame", () => {
    const state = run([300, 316, 332, 348, 364, 380]);
    expect(state.frozen).toBe(true);
    expect(state.height).toBe(332);
    expect(nextHtmlBlockHeight(state, 100).height).toBe(332);
  });

  it("does not stop content that grows by different amounts", () => {
    const state = run([100, 180, 300, 330, 480]);
    expect(state.frozen).toBe(false);
    expect(state.height).toBe(480);
  });
});
