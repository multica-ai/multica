import { useId, useState } from "react";
import type { SharedIssue, SharedIssueComment, SharedIssueRun } from "@multica/core/api";
import { isBuiltInIssueStatus } from "@multica/core/issue-statuses";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { ChevronDown } from "@multica/ui/icons";
import { useLocale, useT } from "@multica/views/i18n";
import { SharedIssueDeliverables } from "./shared-issue-deliverables";
import { SharedRunTranscript } from "./shared-run-transcript";

type CommentThreads = Map<string | null, SharedIssueComment[]>;

function SharedAvatar({ name, avatarUrl, agent }: {
  name: string;
  avatarUrl?: string | null;
  agent: boolean;
}) {
  const initials = name.trim().split(/\s+/).slice(0, 2).map((word) => Array.from(word)[0]).join("").toLocaleUpperCase();
  return (
    <ActorAvatar
      name={name}
      initials={initials}
      avatarUrl={resolvePublicFileUrl(avatarUrl)}
      isAgent={agent}
      size="sm"
    />
  );
}

function ActivityComment({ comment, threads, runs, formatDate, depth = 0, token, userId }: {
  comment: SharedIssueComment;
  threads: CommentThreads;
  runs: Map<string, SharedIssueRun>;
  formatDate: Intl.DateTimeFormat;
  depth?: number;
  token: string;
  userId: string;
}) {
  const { t } = useT("issues");
  const [expanded, setExpanded] = useState(true);
  const repliesId = useId();
  const name = comment.author_name || t(($) => $.workflows.handler[comment.author_type]);
  const replies = threads.get(comment.id);
  const run = comment.source_task_id ? runs.get(comment.source_task_id) : undefined;

  return (
    <li className="min-w-0">
      <div className="flex items-start gap-3">
        <SharedAvatar name={name} avatarUrl={comment.author_avatar_url} agent={comment.author_type === "agent"} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 text-caption">
            <span className="break-words font-medium">{name}</span>
            <time className="text-muted-foreground" dateTime={comment.created_at}>
              {formatDate.format(new Date(comment.created_at))}
            </time>
            {replies?.length ? (
              <Button
                className="ml-auto"
                variant="ghost"
                size="icon-xs"
                aria-label={expanded ? t(($) => $.comment.collapse_thread) : t(($) => $.comment.expand_thread)}
                aria-controls={repliesId}
                aria-expanded={expanded}
                onClick={() => setExpanded((value) => !value)}
              >
                <ChevronDown className={expanded ? "" : "-rotate-90"} aria-hidden />
              </Button>
            ) : null}
          </div>
          <p className="mt-1 whitespace-pre-wrap break-words text-body">{comment.content}</p>
          {run && (
            <SharedRunTranscript
              run={run} userId={userId} token={token}
              label={t(($) => $.execution_log.transcript_tooltip)}
            />
          )}
        </div>
      </div>
      {replies?.length ? (
        <ol id={repliesId} hidden={!expanded} className={depth < 3 ? "mt-4 space-y-4 border-l pl-3 sm:pl-5" : "mt-4 space-y-4"}>
          {replies.map((reply) => (
            <ActivityComment key={reply.id} comment={reply} threads={threads} runs={runs}
              formatDate={formatDate} depth={depth + 1} token={token} userId={userId} />
          ))}
        </ol>
      ) : null}
    </li>
  );
}

export function SharedIssueBody({ issue, userId, token }: {
  issue: SharedIssue;
  userId: string;
  token: string;
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const formatDate = new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" });
  const comments = new Set(issue.comments.map((comment) => comment.id));
  const threads: CommentThreads = new Map();
  for (const comment of issue.comments) {
    const parent = comment.parent_id && comments.has(comment.parent_id) ? comment.parent_id : null;
    const replies = threads.get(parent);
    if (replies) replies.push(comment);
    else threads.set(parent, [comment]);
  }
  const roots = threads.get(null) ?? [];
  const runs = new Map(issue.runs.map((run) => [run.id, run]));
  const runStatuses = new Map([
    ["completed", t(($) => $.execution_log.status_completed)],
    ["failed", t(($) => $.execution_log.status_failed)],
    ["cancelled", t(($) => $.execution_log.status_cancelled)],
  ]);
  const status = issue.status;
  const statusLabel = issue.status_label || (isBuiltInIssueStatus(status) ? t(($) => $.status[status]) : status.replaceAll("_", " "));

  return (
    <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_18rem]">
      <article className="min-w-0">
        <div className="mb-3 flex flex-wrap items-center gap-3 text-caption text-muted-foreground">
          <span className="font-mono">{issue.identifier}</span>
          <span className="rounded-md border border-surface-border px-2 py-0.5">{statusLabel}</span>
        </div>
        <h1 className="break-words text-display font-semibold">{issue.title}</h1>
        {issue.description && (
          <p className="mt-6 whitespace-pre-wrap break-words text-body leading-relaxed">{issue.description}</p>
        )}
        <section className="mt-8 space-y-4 border-t pt-5" aria-labelledby="shared-issue-activity">
          <h2 id="shared-issue-activity" className="text-label font-semibold">{t(($) => $.detail.activity_section)}</h2>
          {roots.length ? (
            <ol className="space-y-4">
              {roots.map((root) => (
                <ActivityComment key={root.id} comment={root} threads={threads} runs={runs}
                  formatDate={formatDate} token={token} userId={userId} />
              ))}
            </ol>
          ) : (
            <p className="text-caption text-muted-foreground">{t(($) => $.sharing.no_activity)}</p>
          )}
        </section>
      </article>
      <aside className="min-w-0 space-y-6 border-t pt-5 lg:border-t-0 lg:border-l lg:pt-0 lg:pl-6">
        <section className="space-y-3" aria-labelledby="shared-issue-runs">
          <h2 id="shared-issue-runs" className="text-label font-semibold">{t(($) => $.execution_log.section)}</h2>
          {issue.runs.length ? (
            <ol className="space-y-2">
              {issue.runs.map((run) => {
                const name = run.agent_name || t(($) => $.workflows.handler.agent);
                return (
                  <li key={run.id} className="flex min-w-0 items-center gap-2 rounded-md border border-surface-border p-2">
                    <SharedAvatar name={name} avatarUrl={run.agent_avatar_url} agent />
                    <div className="min-w-0 flex-1 text-caption">
                      <p className="truncate font-medium">{name}</p>
                      <p className="text-muted-foreground">{runStatuses.get(run.status) ?? run.status}</p>
                      <time className="text-muted-foreground" dateTime={run.completed_at}>
                        {formatDate.format(new Date(run.completed_at))}
                      </time>
                    </div>
                    <SharedRunTranscript
                      run={run} userId={userId} token={token} iconOnly
                      label={t(($) => $.execution_log.transcript_tooltip)}
                    />
                  </li>
                );
              })}
            </ol>
          ) : (
            <p className="text-caption text-muted-foreground">{t(($) => $.sharing.no_runs)}</p>
          )}
        </section>
        <SharedIssueDeliverables files={issue.deliverables} userId={userId} token={token} />
      </aside>
    </div>
  );
}
