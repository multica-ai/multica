import { describe, expect, it } from "vitest";

import { isEnvFilePaste, parseEnvFile } from "./env-file";

describe("parseEnvFile", () => {
  it("parses Bash-style assignments while preserving values", () => {
    expect(
      parseEnvFile(String.raw`
# Agent credentials
export API_KEY="secret value"
EMPTY=
URL=https://example.com/path?first=one&second=two
HASH='value # kept'
PLAIN=value # ignored comment
ESCAPED=hello\ world
ESCAPED_HASH=value\ # kept
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
      { key: "ESCAPED", value: "hello world" },
      { key: "ESCAPED_HASH", value: "value # kept" },
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
    expect(parseEnvFile('EXPANDED="$HOME"')).toBeNull();
  });

  it("does not treat ordinary key text as an environment file", () => {
    expect(parseEnvFile("API_KEY")).toBeNull();
    expect(isEnvFilePaste("API_KEY")).toBe(false);
    expect(isEnvFilePaste("API_KEY=value")).toBe(true);
    expect(isEnvFilePaste("FIRST=one\nSECOND=two")).toBe(true);
  });
});
