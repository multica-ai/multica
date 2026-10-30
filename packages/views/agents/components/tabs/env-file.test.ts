// @vitest-environment node
import { describe, expect, it } from "vitest";

import type { EnvAssignment } from "./env-file";
import {
  formatEnvFile,
  isEnvFilePaste,
  parseEnvFile,
  parseEnvFileResult,
} from "./env-file";

describe("parseEnvFile", () => {
  it("parses dotenv-style assignments while preserving literal values", () => {
    expect(
      parseEnvFile(String.raw`
# Agent credentials
export API_KEY="secret value"
EMPTY=
URL=https://example.com/path?first=one&second=two
HASH='value # kept'
PLAIN=value # ignored comment
WIN_DIR=C:\Users\me\.ssh
PASSWORD=p$ss${"`"}word
JSON={"a":1}
QUOTED='say "hello" with $HOME and C:\path'
`),
    ).toEqual([
      { key: "API_KEY", value: "secret value" },
      { key: "EMPTY", value: "" },
      {
        key: "URL",
        value: "https://example.com/path?first=one&second=two",
      },
      { key: "HASH", value: "value # kept" },
      { key: "PLAIN", value: "value" },
      { key: "WIN_DIR", value: String.raw`C:\Users\me\.ssh` },
      { key: "PASSWORD", value: "p$ss`word" },
      { key: "JSON", value: '{"a":1}' },
      { key: "QUOTED", value: String.raw`say "hello" with $HOME and C:\path` },
    ]);
  });

  it("supports Windows line endings", () => {
    expect(parseEnvFile("FIRST=one\r\nSECOND=two\r\n")).toEqual([
      { key: "FIRST", value: "one" },
      { key: "SECOND", value: "two" },
    ]);
  });

  it("rejects partial files instead of silently dropping unsupported lines", () => {
    expect(parseEnvFile("FIRST=one\necho hello\nSECOND=two")).toBeNull();
    expect(parseEnvFile('BROKEN="unterminated')).toBeNull();
    expect(parseEnvFile('TOKEN="abc"#suffix')).toBeNull();
    expect(parseEnvFile("FIRST=one\nFIRST=two")).toBeNull();
  });

  it("does not treat ordinary key text as an environment file", () => {
    expect(parseEnvFile("API_KEY")).toBeNull();
    expect(isEnvFilePaste("API_KEY")).toBe(false);
    expect(isEnvFilePaste("API_KEY\n")).toBe(false);
    expect(isEnvFilePaste("# database settings\n")).toBe(false);
    expect(isEnvFilePaste("API_KEY=value")).toBe(true);
    expect(isEnvFilePaste("FIRST=one\nSECOND=two")).toBe(true);
  });
});

describe("parseEnvFileResult", () => {
  it("reports the physical line of the first malformed line", () => {
    expect(parseEnvFileResult("# comment\n\nFIRST=one\necho hello")).toEqual({
      ok: false,
      error: { kind: "malformed", line: 4 },
    });
    expect(parseEnvFileResult('BROKEN="unterminated')).toEqual({
      ok: false,
      error: { kind: "malformed", line: 1 },
    });
  });

  it("reports which key was duplicated and where", () => {
    expect(parseEnvFileResult("FIRST=one\nSECOND=two\nFIRST=three")).toEqual({
      ok: false,
      error: { kind: "duplicate", line: 3, key: "FIRST" },
    });
  });

  it("treats an assignment-free input as an empty success", () => {
    expect(parseEnvFileResult("")).toEqual({ ok: true, assignments: [] });
    expect(parseEnvFileResult("# only a comment\n")).toEqual({
      ok: true,
      assignments: [],
    });
    // parseEnvFile keeps the stricter "this is not an env file" contract the
    // paste path depends on.
    expect(parseEnvFile("# only a comment\n")).toBeNull();
  });
});

