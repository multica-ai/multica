"use client";

import { useEffect, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { CheckCircle2, Circle, LoaderCircle, TriangleAlert, XCircle } from "@multica/ui/icons";
import {
  classifyGitHubActionError,
  deriveChecksStatus,
  pullRequestMergeOptionsOptions,
  usePullRequestAction,
  type GitHubActionFailure,
} from "@multica/core/github";
import type {
  GitHubMergeMethod,
  GitHubMergeOptions,
  GitHubPullRequest,
  PRAutoComplete,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Kbd, KbdGroup } from "@multica/ui/components/ui/kbd";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useT } from "../../i18n";
import { useStatusLabel } from "../utils/status-label";
import { StatusIcon } from "./status-icon";

type IssuesT = ReturnType<typeof useT<"issues">>["t"];

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

export interface MergeIssueContext {
  identifier: string;
  /** The issue's current status key, when known. */
  status: string | null;
  autoComplete: PRAutoComplete | null;
}

/**
 * Confirms a merge with what GitHub will check and what will happen. The
 * merge carries the head commit this dialog showed, so a push made while it
 * was open fails instead of merging unseen commits.
 */
export function PullRequestMergeDialog({
  issueId,
  pr,
  issue,
  open,
  onOpenChange,
  onFailure,
}: {
  issueId: string;
  pr: GitHubPullRequest;
  issue: MergeIssueContext;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Failures that need another surface (connect, permissions). */
  onFailure: (failure: GitHubActionFailure) => boolean;
}) {
  const { t } = useT("issues");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {open ? (
          <MergeForm
            issueId={issueId}
            pr={pr}
            issue={issue}
            onDone={() => onOpenChange(false)}
            onFailure={onFailure}
          />
        ) : (
          <DialogTitle className="sr-only">{t(($) => $.pr_actions.merge_dialog.title, { pr: `#${pr.number}` })}</DialogTitle>
        )}
      </DialogContent>
    </Dialog>
  );
}

