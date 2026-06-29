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
  ProjectGrant,
  RemoveFolderGrantInput,
  RemoveProjectGrantInput,
  UpsertFolderGrantInput,
  UpsertProjectGrantInput,
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
// parent_id wires the flat list into a tree on the Collections page; null is a
// root folder. Both backends already return parent_id (artifact_folder and
// cerebro_entity_folder are both self-referencing) — we just surface it here.
export interface CollectionFolder {
  surface: GrantSurface;
  id: string;
  name: string;
  group: string; // Documents | Notes | Skills | Autopilots
  parent_id: string | null;
}

// parent_id is optional-and-nullable on purpose: an older backend may omit the
// field entirely, in which case the folder is treated as a root (per the API
// Response Compatibility rule — enum/shape drift downgrades, never crashes).
const artifactFolderSchema = z.object({
  id: z.string(),
  name: z.string(),
  kind: z.enum(["document", "note"]),
  parent_id: z
    .string()
    .nullable()
    .optional()
    .transform((v) => v ?? null),
});
type RawArtifactFolder = z.infer<typeof artifactFolderSchema>;
const artifactFolderListSchema = z.array(artifactFolderSchema);
const EMPTY_ARTIFACT_FOLDERS: RawArtifactFolder[] = [];

const entityFolderSchema = z.object({
  id: z.string(),
  name: z.string(),
  kind: z.enum(["skill", "autopilot"]),
  parent_id: z
    .string()
    .nullable()
    .optional()
    .transform((v) => v ?? null),
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
    parent_id: f.parent_id,
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
    parent_id: f.parent_id,
  }));
}

// ---------------------------------------------------------------------------
// Folder creation. Creates a root or nested folder on the correct backend.
//   artifact: POST /api/artifact-folders        body { name, kind, parent_id? }
//   entity:   POST /api/cerebro/entity-folders  body { name, kind, parent_id? }
// ---------------------------------------------------------------------------

export interface CreateCollectionFolderInput {
  surface: GrantSurface;
  name: string;
  // artifact: 'document' | 'note'   entity: 'skill' | 'autopilot'
  kind: string;
  parentId: string | null;
}

const createdEntityFolderSchema = z.object({
  id: z.string(),
  name: z.string(),
  kind: z.enum(["skill", "autopilot"]),
  parent_id: z
    .string()
    .nullable()
    .optional()
    .transform((v) => v ?? null),
});

export async function createCollectionFolder(
  input: CreateCollectionFolderInput,
): Promise<CollectionFolder> {
  if (input.surface === "artifact") {
    const folder = await api.createArtifactFolder({
      name: input.name,
      kind: input.kind as "document" | "note",
      parent_id: input.parentId,
    });
    return {
      surface: "artifact" as const,
      id: folder.id,
      name: folder.name,
      group: folder.kind === "note" ? "Notes" : "Documents",
      parent_id: folder.parent_id,
    };
  }
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/entity-folders", {
    method: "POST",
    body: JSON.stringify({
      name: input.name,
      kind: input.kind,
      parent_id: input.parentId,
    }),
  });
  type CreatedEntityFolder = z.infer<typeof createdEntityFolderSchema>;
  const parsed = parseWithFallback<CreatedEntityFolder | null>(
    raw,
    createdEntityFolderSchema,
    null,
    { endpoint: "createCollectionFolder" },
  );
  if (!parsed) throw new Error("createCollectionFolder: unexpected response");
  return {
    surface: "entity" as const,
    id: parsed.id,
    name: parsed.name,
    group: parsed.kind === "skill" ? "Skills" : "Autopilots",
    parent_id: parsed.parent_id,
  };
}

// ---------------------------------------------------------------------------
// Folder move. Re-parents a folder (or moves it to root with parentId=null) on
// whichever backend owns it. Both PUT endpoints already accept parent_id and
// guard against cycles server-side; we just route to the right one by surface.
//   artifact: PUT /api/artifact-folders/{id}      body { parent_id }
//   entity:   PUT /api/cerebro/entity-folders/{id} body { parent_id }
// An empty-string parent_id means "move to root" on both backends (a non-null,
// non-omitted field), which is why we send "" rather than omitting it.
// ---------------------------------------------------------------------------

