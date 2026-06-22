"use client";

// FIR-1479 per-actor credential grants — the credentials column of the agent
// permissions interface, the mirror of the tools column. One Allow/Block toggle
// per Agent Vault box (credential_key): Allow grants the agent access at the
// agent layer; Block clears the grant so the box reverts to the deny-by-default
// base (least-privilege). It reuses the credential-policy write-API
// (POST/DELETE /api/agents/{id}/credential-grants), gated server-side by
// cerebro_credentials_per_actor — the caller only mounts this panel when the flag
// is on. Boxes come from the workspace credential list (the same source as the
// Credentials settings page); the agent grants stamp each row's current verdict.

import { useMemo, useState } from "react";
import { KeyRound, Search, ShieldOff } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { useCurrentMember } from "@multica/core/permissions";
import { useWorkspaceId } from "@multica/core/hooks";

import { useCredentialsList } from "../queries";
import {
  useAgentCredentialGrants,
  useSetAgentCredentialGrant,
  useClearAgentCredentialGrant,
} from "../queries";

// The two authorable verdicts in display order. Credentials have no Ask — a box
// is reachable or it is not — so this is a 2-state toggle, unlike tools.
type CredentialVerdict = "allow" | "deny";
const VERDICTS: CredentialVerdict[] = ["allow", "deny"];
const VERDICT_LABEL: Record<CredentialVerdict, string> = {
  allow: "Allow",
  deny: "Block",
};
const VERDICT_ACTIVE_CLASS: Record<CredentialVerdict, string> = {
  allow: "text-emerald-700 dark:text-emerald-300",
  deny: "text-destructive",
};

export function AgentCredentialGrantsPanel({ agentId }: { agentId: string }) {
  const wsId = useWorkspaceId();
  const { role, isLoading: memberLoading } = useCurrentMember(wsId);
  const { data: credentials = [], isLoading: boxesLoading } =
    useCredentialsList(wsId);
  const { data: grants = [], isLoading: grantsLoading } =
    useAgentCredentialGrants(agentId);
  const setGrant = useSetAgentCredentialGrant(agentId);
  const clearGrant = useClearAgentCredentialGrant(agentId);

  const [search, setSearch] = useState("");

  // Index the explicit agent-layer settings by box name so each row resolves its
  // current verdict in one pass. A box with no row is deny-by-default (Block).
  const settingByKey = useMemo(() => {
    const m = new Map<string, string>();
    for (const g of grants) m.set(g.credential, g.setting);
    return m;
  }, [grants]);

  const visible = useMemo(() => {
    const q = search.trim().toLowerCase();
    return q
      ? credentials.filter((c) =>
          `${c.name} ${c.type}`.toLowerCase().includes(q),
        )
      : credentials;
  }, [credentials, search]);

  if (!wsId || memberLoading) {
    return (
      <div className="flex h-24 items-center justify-center text-sm text-muted-foreground">
        Loading…
      </div>
    );
  }

  const isAdmin = role === "owner" || role === "admin";
  if (!isAdmin) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 p-6 text-center">
        <ShieldOff className="size-8 text-muted-foreground" />
        <h2 className="text-base font-medium">Access denied</h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          Only workspace owners and admins can manage credential access.
        </p>
      </div>
    );
  }

  const pending = setGrant.isPending || clearGrant.isPending;

  async function onSet(box: string, verdict: CredentialVerdict) {
    try {
      if (verdict === "allow") {
        await setGrant.mutateAsync({ credential: box, setting: "allow" });
        toast.success(`Allowed ${box}`);
      } else {
        // Block = clear the grant → revert to the deny-by-default base.
        await clearGrant.mutateAsync(box);
        toast.success(`Blocked ${box}`);
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update access");
    }
  }

  const loading = boxesLoading || grantsLoading;

  return (
    <div className="flex flex-col gap-4" data-testid="agent-credential-grants-panel">
      <div className="flex items-center gap-2">
        <KeyRound className="size-4 text-muted-foreground" />
        <p className="text-sm text-muted-foreground">
          Boxes this agent may reach. Allow grants access; Block reverts to no
          access (deny by default).
        </p>
      </div>

      <div className="relative w-full max-w-xs">
        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search boxes…"
          className="h-9 pl-9"
          aria-label="Search boxes"
        />
      </div>

      {loading ? (
        <p className="text-sm text-muted-foreground">Loading boxes…</p>
      ) : visible.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {credentials.length === 0
            ? "No credential boxes in this workspace yet."
            : "No boxes match the search."}
        </p>
      ) : (
        <div className="overflow-hidden rounded-lg border">
          <div className="grid grid-cols-[1fr_auto] items-center gap-4 border-b px-5 py-2.5 text-xs font-medium text-muted-foreground">
            <span>Box</span>
            <span className="justify-self-center">Access</span>
          </div>
          {visible.map((c) => {
            const verdict: CredentialVerdict =
              settingByKey.get(c.name) === "allow" ? "allow" : "deny";
            return (
              <div
                key={c.id || c.name}
                data-testid={`credential-row-${c.name}`}
                className="grid grid-cols-[1fr_auto] items-center gap-4 border-b px-5 py-3 last:border-b-0"
              >
                <div className="min-w-0">
                  <div className="truncate text-sm font-medium">{c.name}</div>
                  {c.type && (
                    <div className="truncate text-xs text-muted-foreground">
                      {c.type}
                    </div>
                  )}
                </div>
                <VerdictToggle
                  value={verdict}
                  disabled={pending}
                  onChange={(v) => onSet(c.name, v)}
                />
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

// VerdictToggle is the 2-state Allow/Block segmented control for one box. Exactly
// one verdict is highlighted; clicking the other writes it (Allow → grant, Block
// → clear).
function VerdictToggle({
  value,
  disabled,
  onChange,
}: {
  value: CredentialVerdict;
  disabled?: boolean;
  onChange: (verdict: CredentialVerdict) => void;
}) {
  return (
    <div
      className="inline-flex gap-0.5 rounded-md bg-muted p-0.5"
      role="group"
      aria-label="Access"
    >
      {VERDICTS.map((verdict) => {
        const isActive = verdict === value;
        return (
          <Button
            key={verdict}
            type="button"
            size="sm"
            variant="ghost"
            disabled={disabled}
            aria-pressed={isActive}
            className={cn(
              "h-7 rounded px-3 text-xs font-medium hover:bg-transparent",
              isActive
                ? cn(
                    "bg-background font-semibold shadow-sm",
                    VERDICT_ACTIVE_CLASS[verdict],
                  )
                : "text-muted-foreground",
            )}
            onClick={() => onChange(verdict)}
          >
            {VERDICT_LABEL[verdict]}
          </Button>
        );
      })}
    </div>
  );
}
