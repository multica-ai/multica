import { api, parseWithFallback } from "@multica/core/api";

import {
  canShareSchema,
  savedFilterListSchema,
  savedFilterSchema,
} from "./api-schemas";
import type {
  CreateSavedFilterInput,
  SavedFilter,
  SavedFilterShareTarget,
  UpdateSavedFilterInput,
} from "./types";

const BASE = "/api/cerebro/saved-filters";

// Serialize share targets to the snake_case wire shape the backend expects.
function sharesToWire(shares?: SavedFilterShareTarget[]) {
  if (!shares) return undefined;
  return shares.map((s) => ({ target_type: s.targetType, target_id: s.targetId }));
}

export async function listSavedFilters(surface?: string): Promise<SavedFilter[]> {
  const qs = surface ? `?surface=${encodeURIComponent(surface)}` : "";
  const raw = await api.cerebroRequest<unknown>(`${BASE}${qs}`);
  return parseWithFallback(raw, savedFilterListSchema, [], {
    endpoint: "listSavedFilters",
  });
}

// getSavedFilter fetches one filter WITH its share targets (owner-only on the
// backend); used by the share dialog to pre-populate who it is shared with.
export async function getSavedFilter(id: string): Promise<SavedFilter | null> {
  const raw = await api.cerebroRequest<unknown>(`${BASE}/${id}`);
  return parseWithFallback(raw, savedFilterSchema, null, {
    endpoint: "getSavedFilter",
  });
}

// canShareFilters resolves the Fase 4 gate for the current member.
export async function canShareFilters(): Promise<boolean> {
  const raw = await api.cerebroRequest<unknown>(`${BASE}/can-share`);
  return parseWithFallback(raw, canShareSchema, false, {
    endpoint: "canShareFilters",
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
      visibility: input.visibility,
      shares: sharesToWire(input.shares),
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
      visibility: input.visibility,
      shares: sharesToWire(input.shares),
    }),
  });
  return parseWithFallback(raw, savedFilterSchema, null, {
    endpoint: "updateSavedFilter",
  });
}

export async function deleteSavedFilter(id: string): Promise<void> {
  await api.cerebroRequest<void>(`${BASE}/${id}`, { method: "DELETE" });
}
