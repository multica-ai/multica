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
});
