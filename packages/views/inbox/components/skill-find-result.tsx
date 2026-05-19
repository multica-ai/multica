"use client";

import { ExternalLink, Sparkles } from "lucide-react";
import type { InboxItem } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  getInboxStringDetail,
  getSkillFindRecommendations,
} from "./inbox-display";
import { useT } from "../../i18n";

export function SkillFindResult({ item }: { item: InboxItem }) {
  const { t } = useT("inbox");
  const recommendations = getSkillFindRecommendations(item);
  const prompt = getInboxStringDetail(item, "original_prompt");

  return (
    <div className="space-y-4">
      {prompt && (
        <div className="rounded-md border bg-muted/40 p-3">
          <p className="text-xs font-medium text-muted-foreground">
            {t(($) => $.detail.original_input)}
          </p>
          <p className="mt-1 whitespace-pre-wrap text-sm">{prompt}</p>
        </div>
      )}
      {recommendations.length === 0 ? (
        <div className="rounded-md border bg-muted/40 p-4 text-sm text-muted-foreground">
          {t(($) => $.detail.skill_find_empty)}
        </div>
      ) : (
        <div className="grid gap-2">
          {recommendations.map((rec) => (
            <div key={rec.source_url} className="rounded-md border bg-card p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    <Sparkles className="h-3.5 w-3.5 shrink-0 text-brand" />
                    <span className="truncate">{rec.name}</span>
                  </div>
                  {rec.description && (
                    <p className="mt-1 text-xs text-muted-foreground">
                      {rec.description}
                    </p>
                  )}
                </div>
                <Button
                  render={<a href={rec.source_url} target="_blank" rel="noreferrer" />}
                  variant="ghost"
                  size="icon-sm"
                  className="shrink-0"
                >
                  <ExternalLink className="h-3.5 w-3.5" />
                </Button>
              </div>
              {rec.reason && (
                <p className="mt-3 text-sm leading-relaxed text-foreground/80">
                  {rec.reason}
                </p>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
