/**
 * Product-map read-only types (SY-20). Mirrors the server response shapes in
 * `server/internal/handler/product_map.go`:
 * - ProductMapNodeResponse is the tree node; `children` nests sub-products.
 * - `status` is one of the node statuses (dev / pending_release /
 *   pending_confirmation / released) — released is the only "live" status, and
 *   it must be backed by `evidence` (PMO 上线状态 or code-repo receipt).
 *   Issue `done` alone never implies live (per the confirmed 口径).
 * - `status_source` is per-product configurable: `pmo` or `code_repo`.
 */

export type ProductMapStatus =
  | "dev"
  | "pending_release"
  | "pending_confirmation"
  | "released";

export type ProductMapStatusSource = "pmo" | "code_repo";

export interface ProductMapRef {
  ref_type: "project" | "issue";
  ref_id: string;
}

export interface ProductMapEditor {
  user_id: string;
}

export interface ProductMapNode {
  id: string;
  workspace_id: string;
  parent_id?: string;
  name: string;
  slug: string;
  description: string;
  sort_order: number;
  status: ProductMapStatus;
  status_source: ProductMapStatusSource;
  evidence: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  refs: ProductMapRef[];
  editors: ProductMapEditor[];
  children?: ProductMapNode[];
  has_live_evidence: boolean;
}

export interface ProductMapResponse {
  nodes: ProductMapNode[];
}
