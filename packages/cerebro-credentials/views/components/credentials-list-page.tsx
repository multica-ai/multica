"use client";

import { useState } from "react";
import { KeyRound } from "lucide-react";

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

import { MOCK_CREDENTIALS } from "../mock-data";
import { CREDENTIAL_TYPE_LABEL } from "../types";
import type { Credential } from "../types";
import { ExpiryBadge } from "./expiry-badge";
import { CredentialStatusBadge } from "./credential-status-badge";
import { CredentialDetailPage } from "./credential-detail-page";

// STUB hook — swap for `useQuery({ queryKey: ["credentials", wsId], ... })`
// once JEH-1196 ships /api/credentials.
function useCredentials(): { data: Credential[]; isLoading: boolean } {
  return { data: MOCK_CREDENTIALS, isLoading: false };
}

export function CredentialsListPage() {
  const { data: credentials } = useCredentials();
  const [filter, setFilter] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);

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
            Workspace credentials and governance policies. Mock data — backend
            API arrives via JEH-1196.
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
