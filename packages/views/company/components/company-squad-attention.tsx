"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  SQUAD_ATTENTION_MAX_CAP,
  squadAttentionOptions,
  useSetSquadAttentionPin,
  type SquadAttention,
  type SquadAttentionReason,
} from "@multica/core/company";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";

import { useT } from "../../i18n";

/** The fewest decided proposals before a squad's limits move (spec 2.6). */
const ATTENTION_MIN_SAMPLE = 5;

const REASON_KEYS = {
  pinned: "attention_reason_pinned",
  sample_too_small: "attention_reason_sample_too_small",
  below_threshold: "attention_reason_below_threshold",
  above_threshold: "attention_reason_above_threshold",
  default: "attention_reason_default",
} as const satisfies Record<Exclude<SquadAttentionReason, "unknown">, string>;

/**
 * A squad's adaptive limits (spec 2.6): the caps filings are checked against, the
 * approval rate behind them and the reason in plain words. Owners and admins can
 * pin the limits or clear a pin; everyone else reads them.
 */
export function CompanySquadAttention({ squadId, canPin }: { squadId: string; canPin: boolean }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: attention } = useQuery(squadAttentionOptions(wsId, squadId));
  if (!attention) return null;

  // A reason a newer server adds has no copy, so it shows no reason line.
  const reasonKey = attention.reason === "unknown" ? null : REASON_KEYS[attention.reason];
  const reason = reasonKey
    ? t(($) => $.company[reasonKey], { count: ATTENTION_MIN_SAMPLE })
    : null;

  return (
    <section aria-label={t(($) => $.company.attention_title)} className="flex flex-col gap-2">
      <h3 className="text-caption font-medium text-muted-foreground">{t(($) => $.company.attention_title)}</h3>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-body">
        <dt className="text-muted-foreground">{t(($) => $.company.attention_pending_cap)}</dt>
        <dd className="tabular-nums">{attention.pending_cap}</dd>
        <dt className="text-muted-foreground">{t(($) => $.company.attention_weekly_cap)}</dt>
        <dd className="tabular-nums">{attention.weekly_cap}</dd>
        <dt className="text-muted-foreground">{t(($) => $.company.attention_acceptance)}</dt>
        <dd>
          {attention.acceptance_rate === null
            ? t(($) => $.company.attention_acceptance_none)
            : t(($) => $.company.attention_acceptance_value, {
                percent: Math.round(attention.acceptance_rate * 100),
                count: attention.sample_size,
              })}
        </dd>
      </dl>
      {reason ? <p className="text-caption text-muted-foreground">{reason}</p> : null}
      {canPin ? (
        <AttentionPinForm
          key={`${attention.pinned}:${attention.pinned_pending_cap}:${attention.pinned_weekly_cap}:${attention.pending_cap}:${attention.weekly_cap}`}
          squadId={squadId}
          attention={attention}
        />
      ) : null}
    </section>
  );
}

/** A whole number from 1 to the server's cap, or null when the input is not one. */
function parseCap(value: string): number | null {
  if (value.trim() === "") return null;
  const n = Number(value);
  return Number.isInteger(n) && n >= 1 && n <= SQUAD_ATTENTION_MAX_CAP ? n : null;
}

function AttentionPinForm({ squadId, attention }: { squadId: string; attention: SquadAttention }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const pin = useSetSquadAttentionPin(wsId, squadId);
  const [pending, setPending] = useState(String(attention.pinned_pending_cap ?? attention.pending_cap));
  const [weekly, setWeekly] = useState(String(attention.pinned_weekly_cap ?? attention.weekly_cap));

  const pendingCap = parseCap(pending);
  const weeklyCap = parseCap(weekly);
  const valid = pendingCap !== null && weeklyCap !== null;
  const range = t(($) => $.company.attention_pin_range, { min: 1, max: SQUAD_ATTENTION_MAX_CAP });

  const onPin = () => {
    if (pendingCap === null || weeklyCap === null) return;
    pin.mutate(
      { pinned: true, pending_cap: pendingCap, weekly_cap: weeklyCap },
      {
        onSuccess: () => toast.success(t(($) => $.company.attention_pin_saved)),
        onError: () => toast.error(t(($) => $.company.attention_pin_failed)),
      },
    );
  };
  const onClear = () =>
    pin.mutate(
      { pinned: false },
      {
        onSuccess: () => toast.success(t(($) => $.company.attention_pin_cleared)),
        onError: () => toast.error(t(($) => $.company.attention_pin_failed)),
      },
    );

  return (
    <div className="flex flex-col gap-2 border-t pt-3">
      <h4 className="text-caption font-medium text-muted-foreground">{t(($) => $.company.attention_pin_title)}</h4>
      <div className="flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1">
          <span className="text-caption text-muted-foreground">{t(($) => $.company.attention_pin_pending)}</span>
          <input
            type="number"
            inputMode="numeric"
            min={1}
            max={SQUAD_ATTENTION_MAX_CAP}
            step={1}
            value={pending}
            onChange={(event) => setPending(event.target.value)}
            className="h-9 w-20 rounded-lg border border-input bg-background px-3 text-body text-foreground tabular-nums focus-visible:outline-2 focus-visible:outline-ring"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-caption text-muted-foreground">{t(($) => $.company.attention_pin_weekly)}</span>
          <input
            type="number"
            inputMode="numeric"
            min={1}
            max={SQUAD_ATTENTION_MAX_CAP}
            step={1}
            value={weekly}
            onChange={(event) => setWeekly(event.target.value)}
            className="h-9 w-20 rounded-lg border border-input bg-background px-3 text-body text-foreground tabular-nums focus-visible:outline-2 focus-visible:outline-ring"
          />
        </label>
        <Button type="button" size="sm" disabled={!valid || pin.isPending} aria-busy={pin.isPending} onClick={onPin}>
          {t(($) => $.company.attention_pin_save)}
        </Button>
        {attention.pinned ? (
          <Button type="button" size="sm" variant="outline" disabled={pin.isPending} onClick={onClear}>
            {t(($) => $.company.attention_pin_clear)}
          </Button>
        ) : null}
      </div>
      {valid ? (
        <p className="text-caption text-muted-foreground">{range}</p>
      ) : (
        <p role="alert" className="text-caption text-destructive">
          {range}
        </p>
      )}
    </div>
  );
}
