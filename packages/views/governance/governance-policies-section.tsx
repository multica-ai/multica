"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  POLICY_KINDS,
  approvalPoliciesOptions,
  policyAutoApprovalsOptions,
  useCreateApprovalPolicy,
  useRevokeApprovalPolicy,
  type ApprovalPolicy,
  type CreateApprovalPolicyRequest,
  type PolicyKind,
} from "@multica/core/governance";
import type { Squad } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../i18n";

/** The most a policy may approve in one week (spec 4.4). */
const MAX_PER_WEEK_LIMIT = 50;
const DEFAULT_MAX_PER_WEEK = 3;

/**
 * Standing approvals (spec 4.4): the Policies list with each policy's weekly
 * count, its revoke action and its auto-approval history. Owners and admins create
 * and revoke; everyone reads.
 */
export function GovernancePoliciesSection({
  wsId,
  editable,
  squads,
}: {
  wsId: string;
  editable: boolean;
  squads: Squad[];
}) {
  const { t } = useT("governance");
  const { data: policies = [], isPending, isError } = useQuery(approvalPoliciesOptions(wsId));

  return (
    <section aria-label={t(($) => $.autonomy.policies_heading)} className="flex flex-col gap-3">
      <h2 className="text-heading">{t(($) => $.autonomy.policies_heading)}</h2>
      <p className="max-w-prose text-caption text-muted-foreground">{t(($) => $.autonomy.policies_note)}</p>
      {editable ? <CreatePolicyForm wsId={wsId} squads={squads} /> : null}
      {isError ? (
        <p className="text-body text-destructive">{t(($) => $.autonomy.policies_load_failed)}</p>
      ) : isPending ? null : policies.length === 0 ? (
        <p className="text-body text-muted-foreground">{t(($) => $.autonomy.policies_empty)}</p>
      ) : (
        <ul className="flex flex-col divide-y rounded-md border">
          {policies.map((policy) => (
            <PolicyRow key={policy.id} wsId={wsId} policy={policy} editable={editable} />
          ))}
        </ul>
      )}
    </section>
  );
}

function PolicyRow({ wsId, policy, editable }: { wsId: string; policy: ApprovalPolicy; editable: boolean }) {
  const { t } = useT("governance");
  const [showHistory, setShowHistory] = useState(false);
  const revoke = useRevokeApprovalPolicy(wsId);
  const squadCount = policy.match.squad_ids?.length ?? 0;
  const kindLabel = (POLICY_KINDS as readonly string[]).includes(policy.kind)
    ? t(($) => $.autonomy.policy_kind[policy.kind as PolicyKind])
    : policy.kind;

  return (
    <li className="flex flex-col gap-2 px-4 py-3">
      <div className="flex min-w-0 flex-wrap items-baseline gap-2">
        <span className="min-w-0 truncate text-body font-medium">{policy.name}</span>
        <span className="text-caption text-muted-foreground">{kindLabel}</span>
        <span className="text-caption text-muted-foreground">
          {policy.active ? t(($) => $.autonomy.policy_active) : t(($) => $.autonomy.policy_revoked)}
        </span>
        {policy.flagged ? (
          <span className="rounded-sm bg-destructive/10 px-1.5 text-caption text-destructive">
            {t(($) => $.autonomy.policy_flagged)}
          </span>
        ) : null}
      </div>
      <p className="text-caption text-muted-foreground">
        {squadCount > 0
          ? t(($) => $.autonomy.policy_scope_squads, { count: squadCount })
          : t(($) => $.autonomy.policy_scope_any)}
        {" · "}
        {t(($) => $.autonomy.policy_week, { count: policy.week_approvals, max: policy.max_per_week })}
        {policy.valid_until ? ` · ${t(($) => $.autonomy.policy_until, { date: policy.valid_until })}` : ""}
      </p>
      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="ghost" size="sm" aria-expanded={showHistory} onClick={() => setShowHistory((v) => !v)}>
          {showHistory ? t(($) => $.autonomy.policy_history_hide) : t(($) => $.autonomy.policy_history)}
        </Button>
        {editable && policy.active ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={revoke.isPending}
            onClick={() =>
              revoke.mutate(policy.id, {
                onSuccess: () => toast.success(t(($) => $.autonomy.policy_revoked_toast)),
                onError: () => toast.error(t(($) => $.autonomy.policy_revoke_failed)),
              })
            }
          >
            {t(($) => $.autonomy.policy_revoke)}
          </Button>
        ) : null}
      </div>
      {showHistory ? <PolicyHistory wsId={wsId} policyId={policy.id} /> : null}
    </li>
  );
}

