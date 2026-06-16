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

// NOTE: `fixed_args` is intentionally NOT exposed in the v1 UI. The server
// still carries the column, but the daemon does not yet splice these args into
// the agent launch command, so surfacing an input/display here would promise
// admins a behavior that does not exist. Re-introduce the parse/format helpers
// and the form field only once the daemon actually passes them to the backend
// (proven by a test). See TODO(MUL-3284) in server/internal/daemon/daemon.go.

export interface ProfileFormValues {
  displayName: string;
  commandName: string;
  description: string;
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
