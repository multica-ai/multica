"use client";

import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Check,
  Copy,
  GitBranch,
  GitMerge,
  GitPullRequestClosed,
  GitPullRequestDraft,
  RefreshCw,
  Sparkles,
  UserPlus,
  XCircle,
} from "@multica/ui/icons";
import { api } from "@multica/core/api";
import {
  classifyGitHubActionError,
  deriveChecksStatus,
  githubAccountOptions,
  primaryPullRequestAction,
  pullRequestMenuActions,
  pullRequestMergeOptionsOptions,
  pullRequestReviewerCandidatesOptions,
  usePullRequestAction,
  type GitHubActionFailure,
  type PullRequestAction,
  type PullRequestActionResult,
  type PullRequestMenuAction,
  type PullRequestPrimaryAction,
} from "@multica/core/github";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useCreateComment } from "@multica/core/issues/mutations";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { GitHubPullRequest, Issue, PRAutoComplete } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
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
import { DropdownMenuItem, DropdownMenuSeparator } from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { escapeMarkdownLabel } from "../../editor/utils/escape-markdown-label";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { gitHubFailureMessage, PullRequestMergeDialog } from "./pull-request-merge-dialog";

type IssuesT = ReturnType<typeof useT<"issues">>["t"];

// Actions that only post a Multica comment; everything else runs on GitHub.
const LOCAL_ACTIONS = new Set<string>(["hand_off_checks", "hand_off_conflict", "copy_branch"]);

type DialogState =
  | null
  | { kind: "merge" }
  | { kind: "request_review" }
  | { kind: "close" }
  | { kind: "connect"; reconnect: boolean }
  | { kind: "permission" };

export interface PullRequestActionsController {
  pr: GitHubPullRequest;
  primary: PullRequestPrimaryAction | null;
  menu: PullRequestMenuAction[];
  /** The kind of the action in flight, for the button that started it. */
  pending: PullRequestAction["kind"] | "enable_auto_merge" | null;
  trigger: (action: PullRequestPrimaryAction | PullRequestMenuAction) => void;
  handOff: HandOffContext;
  dialogs: ReactNode;
}

interface HandOffContext {
  issueId: string;
  pr: GitHubPullRequest;
  /** The issue's assignee when it is an agent. */
  assignee: { id: string; name: string } | null;
}

/**
 * Everything a PR row can do, shared by its primary button and its menu. The
 * signed-in user's GitHub account decides what is offered: write actions are
 * hidden until the server supports them, and a missing or expired account
 * opens the connect prompt instead of failing.
 */
