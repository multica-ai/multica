"use client";

import { IssueSidebarSectionHeader } from "./issue-sidebar-section";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { CircleSlash, GitPullRequest, MoreHorizontal, Plus, RotateCcw, Settings } from "@multica/ui/icons";
import { ApiError } from "@multica/core/api";
import {
  issuePullRequestsOptions,
  useGitHubSettings,
  useLinkIssuePullRequest,
  useSetIssuePRAutoComplete,
} from "@multica/core/github";
import { useWorkspacePaths } from "@multica/core/paths";
import type { PRAutoComplete } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { useIssueSidebarAddEntry } from "./issue-sidebar-add-menu";
import { PullRequestList } from "./pull-request-list";

/**
 * The issue sidebar's Pull requests section: the linked PRs, what merging them
 * will do to this issue's status, and the two exceptions a person can make —
 * link a PR by hand, or keep this one issue's status when its PRs merge
 * (MUL-7429, MUL-7726). With no PR linked the section renders nothing and
 * offers both from the sidebar's "+" menu: keeping the status has to be
 * settable before the first link, because a PR that is already merged when
 * it is linked moves the issue right then.
 */
export function PullRequestsSection({
  issueId,
  identifier,
  open,
  onOpenChange,
}: {
  issueId: string;
  identifier: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("issues");
  const { data, isLoading, isError, errorUpdateCount } = useQuery(issuePullRequestsOptions(issueId));
  const [linkOpen, setLinkOpen] = useState(false);
  // Older backends have no link / auto-complete endpoints; keep the header
  // actions hidden until the server says it supports them.
  const autoComplete = data?.auto_complete ?? null;
  const count = data?.pull_requests.length ?? 0;
  // A retry after a failure stays on the error row; dropping it would unmount
  // the list, whose remount retries again.
  const failed = isError || (isLoading && errorUpdateCount > 0);
  const empty = !isLoading && !failed && count === 0;

  useIssueSidebarAddEntry("pull_requests", empty && autoComplete ? {
    order: 0,
    items: (
      <DropdownMenuGroup>
        <DropdownMenuLabel>{t(($) => $.detail.section_pull_requests)}</DropdownMenuLabel>
        <DropdownMenuItem onClick={() => setLinkOpen(true)}>
          <GitPullRequest />
          {t(($) => $.pr_automation.link_action)}
        </DropdownMenuItem>
        <AutoCompleteItems issueId={issueId} autoComplete={autoComplete} />
      </DropdownMenuGroup>
    ),
  } : null);

  const linked = () => {
    setLinkOpen(false);
    onOpenChange(true);
  };

  if (isLoading && !failed) return null;
  if (empty) {
    return (
      <Dialog open={linkOpen} onOpenChange={setLinkOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>{t(($) => $.pr_automation.link_action)}</DialogTitle>
          </DialogHeader>
          <LinkPullRequestForm issueId={issueId} identifier={identifier} onLinked={linked} labelHidden />
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <div>
      <IssueSidebarSectionHeader
        title={t(($) => $.detail.section_pull_requests)} open={open} onOpenChange={onOpenChange} count={count}
        actions={autoComplete ? <>
          <LinkPullRequestPopover issueId={issueId} identifier={identifier} onLinked={() => onOpenChange(true)} />
          <AutoCompleteMenu issueId={issueId} autoComplete={autoComplete} />
        </> : undefined}
      />
      {open && (
        <div className="px-2">
          <PullRequestList issueId={issueId} identifier={identifier} />
        </div>
      )}
    </div>
  );
}

function LinkPullRequestPopover({
  issueId,
  identifier,
  onLinked,
}: {
  issueId: string;
  identifier: string;
  onLinked: () => void;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <IconButton variant="ghost" size="icon-xs" label={t(($) => $.pr_automation.link_action)}>
            <Plus />
          </IconButton>
        }
      />
      <PopoverContent align="end" className="w-80">
        <LinkPullRequestForm
          issueId={issueId}
          identifier={identifier}
          onLinked={() => {
            setOpen(false);
            onLinked();
          }}
        />
      </PopoverContent>
    </Popover>
  );
}

/** Mounted only while its popover or dialog is open, so closing it clears the draft. */
function LinkPullRequestForm({
  issueId,
  identifier,
  onLinked,
  labelHidden = false,
}: {
  issueId: string;
  identifier: string;
  onLinked: () => void;
  /** Whether a dialog title already names the field. */
  labelHidden?: boolean;
}) {
  const { t } = useT("issues");
  const { autoLinkPRs } = useGitHubSettings();
  const [url, setUrl] = useState("");
  const link = useLinkIssuePullRequest(issueId);
  const error = link.error
    ? link.error instanceof ApiError && link.error.status === 404
      ? t(($) => $.pr_automation.link_not_found)
      : t(($) => $.pr_automation.link_failed)
    : null;

  return (
    <form
      className="flex flex-col gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        const value = url.trim();
        if (!value || link.isPending) return;
        link.mutate({ url: value }, { onSuccess: onLinked });
      }}
    >
      <label htmlFor={`link-pr-${issueId}`} className={labelHidden ? "sr-only" : "text-caption font-medium"}>
        {t(($) => $.pr_automation.link_action)}
      </label>
      <Input
        id={`link-pr-${issueId}`}
        // Not type="url": the browser would block "github.com/…" without a
        // scheme, which the server accepts.
        type="text"
        inputMode="url"
        autoComplete="off"
        spellCheck={false}
        autoFocus
        value={url}
        placeholder={t(($) => $.pr_automation.link_placeholder)}
        aria-invalid={error ? true : undefined}
        aria-describedby={error || !autoLinkPRs ? undefined : `link-pr-${issueId}-hint`}
        onChange={(e) => {
          setUrl(e.target.value);
          if (link.error) link.reset();
        }}
      />
      {error ? (
        <p role="alert" className="text-caption text-destructive">
          {error}
        </p>
      ) : autoLinkPRs ? (
        // Only true while the workspace auto-links PRs.
        <p id={`link-pr-${issueId}-hint`} className="text-caption text-muted-foreground">
          {t(($) => $.pr_automation.link_hint, { identifier })}
        </p>
      ) : null}
      <div className="flex justify-end">
        <Button
          type="submit"
          size="sm"
          disabled={!url.trim() || link.isPending}
          aria-busy={link.isPending || undefined}
        >
          {t(($) => $.pr_automation.link_submit)}
        </Button>
      </div>
    </form>
  );
}

