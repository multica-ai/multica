"use client";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@multica/ui/components/ui/tabs";

import { useWorkspaceId } from "@multica/core/hooks";
import type { Credential } from "../types";
import { CREDENTIAL_TYPE_LABEL } from "../types";
import { MOCK_POLICIES } from "../mock-data";
import { useCredentialAudit, useCredentialBindings } from "../queries";
import { PolicyEditor } from "./policy-editor";
import { AuditLogView } from "./audit-log-view";
import { CredentialStatusBadge } from "./credential-status-badge";
import { ExpiryBadge } from "./expiry-badge";

export function CredentialDetailPage({
  credential,
  onClose,
}: {
  credential: Credential;
  onClose: () => void;
}) {
  const wsId = useWorkspaceId();
  // Policies are Persona grants (JEH-1197 + JEH-1179); no /policies endpoint
  // on the credential resource. UI shows whatever the local stub knows about
  // until the Persona grant admin API lands. Read-only.
  const policies = MOCK_POLICIES.filter((p) => p.credential_id === credential.id);
  const { data: audit = [] } = useCredentialAudit(wsId, credential.id);
  const { data: bindings = [] } = useCredentialBindings(wsId, credential.id);

  return (
    <Dialog open onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {credential.name}
            <CredentialStatusBadge status={credential.status} />
          </DialogTitle>
          <DialogDescription>
            {CREDENTIAL_TYPE_LABEL[credential.type]} ·{" "}
            <span className="font-mono">{credential.redacted_value}</span>
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="overview" className="mt-2">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="policy">
              Policy ({policies.length})
            </TabsTrigger>
            <TabsTrigger value="audit">Audit ({audit.length})</TabsTrigger>
            <TabsTrigger value="bindings">
              Bindings ({bindings.length})
            </TabsTrigger>
          </TabsList>

          <TabsContent value="overview" className="space-y-3 pt-4 text-sm">
            {credential.description ? (
              <p className="text-muted-foreground">{credential.description}</p>
            ) : null}
            <dl className="grid grid-cols-[120px_1fr] gap-y-2">
              <dt className="text-muted-foreground">Type</dt>
              <dd>{CREDENTIAL_TYPE_LABEL[credential.type]}</dd>
              <dt className="text-muted-foreground">Status</dt>
              <dd>
                <CredentialStatusBadge status={credential.status} />
              </dd>
              <dt className="text-muted-foreground">Created</dt>
              <dd>{new Date(credential.created_at).toLocaleString()}</dd>
              <dt className="text-muted-foreground">Last rotated</dt>
              <dd>
                {credential.last_rotated_at
                  ? new Date(credential.last_rotated_at).toLocaleString()
                  : "—"}
              </dd>
              <dt className="text-muted-foreground">Expires</dt>
              <dd>
                <ExpiryBadge expiresAt={credential.expires_at} />
              </dd>
            </dl>
          </TabsContent>

          <TabsContent value="policy" className="pt-4">
            <PolicyEditor credentialId={credential.id} initialPolicies={policies} />
          </TabsContent>

          <TabsContent value="audit" className="pt-4">
            <AuditLogView entries={audit} />
          </TabsContent>

          <TabsContent value="bindings" className="pt-4">
            {bindings.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                Not bound to any resource.
              </p>
            ) : (
              <ul className="space-y-2 text-sm">
                {bindings.map((b) => (
                  <li
                    key={b.id}
                    className="flex items-center justify-between rounded border p-2"
                  >
                    <span className="font-medium">{b.resource_label}</span>
                    <span className="text-xs text-muted-foreground">
                      {b.resource_kind} · attached{" "}
                      {new Date(b.created_at).toLocaleDateString()}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
