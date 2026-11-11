import { describe, expect, it } from "vitest";
import { preprocessMobileMarkdown } from "./preprocess";
import { splitMarkdown } from "./split-markdown";

const UUID = "019f49e2-5b07-7970-beef-c0d537fb8c1d";
const ABS_URL = `https://multica-app.copilothub.ai/api/attachments/${UUID}/download`;
const REL_URL = `/api/attachments/${UUID}/download`;

describe("preprocessMobileMarkdown — !file file cards", () => {
  it("keeps channel images visible while hiding their provenance marker", () => {
    const image = `![](${REL_URL})`;
    const marker = `<!-- multica:channel-media:${UUID} -->`;

    expect(preprocessMobileMarkdown(`${image}\n\n${marker}`)).toBe(`${image}\n\n`);
  });

  it("matches the CLI's escaped-bracket label and keeps it markdown-safe", () => {
    // CLI emits `a]b.pdf` escaped as `a\]b.pdf` (cmd_attachment.go
    // escapeMarkdownLabel). The old regex stopped at the first `]` and left the
    // line literal; now it is captured whole and re-emitted as a tappable link
    // whose label stays escaped so the `]` doesn't truncate the link text.
    const out = preprocessMobileMarkdown(`!file[a\\]b.pdf](${ABS_URL})`);
    expect(out).toBe(`[📎 a\\]b.pdf](${ABS_URL})`);
  });

  it("unescapes non-breaking metacharacters (parens) in the displayed name", () => {
    // Parens are legal inside a markdown link label, so they are unescaped for
    // display and not re-escaped.
    const out = preprocessMobileMarkdown(`!file[report\\(1\\).pdf](${ABS_URL})`);
    expect(out).toBe(`[📎 report(1).pdf](${ABS_URL})`);
  });

  it("unescapes an escaped backslash in the label", () => {
    const out = preprocessMobileMarkdown(`!file[a\\\\b.pdf](${ABS_URL})`);
    expect(out).toBe(`[📎 a\\\\b.pdf](${ABS_URL})`);
  });

  it("renders a plain (unescaped) label", () => {
    const out = preprocessMobileMarkdown(`!file[notes.txt](${ABS_URL})`);
    expect(out).toBe(`[📎 notes.txt](${ABS_URL})`);
  });

  it("accepts the site-relative /api/attachments URL form (web parity)", () => {
    const out = preprocessMobileMarkdown(`!file[a.pdf](${REL_URL})`);
    expect(out).toBe(`[📎 a.pdf](${REL_URL})`);
  });

  it("leaves a disallowed-scheme URL as plain text (no out-of-band navigation)", () => {
    const line = `!file[x.pdf](javascript:alert(1))`;
    expect(preprocessMobileMarkdown(line)).toBe(line);
  });

  it("does not touch inline images (![...] is not a file card)", () => {
    const line = `![chart.png](${ABS_URL})`;
    expect(preprocessMobileMarkdown(line)).toBe(line);
  });

  it("only transforms the standalone file-card line, leaving surrounding text", () => {
    const input = `here is the file\n\n!file[a\\]b.pdf](${ABS_URL})\n\nlet me know`;
    const output = `here is the file\n\n[📎 a\\]b.pdf](${ABS_URL})\n\nlet me know`;
    expect(preprocessMobileMarkdown(input)).toBe(output);
  });
});

describe("preprocessMobileMarkdown — angle-bracket image destinations", () => {
  it.each([
    "![image](<https://cdn.example/a b.png>)",
    "![image](</uploads/a b.png>)",
    "![image](</chart v2.png>)",
    "![image][shot]\n\n[shot]: <https://cdn.example/a b.png>",
  ])("preserves %s", (content) => {
    expect(preprocessMobileMarkdown(content)).toBe(content);
  });

  it("still strips HTML tags while preserving text and hard breaks", () => {
    expect(preprocessMobileMarkdown('<div class="note">A<br/><b>B</b></div>'))
      .toBe("A  \nB");
  });
});

describe("preprocessMobileMarkdown — HTML images", () => {
  it("renders an HTML <img> as an image segment", () => {
    const out = preprocessMobileMarkdown(
      'Text\n\n<img src="https://host/a.png" width="400" />\n\nEnd',
    );
    expect(out).toBe("Text\n\n![](https://host/a.png)\n\nEnd");
    expect(splitMarkdown(out)).toEqual([
      { type: "prose", content: "Text" },
      { type: "image", uri: "https://host/a.png", alt: "" },
      { type: "prose", content: "End" },
    ]);
  });

  it.each([
    ['<img alt="a [b]" src=\'https://host/a.png\'>', "![a \\[b\\]](https://host/a.png)"],
    ["<IMG SRC=https://host/a.png ALT=chart>", "![chart](https://host/a.png)"],
    ['<img\n  src="https://host/a.png"\n  alt="multi\nline"\n/>', "![multi line](https://host/a.png)"],
    ['<img src="https://host/a(1).png">', "![](<https://host/a(1).png>)"],
    ['<p align="center"><a href="https://host/a.png"><img src="https://host/a.png"></a></p>', "![](https://host/a.png)"],
  ])("rewrites %j", (input, expected) => {
    expect(preprocessMobileMarkdown(input)).toBe(expected);
  });

  it.each([
    '<img src="javascript:alert(1)">',
    '<img src="data:image/png;base64,AAAA">',
    '<img src="/uploads/a.png">',
    '<img alt="no source">',
  ])("drops %j without an http(s) source", (input) => {
    expect(preprocessMobileMarkdown(input)).toBe("");
  });

  it.each([
    'Use `<img src="https://host/a.png">` here',
    '```html\n<img src="https://host/a.png" width="400" />\n<br>\n```',
    '- item\n\n  ~~~\n  <img src="https://host/a.png">\n  ~~~',
    '    <img src="https://host/a.png">',
  ])("keeps HTML inside code literal: %j", (input) => {
    expect(preprocessMobileMarkdown(input)).toBe(input);
  });

  it("rewrites images around code without touching the code", () => {
    const input = '<img src="https://host/a.png">\n\n`<img src="https://host/b.png">` <b>x</b>';
    expect(preprocessMobileMarkdown(input))
      .toBe('![](https://host/a.png)\n\n`<img src="https://host/b.png">` x');
  });
});
