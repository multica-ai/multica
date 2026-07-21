"use client";

import { useMemo, useState } from "react";
import { api } from "@multica/core/api";
import type { MemberRole, ProvisionMembersResponse } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { parseProvisioningEmails } from "./member-provisioning";

type ProvisionableRole = Exclude<MemberRole, "owner">;

interface BulkMemberProvisionDialogProps {
  workspaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCompleted: () => void;
}

export function BulkMemberProvisionDialog({
  workspaceId,
  open,
  onOpenChange,
  onCompleted,
}: BulkMemberProvisionDialogProps) {
  const { t } = useT("settings");
  const [input, setInput] = useState("");
  const [role, setRole] = useState<ProvisionableRole>("member");
  const [submitting, setSubmitting] = useState(false);
  const [response, setResponse] = useState<ProvisionMembersResponse | null>(null);
  const [error, setError] = useState("");
  const parsed = useMemo(() => parseProvisioningEmails(input), [input]);
  const tooMany = parsed.emails.length > 100;
  const canSubmit = parsed.emails.length > 0 && parsed.invalid.length === 0 && !tooMany && !submitting;

  const reset = () => {
    setInput("");
    setRole("member");
    setResponse(null);
    setError("");
  };

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && !submitting) reset();
    onOpenChange(nextOpen);
  };

  const submit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setError("");
    try {
      const result = await api.provisionMembers(workspaceId, {
        entries: parsed.emails.map((email) => ({ email, role })),
      });
      setResponse(result);
      onCompleted();
    } catch (err) {
      setError(err instanceof Error ? err.message : t(($) => $.members.provision.error_generic));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.members.provision.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.members.provision.description)}</DialogDescription>
        </DialogHeader>

        {response ? (
          <div className="space-y-3">
            <p className="font-medium">
              {t(($) => $.members.provision.success_summary, {
                count: response.summary.created + response.summary.already_member,
              })}
            </p>
            <div className="grid grid-cols-2 gap-2 rounded-lg border p-3 text-xs text-muted-foreground sm:grid-cols-4">
              <span>{t(($) => $.members.provision.result_created, { count: response.summary.created })}</span>
              <span>{t(($) => $.members.provision.result_existing, { count: response.summary.already_member })}</span>
              <span>{t(($) => $.members.provision.result_invalid, { count: response.summary.invalid })}</span>
              <span>{t(($) => $.members.provision.result_failed, { count: response.summary.failed })}</span>
            </div>
            {response.results.some((result) => result.error) && (
              <ul className="max-h-36 space-y-1 overflow-y-auto rounded-lg bg-muted/50 p-3 text-xs">
                {response.results.filter((result) => result.error).map((result) => (
                  <li key={`${result.email}-${result.status}`}>
                    <span className="font-medium">{result.email}</span>: {result.error}
                  </li>
                ))}
              </ul>
            )}
          </div>
        ) : (
          <div className="space-y-4">
            <div className="space-y-1.5">
              <label htmlFor="bulk-member-emails" className="text-sm font-medium">
                {t(($) => $.members.provision.email_label)}
              </label>
              <Textarea
                id="bulk-member-emails"
                aria-label={t(($) => $.members.provision.email_label)}
                value={input}
                onChange={(event) => setInput(event.target.value)}
                placeholder={t(($) => $.members.provision.email_placeholder)}
                rows={8}
              />
              <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                <span>{t(($) => $.members.provision.valid_count, { count: parsed.emails.length })}</span>
                {parsed.duplicates.length > 0 && (
                  <span>{t(($) => $.members.provision.duplicate_count, { count: parsed.duplicates.length })}</span>
                )}
                {parsed.invalid.length > 0 && (
                  <span className="text-destructive">
                    {t(($) => $.members.provision.invalid_count, { count: parsed.invalid.length })}
                  </span>
                )}
                {tooMany && (
                  <span className="text-destructive">
                    {t(($) => $.members.provision.max_batch_size)}
                  </span>
                )}
              </div>
            </div>

            <div className="space-y-1.5">
              <label className="text-sm font-medium">{t(($) => $.members.provision.role_label)}</label>
              <Select
                items={(["member", "admin"] as const).map((value) => ({
                  value,
                  label: t(($) => $.members.roles[value].label),
                }))}
                value={role}
                onValueChange={(value) => setRole(value as ProvisionableRole)}
              >
                <SelectTrigger>
                  <SelectValue>{() => t(($) => $.members.roles[role].label)}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="member">
                    {t(($) => $.members.roles.member.label)}
                  </SelectItem>
                  <SelectItem value="admin">
                    {t(($) => $.members.roles.admin.label)}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>

            <p className="rounded-lg bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
              {t(($) => $.members.provision.no_invitation_notice)}
            </p>
            {error && <p className="text-sm text-destructive">{error}</p>}
          </div>
        )}

        <DialogFooter>
          {response ? (
            <Button onClick={() => handleOpenChange(false)}>{t(($) => $.members.provision.done)}</Button>
          ) : (
            <>
              <Button variant="outline" onClick={() => handleOpenChange(false)} disabled={submitting}>
                {t(($) => $.members.provision.cancel)}
              </Button>
              <Button onClick={submit} disabled={!canSubmit}>
                {submitting
                  ? t(($) => $.members.provision.submitting)
                  : t(($) => $.members.provision.submit)}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
