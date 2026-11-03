"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  knowledgePoliciesOptions,
  useDeleteKnowledgePolicy,
  useSetKnowledgePolicy,
  type KnowledgeBase,
  type KnowledgeKind,
  type KnowledgePublishMode,
} from "@multica/core/knowledge";
import { useT } from "../i18n";

const EDITABLE_MODES = ["manual", "auto_after_review", "auto"] as const;
type EditableMode = (typeof EDITABLE_MODES)[number];

/**
 * The publish policy of every knowledge base and kind. An agent's draft follows
 * its kind's policy when there is one, else its base's; a scope with no row is
 * manual. Owners and admins change it; the server enforces the same rule.
 */
export function KnowledgePolicyList({
  wsId,
  knowledgeBases,
  kinds,
}: {
  wsId: string;
  knowledgeBases: KnowledgeBase[];
  kinds: KnowledgeKind[];
}) {
  const { t } = useT("knowledge");
  const policies = useQuery(knowledgePoliciesOptions(wsId));
  const setPolicy = useSetKnowledgePolicy(wsId);
  const deletePolicy = useDeleteKnowledgePolicy(wsId);

  const modeLabel = (mode: KnowledgePublishMode): string => {
    switch (mode) {
      case "manual":
        return t(($) => $.taxonomy.policy.mode.manual);
      case "auto_after_review":
        return t(($) => $.taxonomy.policy.mode.auto_after_review);
      case "auto":
        return t(($) => $.taxonomy.policy.mode.auto);
      default:
        return t(($) => $.taxonomy.policy.mode.unknown);
    }
  };

  const rows: Array<{ scope: "base" | "kind"; key: string; label: string }> = [
    ...knowledgeBases.map((kb) => ({ scope: "base" as const, key: kb.key, label: kb.name || kb.key })),
    ...kinds.map((kind) => ({ scope: "kind" as const, key: kind.key, label: kind.name || kind.key })),
  ];
  const explicit = (scope: string, key: string) =>
    policies.data?.policies.find((p) => p.scope === scope && p.key === key);
  const defaultMode = policies.data?.default_mode ?? "manual";

  const choose = (scope: "base" | "kind", key: string, mode: EditableMode) =>
    setPolicy.mutate(
      { scope, key, mode },
      {
        onError: () => toast.error(t(($) => $.taxonomy.policy.toast_failed)),
      },
    );

  const reset = (scope: "base" | "kind", key: string) =>
    deletePolicy.mutate(
      { scope, key },
      {
        onSuccess: () => toast.success(t(($) => $.taxonomy.policy.toast_reset)),
        onError: () => toast.error(t(($) => $.taxonomy.policy.toast_failed)),
      },
    );

  return (
    <section className="flex flex-col gap-3" aria-labelledby="knowledge-policy-title">
      <div className="flex flex-col gap-1">
        <h2 id="knowledge-policy-title" className="text-body font-medium">
          {t(($) => $.taxonomy.policy.title)}
        </h2>
        <p className="text-caption text-muted-foreground">{t(($) => $.taxonomy.policy.description)}</p>
      </div>
      {policies.isError ? (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.taxonomy.policy.load_error)}
        </p>
      ) : (
        <ul className="flex flex-col divide-y rounded-lg border">
          {rows.map((row) => {
            const policy = explicit(row.scope, row.key);
            const current = policy?.mode ?? defaultMode;
            const editable = (EDITABLE_MODES as readonly string[]).includes(current);
            const items = EDITABLE_MODES.map((mode) => ({ value: mode, label: modeLabel(mode) }));
            return (
              <li key={`${row.scope}-${row.key}`} className="flex flex-wrap items-center justify-between gap-3 px-4 py-2.5">
                <div className="flex min-w-0 flex-col">
                  <span className="truncate text-body">{row.label}</span>
                  <span className="text-caption text-muted-foreground">
                    {row.scope === "base"
                      ? t(($) => $.taxonomy.policy.scope_base)
                      : t(($) => $.taxonomy.policy.scope_kind)}
                    {policy ? null : ` · ${t(($) => $.taxonomy.policy.inherited)}`}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  {editable ? (
                    <Select
                      items={items}
                      value={current}
                      onValueChange={(next) =>
                        next && choose(row.scope, row.key, next as EditableMode)
                      }
                    >
                      <SelectTrigger aria-label={t(($) => $.taxonomy.policy.aria_mode, { name: row.label })} className="w-56">
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
                  ) : (
                    <span className="text-caption text-muted-foreground">{modeLabel(current)}</span>
                  )}
                  {policy ? (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => reset(row.scope, row.key)}
                      disabled={deletePolicy.isPending}
                    >
                      {t(($) => $.taxonomy.policy.reset)}
                    </Button>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
