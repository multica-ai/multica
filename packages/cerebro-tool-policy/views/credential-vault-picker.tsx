"use client";

// FIR-1739 v1 (vault-level) — the credential vault-picker on the Permissions
// surface. Credential access is authored here as an ordinary Permissions rule,
// not a parallel panel: granting a box writes a `credential.reveal` Allow on
// resource `agentvault-vault:<name>` at the AGENT layer of the unified
// tool-policy chain — exactly the rule the Agent Vault connector projects into
// the agent's vault-scoped token at claim (server cerebro_agentvault_mirror.go).
// So the toggle here IS the access decision; there is no advisory display.
//
// Per-key selection within a vault is the FIR-2108 follow-up: Agent Vault scopes
// per vault today, so vault-level is the only granularity that can be truly
// enforced now. The available boxes are sourced live from Agent Vault via the
// read-only GET /api/workspaces/{id}/agentvault/vaults endpoint.

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { KeyRound } from "lucide-react";
import { toast } from "sonner";

import { Switch } from "@multica/ui/components/ui/switch";
import { useAgentVaultVaults } from "@multica/cerebro-agentvault/views";

import {
  toolPolicyTableOptions,
  useSetToolPolicy,
  useClearToolPolicy,
  type ToolPolicyRow,
} from "../core";

// The tool-policy action whose Allow grants a vault, and the resource prefix
// that names an Agent Vault box directly. Must stay in lockstep with the server
// connector (credentialBrokerAction / agentvault.VaultResourcePrefix).
export const CREDENTIAL_REVEAL_TOOL_KEY = "credential.reveal";
export const VAULT_RESOURCE_PREFIX = "agentvault-vault:";

export interface CredentialVaultPickerProps {
  wsId: string;
  /** The agent whose credential grants this picker edits (writes the agent layer). */
  agentId: string;
  /** The runtime below the agent, so the chain query resolves the inherited layers. */
  runtimeId?: string | null;
  /** The agent owner, so the inherited chain reflects the real ceiling. */
  userId?: string | null;
}

// A vault is granted when an authored agent-layer rule sets credential.reveal to
// Allow on its agentvault-vault:<name> resource. Read from the same chain query
// the Permissions table uses, so a write here invalidates and re-renders both.
function grantedVaultNames(rows: ToolPolicyRow[]): Set<string> {
  const granted = new Set<string>();
  for (const r of rows) {
    if (r.tool_key !== CREDENTIAL_REVEAL_TOOL_KEY) continue;
    if (!r.resource_pattern.startsWith(VAULT_RESOURCE_PREFIX)) continue;
    if (r.layers.agent !== "allow") continue;
    granted.add(r.resource_pattern.slice(VAULT_RESOURCE_PREFIX.length).trim());
  }
  return granted;
}

export function CredentialVaultPicker({
  wsId,
  agentId,
  runtimeId,
  userId,
}: CredentialVaultPickerProps) {
  const { data: vaults = [], isLoading: vaultsLoading } =
    useAgentVaultVaults(wsId);

  // The agent chain rows, so the picker reflects what is actually authored.
  const { data: rows = [] } = useQuery(
    toolPolicyTableOptions(wsId, { agentId, runtimeId, userId }),
  );
  const granted = useMemo(() => grantedVaultNames(rows), [rows]);

  const setPolicy = useSetToolPolicy();
  const clearPolicy = useClearToolPolicy();

  function toggle(vaultName: string, next: boolean) {
    const resource_pattern = VAULT_RESOURCE_PREFIX + vaultName;
    if (next) {
      setPolicy.mutate(
        {
          tool_key: CREDENTIAL_REVEAL_TOOL_KEY,
          layer: "agent",
          subject_id: agentId,
          setting: "allow",
          resource_pattern,
        },
        { onSuccess: () => toast.success(`Granted ${vaultName}`) },
      );
    } else {
      clearPolicy.mutate(
        {
          tool_key: CREDENTIAL_REVEAL_TOOL_KEY,
          layer: "agent",
          subject_id: agentId,
          resource_pattern,
        },
        { onSuccess: () => toast.success(`Removed ${vaultName}`) },
      );
    }
  }

  const busy = setPolicy.isPending || clearPolicy.isPending;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <KeyRound className="size-4 text-muted-foreground" />
        <p className="text-sm text-muted-foreground">
          Secret boxes (vaults) this agent may use via Agent Vault. The agent uses
          a secret without ever holding it.
        </p>
      </div>

      {vaultsLoading ? (
        <div className="flex h-16 items-center justify-center text-sm text-muted-foreground">
          Loading…
        </div>
      ) : vaults.length === 0 ? (
        <p className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
          No vaults available. Create one in Agent Vault first.
        </p>
      ) : (
        <ul className="flex flex-col divide-y rounded-md border">
          {vaults.map((v) => {
            const on = granted.has(v.name);
            return (
              <li
                key={v.id || v.name}
                className="flex items-center justify-between gap-3 px-3 py-2"
              >
                <span className="text-sm font-medium">{v.name}</span>
                <Switch
                  checked={on}
                  disabled={busy}
                  onCheckedChange={(next) => toggle(v.name, next)}
                  aria-label={`Grant ${v.name}`}
                />
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
