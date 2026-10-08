export interface EnvAssignment {
  key: string;
  value: string;
}

/**
 * Why the line number matters: a paste or a bulk edit can carry dozens of
 * lines, and "couldn't parse" without a location leaves the user guessing.
 * `line` is 1-based and counts every physical line, including the blanks and
 * comments the parser skips, so it matches what the editor shows.
 */
export type EnvParseError =
  | { kind: "malformed"; line: number }
  | { kind: "duplicate"; line: number; key: string };

export type EnvParseResult =
  | { ok: true; assignments: EnvAssignment[] }
  | { ok: false; error: EnvParseError };

const ASSIGNMENT_PATTERN =
  /^(?:export[\t ]+)?([A-Za-z_][A-Za-z0-9_]*)[\t ]*=(.*)$/;
const ASSIGNMENT_PREFIX_PATTERN =
  /^(?:export[\t ]+)?[A-Za-z_][A-Za-z0-9_]*[\t ]*=/;

function parseQuotedValue(rawValue: string): string | null {
  const quote = rawValue[0];
  if (quote !== '"' && quote !== "'") return null;

  for (let index = 1; index < rawValue.length; index += 1) {
    if (rawValue[index] === quote) {
      const remainder = rawValue.slice(index + 1);
      if (remainder.trim() === "" || /^[\t ]+#/.test(remainder)) {
        return rawValue.slice(1, index);
      }
    }
  }

  return null;
}

function parseUnquotedValue(
  rawValue: string,
  hadLeadingWhitespace: boolean,
): string {
  for (let index = 0; index < rawValue.length; index += 1) {
    const character = rawValue[index];

    if (
      character === "#" &&
      ((index === 0 && hadLeadingWhitespace) ||
        /\s/.test(rawValue[index - 1] ?? ""))
    ) {
      return rawValue.slice(0, index).trimEnd();
    }
  }

  return rawValue.trimEnd();
}

function parseValue(rawValue: string): string | null {
  const hadLeadingWhitespace = /^[\t ]/.test(rawValue);
  const value = rawValue.trimStart();
  if (value.startsWith('"') || value.startsWith("'")) {
    return parseQuotedValue(value);
  }

  return parseUnquotedValue(value, hadLeadingWhitespace);
}

export function isEnvFilePaste(text: string): boolean {
  const trimmedText = text.trim();
  if (!trimmedText) return false;

  const meaningfulLines = trimmedText
    .replace(/\r\n?/g, "\n")
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "" && !line.startsWith("#"));

  return meaningfulLines.some((line) => ASSIGNMENT_PREFIX_PATTERN.test(line));
}

/**
 * Parse a dotenv-style file without evaluating or rewriting values, reporting
 * where the first bad line is. An input with no assignments at all is a
 * success with an empty list — that is how bulk editing expresses "clear
 * everything". Callers that need "this isn't an env file" should use
 * `parseEnvFile`.
 */
export function parseEnvFileResult(text: string): EnvParseResult {
  const assignments: EnvAssignment[] = [];
  const keys = new Set<string>();
  const lines = text.replace(/\r\n?/g, "\n").split("\n");

  for (const [index, line] of lines.entries()) {
    const trimmedLine = line.trim();
    if (trimmedLine === "" || trimmedLine.startsWith("#")) continue;

    const lineNumber = index + 1;
    const match = ASSIGNMENT_PATTERN.exec(trimmedLine);
    if (!match) {
      return { ok: false, error: { kind: "malformed", line: lineNumber } };
    }

    const value = parseValue(match[2] ?? "");
    if (value === null) {
      return { ok: false, error: { kind: "malformed", line: lineNumber } };
    }

    const key = match[1] ?? "";
    if (keys.has(key)) {
      return { ok: false, error: { kind: "duplicate", line: lineNumber, key } };
    }

    keys.add(key);
    assignments.push({ key, value });
  }

  return { ok: true, assignments };
}

/** Parse a pasted dotenv-style file without evaluating or rewriting values. */
export function parseEnvFile(text: string): EnvAssignment[] | null {
  const result = parseEnvFileResult(text);
  if (!result.ok || result.assignments.length === 0) return null;
  return result.assignments;
}

export type EnvFormatResult =
  | { ok: true; text: string }
  | { ok: false; reason: "unrepresentable" | "duplicate"; key: string };

const KEY_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;

/**
 * Pick an encoding by round-tripping each candidate through the very parser
 * that will read it back, instead of reasoning about which characters are
 * dangerous — that reasoning is what previously turned `foo" #bar` into `foo`.
 *
 * Some values have no representation at all: an embedded newline (the parser
 * splits on those first) or a value that closes early under both quote styles,
 * such as `a" #b' #c`. The server accepts those, so they must be reported
 * rather than mangled into something that merely looks editable.
 */
function encodeValue(key: string, value: string): string | null {
  if (/[\r\n]/.test(value)) return null;

  for (const candidate of [value, `"${value}"`, `'${value}'`]) {
    const result = parseEnvFileResult(`${key}=${candidate}`);
    if (result.ok && result.assignments[0]?.value === value) return candidate;
  }

  return null;
}

/**
 * Serialize entries back into text `parseEnvFileResult` reads identically, or
 * name the first entry that cannot be represented so the caller can refuse to
 * enter bulk editing without touching any data.
 *
 * Success means the whole text round-trips, which is why duplicate keys are a
 * refusal rather than a formatting detail: two entries sharing a key can only
 * ever read back as one, so no text could satisfy the contract.
 */
export function formatEnvFile(assignments: EnvAssignment[]): EnvFormatResult {
  const lines: string[] = [];
  const keys = new Set<string>();

  for (const { key, value } of assignments) {
    if (!KEY_PATTERN.test(key)) {
      return { ok: false, reason: "unrepresentable", key };
    }
    if (keys.has(key)) return { ok: false, reason: "duplicate", key };

    const encoded = encodeValue(key, value);
    if (encoded === null) {
      return { ok: false, reason: "unrepresentable", key };
    }

    keys.add(key);
    lines.push(`${key}=${encoded}`);
  }

  return { ok: true, text: lines.join("\n") };
}
