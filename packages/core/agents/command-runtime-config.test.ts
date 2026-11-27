// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  commandExecutable,
  commandRuntimeConfigEquals,
  formatCommandArgsInput,
  isValidCommandToken,
  parseCommandArgsInput,
  parseCommandRuntimeConfig,
  serializeCommandRuntimeConfig,
} from "./command-runtime-config";

describe("command-runtime-config", () => {
  it("parses and serializes valid command runtime_config fields", () => {
    const raw = {
      command: "  python3 ",
      args: [" scripts/check.py ", "", "--strict"],
      working_dir: " ./checks ",
      timeout_seconds: 120,
    };

    const parsed = parseCommandRuntimeConfig(raw);
    expect(parsed).toEqual({
      command: "python3",
      args: ["scripts/check.py", "--strict"],
      workingDir: "./checks",
      timeoutSeconds: 120,
    });
    expect(commandExecutable(raw)).toBe("python3");

    expect(serializeCommandRuntimeConfig(parsed)).toEqual({
      command: "python3",
      args: ["scripts/check.py", "--strict"],
      working_dir: "./checks",
      timeout_seconds: 120,
    });
  });

  it("ignores malformed or empty inputs safely", () => {
    expect(parseCommandRuntimeConfig(null)).toEqual({});
    expect(parseCommandRuntimeConfig("invalid")).toEqual({});
    expect(parseCommandRuntimeConfig([])).toEqual({});
    expect(
      parseCommandRuntimeConfig({
        command: "   ",
        args: [123, ""],
        working_dir: "",
        timeout_seconds: -10,
      }),
    ).toEqual({});
  });

  it("validates single-token executable names", () => {
    expect(isValidCommandToken("python3")).toBe(true);
    expect(isValidCommandToken("/usr/local/bin/check-runner")).toBe(true);
    expect(isValidCommandToken("")).toBe(false);
    expect(isValidCommandToken("   ")).toBe(false);
    expect(isValidCommandToken("python3 script.py")).toBe(false);
    expect(isValidCommandToken("python3\0")).toBe(false);
  });

  it("parses and formats command argument strings with quotes and env placeholders", () => {
    const parsed = parseCommandArgsInput(
      'review --from ${MULTICA_BASE_BRANCH} --to "${MULTICA_WORK_BRANCH}"',
    );
    expect(parsed).toEqual([
      "review",
      "--from",
      "${MULTICA_BASE_BRANCH}",
      "--to",
      "${MULTICA_WORK_BRANCH}",
    ]);
    expect(formatCommandArgsInput(parsed)).toBe(
      "review --from ${MULTICA_BASE_BRANCH} --to ${MULTICA_WORK_BRANCH}",
    );
  });

  it("compares normalized configs for equality", () => {
    expect(
      commandRuntimeConfigEquals(
        { command: "python3", args: ["a.py"] },
        { command: " python3 ", args: [" a.py ", ""] },
      ),
    ).toBe(true);
    expect(
      commandRuntimeConfigEquals(
        { command: "python3", args: ["a.py"] },
        { command: "python3", args: ["b.py"] },
      ),
    ).toBe(false);
  });
});
