"use client";

import { useState } from "react";
import { RotateCw, Trash2 } from "lucide-react";
import { toast } from "sonner";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Textarea } from "@multica/ui/components/ui/textarea";
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
  const [rotateValue, setRotateValue] = useState("");
  const [confirmRevoke, setConfirmRevoke] = useState(false);
  // Policies are Persona grants (JEH-1197 + JEH-1179); no /policies endpoint
  // on the credential resource. UI shows whatever the local stub knows about
  // until the Persona grant admin API lands. Read-only.
  const policies = MOCK_POLICIES.filter((p) => p.credential_id === credential.id);
  const { data: audit = [] } = useCredentialAudit(wsId, credential.id);
  const { data: bindings = [] } = useCredentialBindings(wsId, credential.id);
  const rotate = useRotateCredential(wsId, credential.id);
  const revoke = useRevokeCredential(wsId, credential.id);
  const canRotate = rotateValue.trim().length > 0 && !rotate.isPending;

  async function onRotate() {
    if (!canRotate) return;
    try {
      await rotate.mutateAsync(rotateValue);
      setRotateValue("");
      toast.success("Credential rotated");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to rotate credential");
    }
  }

  async function onRevoke() {
    try {
      await revoke.mutateAsync();
      toast.success("Credential revoked");
      setConfirmRevoke(false);
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to revoke credential");
    }
  }

  return (
    <>
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

            <TabsContent value="overview" className="space-y-5 pt-4 text-sm">
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

              <div className="space-y-3 border-t pt-4">
                <div>
                  <h3 className="text-sm font-medium">Rotate value</h3>
                  <p className="text-xs text-muted-foreground">
                    Replaces the encrypted secret and records a rotation audit row.
                  </p>
                </div>
                <Textarea
                  value={rotateValue}
                  onChange={(e) => setRotateValue(e.target.value)}
                  placeholder="Paste new secret value"
                  className="min-h-24 font-mono text-xs"
                />
                <div className="flex justify-between gap-3">
                  <Button
                    type="button"
                    variant="outline"
                    onClick={onRotate}
                    disabled={!canRotate}
                  >
                    <RotateCw className="size-4" />
                    {rotate.isPending ? "Rotating…" : "Rotate"}
                  </Button>
                  <Button
                    type="button"
                    variant="destructive"
                    onClick={() => setConfirmRevoke(true)}
                    disabled={revoke.isPending}
                  >
                    <Trash2 className="size-4" />
                    Revoke
                  </Button>
                </div>
              </div>
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

      <AlertDialog open={confirmRevoke} onOpenChange={setConfirmRevoke}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Revoke credential?</AlertDialogTitle>
            <AlertDialogDescription>
              This removes {credential.name} and writes a revoke audit row.
              Bound resources will stop using this credential.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={revoke.isPending}>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={onRevoke} disabled={revoke.isPending}>
              {revoke.isPending ? "Revoking…" : "Revoke"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
