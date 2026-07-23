import { z } from "zod";

/**
 * Client-side contract for the FIR-3608 service-token management surface
 * (`/api/service-tokens`). The cerebro package owns these schemas so the
 * upstream API client can stay generic (`Promise<unknown>`) — see the
 * "API Response Compatibility" rule in CLAUDE.md. Every response is parsed
 * with `parseWithFallback` against the lenient schemas below, so a drifted
 * field degrades to a safe default instead of white-screening the tab.
 */

/**
 * The grantable scopes, mirrored from the backend closed set
 * (`server/internal/cerebro/servicetoken/scopes.go`). Kept as a plain list so
 * the create form can render one toggle per scope. `host:action` strings.
 */
export const SERVICE_TOKEN_SCOPES = [
  "skills:read",
  "skills:write",
  "agents:read",
  "agents:write",
  "issues:read",
  "issues:write",
] as const;

export type ServiceTokenScope = (typeof SERVICE_TOKEN_SCOPES)[number];

export interface ServiceToken {
  id: string;
  name: string;
  token_prefix: string;
  scopes: string[];
  expires_at: string | null;
  last_used_at: string | null;
  revoked: boolean;
  created_at: string;
}

export interface CreateServiceTokenResponse extends ServiceToken {
  /** Raw secret, returned exactly once at creation. */
  token: string;
}

// Lenient schemas: server-driven strings stay `z.string()` so an unknown
// scope or a new field never fails the parse.
const serviceTokenSchema = z.object({
  id: z.string(),
  name: z.string(),
  token_prefix: z.string(),
  scopes: z.array(z.string()).nullish().transform((v) => v ?? []),
  expires_at: z.string().nullish().transform((v) => v ?? null),
  last_used_at: z.string().nullish().transform((v) => v ?? null),
  revoked: z.boolean().nullish().transform((v) => v ?? false),
  created_at: z.string(),
});

export const serviceTokenListSchema = z.array(serviceTokenSchema);

export const createServiceTokenSchema = serviceTokenSchema.extend({
  token: z.string(),
});
