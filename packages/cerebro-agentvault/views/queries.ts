import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";

import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";

import type { AgentVaultAccess, AgentVaultBox, AgentVaultRole } from "./types";

// Lenient per CLAUDE.md "API Response Compatibility".
const AccessSchema = z
  .object({
    agent_id: z.string().default(""),
    vault: z.string(),
    role: z.string().default("read-only"),
    updated_at: z.string().default(""),
  })
  .loose();

const ListSchema = z.object({ access: z.array(AccessSchema).default([]) }).loose();

// Vault list — id + name; tolerate extra fields (membership, credential_store…).
const VaultSchema = z
  .object({ id: z.string().default(""), name: z.string().default("") })
  .loose();
const VaultsListSchema = z
  .object({ vaults: z.array(VaultSchema).default([]) })
  .loose();

const vaultsKey = (wsId: string) => ["cerebro-agentvault-vaults", wsId] as const;

// Lists the Agent Vault boxes available to author a credential grant against
// (FIR-1739 v1). Read-only; returns [] when Agent Vault is not configured.
export function useAgentVaultVaults(wsId: string) {
  return useQuery({
    queryKey: vaultsKey(wsId),
    enabled: Boolean(wsId),
    queryFn: async (): Promise<AgentVaultBox[]> => {
      const raw = await api.listCerebroAgentVaultVaults(wsId);
      const parsed = parseWithFallback(
        raw,
        VaultsListSchema,
        { vaults: [] as Array<{ id: string; name: string }> },
        { endpoint: "GET /api/workspaces/:id/agentvault/vaults" },
      );
      return parsed.vaults
        .filter((v) => v.name.trim() !== "")
        .map((v) => ({ id: v.id, name: v.name }));
    },
  });
}

function normalizeRole(r: string): AgentVaultRole {
  return r === "member" || r === "admin" ? r : "read-only";
}

const accessKey = (wsId: string, agentId: string) =>
  ["cerebro-agentvault-access", wsId, agentId] as const;

export function useAgentVaultAccess(wsId: string, agentId: string) {
  return useQuery({
    queryKey: accessKey(wsId, agentId),
    enabled: Boolean(wsId && agentId),
    queryFn: async (): Promise<AgentVaultAccess[]> => {
      const raw = await api.listCerebroAgentVaultAccess(wsId, agentId);
      const parsed = parseWithFallback(
        raw,
        ListSchema,
        {
          access: [] as Array<{
            agent_id: string;
            vault: string;
            role: string;
            updated_at: string;
          }>,
        },
        { endpoint: "GET /api/workspaces/:id/agentvault/access" },
      );
      return parsed.access.map((a) => ({
        agent_id: a.agent_id,
        vault: a.vault,
        role: normalizeRole(a.role),
        updated_at: a.updated_at,
      }));
    },
  });
}

export function useSetAgentVaultAccess(wsId: string, agentId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: { vault: string; role: AgentVaultRole }) => {
      await api.setCerebroAgentVaultAccess(wsId, {
        agent_id: agentId,
        vault: input.vault,
        role: input.role,
      });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: accessKey(wsId, agentId) }),
  });
}

export function useDeleteAgentVaultAccess(wsId: string, agentId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vault: string) => {
      await api.deleteCerebroAgentVaultAccess(wsId, { agent_id: agentId, vault });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: accessKey(wsId, agentId) }),
  });
}