export function usePullRequestActions({
  issueId,
  identifier,
  pr,
  autoComplete,
  knownIssue,
}: {
  issueId: string;
  identifier: string;
  pr: GitHubPullRequest;
  autoComplete: PRAutoComplete | null;
  /** The issue when the caller already has it, so list rows don't fetch each one. */
  knownIssue?: Pick<Issue, "status" | "assignee_type" | "assignee_id">;
}): PullRequestActionsController {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const account = useQuery(githubAccountOptions());
  const { data: fetchedIssue } = useQuery({ ...issueDetailOptions(wsId, issueId), enabled: !knownIssue });
  const issue = knownIssue ?? fetchedIssue;
  const { data: agents } = useQuery(agentListOptions(wsId));
  const mutation = usePullRequestAction(issueId, pr.id);
  const [dialog, setDialog] = useState<DialogState>(null);
  const [autoMergePending, setAutoMergePending] = useState(false);

  const isGitHub = !pr.provider || pr.provider === "github";
  const githubEnabled = isGitHub && account.data?.configured === true;
  const viewerLogin = account.data?.connected ? account.data.login : null;
  const prLabel = `#${pr.number}`;

  const allowed = (action: string) => LOCAL_ACTIONS.has(action) || githubEnabled;
  const rawPrimary = primaryPullRequestAction(pr, viewerLogin);
  const primary = rawPrimary && allowed(rawPrimary) ? rawPrimary : null;
  const menu = pullRequestMenuActions(pr, viewerLogin).filter(allowed);

  const assignee = useMemo(() => {
    if (issue?.assignee_type !== "agent" || !issue.assignee_id) return null;
    const agent = agents?.find((a) => a.id === issue.assignee_id);
    return agent && !agent.archived_at ? { id: agent.id, name: agent.name } : null;
  }, [issue?.assignee_type, issue?.assignee_id, agents]);

  /** Opens the surface a failure needs; false when a message should be shown instead. */
  const route = (failure: GitHubActionFailure): boolean => {
    switch (failure.kind) {
      case "connect":
        setDialog({ kind: "connect", reconnect: false });
        return true;
      case "reconnect":
        setDialog({ kind: "connect", reconnect: true });
        return true;
      case "app_permission":
        setDialog({ kind: "permission" });
        return true;
      default:
        return false;
    }
  };
  const fail = (err: unknown) => {
    const failure = classifyGitHubActionError(err);
    if (failure.kind === "connect" || failure.kind === "reconnect") {
      void qc.invalidateQueries({ queryKey: githubAccountOptions().queryKey });
    }
    if (!route(failure)) toast.error(gitHubFailureMessage(failure, t, prLabel));
  };

  const run = (action: PullRequestAction) =>
    mutation.mutate(action, {
      onSuccess: (result) => {
        const message = successMessage(result, t, prLabel);
        if (!message) return;
        if (message.error) toast.warning(message.text);
        else toast.success(message.text);
      },
      onError: fail,
    });

  const enableAutoMerge = async () => {
    setAutoMergePending(true);
    try {
      const options = await qc.fetchQuery(pullRequestMergeOptionsOptions(issueId, pr.id));
      if (!options.auto_merge_allowed) {
        toast.error(t(($) => $.pr_actions.toast.auto_merge_not_allowed));
        return;
      }
      const method = options.default_method ?? options.allowed_methods[0];
      if (!method) {
        toast.error(t(($) => $.pr_actions.toast.no_merge_method));
        return;
      }
      run({ kind: "enable_auto_merge", method });
    } catch (err) {
      fail(err);
    } finally {
      setAutoMergePending(false);
    }
  };

  const copyBranch = () => {
    if (!pr.branch) return;
    navigator.clipboard.writeText(pr.branch).then(
      () => toast.success(t(($) => $.pr_actions.toast.branch_copied)),
      () => toast.error(t(($) => $.pr_actions.toast.copy_failed)),
    );
  };

  const trigger = (action: PullRequestPrimaryAction | PullRequestMenuAction) => {
    if (action === "copy_branch") return copyBranch();
    if (action === "hand_off_checks" || action === "hand_off_conflict") return;
    if (!account.data?.connected) {
      setDialog({ kind: "connect", reconnect: false });
      return;
    }
    switch (action) {
      case "merge":
        return setDialog({ kind: "merge" });
      case "request_review":
        return setDialog({ kind: "request_review" });
      case "close":
        return setDialog({ kind: "close" });
      case "enable_auto_merge":
        return void enableAutoMerge();
      default:
        return run({ kind: action });
    }
  };

  const close = () => setDialog(null);
  const dialogs = (
    <>
      <PullRequestMergeDialog
        issueId={issueId}
        pr={pr}
        issue={{ identifier, status: issue?.status ?? null, autoComplete }}
        open={dialog?.kind === "merge"}
        onOpenChange={(open) => !open && close()}
        onFailure={route}
      />
      <RequestReviewDialog
        issueId={issueId}
        pr={pr}
        open={dialog?.kind === "request_review"}
        onOpenChange={(open) => !open && close()}
        onSubmit={(reviewers, done) =>
          mutation.mutate(
            { kind: "request_review", reviewers },
            {
              onSuccess: () => {
                toast.success(
                  t(($) => $.pr_actions.toast.review_requested, {
                    reviewers: reviewers.map((login) => `@${login}`).join(", "),
                  }),
                );
                done();
              },
              onError: (err) => {
                const failure = classifyGitHubActionError(err);
                if (route(failure)) return;
                toast.error(gitHubFailureMessage(failure, t, prLabel));
              },
            },
          )
        }
        pending={mutation.isPending && mutation.variables?.kind === "request_review"}
      />
      <AlertDialog open={dialog?.kind === "close"} onOpenChange={(open) => !open && close()}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.pr_actions.close_dialog.title, { pr: prLabel })}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.pr_actions.close_dialog.description)}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.pr_actions.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                close();
                run({ kind: "close" });
              }}
            >
              {t(($) => $.pr_actions.close_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <ConnectGitHubDialog
        open={dialog?.kind === "connect"}
        reconnect={dialog?.kind === "connect" && dialog.reconnect}
        onOpenChange={(open) => !open && close()}
      />
      <AlertDialog open={dialog?.kind === "permission"} onOpenChange={(open) => !open && close()}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.pr_actions.permission.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.pr_actions.permission.description, { owner: pr.repo_owner })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogAction onClick={close}>{t(($) => $.pr_actions.permission.done)}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );

  return {
    pr,
    primary,
    menu,
    pending: autoMergePending ? "enable_auto_merge" : mutation.isPending ? (mutation.variables?.kind ?? null) : null,
    trigger,
    handOff: { issueId, pr, assignee },
    dialogs,
  };
}

