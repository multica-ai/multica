// FIR-1412: data layer for skill/autopilot folders. Talks to the cerebro REST
// surface via api.cerebroRequest (auth + workspace headers are auto-attached);
// every response is parsed through a zod schema with a safe fallback per the
// "API Response Compatibility" rule in CLAUDE.md.

import { api, parseWithFallback } from "@multica/core/api";
import { z } from "zod";
import type {
  CreateEntityFolderInput,
  EntityFolder,
  EntityFolderItem,
  EntityFolderKind,
  SetEntityFolderItemInput,
  UpdateEntityFolderInput,
} from "./types";

const kindSchema = z.enum(["skill", "autopilot"]);

const folderSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  parent_id: z
    .string()
    .nullable()
    .optional()
    .transform((v) => v ?? null),
  kind: kindSchema,
  name: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
});
const foldersListSchema = z.array(folderSchema);

const itemSchema = z.object({
  kind: kindSchema,
  item_id: z.string(),
  folder_id: z.string(),
});
const itemsListSchema = z.array(itemSchema);

const EMPTY_FOLDERS: EntityFolder[] = [];
const EMPTY_ITEMS: EntityFolderItem[] = [];

export async function fetchEntityFolders(
  kind: EntityFolderKind,
): Promise<EntityFolder[]> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/entity-folders?kind=${kind}`,
  );
  return parseWithFallback(raw, foldersListSchema, EMPTY_FOLDERS, {
    endpoint: "fetchEntityFolders",
  });
}

export async function fetchEntityFolderItems(
  kind: EntityFolderKind,
): Promise<EntityFolderItem[]> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/entity-folder-items?kind=${kind}`,
  );
  return parseWithFallback(raw, itemsListSchema, EMPTY_ITEMS, {
    endpoint: "fetchEntityFolderItems",
  });
}

export async function createEntityFolder(
  input: CreateEntityFolderInput,
): Promise<EntityFolder | null> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/entity-folders", {
    method: "POST",
    body: JSON.stringify({
      kind: input.kind,
      name: input.name,
      parent_id: input.parent_id ?? null,
    }),
  });
  return parseWithFallback(raw, folderSchema, null, {
    endpoint: "createEntityFolder",
  });
}

export async function updateEntityFolder(
  id: string,
  input: UpdateEntityFolderInput,
): Promise<EntityFolder | null> {
  const body: Record<string, unknown> = {};
  if (input.name !== undefined) body.name = input.name;
  if (input.parent_id !== undefined) body.parent_id = input.parent_id;
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/entity-folders/${id}`,
    {
      method: "PUT",
      body: JSON.stringify(body),
    },
  );
  return parseWithFallback(raw, folderSchema, null, {
    endpoint: "updateEntityFolder",
  });
}

export async function deleteEntityFolder(id: string): Promise<void> {
  await api.cerebroRequest<void>(`/api/cerebro/entity-folders/${id}`, {
    method: "DELETE",
  });
}

export async function setEntityFolderItem(
  input: SetEntityFolderItemInput,
): Promise<void> {
  await api.cerebroRequest<void>("/api/cerebro/entity-folder-items", {
    method: "PUT",
    body: JSON.stringify({
      kind: input.kind,
      item_id: input.item_id,
      folder_id: input.folder_id ?? null,
    }),
  });
}