/** A policy's recent auto-approvals, with the reversals a review recorded. */
function PolicyHistory({ wsId, policyId }: { wsId: string; policyId: string }) {
  const { t } = useT("governance");
  const { data, isPending, isError } = useQuery(policyAutoApprovalsOptions(wsId, policyId));
  if (isPending) return null;
  if (isError) return <p className="text-caption text-destructive">{t(($) => $.autonomy.history_load_failed)}</p>;
  const entries = data?.auto_approvals ?? [];
  if (entries.length === 0) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.autonomy.history_empty)}</p>;
  }
  return (
    <ul className="flex flex-col gap-1 border-l pl-3">
      {entries.map((entry) => (
        <li key={entry.proposal_id} className="flex flex-col gap-0.5 text-caption">
          <span className="text-body">
            {entry.identifier ? `${entry.identifier} ` : ""}
            {entry.title}
          </span>
          <span className="text-muted-foreground">
            {new Date(entry.decided_at).toLocaleDateString()}
          </span>
          {entry.flagged ? (
            <span className="text-destructive">
              {t(($) => $.autonomy.history_reversed, { note: entry.reversal_note || "" })}
            </span>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

/** The form that creates a policy. Empty squads means any squad at the right level. */
function CreatePolicyForm({ wsId, squads }: { wsId: string; squads: Squad[] }) {
  const { t } = useT("governance");
  const create = useCreateApprovalPolicy(wsId);
  const [name, setName] = useState("");
  const [kind, setKind] = useState<PolicyKind>("decision");
  const [picked, setPicked] = useState<string[]>([]);
  const [maxPerWeek, setMaxPerWeek] = useState(String(DEFAULT_MAX_PER_WEEK));
  const [maxCost, setMaxCost] = useState("");

  const trimmed = name.trim();
  const weekly = Number(maxPerWeek);
  const weeklyValid = Number.isInteger(weekly) && weekly >= 1 && weekly <= MAX_PER_WEEK_LIMIT;
  const costValid = maxCost === "" || (Number.isInteger(Number(maxCost)) && Number(maxCost) >= 0);
  const canSubmit = trimmed !== "" && weeklyValid && costValid && !create.isPending;

  return (
    <form
      className="flex max-w-prose flex-col gap-3 rounded-md border p-4"
      aria-label={t(($) => $.autonomy.create_heading)}
      onSubmit={(event) => {
        event.preventDefault();
        if (!canSubmit) return;
        const body: CreateApprovalPolicyRequest = {
          name: trimmed,
          kind,
          match: {
            squad_ids: picked,
            ...(maxCost !== "" ? { max_cost_micros: Number(maxCost) } : {}),
          },
          max_per_week: weekly,
        };
        create.mutate(body, {
          onSuccess: () => {
            toast.success(t(($) => $.autonomy.create_done));
            setName("");
            setPicked([]);
            setMaxCost("");
            setMaxPerWeek(String(DEFAULT_MAX_PER_WEEK));
          },
          onError: () => toast.error(t(($) => $.autonomy.create_failed)),
        });
      }}
    >
      <h3 className="text-body font-medium">{t(($) => $.autonomy.create_heading)}</h3>
      <div className="flex flex-col gap-1">
        <Label htmlFor="policy-name" className="text-caption">
          {t(($) => $.autonomy.create_name)}
        </Label>
        <Input id="policy-name" value={name} maxLength={100} onChange={(event) => setName(event.target.value)} />
      </div>
      <div className="flex flex-col gap-1">
        <Label className="text-caption">{t(($) => $.autonomy.create_kind)}</Label>
        <Select
          items={POLICY_KINDS.map((value) => ({ value, label: t(($) => $.autonomy.policy_kind[value]) }))}
          value={kind}
          onValueChange={(value) => {
            if (value) setKind(value as PolicyKind);
          }}
        >
          <SelectTrigger aria-label={t(($) => $.autonomy.create_kind)} className="w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {POLICY_KINDS.map((value) => (
              <SelectItem key={value} value={value}>
                {t(($) => $.autonomy.policy_kind[value])}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <fieldset className="flex flex-col gap-1">
        <legend className="text-caption">{t(($) => $.autonomy.create_squads)}</legend>
        <p className="text-caption text-muted-foreground">{t(($) => $.autonomy.create_squads_hint)}</p>
        {squads.map((squad) => (
          <label key={squad.id} className="flex items-center gap-2 text-body">
            <Checkbox
              checked={picked.includes(squad.id)}
              onCheckedChange={(checked) =>
                setPicked((current) => (checked ? [...current, squad.id] : current.filter((id) => id !== squad.id)))
              }
            />
            {squad.name}
          </label>
        ))}
      </fieldset>
      <div className="flex flex-wrap gap-3">
        <div className="flex flex-col gap-1">
          <Label htmlFor="policy-max-week" className="text-caption">
            {t(($) => $.autonomy.create_max_per_week)}
          </Label>
          <Input
            id="policy-max-week"
            type="number"
            min={1}
            max={MAX_PER_WEEK_LIMIT}
            value={maxPerWeek}
            className="w-28"
            onChange={(event) => setMaxPerWeek(event.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="policy-max-cost" className="text-caption">
            {t(($) => $.autonomy.create_max_cost)}
          </Label>
          <Input
            id="policy-max-cost"
            type="number"
            min={0}
            value={maxCost}
            className="w-40"
            onChange={(event) => setMaxCost(event.target.value)}
          />
        </div>
      </div>
      <div>
        <Button type="submit" size="sm" disabled={!canSubmit}>
          {t(($) => $.autonomy.create_submit)}
        </Button>
      </div>
    </form>
  );
}
