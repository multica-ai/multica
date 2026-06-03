"use client";

import { useState } from "react";
import { RotateCw, Trash2 } from "lucide-react";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
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
import {
  useCredentialAudit,
  useCredentialBindings,
  useRevokeCredential,
  useRotateCredential,
} from "../queries";
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
  const [localCredential, setLocalCredential] = useState(credential);
  const [isRevoked, setIsRevoked] = useState(false);
  // Policies are Persona grants (JEH-1197 + JEH-1179); no /policies endpoint
  // on the credential resource. UI shows whatever the local stub knows about
  // until the Persona grant admin API lands. Read-only.
  const policies = MOCK_POLICIES.filter((p) => p.credential_id === localCredential.id);
  const { data: audit = [] } = useCredentialAudit(wsId, localCredential.id);
  const { data: bindings = [] } = useCredentialBindings(wsId, localCredential.id);
  const rotateCredential = useRotateCredential(wsId, localCredential.id);
  const revokeCredential = useRevokeCredential(wsId, localCredential.id);
  const isQAFixture = localCredential.metadata?.qa_fixture === true;

  return (
    <Dialog open onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <div className="flex items-start justify-between gap-4">
            <div>
              <DialogTitle className="flex items-center gap-2">
                {localCredential.name}
                <CredentialStatusBadge status={isRevoked ? "revoked" : localCredential.status} />
              </DialogTitle>
              <DialogDescription>
                {CREDENTIAL_TYPE_LABEL[localCredential.type]} ·{" "}
                <span className="font-mono">{localCredential.redacted_value}</span>
              </DialogDescription>
            </div>
            {isQAFixture ? (
              <div className="flex shrink-0 items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    rotateCredential.mutate(undefined, {
                      onSuccess: (updated) => setLocalCredential(updated),
                    });
                  }}
                  disabled={isRevoked || rotateCredential.isPending}
                >
                  <RotateCw className="size-4" />
                  {rotateCredential.isPending ? "Rotating..." : "Rotate fake value"}
                </Button>
                <AlertDialog>
                  <AlertDialogTrigger
                    render={
                      <Button
                        type="button"
                        variant="destructive"
                        size="sm"
                        disabled={isRevoked || revokeCredential.isPending}
                      />
                    }
                  >
                    <Trash2 className="size-4" />
                    {revokeCredential.isPending ? "Revoking..." : "Revoke"}
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>Revoke credential?</AlertDialogTitle>
                      <AlertDialogDescription>
                        This removes the fake QA credential and leaves the audit trail available for verification.
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>Cancel</AlertDialogCancel>
                      <AlertDialogAction
                        onClick={() => {
                          revokeCredential.mutate(undefined, {
                            onSuccess: () => setIsRevoked(true),
                          });
                        }}
                      >
                        Revoke
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </div>
            ) : null}
          </div>
        </DialogHeader>
        {rotateCredential.isError || revokeCredential.isError ? (
          <p className="text-sm text-destructive">
            Credential action failed. Check your credential governance permissions.
          </p>
        ) : null}

        <Tabs defaultValue="overview" className="mt-2">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="policy">Policy ({policies.length})</TabsTrigger>
            <TabsTrigger value="audit">Audit ({audit.length})</TabsTrigger>
            <TabsTrigger value="bindings">Bindings ({bindings.length})</TabsTrigger>
          </TabsList>

          <TabsContent value="overview" className="space-y-3 pt-4 text-sm">
            {localCredential.description ? (
              <p className="text-muted-foreground">{localCredential.description}</p>
            ) : null}
            <dl className="grid grid-cols-[120px_1fr] gap-y-2">
              <dt className="text-muted-foreground">Type</dt>
              <dd>{CREDENTIAL_TYPE_LABEL[localCredential.type]}</dd>
              <dt className="text-muted-foreground">Status</dt>
              <dd>
                <CredentialStatusBadge status={isRevoked ? "revoked" : localCredential.status} />
              </dd>
              <dt className="text-muted-foreground">Created</dt>
              <dd>{new Date(localCredential.created_at).toLocaleString()}</dd>
              <dt className="text-muted-foreground">Last rotated</dt>
              <dd>
                {localCredential.last_rotated_at
                  ? new Date(localCredential.last_rotated_at).toLocaleString()
                  : "-"}
              </dd>
              <dt className="text-muted-foreground">Expires</dt>
              <dd>
                <ExpiryBadge expiresAt={localCredential.expires_at} />
              </dd>
            </dl>
          </TabsContent>

          <TabsContent value="policy" className="pt-4">
            <PolicyEditor credentialId={localCredential.id} initialPolicies={policies} />
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
