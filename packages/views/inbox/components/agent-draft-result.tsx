"use client";

import { Bot, ExternalLink, Sparkles } from "lucide-react";
import type { InboxItem } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import {
  getInboxStringArrayDetail,
  getInboxStringDetail,
} from "./inbox-display";
import { useT } from "../../i18n";

export function AgentDraftResult({ item }: { item: InboxItem }) {
  const { t } = useT("inbox");
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const agentId = getInboxStringDetail(item, "drafted_agent_id") || getInboxStringDetail(item, "agent_id");
  const name = getInboxStringDetail(item, "drafted_agent_name") || getInboxStringDetail(item, "name");
  const summary = getInboxStringDetail(item, "summary") || item.body;
  const prompt = getInboxStringDetail(item, "original_prompt");
  const skillURLs = getInboxStringArrayDetail(item, "skill_source_urls");

  return (
    <div className="space-y-4">
      <div className="rounded-md border bg-card p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-sm font-medium">
              <Bot className="h-3.5 w-3.5 shrink-0 text-brand" />
              <span className="truncate">{name || t(($) => $.detail.agent_draft_fallback_name)}</span>
            </div>
            {summary && (
              <p className="mt-2 whitespace-pre-wrap text-sm leading-relaxed text-foreground/80">
                {summary}
              </p>
            )}
          </div>
          {agentId && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="shrink-0"
              onClick={() => navigation.push(paths.agentDetail(agentId))}
            >
              <Sparkles className="h-3.5 w-3.5" />
              {t(($) => $.detail.open_agent)}
            </Button>
          )}
        </div>
      </div>

      {skillURLs.length > 0 && (
        <div className="rounded-md border bg-muted/30 p-3">
          <p className="text-xs font-medium text-muted-foreground">
            {t(($) => $.detail.attached_skills)}
          </p>
          <div className="mt-2 grid gap-1.5">
            {skillURLs.map((url) => (
              <a
                key={url}
                href={url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex min-w-0 items-center gap-1.5 text-xs text-foreground hover:underline"
              >
                <ExternalLink className="h-3 w-3 shrink-0 text-muted-foreground" />
                <span className="truncate">{url}</span>
              </a>
            ))}
          </div>
        </div>
      )}

      {prompt && (
        <div className="rounded-md border bg-muted/40 p-3">
          <p className="text-xs font-medium text-muted-foreground">
            {t(($) => $.detail.original_input)}
          </p>
          <p className="mt-1 whitespace-pre-wrap text-sm">{prompt}</p>
        </div>
      )}
    </div>
  );
}
