// CEREBRO-PATCH(capability-register-types): FIR-2129 capability register types.
// Mirrors the server-side capability register API (GET /api/capabilities and
// POST /api/capabilities/report). Lives in packages/core so both the web and
// desktop apps share one source of truth for these shapes.

export type CapabilitySubjectType =
  | "member"
  | "agent"
  | "group"
  | "runtime"
  | "workspace";

export interface CapabilitySubject {
  type: CapabilitySubjectType;
  id: string;
  display_name?: string;
  metadata?: Record<string, unknown>;
}

export interface CapabilityReportInput {
  key: string;
  title: string;
  category: string;
  description?: string;
  source?: string;
  metadata?: Record<string, unknown>;
  owners?: CapabilitySubject[];
  users?: CapabilitySubject[];
}

export interface Capability {
  id: string;
  workspace_id: string;
  key: string;
  title: string;
  category: string;
  description: string;
  source: string;
  metadata: Record<string, unknown>;
  owners: CapabilitySubject[];
  users: CapabilitySubject[];
  reporters: CapabilitySubject[];
  first_seen_at: string;
  last_reported_at: string;
  updated_at: string;
}

export interface CapabilityListResponse {
  capabilities: Capability[];
}
