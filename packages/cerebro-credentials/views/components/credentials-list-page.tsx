"use client";

import { useState } from "react";
import { KeyRound, Plus, ShieldOff } from "lucide-react";

import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from "@multica/ui/components/ui/table";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useCurrentMember } from "@multica/core/permissions";
import { useWorkspaceId } from "@multica/core/hooks";

import { CREDENTIAL_TYPE_LABEL } from "../types";
import type { Credential } from "../types";
import { useCreateQAFixtureCredential, useCredentialsList } from "../queries";
import { ExpiryBadge } from "./expiry-badge";
import { CredentialStatusBadge } from "./credential-status-badge";
import { CredentialDetailPage } from "./credential-detail-page";

export function CredentialsListPage() {
  const wsId = useWorkspaceId();
  const { role, isLoading: isMemberLoading } = useCurrentMember(wsId);
  const { data: credentials = [], isLoading, isError } = useCredentialsList(wsId);
  const createFixture = useCreateQAFixtureCredential(wsId);
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<Credential | null>(null);

  if (!wsId || isMemberLoading) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Loading…
      </div>
    );
  }

  const isAdmin = role === "owner" || role === "admin";
  if (!isAdmin) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center">
        <ShieldOff className="size-8 text-muted-foreground" />
        <h2 className="text-base font-medium">Access denied</h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          Only workspace owners and admins can manage credentials.
          The backend gates all write operations via the policy checker
          (JEH-1197); this page is hidden in the UI as an extra layer.
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

  return (
    <div className="flex h-full flex-col gap-6 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            <KeyRound className="size-6 text-muted-foreground" />
            Credentials
          </h1>
          <p className="text-sm text-muted-foreground">
            Workspace credentials and governance policies.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              createFixture.mutate(undefined, {
                onSuccess: (credential) => setSelected(credential),
              });
            }}
            disabled={createFixture.isPending}
          >
            <Plus className="size-4" />
            {createFixture.isPending ? "Creating…" : "Create QA fixture"}
          </Button>
          <Badge variant="outline">
            {isLoading ? "Loading…" : `${visible.length} credentials`}
          </Badge>
        </div>
      </header>

      {isError ? (
        <p className="text-sm text-destructive">
          Failed to load credentials. The encryption key may not be configured
          on this server (MULTICA_CREDENTIALS_KEY).
        </p>
      ) : null}
      {createFixture.isError ? (
        <p className="text-sm text-destructive">
          Failed to create QA fixture.
        </p>
      ) : null}

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
              onClick={() => setSelected(c)}
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
          onClose={() => setSelected(null)}
        />
      ) : null}
    </div>
  );
}
