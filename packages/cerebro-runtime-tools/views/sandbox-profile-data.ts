// Frontend data layer for the FIR-2230 phase-6 sandbox isolation profile picker
// on the runtime page. The picker is the OUTER WALL (how locked-down the machine
// is); the per-tool permission table is the rules inside it.
//
// Two reads, one write, all through the generic cerebro request primitive so no
// bespoke method lands in the upstream api client:
//   - the profile catalog        GET  /api/workspaces/{ws}/sandbox-profiles
//   - the runtime's CURRENT profile comes from runtime.sandbox_profile (the
//     server classifies the real stored policy — this screen never guesses)
//   - applying a profile          PATCH /api/runtimes/{id}/sandbox-policy {profile}
//
// Schemas fail CLOSED per CLAUDE.md → API Response Compatibility: a drifted
// catalog response yields an empty list (the picker shows nothing to pick)
// rather than crashing the runtime page.

import { queryOptions } from "@tanstack/react-query";
import { api, parseWithFallback } from "@multica/core/api";
import { z } from "zod";

/** One selectable isolation profile as the server describes it. */
export interface SandboxProfileDefinition {
  id: string;
  label: string;
  description: string;
  /** false for "custom": you land on it by hand-editing, you don't pick it. */
  selectable: boolean;
}

const definitionSchema = z.object({
  id: z.string(),
  label: z.string().default(""),
  description: z.string().default(""),
  selectable: z.boolean().default(false),
});

const catalogSchema = z.object({
  profiles: z.array(definitionSchema).default([]),
});

/** Fetch the catalog of sandbox isolation profiles for the workspace. */
export async function fetchSandboxProfiles(
  wsId: string,
): Promise<SandboxProfileDefinition[]> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/workspaces/${wsId}/sandbox-profiles`,
  );
  const parsed = parseWithFallback(raw, catalogSchema, { profiles: [] }, {
    endpoint: "sandboxProfiles",
  });
  return parsed.profiles;
}

/** Apply a named profile to a runtime; the server expands it to the canonical policy. */
export async function applySandboxProfile(
  runtimeId: string,
  profile: string,
): Promise<void> {
  await api.cerebroRequest<void>(
    `/api/runtimes/${runtimeId}/sandbox-policy`,
    { method: "PATCH", body: JSON.stringify({ profile }) },
  );
}

export const sandboxProfileKeys = {
  catalog: (wsId: string) => ["cerebro", "sandbox-profiles", wsId] as const,
};

export function sandboxProfileCatalogOptions(wsId: string) {
  return queryOptions({
    queryKey: sandboxProfileKeys.catalog(wsId),
    queryFn: () => fetchSandboxProfiles(wsId),
    enabled: !!wsId,
    staleTime: 5 * 60 * 1000,
  });
}
