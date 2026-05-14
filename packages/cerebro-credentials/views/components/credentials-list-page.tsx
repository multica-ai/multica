"use client";

import { useState } from "react";
import { KeyRound, ShieldOff } from "lucide-react";

import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from "@multica/ui/components/ui/table";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";

import { MOCK_CREDENTIALS } from "../mock-data";
import { CREDENTIAL_TYPE_LABEL } from "../types";
import type { Credential } from "../types";
import { ExpiryBadge } from "./expiry-badge";
import { CredentialStatusBadge } from "./credential-status-badge";
import { CredentialDetailPage } from "./credential-detail-page";

// STUB hook — JEH-1196 has shipped the registry REST API at
// `/api/workspaces/{id}/credentials`; live wiring (TanStack Query +
// parseWithFallback schema) is tracked as follow-up to JEH-1199. The UI
// currently still renders deterministic mock data so the policy admin
// surfaces can be reviewed without an admin first seeding real rows.
function useCredentials(): { data: Credential[]; isLoading: boolean } {
  return { data: MOCK_CREDENTIALS, isLoading: false };
}

export function CredentialsListPage() {
  const workspace = useCurrentWorkspace();
  const { role, isLoading: isMemberLoading } = useCurrentMember(workspace?.id ?? "");
  const { data: credentials } = useCredentials();
  const [filter, setFilter] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);

  if (!workspace || isMemberLoading) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Indlæser…
      </div>
    );
  }

  const isAdmin = role === "owner" || role === "admin";
  if (!isAdmin) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center">
        <ShieldOff className="size-8 text-muted-foreground" />
        <h2 className="text-base font-medium">Adgang nægtet</h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          Kun workspace-owners og -admins kan administrere credentials.
          Backenden gater alle skrive-handlinger via policy-checkeren
          (JEH-1197); denne side er skjult i UI'et som et ekstra lag.
        </p>
      </div>
    );
  }

  const visible = credentials.filter((c) => {
    if (!filter) return true;
    const q = filter.toLowerCase();
    return (
      c.name.toLowerCase().includes(q) ||
      c.type.toLowerCase().includes(q) ||
      c.redacted_value.toLowerCase().includes(q)
    );
  });

  const selected = credentials.find((c) => c.id === selectedId) ?? null;

  return (
    <div className="flex h-full flex-col gap-6 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            <KeyRound className="size-6 text-muted-foreground" />
            Credentials
          </h1>
          <p className="text-sm text-muted-foreground">
            Workspace credentials and governance policies. Showing mock data
            until live wiring lands; policy edit-paths are gated until
            JEH-1179 ships the Persona grants admin API.
          </p>
        </div>
        <Badge variant="outline">{visible.length} credentials</Badge>
      </header>

      <Input
        type="search"
        placeholder="Search by name, type, or value…"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        className="max-w-md"
      />

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Type</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Value</TableHead>
            <TableHead>Last rotated</TableHead>
            <TableHead>Expiry</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {visible.map((c) => (
            <TableRow
              key={c.id}
              data-testid={`credential-row-${c.id}`}
              className="cursor-pointer"
              onClick={() => setSelectedId(c.id)}
            >
              <TableCell className="font-medium">{c.name}</TableCell>
              <TableCell>
                <Badge variant="outline">{CREDENTIAL_TYPE_LABEL[c.type]}</Badge>
              </TableCell>
              <TableCell>
                <CredentialStatusBadge status={c.status} />
              </TableCell>
              <TableCell className="font-mono text-xs">{c.redacted_value}</TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {c.last_rotated_at
                  ? new Date(c.last_rotated_at).toLocaleDateString()
                  : "—"}
              </TableCell>
              <TableCell>
                <ExpiryBadge expiresAt={c.expires_at} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      {selected ? (
        <CredentialDetailPage
          credential={selected}
          onClose={() => setSelectedId(null)}
        />
      ) : null}
    </div>
  );
}