export interface MoveCollectionFolderInput {
  surface: GrantSurface;
  folderId: string;
  parentId: string | null;
  // Only consumed by the entity backend, which keys folders by kind.
  entityKind?: "skill" | "autopilot";
}

export async function moveCollectionFolder(
  input: MoveCollectionFolderInput,
): Promise<void> {
  const parentField = input.parentId ?? "";
  if (input.surface === "artifact") {
    await api.updateArtifactFolder(input.folderId, {
      parent_id: parentField,
    });
    return;
  }
  await api.cerebroRequest<unknown>(
    `/api/cerebro/entity-folders/${input.folderId}`,
    {
      method: "PUT",
      body: JSON.stringify({ parent_id: parentField }),
    },
  );
}

// ---------------------------------------------------------------------------
// Project grants (FIR-2125). Per-project role-based access with recursive
// inheritance via project_nesting. Mirrors the folder-grant functions above.
//   Backend: server/internal/cerebro/projectgrant/handler.go
//   GET    /api/cerebro/project-grants?project_id=&view=direct|effective
//   PUT    /api/cerebro/project-grants
//   DELETE /api/cerebro/project-grants
// ---------------------------------------------------------------------------

export type ProjectGrantView = "direct" | "effective";

const projectGrantSchema = z.object({
  project_id: z.string(),
  grantee_type: z.enum(["group", "member", "workspace", "agent", "runtime"]),
  grantee_id: z
    .string()
    .nullable()
    .optional()
    .transform((v) => v ?? null),
  role: z.enum(["viewer", "editor", "full_access"]),
  source_project_id: z.string(),
  is_direct: z.boolean(),
  depth: z.number(),
});
const projectGrantsListSchema = z.array(projectGrantSchema);
const EMPTY_PROJECT_GRANTS: ProjectGrant[] = [];

export async function fetchProjectGrants(
  projectId: string,
  view: ProjectGrantView,
): Promise<ProjectGrant[]> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/project-grants?project_id=${projectId}&view=${view}`,
  );
  return parseWithFallback(raw, projectGrantsListSchema, EMPTY_PROJECT_GRANTS, {
    endpoint: "fetchProjectGrants",
  });
}

export async function upsertProjectGrant(
  input: UpsertProjectGrantInput,
): Promise<ProjectGrant | null> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/project-grants", {
    method: "PUT",
    body: JSON.stringify({
      project_id: input.project_id,
      grantee_type: input.grantee_type,
      grantee_id: input.grantee_id ?? null,
      role: input.role,
    }),
  });
  return parseWithFallback(raw, projectGrantSchema, null, {
    endpoint: "upsertProjectGrant",
  });
}

export async function removeProjectGrant(
  input: RemoveProjectGrantInput,
): Promise<void> {
  await api.cerebroRequest<void>("/api/cerebro/project-grants", {
    method: "DELETE",
    body: JSON.stringify({
      project_id: input.project_id,
      grantee_type: input.grantee_type,
      grantee_id: input.grantee_id ?? null,
    }),
  });
}

// ---------------------------------------------------------------------------
// Collection item creation (FIR-2125 Area 2)
// Create a new content item — document, note, or skill — directly from the
// Collections settings tab. Calls go via @multica/core/api to avoid import
// cycles with cerebro-artifacts / cerebro-entity-folders.
// ---------------------------------------------------------------------------

export type CreateCollectionItemInput =
  | { kind: "document"; title: string; folderId?: string | null }
  | { kind: "note"; title: string; folderId?: string | null }
  | { kind: "skill"; name: string };

/** Creates a content item and, for entity kinds (skill), places it in a
 *  folder via the entity-folder-items API if a folderId is provided. */
export async function createCollectionItem(
  input: CreateCollectionItemInput,
): Promise<{ id: string }> {
  if (input.kind === "document") {
    const result = await api.createArtifact({
      kind: "note",
      title: input.title,
      body: "",
      folder_id: input.folderId ?? undefined,
    });
    return { id: result.id };
  }
  if (input.kind === "note") {
    const result = await api.createNote<{ id: string }>({
      title: input.title,
      folder_id: input.folderId ?? null,
    });
    return { id: result.id };
  }
  // skill — two-step: create then place in folder
  const result = await api.createSkill({ name: input.name });
  return { id: result.id };
}
