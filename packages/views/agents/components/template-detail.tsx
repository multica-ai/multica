"use client";

import { Check, ChevronRight, Loader2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { agentTemplateDetailOptions } from "@multica/core/agents/queries";
import type { AgentTemplateSummary } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { getAccentClass, getTemplateIcon } from "./template-picker";

interface TemplateDetailProps {
  template: AgentTemplateSummary;
  /** Fired when the user clicks "Use this template" — the dialog calls
   *  the create API and navigates to the new agent. */
  onUse: (template: AgentTemplateSummary) => void;
  /** True while the parent's create request is in flight; we disable the
   *  Use button so the user can't double-click. */
  creating?: boolean;
  /** Upstream URLs the server reported as unreachable on the most recent
   *  create attempt. Surfaces an inline error banner so the user knows
   *  *why* Create didn't navigate. The detail step is the only place
   *  this banner can render — `quickCreateFromTemplate` fires from here
   *  and never advances to a different step on failure. */
  failedURLs?: readonly string[] | null;
}

/**
 * Step 3 of the create-agent flow: a read-only preview of the picked
 * template — instructions, skill list with cached descriptions, and a
 * "Use this template" CTA at the bottom. Clicking Use kicks off a
 * one-shot create with default settings (no form step in between).
 *
 * Instructions come from the lazy-fetched detail endpoint (the picker
 * only carries the summary). Cached through TanStack Query keyed by
 * slug with `staleTime: Infinity`, so navigating back and forth between
 * picker and detail doesn't re-fetch. Visual rhythm matches the picker
 * card so the transition feels seamless.
 */
export function TemplateDetail({
  template,
  onUse,
  creating = false,
  failedURLs,
}: TemplateDetailProps) {
  const { t } = useT("agents");
  const { data: detail, isLoading, error } = useQuery(
    agentTemplateDetailOptions(template.slug),
  );

  const Icon = getTemplateIcon(template.icon);
  const accentClass = getAccentClass(template.accent);

  return (
    <>
      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl p-6">
          {/* failedURLs banner — sits above the header so it's the first
              thing the user sees after the spinner clears on a 422. */}
          {failedURLs && failedURLs.length > 0 && (
            <div className="mb-5 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm">
              <div className="font-medium text-destructive">
                {t(($) => $.create_dialog.template_failure.title)}
              </div>
              <div className="mt-1 text-xs text-muted-foreground">
                {t(($) => $.create_dialog.template_failure.body)}
              </div>
              <ul className="mt-2 space-y-0.5 text-xs">
                {failedURLs.map((u) => (
                  <li key={u} className="break-all font-mono">
                    {u}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* Header: icon + name + description. Same rhythm as the picker
              card so the user reads the transition as "the same item,
              expanded". */}
          <div className="flex items-start gap-3">
            <div className={cn("flex h-12 w-12 shrink-0 items-center justify-center rounded-lg", accentClass)}>
              <Icon className="h-6 w-6" />
            </div>
            <div className="min-w-0 flex-1">
              <h2 className="text-lg font-semibold">{template.name}</h2>
              <p className="mt-0.5 text-sm text-muted-foreground">{template.description}</p>
              {template.category ? (
                <div className="mt-2 inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  {template.category}
                </div>
              ) : null}
            </div>
          </div>

          {/* Skill list — always visible (summary has cached descriptions) */}
          <section className="mt-6">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              {t(($) => $.create_dialog.template_detail.skill_count, {
                count: template.skills.length,
              })}
            </h3>
            <ul className="mt-3 space-y-2">
              {template.skills.map((s) => (
                <li
                  key={s.source_url}
                  className="rounded-lg border bg-card px-3 py-2.5"
                >
                  <div className="flex items-center gap-2">
                    <Check className="h-4 w-4 text-success" />
                    <span className="font-mono text-xs font-medium">{s.cached_name}</span>
                  </div>
                  {s.cached_description ? (
                    <p className="mt-1 ml-6 text-xs text-muted-foreground">
                      {s.cached_description}
                    </p>
                  ) : null}
                </li>
              ))}
            </ul>
          </section>

          {/* Instructions — lazy fetch + loading/error states */}
          <section className="mt-6">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              {t(($) => $.create_dialog.template_detail.instructions_label)}
            </h3>
            <div className="mt-3 rounded-lg border bg-muted/30 px-4 py-3">
              {isLoading ? (
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  {t(($) => $.create_dialog.template_detail.instructions_loading)}
                </div>
              ) : error ? (
                <div className="text-xs text-destructive">
                  {error instanceof Error
                    ? error.message
                    : t(($) => $.create_dialog.template_detail.load_failed)}
                </div>
              ) : (
                <pre className="max-h-60 overflow-y-auto whitespace-pre-wrap text-xs leading-relaxed text-foreground/80">
                  {detail?.instructions ?? ""}
                </pre>
              )}
            </div>
          </section>
        </div>
      </div>

      {/* Sticky CTA footer — click Use kicks off the create API call;
          parent shows a creating spinner and navigates on success. */}
      <div className="flex items-center justify-end gap-2 border-t bg-background px-5 py-3">
        <Button
          onClick={() => onUse(template)}
          disabled={creating}
          className="gap-1.5"
        >
          {creating ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" />
              {t(($) => $.create_dialog.template_detail.creating)}
            </>
          ) : (
            <>
              {t(($) => $.create_dialog.template_detail.use)}
              <ChevronRight className="h-4 w-4" />
            </>
          )}
        </Button>
      </div>
    </>
  );
}
