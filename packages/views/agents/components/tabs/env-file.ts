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

  let value = "";
  for (let index = 1; index < rawValue.length; index += 1) {
    const character = rawValue[index];

    if (character === quote) {
      const remainder = rawValue.slice(index + 1);
      return remainder.trim() === "" || /^[\t ]+#/.test(remainder)
        ? value
        : null;
    }

    if (quote === '"' && character === "\\") {
      const nextCharacter = rawValue[index + 1];
      if (
        nextCharacter === '"' ||
        nextCharacter === "\\" ||
        nextCharacter === "$" ||
        nextCharacter === "`"
      ) {
        value += nextCharacter;
        index += 1;
        continue;
      }
    }

    if (quote === '"' && (character === "$" || character === "`")) {
      return null;
    }

    value += character;
  }

  return null;
}

function parseUnquotedValue(
  rawValue: string,
  hadLeadingWhitespace: boolean,
): string | null {
  let value = "";
  let previousCharacterWasEscaped = false;

  for (let index = 0; index < rawValue.length; index += 1) {
    const character = rawValue[index];

    if (character === "\\") {
      const nextCharacter = rawValue[index + 1];
      if (nextCharacter === undefined) return null;

      value += nextCharacter;
      previousCharacterWasEscaped = true;
      index += 1;
      continue;
    }

    if (
      character === "#" &&
      ((index === 0 && hadLeadingWhitespace) ||
        (/\s/.test(rawValue[index - 1] ?? "") && !previousCharacterWasEscaped))
    ) {
      return value.trimEnd();
    }

    if (
      character === '"' ||
      character === "'" ||
      character === "$" ||
      character === "`"
    ) {
      return null;
    }

    value += character;
    previousCharacterWasEscaped = false;
  }

  return value.trimEnd();
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
  return (
    /[\r\n]/.test(text) || ASSIGNMENT_PREFIX_PATTERN.test(text.trimStart())
  );
}

/** Parse a pasted Bash-style environment file without evaluating shell code. */
export function parseEnvFile(text: string): EnvAssignment[] | null {
  const assignments: EnvAssignment[] = [];

  for (const line of text.replace(/\r\n?/g, "\n").split("\n")) {
    const trimmedLine = line.trim();
    if (trimmedLine === "" || trimmedLine.startsWith("#")) continue;

    const match = ASSIGNMENT_PATTERN.exec(trimmedLine);
    if (!match) return null;

    const value = parseValue(match[2] ?? "");
    if (value === null) return null;

    assignments.push({ key: match[1] ?? "", value });
  }

  return assignments.length > 0 ? assignments : null;
}
