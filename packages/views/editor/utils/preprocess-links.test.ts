import { describe, expect, it } from "vitest";
import { preprocessLinks } from "@multica/ui/markdown/linkify";

// The bug: linkify-it does not treat CJK full-width punctuation as a URL
// boundary, so the href can swallow trailing punctuation and the Chinese
// characters that follow it (up to the next space). The fix truncates the
// detected URL at the first CJK full-width punctuation character.

describe("preprocessLinks — CJK punctuation boundary", () => {
  it("stops URL at ideographic full stop 。", () => {
    const out = preprocessLinks("见 https://example.com/path。然后继续");
    expect(out).toBe("见 [https://example.com/path](https://example.com/path)。然后继续");
  });

  it("stops URL at fullwidth comma ，", () => {
    const out = preprocessLinks("打开 https://example.com/a，以及其他");
    expect(out).toBe("打开 [https://example.com/a](https://example.com/a)，以及其他");
  });

  it("stops URL at ideographic comma 、", () => {
    const out = preprocessLinks("两个地址 https://a.com/x、https://b.com/y");
    expect(out).toBe(
      "两个地址 [https://a.com/x](https://a.com/x)、[https://b.com/y](https://b.com/y)",
    );
  });

  it("stops URL at fullwidth right paren ）", () => {
    const out = preprocessLinks("（见 https://example.com/x）后文");
    expect(out).toBe("（见 [https://example.com/x](https://example.com/x)）后文");
  });

  it("stops URL at corner bracket 」", () => {
    const out = preprocessLinks("「https://example.com/a」后文");
    expect(out).toBe("「[https://example.com/a](https://example.com/a)」后文");
  });

  it("stops URL at fullwidth exclamation ！", () => {
    const out = preprocessLinks("太好了 https://example.com/x！继续");
    expect(out).toBe("太好了 [https://example.com/x](https://example.com/x)！继续");
  });

  it("handles the original bug report (PR link then 。 then more text)", () => {
    const out = preprocessLinks(
      "已合并 PR #1623：https://github.com/multica-ai/multica/pull/1623。merge commit",
    );
    expect(out).toBe(
      "已合并 PR #1623：[https://github.com/multica-ai/multica/pull/1623](https://github.com/multica-ai/multica/pull/1623)。merge commit",
    );
  });

  it("does not swallow the entire remainder when there is no trailing space", () => {
    const out = preprocessLinks("https://github.com/x/y/issues/1619。我接下来把这个");
    expect(out).toBe(
      "[https://github.com/x/y/issues/1619](https://github.com/x/y/issues/1619)。我接下来把这个",
    );
  });

  it("preserves ASCII trailing period handling (no regression)", () => {
    const out = preprocessLinks("visit https://example.com/path. next.");
    expect(out).toBe("visit [https://example.com/path](https://example.com/path). next.");
  });

  it("preserves plain URL with no trailing punctuation (no regression)", () => {
    const out = preprocessLinks("go https://example.com/path");
    expect(out).toBe("go [https://example.com/path](https://example.com/path)");
  });

  it("preserves CJK letters inside URL path (only trims on punctuation)", () => {
    const out = preprocessLinks("https://zh.wikipedia.org/wiki/中国 参考");
    expect(out).toBe(
      "[https://zh.wikipedia.org/wiki/中国](https://zh.wikipedia.org/wiki/中国) 参考",
    );
  });

  it("does not re-link an already-linked URL that contains 。", () => {
    // If a user or upstream already wrote [text](url。), we leave it alone.
    const input = "见 [link](https://example.com/x。)后文";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("does not linkify fuzzy domains inside existing markdown link labels", () => {
    const input =
      "数据来源：[NBA.com Schedule](https://www.nba.com/schedule)、[NBC Insider](https://www.nbc.com/nbc-insider/every-nba-playoff-game-this-week-on-nbc-peacock-april-25-28)";

    expect(preprocessLinks(input)).toBe(input);
  });

  it("still linkifies fuzzy domains outside existing markdown links", () => {
    const input = "数据来源：[NBA.com Schedule](https://www.nba.com/schedule)，官网 NBA.com";

    expect(preprocessLinks(input)).toBe(
      "数据来源：[NBA.com Schedule](https://www.nba.com/schedule)，官网 [NBA.com](http://NBA.com)",
    );
  });
});

// The bug (#4222): an agent mentions a project file like `plan.md` in a comment.
// linkify-it fuzzy-matches it as the domain `plan.md` (md is Moldova's ccTLD) and
// turns it into a clickable https://plan.md link that goes nowhere. Bare filename
// tokens must stay plain text — only an explicit scheme makes them a link.
describe("preprocessLinks — bare filenames are not auto-linked as URLs", () => {
  it("leaves a bare .md filename in CJK prose as plain text", () => {
    const out = preprocessLinks("决策已锁定，plan.md 已更新");
    expect(out).toBe("决策已锁定，plan.md 已更新");
  });

  it("leaves README.md as plain text", () => {
    expect(preprocessLinks("see README.md for details")).toBe("see README.md for details");
  });

  it("leaves other extensions that collide with TLDs (sh, rs, py) as plain text", () => {
    expect(preprocessLinks("run build.sh then main.rs and app.py")).toBe(
      "run build.sh then main.rs and app.py",
    );
  });

  it("honors an explicit scheme on a filename-shaped host", () => {
    expect(preprocessLinks("open https://plan.md now")).toBe(
      "open [https://plan.md](https://plan.md) now",
    );
  });

  it("still linkifies real fuzzy domains whose TLD is not a file extension", () => {
    expect(preprocessLinks("官网 NBA.com")).toBe("官网 [NBA.com](http://NBA.com)");
  });

  it("suppresses the bare filename but still linkifies a real domain after it", () => {
    expect(preprocessLinks("plan.md，example.com")).toBe(
      "plan.md，[example.com](http://example.com)",
    );
  });

  it("still detects explicit ./ file paths (FILE_PATH_REGEX regression)", () => {
    expect(preprocessLinks("see ./src/main.go here")).toBe(
      "see [./src/main.go](./src/main.go) here",
    );
  });
});

// Trailing markdown emphasis / strikethrough delimiters that linkify-it counts
// as URL characters must be dropped from the URL, so the closing `**` of
// `**url**` stays as emphasis instead of being swallowed into the href — that
// swallow was the MUL-4242 render bug. Mirrors GFM's own autolink trailing trim.
describe("preprocessLinks — trailing markdown delimiter is not part of the URL", () => {
  it("keeps the closing ** outside a bold-wrapped bare URL", () => {
    expect(preprocessLinks("**https://example.com/x**")).toBe(
      "**[https://example.com/x](https://example.com/x)**",
    );
  });

  it("handles the original bug shape (bold + fullwidth colon)", () => {
    expect(preprocessLinks("**PR：https://example.com/x**")).toBe(
      "**PR：[https://example.com/x](https://example.com/x)**",
    );
  });

  it("keeps ** outside when a CJK punctuation immediately follows (variant B)", () => {
    expect(preprocessLinks("**https://example.com/x**（MUL-4277）")).toBe(
      "**[https://example.com/x](https://example.com/x)**（MUL-4277）",
    );
  });

  it("keeps the closing * outside an italic-wrapped bare URL", () => {
    expect(preprocessLinks("*https://example.com/x*")).toBe(
      "*[https://example.com/x](https://example.com/x)*",
    );
  });

  it("strips a trailing * that really ends a URL — documented tradeoff, matches GFM", () => {
    expect(preprocessLinks("see https://example.com/glob/* here")).toBe(
      "see [https://example.com/glob/](https://example.com/glob/)* here",
    );
  });

  it("keeps a * in the middle of a URL", () => {
    expect(preprocessLinks("https://example.com/a*b/c")).toBe(
      "[https://example.com/a*b/c](https://example.com/a*b/c)",
    );
  });
});
