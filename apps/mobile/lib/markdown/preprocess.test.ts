import { describe, expect, it } from "vitest";
import { preprocessMobileMarkdown } from "./preprocess";

const UUID = "019f49e2-5b07-7970-beef-c0d537fb8c1d";
const ABS_URL = `https://multica-app.copilothub.ai/api/attachments/${UUID}/download`;
const REL_URL = `/api/attachments/${UUID}/download`;

describe("preprocessMobileMarkdown — !file file cards", () => {
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
