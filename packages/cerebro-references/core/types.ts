// Wire shape returned by the cerebro reference API
// (server/internal/cerebro/references/handler.go). Mirrors ReferenceResponse
// exactly — keep the two in sync.

export type ReferenceObject = string;

export interface IssueReference {
  id: string;
  issue_id: string;
  object: ReferenceObject;
  type: string | null;
  ref_id: string;
  label: string | null;
  url: string | null;
  metadata: Record<string, unknown>;
  created_by_type: string;
  created_by_id: string;
  created_at: string;
  updated_at: string;
}

export interface CreateReferenceInput {
  object: ReferenceObject;
  type?: string | null;
  ref_id: string;
  label?: string | null;
  url?: string | null;
  metadata?: Record<string, unknown>;
}

export interface UpdateReferenceInput {
  label?: string | null;
  url?: string | null;
  metadata?: Record<string, unknown>;
}

// Per-object renderer. The registry pairs an `object` string with a small
// behaviour bundle: how to derive a human-friendly title, a small status
// badge, and an outbound URL. Built-in renderers (github_pr, …) register
// themselves at import time; consumers can register additional kinds.
//
// Every method is optional — the registry's fallback covers the cases
// where a renderer omits one. That keeps new object kinds cheap to add:
// register {object: "stripe_customer"} with just `formatTitle` and the
// fallback handles the rest.
export interface ObjectRenderer {
  object: ReferenceObject;
  formatTitle?: (ref: IssueReference) => string;
  formatBadge?: (ref: IssueReference) => string | null;
  resolveUrl?: (ref: IssueReference) => string | null;
}
