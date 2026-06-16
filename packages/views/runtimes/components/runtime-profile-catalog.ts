import {
  RUNTIME_PROFILE_PROTOCOL_FAMILIES,
  type RuntimeProfile,
  type RuntimeProtocolFamily,
} from "@multica/core/types";

// A single row in the runtimes catalog the management dialog renders: the
// built-in protocol families ship as read-only reference rows, the custom
// profiles as editable rows. They render mixed in one list, each tagged with
// its kind so the row can stamp the right badge (built-in vs custom).
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

// Re-export the whitelist as a typed array so callers (the family picker,
// the catalog builder) share the single source of truth.
export const PROTOCOL_FAMILIES: readonly RuntimeProtocolFamily[] =
  RUNTIME_PROFILE_PROTOCOL_FAMILIES;

// buildRuntimeCatalog produces the mixed, flat list: every built-in family
// first (in whitelist order), then the custom profiles (alphabetical by
// display name, case-insensitive). No grouping / headers — the row badge is
// the only built-in-vs-custom signal, matching the locked progressive-
// disclosure design.
export function buildRuntimeCatalog(
  profiles: RuntimeProfile[],
): RuntimeCatalogEntry[] {
  const builtins: RuntimeCatalogEntry[] = PROTOCOL_FAMILIES.map((family) => ({
    kind: "builtin" as const,
    id: `builtin:${family}`,
    protocolFamily: family,
  }));

  const customs: RuntimeCatalogEntry[] = [...profiles]
    .sort((a, b) =>
      a.display_name.localeCompare(b.display_name, undefined, {
        sensitivity: "base",
      }),
    )
    .map((profile) => ({
      kind: "custom" as const,
      id: profile.id,
      protocolFamily: profile.protocol_family,
      profile,
    }));

  return [...builtins, ...customs];
}

// Splits a multi-line textarea value into the `fixed_args` string array —
// one arg per non-blank line, trimmed. Returns `undefined` when there are no
// args so the caller can omit the key entirely (the server then applies its
// own default), per the brief's "if omitted, do not send the key".
export function parseFixedArgs(raw: string): string[] | undefined {
  const args = raw
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
  return args.length > 0 ? args : undefined;
}

// Joins a `fixed_args` array back into the textarea value for the edit form.
export function fixedArgsToText(args: string[] | null | undefined): string {
  return (args ?? []).join("\n");
}

export interface ProfileFormValues {
  displayName: string;
  commandName: string;
  description: string;
  fixedArgs: string;
}

export type ProfileFormErrorField = "displayName" | "commandName";

// Pure, synchronous validation for the create/edit form. Returns the set of
// invalid fields (empty = valid). Display name and command name are the only
// hard-required fields; description and fixed args are optional.
export function validateProfileForm(
  values: ProfileFormValues,
): ProfileFormErrorField[] {
  const errors: ProfileFormErrorField[] = [];
  if (!values.displayName.trim()) errors.push("displayName");
  if (!values.commandName.trim()) errors.push("commandName");
  return errors;
}

// Returns true when the entry should be treated as a built-in (read-only).
export function isBuiltinEntry(entry: RuntimeCatalogEntry): boolean {
  return entry.kind === "builtin";
}
