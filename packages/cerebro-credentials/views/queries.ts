// Live-wire to JEH-1196 backend. Replaces the mock hooks in
// credentials-list-page.tsx / credential-detail-page.tsx. The shape mirrors
// `server/internal/cerebro/credentials/types.go` and `handler.go`.

import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";

import type {
  AuditEntry,
  Credential,
  CredentialBinding,
} from "./types";

// ---------------------------------------------------------------------------
// Schemas — intentionally LENIENT per CLAUDE.md "API Response Compatibility":
// string enums are stored as `z.string()`, objects end with `.loose()`,
// arrays default to `[]`. The strict TS types in `./types.ts` still flow at
// call sites; the schemas only guard shape.
// ---------------------------------------------------------------------------

const CredentialServerSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    type: z.string(),
    name: z.string(),
    description: z.string().nullable().optional(),
    value_hint: z.string().nullable().optional(),
    created_at: z.string(),
    updated_at: z.string(),
    expires_at: z.string().nullable().optional(),
    last_rotated_at: z.string().nullable().optional(),
  })
  .loose();

const CredentialListSchema = z
  .object({ credentials: z.array(CredentialServerSchema).default([]) })
  .loose();

const AuditServerSchema = z
  .object({
    id: z.string(),
    credential_id: z.string(),
    action: z.string(),
    actor_type: z.string(),
    actor_id: z.string(),
    metadata: z.unknown().optional(),
    created_at: z.string(),
  })
  .loose();

const AuditListSchema = z
  .object({ audit: z.array(AuditServerSchema).default([]) })
  .loose();

const BindingServerSchema = z
  .object({
    id: z.string(),
    credential_id: z.string(),
    resource_type: z.string(),
    resource_id: z.string(),
    created_at: z.string(),
  })
  .loose();

const BindingListSchema = z
  .object({ bindings: z.array(BindingServerSchema).default([]) })
  .loose();

// ---------------------------------------------------------------------------
// Coercions — translate the server's redacted shape to the UI's `Credential`
// type. Status is derived from `expires_at` + presence in metadata, because
// the server doesn't yet expose a status enum (see JEH-1198 rotation policy).
// ---------------------------------------------------------------------------

function deriveStatus(c: z.infer<typeof CredentialServerSchema>): Credential["status"] {
  if (c.expires_at) {
    const expires = Date.parse(c.expires_at);
    if (Number.isFinite(expires) && expires < Date.now()) {
      return "expired";
    }
  }
  return "active";
}

function toUICredential(c: z.infer<typeof CredentialServerSchema>): Credential {
  return {
    id: c.id,
    workspace_id: c.workspace_id,
    type: c.type as Credential["type"],
    name: c.name,
    description: c.description ?? undefined,
    status: deriveStatus(c),
    redacted_value: c.value_hint ?? "",
    created_at: c.created_at,
    updated_at: c.updated_at,
    expires_at: c.expires_at ?? undefined,
    last_rotated_at: c.last_rotated_at ?? undefined,
  };
}

function toUIAudit(a: z.infer<typeof AuditServerSchema>): AuditEntry {
  return {
    id: a.id,
    credential_id: a.credential_id,
    actor_kind: (a.actor_type === "agent" ? "agent" : "member") as AuditEntry["actor_kind"],
    actor_id: a.actor_id,
    actor_label: a.actor_id,
    action: a.action as AuditEntry["action"],
    // The backend doesn't yet record allow/deny per row — every audit row is
    // an action that actually happened, so default to "allow". JEH-1197
    // (policy enforcement) will introduce deny entries.
    outcome: "allow",
    occurred_at: a.created_at,
  };
}

function toUIBinding(b: z.infer<typeof BindingServerSchema>): CredentialBinding {
  return {
    id: b.id,
    credential_id: b.credential_id,
    resource_kind: b.resource_type as CredentialBinding["resource_kind"],
    resource_id: b.resource_id,
    resource_label: b.resource_id,
    created_at: b.created_at,
  };
}

// ---------------------------------------------------------------------------
// Query options (TanStack Query). Workspace-scoped: keyed on wsId so a
// workspace switch automatically refetches. Disabled when wsId is empty
// (pre-workspace mount).
// ---------------------------------------------------------------------------

export const credentialKeys = {
  all: (wsId: string) => ["credentials", wsId] as const,
  list: (wsId: string) => ["credentials", wsId, "list"] as const,
  audit: (wsId: string, credId: string) =>
    ["credentials", wsId, credId, "audit"] as const,
  bindings: (wsId: string, credId: string) =>
    ["credentials", wsId, credId, "bindings"] as const,
};

export function useCredentialsList(wsId: string) {
  return useQuery({
    queryKey: credentialKeys.list(wsId),
    enabled: !!wsId,
    queryFn: async (): Promise<Credential[]> => {
      const raw = await api.listCerebroCredentials(wsId);
      const parsed = parseWithFallback<{ credentials: z.infer<typeof CredentialServerSchema>[] }>(
        raw,
        CredentialListSchema,
        { credentials: [] },
        { endpoint: "GET /api/workspaces/:id/credentials" },
      );
      return parsed.credentials.map(toUICredential);
    },
  });
}

export function useCredentialAudit(wsId: string, credId: string | null) {
  return useQuery({
    queryKey: credId ? credentialKeys.audit(wsId, credId) : ["credentials", wsId, "audit-disabled"],
    enabled: !!wsId && !!credId,
    queryFn: async (): Promise<AuditEntry[]> => {
      const raw = await api.listCerebroCredentialAudit(wsId, credId!);
      const parsed = parseWithFallback<{ audit: z.infer<typeof AuditServerSchema>[] }>(
        raw,
        AuditListSchema,
        { audit: [] },
        { endpoint: "GET /api/workspaces/:id/credentials/:credId/audit" },
      );
      return parsed.audit.map(toUIAudit);
    },
  });
}

export function useCredentialBindings(wsId: string, credId: string | null) {
  return useQuery({
    queryKey: credId
      ? credentialKeys.bindings(wsId, credId)
      : ["credentials", wsId, "bindings-disabled"],
    enabled: !!wsId && !!credId,
    queryFn: async (): Promise<CredentialBinding[]> => {
      const raw = await api.listCerebroCredentialBindings(wsId, credId!);
      const parsed = parseWithFallback<{ bindings: z.infer<typeof BindingServerSchema>[] }>(
        raw,
        BindingListSchema,
        { bindings: [] },
        { endpoint: "GET /api/workspaces/:id/credentials/:credId/bindings" },
      );
      return parsed.bindings.map(toUIBinding);
    },
  });
}
