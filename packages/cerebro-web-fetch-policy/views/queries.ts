import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";

// ---------------------------------------------------------------------------
// Schema — lenient per CLAUDE.md "API Response Compatibility"
// ---------------------------------------------------------------------------

const WebFetchPolicySchema = z
  .object({
    mode: z.enum(["allow", "disallow"]).default("allow"),
    rules: z.array(z.string()).default([]),
    configured: z.boolean().default(false),
  })
  .loose();

export type WebFetchPolicy = z.infer<typeof WebFetchPolicySchema>;

const FALLBACK: WebFetchPolicy = { mode: "allow", rules: [], configured: false };

// ---------------------------------------------------------------------------
// Query keys + hooks
// ---------------------------------------------------------------------------

export const webFetchPolicyKeys = {
  one: (wsId: string) => ["web-fetch-policy", wsId] as const,
};

export function useWebFetchPolicy(wsId: string) {
  return useQuery({
    queryKey: webFetchPolicyKeys.one(wsId),
    queryFn: async (): Promise<WebFetchPolicy> => {
      const raw = await api.getCerebroWebFetchPolicy(wsId);
      return parseWithFallback(raw, WebFetchPolicySchema, FALLBACK, {
        endpoint: "GET /api/workspaces/:id/web-fetch-policy",
      }) as WebFetchPolicy;
    },
    enabled: !!wsId,
  });
}

export function useUpdateWebFetchPolicy(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: { mode: "allow" | "disallow"; rules: string[] }): Promise<WebFetchPolicy> => {
      const raw = await api.updateCerebroWebFetchPolicy(wsId, input);
      return parseWithFallback(raw, WebFetchPolicySchema, { ...FALLBACK, ...input, configured: true }, {
        endpoint: "PUT /api/workspaces/:id/web-fetch-policy",
      }) as WebFetchPolicy;
    },
    onSuccess: (data) => {
      qc.setQueryData(webFetchPolicyKeys.one(wsId), data);
      void qc.invalidateQueries({ queryKey: webFetchPolicyKeys.one(wsId) });
    },
  });
}
