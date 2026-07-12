// CEREBRO-PATCH(model-registry-types): FIR-2698 the single-source model
// registry — one versioned document holding every LLM model's display
// label, provider, context
// window, and list prices, governed by the same propose → review → approve
// pattern as skills and agent context. Mirrors
// server/internal/cerebro/modelregistry/{registry.go,handler.go}.

export interface ModelRegistryEntry {
  label: string;
  provider: string;
  context_window: number;
  cache_ttl_seconds?: number; // CEREBRO-PATCH(model-registry-cache-ttl): FIR-2698 editable cache lifetime metadata.
  input_usd_per_mtok: number;
  output_usd_per_mtok: number;
  cache_read_usd_per_mtok: number;
  cache_write_usd_per_mtok: number;
}

export interface ModelRegistrySnapshot {
  fallback_model: string;
  models: Record<string, ModelRegistryEntry>;
}

export interface ModelRegistry {
  id: string;
  owner_id: string | null;
  approver_ids: string[];
  current_version: string;
  snapshot: ModelRegistrySnapshot;
  updated_at: string;
}

export interface ModelRegistryVersion {
  id: string;
  version: string;
  snapshot: ModelRegistrySnapshot;
  description: string;
  created_by: string | null;
  created_at: string;
}

export type ModelRegistryChangeRequestStatus =
  | "pending"
  | "approved"
  | "rejected"
  | "merged";

export interface ModelRegistryChangeRequest {
  id: string;
  title: string;
  description: string;
  base_version: string;
  proposed_version: string;
  proposed_snapshot: ModelRegistrySnapshot;
  diff: string;
  status: ModelRegistryChangeRequestStatus;
  proposed_by: string;
  reviewed_by: string | null;
  reviewed_at: string | null;
  review_comment: string;
  created_at: string;
  updated_at: string;
}

export interface CreateModelRegistryChangeRequestRequest {
  title: string;
  description?: string;
  proposed_version: string;
  /** Full replacement snapshot, or use the convenience overrides below. */
  proposed_snapshot?: ModelRegistrySnapshot;
  set_models?: Record<string, ModelRegistryEntry>;
  remove_models?: string[];
  fallback_model?: string;
  work_session_id?: string;
}

export interface ReviewModelRegistryChangeRequestRequest {
  action: "approve" | "reject";
  comment?: string;
}

export interface RollbackModelRegistryRequest {
  version: string;
  comment?: string;
}
