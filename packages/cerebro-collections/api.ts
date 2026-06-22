// Collections data layer. Talks to the cerebro REST surface via
// api.cerebroRequest (auth + workspace headers are auto-attached); every
// response is parsed through a zod schema with a safe fallback per the "API
// Response Compatibility" rule in CLAUDE.md.
//
// Backend: server/internal/cerebro/foldergrant/handler.go
//   GET    /api/cerebro/folder-grants?surface=&folder_id=&view=direct|effective
//   PUT    /api/cerebro/folder-grants                 (upsert one grant)
//   DELETE /api/cerebro/folder-grants                 (remove one direct grant)

import { api, parseWithFallback } from "@multica/core/api";
import { z } from "zod";
import type {
  FolderGrant,
  GrantSurface,
  RemoveFolderGrantInput,
  UpsertFolderGrantInput,
} from "./types";

const surfaceSchema = z.enum(["artifact", "entity"]);
const granteeTypeSchema = z.enum([
  "group",
  "member",
  "workspace",
  "agent",
  "runtime",
]);
const roleSchema = z.enum(["viewer", "editor", "full_access"]);

const grantSchema = z.object({
  surface: surfaceSchema,
  folder_id: z.string(),
  grantee_type: granteeTypeSchema,
  grantee_id: z
    .string()
    .nullable()
    .optional()
    .transform((v) => v ?? null),
  role: roleSchema,
  source_folder_id: z.string(),
  is_direct: z.boolean(),
  depth: z.number(),
});
const grantsListSchema = z.array(grantSchema);

const EMPTY_GRANTS: FolderGrant[] = [];

export type GrantView = "direct" | "effective";

export async function fetchFolderGrants(
  surface: GrantSurface,
  folderId: string,
  view: GrantView,
): Promise<FolderGrant[]> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/folder-grants?surface=${surface}&folder_id=${folderId}&view=${view}`,
  );
  return parseWithFallback(raw, grantsListSchema, EMPTY_GRANTS, {
    endpoint: "fetchFolderGrants",
  });
}

export async function upsertFolderGrant(
  input: UpsertFolderGrantInput,
): Promise<FolderGrant | null> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/folder-grants", {
    method: "PUT",
    body: JSON.stringify({
      surface: input.surface,
      folder_id: input.folder_id,
      grantee_type: input.grantee_type,
      grantee_id: input.grantee_id ?? null,
      role: input.role,
    }),
  });
  return parseWithFallback(raw, grantSchema, null, {
    endpoint: "upsertFolderGrant",
  });
}

export async function removeFolderGrant(
  input: RemoveFolderGrantInput,
): Promise<void> {
  await api.cerebroRequest<void>("/api/cerebro/folder-grants", {
    method: "DELETE",
    body: JSON.stringify({
      surface: input.surface,
      folder_id: input.folder_id,
      grantee_type: input.grantee_type,
      grantee_id: input.grantee_id ?? null,
    }),
  });
}

// ---------------------------------------------------------------------------
// Folder lists for the Settings → Collections page. Fetched straight off the
// core api client so this package stays a leaf — it must NOT import
// cerebro-artifacts / cerebro-entity-folders, which import this package's
// FolderAccessColumn (a cycle otherwise).
// ---------------------------------------------------------------------------

// A folder surfaced on the Collections page, normalised across both backends.
export interface CollectionFolder {
  surface: GrantSurface;
  id: string;
  name: string;
  group: string; // Documents | Notes | Skills | Autopilots
}

const artifactFolderSchema = z.object({
  id: z.string(),
  name: z.string(),
  kind: z.enum(["document", "note"]),
});
type RawArtifactFolder = z.infer<typeof artifactFolderSchema>;
const artifactFolderListSchema = z.array(artifactFolderSchema);
const EMPTY_ARTIFACT_FOLDERS: RawArtifactFolder[] = [];

const entityFolderSchema = z.object({
  id: z.string(),
  name: z.string(),
  kind: z.enum(["skill", "autopilot"]),
});
type RawEntityFolder = z.infer<typeof entityFolderSchema>;
const entityFolderListSchema = z.array(entityFolderSchema);
const EMPTY_ENTITY_FOLDERS: RawEntityFolder[] = [];

export async function fetchArtifactCollectionFolders(): Promise<
  CollectionFolder[]
> {
  const raw = await api.listArtifactFolders();
  const parsed = parseWithFallback(
    raw,
    artifactFolderListSchema,
    EMPTY_ARTIFACT_FOLDERS,
    { endpoint: "fetchArtifactCollectionFolders" },
  );
  return parsed.map((f) => ({
    surface: "artifact" as const,
    id: f.id,
    name: f.name,
    group: f.kind === "note" ? "Notes" : "Documents",
  }));
}

export async function fetchEntityCollectionFolders(
  kind: "skill" | "autopilot",
): Promise<CollectionFolder[]> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/entity-folders?kind=${kind}`,
  );
  const parsed = parseWithFallback(
    raw,
    entityFolderListSchema,
    EMPTY_ENTITY_FOLDERS,
    { endpoint: "fetchEntityCollectionFolders" },
  );
  return parsed.map((f) => ({
    surface: "entity" as const,
    id: f.id,
    name: f.name,
    group: f.kind === "skill" ? "Skills" : "Autopilots",
  }));
}
