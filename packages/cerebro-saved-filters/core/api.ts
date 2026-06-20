import { api, parseWithFallback } from "@multica/core/api";

import { savedFilterListSchema, savedFilterSchema } from "./api-schemas";
import type { CreateSavedFilterInput, SavedFilter, UpdateSavedFilterInput } from "./types";

const BASE = "/api/cerebro/saved-filters";

export async function listSavedFilters(surface?: string): Promise<SavedFilter[]> {
  const qs = surface ? `?surface=${encodeURIComponent(surface)}` : "";
  const raw = await api.cerebroRequest<unknown>(`${BASE}${qs}`);
  return parseWithFallback(raw, savedFilterListSchema, [], {
    endpoint: "listSavedFilters",
  });
}

export async function createSavedFilter(input: CreateSavedFilterInput): Promise<SavedFilter | null> {
  const raw = await api.cerebroRequest<unknown>(BASE, {
    method: "POST",
    body: JSON.stringify({
      name: input.name,
      surface: input.surface,
      filter_state: input.filterState,
      position: input.position,
    }),
  });
  return parseWithFallback(raw, savedFilterSchema, null, {
    endpoint: "createSavedFilter",
  });
}

export async function updateSavedFilter(
  id: string,
  input: UpdateSavedFilterInput,
): Promise<SavedFilter | null> {
  const raw = await api.cerebroRequest<unknown>(`${BASE}/${id}`, {
    method: "PATCH",
    body: JSON.stringify({
      name: input.name,
      filter_state: input.filterState,
      position: input.position,
    }),
  });
  return parseWithFallback(raw, savedFilterSchema, null, {
    endpoint: "updateSavedFilter",
  });
}

export async function deleteSavedFilter(id: string): Promise<void> {
  await api.cerebroRequest<void>(`${BASE}/${id}`, { method: "DELETE" });
}