function successMessage(
  result: PullRequestActionResult,
  t: IssuesT,
  pr: string,
): { text: string; error?: boolean } | null {
  switch (result.kind) {
    case "merge":
      return { text: t(($) => $.pr_actions.toast.merged, { pr }) };
    case "enable_auto_merge":
      return { text: t(($) => $.pr_actions.toast.auto_merge_enabled, { pr }) };
    case "disable_auto_merge":
      return { text: t(($) => $.pr_actions.toast.auto_merge_disabled, { pr }) };
    case "update_branch":
      return { text: t(($) => $.pr_actions.toast.branch_updated, { pr }) };
    case "ready_for_review":
      return { text: t(($) => $.pr_actions.toast.ready_for_review, { pr }) };
    case "convert_to_draft":
      return { text: t(($) => $.pr_actions.toast.converted_to_draft, { pr }) };
    case "approve":
      return { text: t(($) => $.pr_actions.toast.approved, { pr }) };
    case "close":
      return { text: t(($) => $.pr_actions.toast.closed, { pr }) };
    case "request_review":
      return null;
    case "rerun_failed_checks": {
      const { rerun_count: count, not_rerunnable: skipped } = result.result;
      const names = skipped.join(", ");
      if (count === 0) return { text: t(($) => $.pr_actions.toast.rerun_none, { names }), error: true };
      if (skipped.length > 0) return { text: t(($) => $.pr_actions.toast.rerun_partial, { names }), error: true };
      return { text: t(($) => $.pr_actions.toast.rerun, { count }) };
    }
  }
}

// ---------------------------------------------------------------------------
// Row surfaces

/** The one button under a PR row that clears the way to a merge. */
export function PullRequestPrimaryAction({ controller }: { controller: PullRequestActionsController }) {
  const { t } = useT("issues");
  const { primary, pending, pr } = controller;
  if (!primary) return null;

  if (primary === "hand_off_checks" || primary === "hand_off_conflict") {
    return <HandOffPopover context={controller.handOff} kind={primary === "hand_off_checks" ? "checks" : "conflict"} />;
  }

  if (primary === "disable_auto_merge") {
    const by = pr.auto_merge?.enabled_by;
    return (
      <div className="flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
        <GitMerge className="size-3.5 shrink-0 text-violet-600 dark:text-violet-400" />
        <span className="min-w-0 truncate" title={by ? t(($) => $.pr_actions.auto_merge_on_by, { login: by }) : undefined}>
          {t(($) => $.pr_actions.auto_merge_on)}
        </span>
        <Button
          variant="ghost"
          size="xs"
          disabled={pending !== null}
          aria-busy={pending === "disable_auto_merge" || undefined}
          onClick={() => controller.trigger("disable_auto_merge")}
        >
          {pending === "disable_auto_merge" ? <Spinner /> : null}
          {t(($) => $.pr_actions.disable_auto_merge)}
        </Button>
      </div>
    );
  }

  const { label, icon: Icon } = primaryLabel(primary, t);
  return (
    <Button
      variant={primary === "merge" ? "default" : "outline"}
      size="xs"
      disabled={pending !== null}
      aria-busy={pending === primary || undefined}
      onClick={() => controller.trigger(primary)}
      data-testid="pull-request-primary-action"
    >
      {pending === primary ? <Spinner /> : <Icon />}
      {label}
    </Button>
  );
}