function MergeForm({
  issueId,
  pr,
  issue,
  onDone,
  onFailure,
}: {
  issueId: string;
  pr: GitHubPullRequest;
  issue: MergeIssueContext;
  onDone: () => void;
  onFailure: (failure: GitHubActionFailure) => boolean;
}) {
  const { t } = useT("issues");
  const prLabel = `#${pr.number}`;
  const options = useQuery(pullRequestMergeOptionsOptions(issueId, pr.id));
  const merge = usePullRequestAction(issueId, pr.id);
  const [method, setMethod] = useState<GitHubMergeMethod | null>(null);
  // null keeps GitHub's default for the chosen method, so switching methods
  // swaps the message until the user edits it.
  const [title, setTitle] = useState<string | null>(null);
  const [body, setBody] = useState<string | null>(null);
  const [deleteBranch, setDeleteBranch] = useState<boolean | null>(null);
  const [error, setError] = useState<string | null>(null);

  const loadFailure = options.error ? classifyGitHubActionError(options.error) : null;
  useEffect(() => {
    if (loadFailure && onFailure(loadFailure)) onDone();
    // Only the first classification of each error matters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [options.error]);

  const data = options.data && options.data.head_sha ? options.data : null;
  const chosen = method ?? data?.default_method ?? data?.allowed_methods[0] ?? null;
  const defaults = chosen ? data?.commit_messages[chosen] : undefined;
  const hasMessage = chosen === "squash" || chosen === "merge";
  const titleValue = title ?? defaults?.title ?? "";
  const bodyValue = body ?? defaults?.body ?? "";
  const canDeleteBranch = !!data && !data.head_is_fork && !!pr.branch;
  const deleteValue = deleteBranch ?? data?.delete_branch_on_merge ?? false;
  const canSubmit = !!data && !!chosen && !merge.isPending;

  const submit = () => {
    if (!data || !chosen || merge.isPending) return;
    setError(null);
    merge.mutate(
      {
        kind: "merge",
        body: {
          method: chosen,
          ...(hasMessage ? { commit_title: titleValue.trim() || undefined, commit_message: bodyValue } : {}),
          delete_branch: canDeleteBranch && deleteValue,
          expected_head_sha: data.head_sha,
        },
      },
      {
        onSuccess: () => {
          toast.success(t(($) => $.pr_actions.toast.merged, { pr: prLabel }));
          onDone();
        },
        onError: (err) => {
          const failure = classifyGitHubActionError(err);
          if (failure.kind === "head_changed") {
            setError(t(($) => $.pr_actions.toast.head_changed, { pr: prLabel }));
            void options.refetch();
            return;
          }
          if (onFailure(failure)) {
            onDone();
            return;
          }
          setError(gitHubFailureMessage(failure, t, prLabel));
        },
      },
    );
  };

  const consequence = <AfterMergeLine pr={pr} issue={issue} />;

  return (
    <form
      className="contents"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
          e.preventDefault();
          submit();
        }
      }}
    >
      <DialogHeader>
        <DialogTitle>{t(($) => $.pr_actions.merge_dialog.title, { pr: prLabel })}</DialogTitle>
        <DialogDescription className="truncate">
          {pr.repo_owner}/{pr.repo_name}
          {pr.branch ? ` · ${pr.branch}` : null}
        </DialogDescription>
      </DialogHeader>

      <p className="line-clamp-2 text-sm font-medium">{pr.title}</p>

      {options.isPending ? (
        <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground" role="status">
          <Spinner className="size-4" />
          {t(($) => $.pr_actions.merge_dialog.loading)}
        </div>
      ) : !data ? (
        <div className="flex items-center justify-between gap-3 rounded-md bg-muted/60 px-3 py-2 text-sm">
          <span>
            {loadFailure && loadFailure.kind !== "unknown"
              ? gitHubFailureMessage(loadFailure, t, prLabel)
              : t(($) => $.pr_actions.merge_dialog.load_failed)}
          </span>
          <Button type="button" variant="outline" size="sm" onClick={() => void options.refetch()}>
            {t(($) => $.pr_actions.merge_dialog.retry)}
          </Button>
        </div>
      ) : (
        <>
          <ReadinessList pr={pr} data={data} t={t} />

          {data.allowed_methods.length === 0 ? (
            <p className="text-sm text-destructive">{t(($) => $.pr_actions.toast.no_merge_method)}</p>
          ) : (
            <div className="grid gap-3">
              <div className="grid gap-1.5">
                <Label htmlFor={`merge-method-${pr.id}`}>{t(($) => $.pr_actions.merge_dialog.method)}</Label>
                <Select
                  items={data.allowed_methods.map((m) => ({ value: m, label: methodLabel(m, t) }))}
                  value={chosen}
                  onValueChange={(next) => next && setMethod(next as GitHubMergeMethod)}
                >
                  <SelectTrigger id={`merge-method-${pr.id}`} size="sm" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {data.allowed_methods.map((m) => (
                      <SelectItem key={m} value={m}>
                        {methodLabel(m, t)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {hasMessage ? (
                <>
                  <div className="grid gap-1.5">
                    <Label htmlFor={`merge-title-${pr.id}`}>{t(($) => $.pr_actions.merge_dialog.commit_title)}</Label>
                    <Input
                      id={`merge-title-${pr.id}`}
                      value={titleValue}
                      onChange={(e) => setTitle(e.target.value)}
                    />
                  </div>
                  <div className="grid gap-1.5">
                    <Label htmlFor={`merge-body-${pr.id}`}>{t(($) => $.pr_actions.merge_dialog.commit_message)}</Label>
                    <Textarea
                      id={`merge-body-${pr.id}`}
                      value={bodyValue}
                      onChange={(e) => setBody(e.target.value)}
                      className="max-h-40 min-h-16 font-mono text-xs"
                    />
                  </div>
                </>
              ) : null}
              {canDeleteBranch ? (
                <Label className="flex items-center gap-2 font-normal">
                  <Checkbox checked={deleteValue} onCheckedChange={(checked) => setDeleteBranch(checked === true)} />
                  <span className="min-w-0 truncate">
                    {t(($) => $.pr_actions.merge_dialog.delete_branch, { branch: pr.branch ?? "" })}
                  </span>
                </Label>
              ) : null}
            </div>
          )}

          {consequence}
        </>
      )}

      {error ? (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      ) : null}

      <DialogFooter className="items-center sm:justify-between">
        <span className="min-w-0 truncate text-xs text-muted-foreground">
          {data ? t(($) => $.pr_actions.merge_dialog.as_viewer, { login: data.viewer.login }) : null}
        </span>
        <div className="flex gap-2">
          <DialogClose render={<Button type="button" variant="outline" />}>{t(($) => $.pr_actions.cancel)}</DialogClose>
          <Button type="submit" disabled={!canSubmit} aria-busy={merge.isPending || undefined}>
            {merge.isPending ? <Spinner /> : null}
            {t(($) => $.pr_actions.merge_dialog.confirm)}
            <KbdGroup className="ml-1 hidden sm:inline-flex">
              <Kbd className="bg-primary-foreground/15 text-primary-foreground">{isMac ? "⌘" : "Ctrl"}</Kbd>
              <Kbd className="bg-primary-foreground/15 text-primary-foreground">↵</Kbd>
            </KbdGroup>
          </Button>
        </div>
      </DialogFooter>
    </form>
  );
}

function ReadinessList({ pr, data, t }: { pr: GitHubPullRequest; data: GitHubMergeOptions; t: IssuesT }) {
  const checks = deriveChecksStatus(pr);
  const items: { key: string; tone: "ok" | "bad" | "wait" | "none"; label: string }[] = [];

  switch (checks.kind) {
    case "failed":
      items.push({ key: "checks", tone: "bad", label: t(($) => $.pr_actions.merge_dialog.checks_failed, { failed: checks.failed, total: checks.total }) });
      break;
    case "pending":
      items.push({ key: "checks", tone: "wait", label: t(($) => $.pr_actions.merge_dialog.checks_running, { running: checks.running }) });
      break;
    case "passed":
      items.push({ key: "checks", tone: "ok", label: t(($) => $.pr_actions.merge_dialog.checks_passed, { passed: checks.total, total: checks.total }) });
      break;
    default:
      items.push({ key: "checks", tone: "none", label: t(($) => $.pr_actions.merge_dialog.checks_none) });
  }

  switch (data.review_decision) {
    case "approved":
      items.push({ key: "review", tone: "ok", label: t(($) => $.pr_actions.merge_dialog.review_approved) });
      break;
    case "review_required":
      items.push({ key: "review", tone: "wait", label: t(($) => $.pr_actions.merge_dialog.review_required) });
      break;
    case "changes_requested":
      items.push({ key: "review", tone: "bad", label: t(($) => $.pr_actions.merge_dialog.review_changes_requested) });
      break;
    default:
      items.push({ key: "review", tone: "none", label: t(($) => $.pr_actions.merge_dialog.review_not_required) });
  }

  if (data.mergeable === "conflicting") {
    items.push({ key: "conflicts", tone: "bad", label: t(($) => $.pr_actions.merge_dialog.conflicting) });
  } else if (data.mergeable === "mergeable") {
    items.push({ key: "conflicts", tone: "ok", label: t(($) => $.pr_actions.merge_dialog.mergeable) });
  } else {
    items.push({ key: "conflicts", tone: "wait", label: t(($) => $.pr_actions.merge_dialog.mergeable_unknown) });
  }

  return (
    <ul className="grid gap-1.5 text-sm" data-testid="merge-readiness">
      {items.map(({ key, tone, label }) => {
        const Icon = tone === "ok" ? CheckCircle2 : tone === "bad" ? (key === "conflicts" ? TriangleAlert : XCircle) : tone === "wait" ? LoaderCircle : Circle;
        return (
          <li key={key} className="flex items-center gap-2">
            <Icon
              className={cn(
                "size-4 shrink-0",
                tone === "ok" && "text-emerald-600 dark:text-emerald-400",
                tone === "bad" && "text-rose-600 dark:text-rose-400",
                tone === "wait" && "text-amber-600 dark:text-amber-400",
                tone === "none" && "text-muted-foreground",
              )}
            />
            <span>{label}</span>
          </li>
        );
      })}
    </ul>
  );
}

function methodLabel(method: GitHubMergeMethod, t: IssuesT): string {
  switch (method) {
    case "squash":
      return t(($) => $.pr_actions.merge_dialog.method_squash);
    case "merge":
      return t(($) => $.pr_actions.merge_dialog.method_merge);
    case "rebase":
      return t(($) => $.pr_actions.merge_dialog.method_rebase);
  }
}

// Slots split the translated sentence so each language keeps its word order
// and the statuses render as chips.
const FROM_SLOT = "\u2063from\u2063";
const TO_SLOT = "\u2063to\u2063";

/** The status move auto-complete will make when this is the last open PR. */
function AfterMergeLine({ pr, issue }: { pr: GitHubPullRequest; issue: MergeIssueContext }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const statusLabel = useStatusLabel(wsId);
  const ac = issue.autoComplete;
  if (!ac || ac.state !== "waiting" || !issue.status) return null;
  if (ac.pull_request_ids.length !== 1 || ac.pull_request_ids[0] !== pr.id) return null;
  const target = ac.target_status ?? "done";
  if (target === "none") return null;

  const chip = (key: string) => (
    <span key={key} className="inline-flex items-center gap-1 whitespace-nowrap align-middle font-medium text-foreground">
      <StatusIcon
        status={key}
        category={catalog.categoryOf(key)}
        color={catalog.colorOf(key)}
        icon={catalog.iconOf(key)}
        className="size-3"
      />
      {statusLabel(key)}
    </span>
  );
  const sentence = t(($) => $.pr_actions.merge_dialog.after_merge, {
    identifier: issue.identifier,
    from: FROM_SLOT,
    to: TO_SLOT,
  });
  const parts: ReactNode[] = [];
  for (const [i, piece] of sentence.split(new RegExp(`(${FROM_SLOT}|${TO_SLOT})`)).entries()) {
    if (piece === FROM_SLOT) parts.push(chip(issue.status));
    else if (piece === TO_SLOT) parts.push(chip(target));
    else if (piece) parts.push(<span key={i}>{piece}</span>);
  }
  return (
    <p className="rounded-md bg-muted/60 px-3 py-2 text-sm text-muted-foreground" data-testid="merge-after">
      {parts}
    </p>
  );
}

/** A toast-sized sentence for failures the caller didn't route elsewhere. */
export function gitHubFailureMessage(failure: GitHubActionFailure, t: IssuesT, prLabel: string): string {
  switch (failure.kind) {
    case "not_configured":
      return t(($) => $.pr_actions.toast.not_configured);
    case "head_changed":
      return t(($) => $.pr_actions.toast.head_changed, { pr: prLabel });
    case "own_pr":
      return t(($) => $.pr_actions.toast.own_pr);
    case "auto_merge_not_allowed":
      return t(($) => $.pr_actions.toast.auto_merge_not_allowed);
    case "rate_limited":
      return t(($) => $.pr_actions.toast.rate_limited);
    case "installation_missing":
      return t(($) => $.pr_actions.toast.installation_missing);
    case "github":
      return t(($) => $.pr_actions.toast.github_said, { message: failure.message });
    case "connect":
      return t(($) => $.pr_actions.connect.title);
    case "reconnect":
      return t(($) => $.pr_actions.connect.reconnect_title);
    case "app_permission":
      return t(($) => $.pr_actions.permission.title);
    case "unknown":
      return t(($) => $.pr_actions.toast.failed);
  }
}
