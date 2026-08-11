export interface EnvAssignment {
  key: string;
  value: string;
}

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

/** Parse a pasted dotenv-style file without evaluating or rewriting values. */
export function parseEnvFile(text: string): EnvAssignment[] | null {
  const assignments: EnvAssignment[] = [];
  const keys = new Set<string>();

  for (const line of text.replace(/\r\n?/g, "\n").split("\n")) {
    const trimmedLine = line.trim();
    if (trimmedLine === "" || trimmedLine.startsWith("#")) continue;

    const match = ASSIGNMENT_PATTERN.exec(trimmedLine);
    if (!match) return null;

    const value = parseValue(match[2] ?? "");
    if (value === null) return null;

    const key = match[1] ?? "";
    if (keys.has(key)) return null;

    keys.add(key);
    assignments.push({ key, value });
  }

  return assignments.length > 0 ? assignments : null;
}