function primaryLabel(
  action: Exclude<PullRequestPrimaryAction, "hand_off_checks" | "hand_off_conflict" | "disable_auto_merge">,
  t: IssuesT,
): { label: string; icon: typeof GitMerge } {
  switch (action) {
    case "merge":
      return { label: t(($) => $.pr_actions.merge), icon: GitMerge };
    case "enable_auto_merge":
      return { label: t(($) => $.pr_actions.merge_when_ready), icon: GitMerge };
    case "update_branch":
      return { label: t(($) => $.pr_actions.update_branch), icon: GitBranch };
    case "approve":
      return { label: t(($) => $.pr_actions.approve), icon: Check };
    case "request_review":
      return { label: t(($) => $.pr_actions.request_review), icon: UserPlus };
    case "ready_for_review":
      return { label: t(($) => $.pr_actions.ready_for_review), icon: GitPullRequestDraft };
  }
}

/** GitHub actions for the row's ⋯ menu, above its link/unlink entries. */
export function PullRequestActionMenuItems({ controller }: { controller: PullRequestActionsController }) {
  const { t } = useT("issues");
  if (controller.menu.length === 0) return null;
  return (
    <>
      {controller.menu.map((action) => {
        const { label, icon: Icon, destructive } = menuLabel(action, t);
        return (
          <DropdownMenuItem
            key={action}
            variant={destructive ? "destructive" : undefined}
            disabled={controller.pending !== null}
            onClick={() => controller.trigger(action)}
          >
            <Icon />
            {label}
          </DropdownMenuItem>
        );
      })}
      <DropdownMenuSeparator />
    </>
  );
}

function menuLabel(
  action: PullRequestMenuAction,
  t: IssuesT,
): { label: string; icon: typeof GitMerge; destructive?: boolean } {
  switch (action) {
    case "approve":
      return { label: t(($) => $.pr_actions.approve), icon: Check };
    case "request_review":
      return { label: t(($) => $.pr_actions.request_review), icon: UserPlus };
    case "update_branch":
      return { label: t(($) => $.pr_actions.update_branch), icon: GitBranch };
    case "rerun_failed_checks":
      return { label: t(($) => $.pr_actions.rerun_failed_checks), icon: RefreshCw };
    case "disable_auto_merge":
      return { label: t(($) => $.pr_actions.disable_auto_merge), icon: XCircle };
    case "convert_to_draft":
      return { label: t(($) => $.pr_actions.convert_to_draft), icon: GitPullRequestDraft };
    case "copy_branch":
      return { label: t(($) => $.pr_actions.copy_branch), icon: Copy };
    case "close":
      return { label: t(($) => $.pr_actions.close), icon: GitPullRequestClosed, destructive: true };
  }
}

// ---------------------------------------------------------------------------
// Hand-off

/**
 * Failing checks and conflicts need code changes, so the row hands them to an
 * agent: an editable comment on the issue that mentions it, defaulting to the
 * issue's assignee.
 */
