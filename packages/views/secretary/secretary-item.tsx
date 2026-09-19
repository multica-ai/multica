"use client";

import { useState } from "react";
import { useWorkspacePaths } from "@multica/core/paths";
import type { ArrangedItem } from "@multica/core/secretary/selectors";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../i18n";
import { AppLink } from "../navigation";
import { InstructionForm } from "./instruction-form";

export function SecretaryItemCard({
  item,
  revision,
  today,
}: {
  item: ArrangedItem;
  revision: number;
  today: string;
}) {
  const { t } = useT("issues");
  const [editing, setEditing] = useState(false);
  const paths = useWorkspacePaths();
  const stageNames = {
    ready: t(($) => $.secretary.stage_ready),
    preparing: t(($) => $.secretary.stage_preparing),
    waiting: t(($) => $.secretary.stage_waiting),
    later: t(($) => $.secretary.stage_later),
    history: t(($) => $.secretary.stage_history),
  };
  const status =
    item.reported === "completed"
      ? item.closure_mode === "self_report"
        ? t(($) => $.secretary.status_completed_confirmed)
        : t(($) => $.secretary.status_completed_pending_review)
      : item.reported === "cancelled"
        ? t(($) => $.secretary.status_cancelled)
        : item.kind === "reference"
          ? t(($) => $.secretary.status_reference)
          : item.kind === "operation"
            ? t(($) => $.secretary.status_operation)
            : stageNames[item.stage];
  const statusLine = [
    status,
    item.scheduled_on
      ? t(($) => $.secretary.planned_on, { date: item.scheduled_on })
      : null,
    item.due_date ? t(($) => $.secretary.due_on, { date: item.due_date }) : null,
    (item.stage === "waiting" || item.stage === "later") && item.follow_up_on
      ? t(($) => $.secretary.follow_up_on, { date: item.follow_up_on })
      : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <article className="rounded-xl border bg-card p-4" aria-label={item.title}>
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="mb-1 text-xs text-muted-foreground">{statusLine}</p>
          <h3 className="font-medium leading-6">{item.title}</h3>
        </div>
        {item.kind === "action" && item.stage !== "history" && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => setEditing((value) => !value)}
            aria-expanded={editing}
          >
            {t(($) => $.secretary.handle)}
          </Button>
        )}
      </div>
      <p className="mt-2 text-sm leading-6">
        {item.reported === "completed"
          ? item.closure_mode === "self_report"
            ? t(($) => $.secretary.completed_self_report_copy)
            : t(($) => $.secretary.completed_review_copy)
          : item.situation}
      </p>
      <p className="mt-2 text-sm leading-6">
        <span className="font-medium">
          {item.reported
            ? t(($) => $.secretary.label_feedback)
            : item.owner === "chairman" && item.stage === "ready"
              ? t(($) => $.secretary.label_need_you)
              : t(($) => $.secretary.label_next_step)}
        </span>
        {item.reported ? item.instruction_note : item.next_step}
      </p>
      {item.stage === "ready" && item.recommendation && (
        <p className="mt-2 text-sm leading-6">
          <span className="font-medium">{t(($) => $.secretary.label_recommendation)}</span>
          {item.recommendation}
        </p>
      )}
      {item.stale && (
        <p className="mt-2 text-sm text-amber-700 dark:text-amber-400">
          {t(($) => $.secretary.stale)}
        </p>
      )}
      <details className="mt-3 text-sm">
        <summary className="w-fit cursor-pointer text-muted-foreground">
          {t(($) => $.secretary.details)}
        </summary>
        <div className="mt-3 space-y-2 border-t pt-3 leading-6">
          {item.why_now && (
            <p>
              <span className="font-medium">{t(($) => $.secretary.label_impact)}</span>
              {item.why_now}
            </p>
          )}
          {item.completion && (
            <p>
              <span className="font-medium">{t(($) => $.secretary.label_completion)}</span>
              {item.completion}
            </p>
          )}
          {(item.follow_up_on || item.follow_up_trigger) && (
            <p>
              <span className="font-medium">{t(($) => $.secretary.label_follow_up)}</span>
              {item.follow_up_on ?? item.follow_up_trigger}
            </p>
          )}
          {item.instruction_note && (
            <p>
              <span className="font-medium">
                {t(($) => $.secretary.label_latest_instruction)}
              </span>
              {item.instruction_note}
            </p>
          )}
          <p className="text-xs text-muted-foreground">
            {t(($) => $.secretary.latest_evidence, {
              date: item.source_updated_at.slice(0, 10),
            })}
          </p>
          <div className="flex flex-wrap gap-4">
            {item.issue_id && (
              <AppLink
                href={paths.issueDetail(item.issue_id)}
                className="underline underline-offset-4"
              >
                {t(($) => $.secretary.open_full_record)}
              </AppLink>
            )}
            {item.linked_issue_ids
              .filter((id) => id !== item.issue_id)
              .map((id) => (
                <AppLink
                  key={id}
                  href={paths.issueDetail(id)}
                  className="underline underline-offset-4"
                >
                  {t(($) => $.secretary.open_completed_stage)}
                </AppLink>
              ))}
            {item.links
              .filter((link) => /^https?:\/\//.test(link.url))
              .map((link) => (
                <a
                  key={link.url}
                  href={link.url}
                  target="_blank"
                  rel="noreferrer"
                  className="underline underline-offset-4"
                >
                  {link.label}
                </a>
              ))}
          </div>
        </div>
      </details>
      {editing && (
        <InstructionForm
          item={item}
          revision={revision}
          today={today}
          onClose={() => setEditing(false)}
        />
      )}
    </article>
  );
}