describe("formatEnvFile", () => {
  // The invariant that matters: anything the API accepts and this serializer
  // agrees to render must read back byte-identical. Reasoning about "which
  // characters are dangerous" is what previously truncated `foo" #bar`.
  const ROUND_TRIP_VALUES = [
    ["plain", "value"],
    ["empty", ""],
    ["windows path", String.raw`C:\Users\me\.ssh`],
    ["dollar and backtick", "p$ss`word"],
    ["json", '{"a":1}'],
    ["padded", "  spaced  "],
    ["inline hash", "value # kept"],
    ["leading hash", "#not-a-comment"],
    ["wrapped in quotes", '"quoted"'],
    ["apostrophe", "it's"],
    ["double quote then hash", 'foo" #bar'],
    ["single quote then hash", "foo' #bar"],
    ["both quote styles", `a"b'c`],
    ["only whitespace", "   "],
    ["equals signs", "a=b=c"],
    ["trailing backslash", "C:\\path\\"],
  ] as const;

  it.each(ROUND_TRIP_VALUES)("round-trips %s", (_label, value) => {
    const assignments = [{ key: "K", value }];
    const formatted = formatEnvFile(assignments);
    expect(formatted.ok).toBe(true);
    if (!formatted.ok) return;
    expect(parseEnvFileResult(formatted.text)).toEqual({
      ok: true,
      assignments,
    });
  });

  it("round-trips a whole file at once", () => {
    const assignments = ROUND_TRIP_VALUES.map(([label, value], index) => ({
      key: `K${index}_${label.replace(/[^a-z]/gi, "")}`,
      value,
    }));
    const formatted = formatEnvFile(assignments);
    expect(formatted.ok).toBe(true);
    if (!formatted.ok) return;
    expect(parseEnvFileResult(formatted.text)).toEqual({
      ok: true,
      assignments,
    });
  });

  it("refuses values it cannot represent instead of mangling them", () => {
    // A real newline: the parser splits on those before it ever sees a value,
    // so there is no encoding. The server does accept these (PEM keys).
    expect(formatEnvFile([{ key: "PEM", value: "line1\nline2" }])).toEqual({
      ok: false,
      reason: "unrepresentable",
      key: "PEM",
    });
    // Closes early under both quote styles.
    expect(formatEnvFile([{ key: "TRICKY", value: `a" #b' #c` }])).toEqual({
      ok: false,
      reason: "unrepresentable",
      key: "TRICKY",
    });
  });

  it("refuses keys the parser could not read back", () => {
    expect(formatEnvFile([{ key: "foo-bar", value: "x" }])).toEqual({
      ok: false,
      reason: "unrepresentable",
      key: "foo-bar",
    });
  });

  it("refuses duplicate keys, which no text could round-trip", () => {
    expect(
      formatEnvFile([
        { key: "A", value: "1" },
        { key: "B", value: "2" },
        { key: "A", value: "3" },
      ]),
    ).toEqual({ ok: false, reason: "duplicate", key: "A" });
  });

  it("guarantees that success means the whole text round-trips", () => {
    // The contract in one assertion: whatever formatEnvFile agrees to render
    // must parse back to exactly what went in — including the inputs it is
    // allowed to reject, which must reject rather than render something lossy.
    const inputs: EnvAssignment[][] = [
      [{ key: "A", value: "1" }],
      [{ key: "PEM", value: "line1\nline2" }],
      [{ key: "TRICKY", value: `a" #b' #c` }],
      [{ key: "QUOTED", value: 'foo" #bar' }],
      [{ key: "foo-bar", value: "x" }],
      [
        { key: "A", value: "1" },
        { key: "A", value: "2" },
      ],
      ROUND_TRIP_VALUES.map(([label, value], index) => ({
        key: `R${index}_${label.replace(/[^a-z]/gi, "")}`,
        value,
      })),
    ];

    for (const assignments of inputs) {
      const formatted = formatEnvFile(assignments);
      if (!formatted.ok) continue;
      expect(parseEnvFileResult(formatted.text)).toEqual({
        ok: true,
        assignments,
      });
    }
  });

  it("leaves values bare unless quoting is required", () => {
    expect(
      formatEnvFile([
        { key: "A", value: "plain" },
        { key: "B", value: "" },
        { key: "C", value: "  padded" },
      ]),
    ).toEqual({ ok: true, text: 'A=plain\nB=\nC="  padded"' });
  });
});