function HandOffPopover({ context, kind }: { context: HandOffContext; kind: "checks" | "conflict" }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { issueId, pr, assignee } = context;
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const comment = useCreateComment(issueId);
  const [open, setOpen] = useState(false);
  const [agentId, setAgentId] = useState<string | null>(null);
  const [message, setMessage] = useState("");

  const activeAgents = agents.filter((a) => !a.archived_at);
  const chosenId = agentId ?? assignee?.id ?? null;
  const chosen = activeAgents.find((a) => a.id === chosenId) ?? null;

  const draft = () => {
    const prLabel = `#${pr.number}`;
    const branch = pr.branch ?? "";
    if (kind === "conflict") return t(($) => $.pr_actions.hand_off.conflict_body, { pr: prLabel, branch });
    const checks = deriveChecksStatus(pr);
    const names = checks.kind === "failed" ? checks.names.join(", ") : "";
    return names
      ? t(($) => $.pr_actions.hand_off.checks_body, { pr: prLabel, checks: names, branch })
      : t(($) => $.pr_actions.hand_off.checks_body_unnamed, { pr: prLabel, branch });
  };

  useEffect(() => {
    if (open) {
      setMessage(draft());
      setAgentId(null);
      comment.reset();
    }
    // Re-draft only when the popover opens; edits survive snapshot refreshes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const label = assignee
    ? kind === "checks"
      ? t(($) => $.pr_actions.hand_off_checks_named, { agent: assignee.name })
      : t(($) => $.pr_actions.hand_off_conflict_named, { agent: assignee.name })
    : kind === "checks"
      ? t(($) => $.pr_actions.hand_off_checks)
      : t(($) => $.pr_actions.hand_off_conflict);

  const send = () => {
    if (!chosen || !message.trim() || comment.isPending) return;
    const mention = `[@${escapeMarkdownLabel(chosen.name)}](mention://agent/${chosen.id})`;
    comment.mutate(
      { content: `${mention} ${message.trim()}` },
      {
        onSuccess: () => {
          setOpen(false);
          toast.success(t(($) => $.pr_actions.toast.handed_off, { agent: chosen.name }));
        },
        onError: () => toast.error(t(($) => $.pr_actions.toast.hand_off_failed)),
      },
    );
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button variant="outline" size="xs" data-testid="pull-request-primary-action">
            <Sparkles />
            <span className="min-w-0 truncate">{label}</span>
          </Button>
        }
      />
      <PopoverContent align="start" className="w-80">
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            send();
          }}
        >
          <p className="text-label font-medium">
            {kind === "checks"
              ? t(($) => $.pr_actions.hand_off.title_checks)
              : t(($) => $.pr_actions.hand_off.title_conflict)}
          </p>
          <div className="grid gap-1.5">
            <Label htmlFor={`hand-off-agent-${pr.id}`}>{t(($) => $.pr_actions.hand_off.agent)}</Label>
            <Select
              items={activeAgents.map((a) => ({ value: a.id, label: a.name }))}
              value={chosenId}
              onValueChange={(next) => next && setAgentId(next as string)}
            >
              <SelectTrigger id={`hand-off-agent-${pr.id}`} size="sm" className="w-full">
                <SelectValue placeholder={t(($) => $.pr_actions.hand_off.agent_placeholder)} />
              </SelectTrigger>
              <SelectContent>
                {activeAgents.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor={`hand-off-message-${pr.id}`}>{t(($) => $.pr_actions.hand_off.message)}</Label>
            <Textarea
              id={`hand-off-message-${pr.id}`}
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault();
                  send();
                }
              }}
              className="max-h-48 min-h-24 text-sm"
            />
          </div>
          <div className="flex justify-end">
            <Button
              type="submit"
              size="sm"
              disabled={!chosen || !message.trim() || comment.isPending}
              aria-busy={comment.isPending || undefined}
            >
              {comment.isPending ? <Spinner /> : null}
              {t(($) => $.pr_actions.hand_off.send)}
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  );
}

// ---------------------------------------------------------------------------
// Dialogs

