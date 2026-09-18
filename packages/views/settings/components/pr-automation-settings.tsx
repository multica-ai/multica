"use client";

import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { prPolicyOptions, usePRPolicyMutations } from "@multica/core/github";
import type { PRPolicy, PRPolicyPlan } from "@multica/core/github";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

export function PRAutomationSettings({
  wsId,
  canManage,
}: {
  wsId: string;
  canManage: boolean;
}) {
  const { t } = useT("settings");
  const fieldId = useId();
  const query = useQuery(prPolicyOptions(wsId));
  const { preview, apply, sync } = usePRPolicyMutations(wsId);
  const [draft, setDraft] = useState<PRPolicy | null>(null);
  const [plan, setPlan] = useState<PRPolicyPlan | null>(null);
  const policy = draft ?? query.data?.policy;
  const busy = preview.isPending || apply.isPending || sync.isPending;
  if (!policy) return null;
  const items = [
    { value: "manual", label: t(($) => $.pr_automation.manual) },
    { value: "title_branch", label: t(($) => $.pr_automation.title_branch) },
    { value: "all", label: t(($) => $.pr_automation.all) },
  ];
  function change(next: PRPolicy) {
    setDraft(next);
    setPlan(null);
    preview.reset();
    apply.reset();
  }
  const affected =
    plan?.issues.filter(
      (i) => i.added.length || i.removed.length || i.decision.complete,
    ) ?? [];
  const error = preview.error ?? apply.error ?? sync.error;
  return (
    <section className="space-y-3">
      <h2 className="text-body font-semibold">
        {t(($) => $.pr_automation.title)}
      </h2>
      <Card>
        <CardContent className="space-y-4">
          {!query.data?.migrated && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.pr_automation.legacy)}
            </p>
          )}
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              try {
                setPlan(await preview.mutateAsync(policy));
              } catch {
                /* mutation error is shown below */
              }
            }}
          >
            <div className="space-y-2">
              <Label htmlFor={`${fieldId}-source`}>
                {t(($) => $.pr_automation.source)}
              </Label>
              <Select
                items={items}
                value={policy.source}
                disabled={!canManage || busy}
                onValueChange={(source) => {
                  if (
                    source === "manual" ||
                    source === "title_branch" ||
                    source === "all"
                  )
                    change({ ...policy, source });
                }}
              >
                <SelectTrigger id={`${fieldId}-source`}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {items.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-caption text-muted-foreground">
                {policy.source === "all"
                  ? t(($) => $.pr_automation.body_note)
                  : t(($) => $.pr_automation.keyword_note)}
              </p>
            </div>
            <div className="flex items-center justify-between gap-4">
              <Label htmlFor={`${fieldId}-complete`}>
                {t(($) => $.pr_automation.complete)}
              </Label>
              <Switch
                id={`${fieldId}-complete`}
                checked={policy.autoComplete}
                disabled={!canManage || busy}
                onCheckedChange={(autoComplete) =>
                  change({ ...policy, autoComplete })
                }
              />
            </div>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.pr_automation.merge_note)}
            </p>
            {canManage && (
              <Button
                type="submit"
                variant="outline"
                disabled={busy}
                aria-busy={preview.isPending}
              >
                {t(($) => $.pr_automation.preview)}
              </Button>
            )}
          </form>
          {plan && (
            <div className="space-y-3 rounded-md border p-3">
              <p className="text-caption">
                {t(($) => $.pr_automation.impact, {
                  count: affected.length,
                  complete: affected.filter((i) => i.decision.complete).length,
                })}
              </p>
              {affected.length > 100 && (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.pr_automation.batched)}
                </p>
              )}
              {plan.pending.length > 0 && (
                <div className="space-y-2">
                  <p className="text-caption text-muted-foreground">
                    {plan.pending.some((p) => p.startsWith("ambiguous:"))
                      ? t(($) => $.pr_automation.ambiguous)
                      : t(($) => $.pr_automation.pending)}
                  </p>
                  <Button
                    variant="outline"
                    disabled={busy}
                    aria-busy={sync.isPending}
                    onClick={async () => {
                      try {
                        await sync.mutateAsync();
                        setPlan(await preview.mutateAsync(policy));
                      } catch {
                        setPlan(null);
                      }
                    }}
                  >
                    {t(($) => $.pr_automation.sync)}
                  </Button>
                </div>
              )}
              {affected.length > 0 && (
                <details>
                  <summary className="cursor-pointer text-caption">
                    {t(($) => $.pr_automation.details)}
                  </summary>
                  <ul className="mt-2 max-h-64 space-y-2 overflow-auto text-caption">
                    {affected.map((i) => (
                      <li key={i.id}>
                        <span className="font-medium">
                          {i.identifier} · {i.title}
                        </span>
                        <div className="text-muted-foreground">
                          {t(($) => $.pr_automation.link_changes, {
                            added: i.added.length,
                            removed: i.removed.length,
                          })}
                          {i.decision.complete
                            ? ` · ${t(($) => $.pr_automation.will_complete)}`
                            : ""}
                        </div>
                        {[...i.added, ...i.removed].map((link) => (
                          <div key={link.prId} className="truncate">
                            {link.title}
                          </div>
                        ))}
                      </li>
                    ))}
                  </ul>
                </details>
              )}
              <Button
                disabled={busy}
                aria-busy={apply.isPending}
                onClick={async () => {
                  try {
                    await apply.mutateAsync({
                      policy: plan.policy,
                      token: plan.token,
                    });
                    setPlan(null);
                    setDraft(null);
                  } catch {
                    setPlan(null);
                  }
                }}
              >
                {t(($) => $.pr_automation.save)}
              </Button>
            </div>
          )}
          {error && (
            <p role="alert" className="text-caption text-destructive">
              {error.message}
            </p>
          )}
        </CardContent>
      </Card>
    </section>
  );
}
