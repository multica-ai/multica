import {
  RUNTIME_PROFILE_PROTOCOL_FAMILIES,
  type RuntimeProfile,
  type RuntimeProtocolFamily,
} from "@multica/core/types";

// A single row in the runtimes catalog the management dialog renders: the
// built-in protocol families ship as read-only reference rows, while custom
// profiles are the user's editable assets.
export type RuntimeCatalogEntry =
  | {
      kind: "builtin";
      // Stable row id — the protocol family doubles as the key for built-ins.
      id: string;
      protocolFamily: RuntimeProtocolFamily;
    }
  | {
      kind: "custom";
      id: string;
      protocolFamily: RuntimeProtocolFamily;
      profile: RuntimeProfile;
    };

export interface RuntimeCatalogSections {
  customs: RuntimeCatalogEntry[];
  builtins: RuntimeCatalogEntry[];
}

// Re-export the whitelist as a typed array so callers (the family picker,
// the catalog builder) share the single source of truth.
export const PROTOCOL_FAMILIES: readonly RuntimeProtocolFamily[] =
  RUNTIME_PROFILE_PROTOCOL_FAMILIES;

// buildRuntimeCatalog keeps user-owned custom profiles separate from built-in
// protocol families. The dialog renders customs as the primary management
// surface and built-ins as a collapsed reference section.
export function buildRuntimeCatalog(
  profiles: RuntimeProfile[],
): RuntimeCatalogSections {
  const builtins: RuntimeCatalogEntry[] = PROTOCOL_FAMILIES.map((family) => ({
    kind: "builtin" as const,
    id: `builtin:${family}`,
    protocolFamily: family,
  }));

  const customs: RuntimeCatalogEntry[] = [...profiles]
    .sort((a, b) => {
      if (a.enabled !== b.enabled) return a.enabled ? -1 : 1;
      const aTime = Date.parse(a.updated_at) || 0;
      const bTime = Date.parse(b.updated_at) || 0;
      if (aTime !== bTime) return bTime - aTime;
      return a.display_name.localeCompare(b.display_name, undefined, {
        sensitivity: "base",
      });
    })
    .map((profile) => ({
      kind: "custom" as const,
      id: profile.id,
      protocolFamily: profile.protocol_family,
      profile,
    }));

  return { customs, builtins };
}

export interface ProfileFormValues {
  displayName: string;
  commandLine: string;
  description: string;
}

export type ProfileFormErrorField = "displayName" | "commandLine";

export type CommandLineParseError =
  | "empty"
  | "unclosed_quote"
  | "trailing_escape"
  | "shell_syntax"
  | "shell_expansion";

export type ParsedCommandLine =
  | { ok: true; commandName: string; fixedArgs: string[] }
  | { ok: false; error: CommandLineParseError };

const SHELL_CONTROL_CHARS = new Set(["|", ">", "<", ";", "&"]);

export function parseCommandLine(input: string): ParsedCommandLine {
  const line = input.trim();
  if (!line) return { ok: false, error: "empty" };

  const tokens: string[] = [];
  let token = "";
  let quote: "'" | '"' | null = null;
  let tokenStarted = false;

  for (let i = 0; i < line.length; i += 1) {
    const ch = line[i] ?? "";
    const next = line[i + 1] ?? "";

    if (quote == null && /\s/.test(ch)) {
      if (tokenStarted) {
        tokens.push(token);
        token = "";
        tokenStarted = false;
      }
      continue;
    }

    if (quote == null) {
      if (ch === "`" || ch === "$") {
        return {
          ok: false,
          error: ch === "$" ? "shell_expansion" : "shell_syntax",
        };
      }
      if (SHELL_CONTROL_CHARS.has(ch)) {
        return { ok: false, error: "shell_syntax" };
      }
      if (ch === "\\" && next) {
        token += next;
        tokenStarted = true;
        i += 1;
        continue;
      }
      if (ch === "\\") {
        return { ok: false, error: "trailing_escape" };
      }
      if (ch === "'" || ch === '"') {
        quote = ch;
        tokenStarted = true;
        continue;
      }
      token += ch;
      tokenStarted = true;
      continue;
    }

    if (ch === quote) {
      quote = null;
      tokenStarted = true;
      continue;
    }
    if (quote === '"' && ch === "\\" && next) {
      token += next;
      tokenStarted = true;
      i += 1;
      continue;
    }
    if (quote === '"' && ch === "\\") {
      return { ok: false, error: "trailing_escape" };
    }
    if (quote !== "'" && (ch === "`" || ch === "$")) {
      return {
        ok: false,
        error: ch === "$" ? "shell_expansion" : "shell_syntax",
      };
    }
    token += ch;
    tokenStarted = true;
  }

  if (quote != null) return { ok: false, error: "unclosed_quote" };
  if (tokenStarted) tokens.push(token);
  if (tokens.length === 0 || !tokens[0]?.trim()) {
    return { ok: false, error: "empty" };
  }

  return { ok: true, commandName: tokens[0], fixedArgs: tokens.slice(1) };
}

export function formatCommandLine(commandName: string, fixedArgs: string[]): string {
  return [commandName, ...fixedArgs].filter(Boolean).map(quoteArg).join(" ");
}

function quoteArg(arg: string): string {
  if (arg === "") return '""';
  if (!/[\s"'\\|<>;&`$]/.test(arg)) return arg;
  return `"${arg.replace(/(["\\$`])/g, "\\$1")}"`;
}

// Pure, synchronous validation for the create/edit form. Returns the set of
// invalid fields (empty = valid). Display name and command name are the only
// hard-required fields; description and fixed args are optional.
export function validateProfileForm(
  values: ProfileFormValues,
): ProfileFormErrorField[] {
  const errors: ProfileFormErrorField[] = [];
  if (!values.displayName.trim()) errors.push("displayName");
  if (!values.commandLine.trim()) errors.push("commandLine");
  return errors;
}

// Returns true when the entry should be treated as a built-in (read-only).
export function isBuiltinEntry(entry: RuntimeCatalogEntry): boolean {
  return entry.kind === "builtin";
}
