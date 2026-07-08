"use client";

import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { agentTemplateDetailOptions } from "@multica/core/agents/queries";
import type { AgentTemplateSummary } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { getAccentClass, getTemplateIcon } from "./template-picker";

interface TemplateDetailProps {
  template: AgentTemplateSummary;
  onUse: (template: AgentTemplateSummary) => void;
  creating: boolean;
  failedURLs: string[] | null;
}

export function TemplateDetail({
  template,
  onUse,
  creating,
  failedURLs,
}: TemplateDetailProps) {
  const { t } = useT("agents");
  const Icon = getTemplateIcon(template.icon);
  const accentClass = getAccentClass(template.accent);
  const { data, isLoading, error } = useQuery(agentTemplateDetailOptions(template.slug));
  const detail = data ?? template;
  const instructions =
    "instructions" in detail && typeof detail.instructions === "string"
      ? detail.instructions
      : "";

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div className="flex-1 overflow-y-auto p-6">
        <div className="mx-auto max-w-3xl space-y-5">
          <div className="flex items-start gap-4">
            <div
              className={cn(
                "flex h-12 w-12 shrink-0 items-center justify-center rounded-xl",
                accentClass,
              )}
            >
              <Icon className="h-6 w-6" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="text-lg font-semibold">{detail.name}</div>
              <p className="mt-1 text-sm text-muted-foreground">
                {detail.description}
              </p>
              <div className="mt-3 inline-flex rounded-full bg-muted px-2.5 py-1 text-xs font-medium text-muted-foreground">
                {t(($) => $.create_dialog.template_detail.skill_count, {
                  count: detail.skills.length,
                })}
              </div>
            </div>
          </div>

          {failedURLs && failedURLs.length > 0 ? (
            <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 text-sm">
              <div className="font-medium text-destructive">
                {t(($) => $.create_dialog.template_failure.title)}
              </div>
              <p className="mt-1 text-muted-foreground">
                {t(($) => $.create_dialog.template_failure.body)}
              </p>
              <ul className="mt-3 list-disc space-y-1 pl-5 text-xs text-muted-foreground">
                {failedURLs.map((url) => (
                  <li key={url} className="break-all">
                    {url}
                  </li>
                ))}
              </ul>
            </div>
          ) : null}

          <section className="rounded-lg border bg-card p-4">
            <h3 className="text-sm font-semibold">
              {t(($) => $.create_dialog.template_detail.instructions_label)}
            </h3>
            {isLoading ? (
              <div className="mt-4 flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
                {t(($) => $.create_dialog.template_detail.instructions_loading)}
              </div>
            ) : error ? (
              <div className="mt-3 text-sm text-destructive">
                {error instanceof Error
                  ? error.message
                  : t(($) => $.create_dialog.template_detail.load_failed)}
              </div>
            ) : (
              <pre className="mt-3 max-h-[340px] overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 text-xs leading-relaxed text-muted-foreground">
                {instructions}
              </pre>
            )}
          </section>

          {detail.skills.length > 0 ? (
            <section className="rounded-lg border bg-card p-4">
              <h3 className="text-sm font-semibold">
                {t(($) => $.create_dialog.skills_section.label)}
              </h3>
              <div className="mt-3 space-y-2">
                {detail.skills.map((skill) => (
                  <div key={skill.source_url} className="rounded-md bg-muted p-3">
                    <div className="text-sm font-medium">{skill.cached_name}</div>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {skill.cached_description}
                    </p>
                  </div>
                ))}
              </div>
            </section>
          ) : null}
        </div>
      </div>

      <div className="flex items-center justify-end gap-2 border-t bg-background px-5 py-3">
        <Button onClick={() => onUse(template)} disabled={creating || isLoading}>
          {creating
            ? t(($) => $.create_dialog.template_detail.creating)
            : t(($) => $.create_dialog.template_detail.use)}
        </Button>
      </div>
    </div>
  );
}