function RequestReviewDialog({
  issueId,
  pr,
  open,
  onOpenChange,
  onSubmit,
  pending,
}: {
  issueId: string;
  pr: GitHubPullRequest;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (reviewers: string[], done: () => void) => void;
  pending: boolean;
}) {
  const { t } = useT("issues");
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const [selected, setSelected] = useState<string[]>([]);

  useEffect(() => {
    const id = setTimeout(() => setDebounced(query.trim()), 200);
    return () => clearTimeout(id);
  }, [query]);
  useEffect(() => {
    if (!open) {
      setQuery("");
      setDebounced("");
      setSelected([]);
    }
  }, [open]);

  const candidates = useQuery({
    ...pullRequestReviewerCandidatesOptions(issueId, pr.id, debounced),
    enabled: open,
  });
  const toggle = (login: string) =>
    setSelected((prev) => (prev.includes(login) ? prev.filter((l) => l !== login) : [...prev, login].slice(0, 15)));
  const list = candidates.data ?? [];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.pr_actions.review_dialog.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.pr_actions.review_dialog.description)}</DialogDescription>
        </DialogHeader>
        <Input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t(($) => $.pr_actions.review_dialog.search)}
          aria-label={t(($) => $.pr_actions.review_dialog.search)}
        />
        <div className="max-h-64 min-h-24 overflow-y-auto rounded-md border" aria-busy={candidates.isFetching || undefined}>
          {candidates.isPending ? (
            <p className="flex items-center gap-2 p-3 text-sm text-muted-foreground">
              <Spinner className="size-4" />
              {t(($) => $.pr_actions.review_dialog.loading)}
            </p>
          ) : candidates.isError ? (
            <p className="p-3 text-sm text-muted-foreground">{t(($) => $.pr_actions.review_dialog.failed)}</p>
          ) : list.length === 0 ? (
            <p className="p-3 text-sm text-muted-foreground">{t(($) => $.pr_actions.review_dialog.empty)}</p>
          ) : (
            <ul className="p-1">
              {list.map((user) => (
                <li key={user.login}>
                  <Label className="flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 font-normal hover:bg-accent">
                    <Checkbox checked={selected.includes(user.login)} onCheckedChange={() => toggle(user.login)} />
                    {user.avatar_url ? (
                      <img src={user.avatar_url} alt="" className="size-5 rounded-full" />
                    ) : null}
                    <span className="truncate">@{user.login}</span>
                  </Label>
                </li>
              ))}
            </ul>
          )}
        </div>
        <DialogFooter>
          <DialogClose render={<Button variant="outline" />}>{t(($) => $.pr_actions.cancel)}</DialogClose>
          <Button
            disabled={selected.length === 0 || pending}
            aria-busy={pending || undefined}
            onClick={() => onSubmit(selected, () => onOpenChange(false))}
          >
            {pending ? <Spinner /> : null}
            {t(($) => $.pr_actions.review_dialog.submit, { count: Math.max(selected.length, 1) })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Starts GitHub authorization in the browser; the web app finishes it. */
export function ConnectGitHubDialog({
  open,
  reconnect,
  onOpenChange,
}: {
  open: boolean;
  reconnect: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("issues");
  const connect = useConnectGitHubAccount();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {reconnect ? t(($) => $.pr_actions.connect.reconnect_title) : t(($) => $.pr_actions.connect.title)}
          </DialogTitle>
          <DialogDescription>
            {reconnect
              ? t(($) => $.pr_actions.connect.reconnect_description)
              : t(($) => $.pr_actions.connect.description)}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <DialogClose render={<Button variant="outline" />}>{t(($) => $.pr_actions.cancel)}</DialogClose>
          <Button
            disabled={connect.pending}
            aria-busy={connect.pending || undefined}
            onClick={() => void connect.start().then((ok) => ok && onOpenChange(false))}
          >
            {connect.pending ? <Spinner /> : null}
            {t(($) => $.pr_actions.connect.action)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Opens GitHub's authorization page in a new browser tab. GitHub returns to
 * the current page with a one-time code, which the dashboard completes. This
 * tab picks the new account up when it regains focus.
 */
export function useConnectGitHubAccount() {
  const { t } = useT("issues");
  const navigation = useNavigation();
  const [pending, setPending] = useState(false);
  const start = async (): Promise<boolean> => {
    setPending(true);
    try {
      const { url } = await api.getMyGitHubAccountConnectURL(navigation.pathname || "/");
      if (!url) throw new Error("empty connect url");
      window.open(url, "_blank", "noopener");
      return true;
    } catch {
      toast.error(t(($) => $.pr_actions.connect.open_failed));
      return false;
    } finally {
      setPending(false);
    }
  };
  return { start, pending };
}