function AutoCompleteMenu({ issueId, autoComplete }: { issueId: string; autoComplete: PRAutoComplete }) {
  const { t } = useT("issues");

  // The only section menu left in the sidebar: it carries a real per-issue
  // action (keep status on merge), not just a link to settings.
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <IconButton variant="ghost" size="icon-xs" label={t(($) => $.pr_automation.section_menu)}>
            <MoreHorizontal />
          </IconButton>
        }
      />
      <DropdownMenuContent align="end" className="w-max max-w-(--available-width)">
        <AutoCompleteItems issueId={issueId} autoComplete={autoComplete} />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AutoCompleteItems({ issueId, autoComplete }: { issueId: string; autoComplete: PRAutoComplete }) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const setAutoComplete = useSetIssuePRAutoComplete(issueId);
  const disabled = autoComplete.issue_disabled;

  return (
    <>
      {/* When the workspace leaves status alone, the issue switch changes nothing. */}
      {autoComplete.workspace_enabled ? (
        <>
          <DropdownMenuItem
            disabled={setAutoComplete.isPending}
            // The menu closes, and unmounts this item, before the request
            // settles; per-call mutate callbacks would never fire.
            onClick={() =>
              setAutoComplete.mutateAsync(!disabled).catch(() => toast.error(t(($) => $.pr_automation.update_failed)))
            }
          >
            {disabled ? <RotateCcw /> : <CircleSlash />}
            {disabled ? t(($) => $.pr_automation.enable) : t(($) => $.pr_automation.disable)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
        </>
      ) : null}
      <DropdownMenuItem onClick={() => navigation.push(`${paths.settings()}?tab=code`)}>
        <Settings />
        {t(($) => $.pr_automation.settings)}
      </DropdownMenuItem>
    </>
  );
}
