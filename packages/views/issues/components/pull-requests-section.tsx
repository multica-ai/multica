"use client";

import { IssueSidebarCollapsible, IssueSidebarSectionHeader } from "./issue-sidebar-section";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { CircleSlash, MoreHorizontal, Plus, RotateCcw, Settings } from "@multica/ui/icons";
import { ApiError } from "@multica/core/api";
import {
  issuePullRequestsOptions,
  useGitHubSettings,
  useLinkIssuePullRequest,
  useSetIssuePRAutoComplete,
} from "@multica/core/github";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { PullRequestList } from "./pull-request-list";
import { useReopenOnReveal, useSidebarRevealMotion } from "./use-sidebar-reveal-motion";

/**
 * The issue sidebar's Pull requests section: the linked PRs, what merging them
 * will do to this issue's status, and the two exceptions a person can make —
 * link a PR by hand, or keep this one issue's status when its PRs merge
 * (MUL-7429, MUL-7726).
 */
export function PullRequestsSection({
  issueId,
  identifier,
  open,
  onOpenChange,
  reveal = false,
  onRevealEnd,
}: {
  issueId: string;
  identifier: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Shows the section while empty with the link form open, until it closes. */
  reveal?: boolean;
  onRevealEnd?: () => void;
}) {
  const { t } = useT("issues");
  const { data, isError: lastFetchFailed, errorUpdateCount } = useQuery(issuePullRequestsOptions(issueId));
  // A retry starts pending again; holding on to the failure keeps the retry
  // entry mounted instead of the section blinking out mid-retry.
  const isError = lastFetchFailed || (data === undefined && errorUpdateCount > 0);
  // Older backends have no link / auto-complete endpoints; keep the header
  // actions hidden until the server says it supports them.
  const supported = !!data?.auto_complete;
  const count = data?.pull_requests.length ?? 0;
  const motion = useSidebarRevealMotion<HTMLDivElement>(reveal && supported, count > 0 || isError, onRevealEnd);

  // An empty section is offered by the sidebar's add row instead.
  if (!motion.shown) return null;

  return (
    <div ref={motion.ref}>
      <IssueSidebarSectionHeader
        title={t(($) => $.detail.section_pull_requests)} open={open} onOpenChange={onOpenChange} count={count} collapsible={count > 0 || isError}
        actions={supported ? <>
          <LinkPullRequestPopover
            issueId={issueId}
            identifier={identifier}
            defaultOpen={reveal}
            onLinked={() => onOpenChange(true)}
            onClosed={() => reveal && motion.end()}
          />
          <AutoCompleteMenu issueId={issueId} disabled={data?.auto_complete?.issue_disabled ?? false} workspaceEnabled={data?.auto_complete?.workspace_enabled ?? true} />
        </> : undefined}
      />
      <IssueSidebarCollapsible open={open && (count > 0 || isError)}>
        <div className="px-2">
          <PullRequestList issueId={issueId} identifier={identifier} />
        </div>
      </IssueSidebarCollapsible>
    </div>
  );
}

function LinkPullRequestPopover({
  issueId,
  identifier,
  defaultOpen = false,
  onLinked,
  onClosed,
}: {
  issueId: string;
  identifier: string;
  defaultOpen?: boolean;
  onLinked: () => void;
  onClosed?: () => void;
}) {
  const { t } = useT("issues");
  const { autoLinkPRs } = useGitHubSettings();
  const [open, setOpenState] = useState(defaultOpen);
  useReopenOnReveal(defaultOpen, () => setOpenState(true));
  const setOpen = (next: boolean) => {
    setOpenState(next);
    if (!next) onClosed?.();
  };
  const [url, setUrl] = useState("");
  const link = useLinkIssuePullRequest(issueId);
  const error = link.error
    ? link.error instanceof ApiError && link.error.status === 404
      ? t(($) => $.pr_automation.link_not_found)
      : t(($) => $.pr_automation.link_failed)
    : null;

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          setUrl("");
          link.reset();
        }
      }}
    >
      <PopoverTrigger
        render={
          <IconButton variant="ghost" size="icon-xs" label={t(($) => $.pr_automation.link_action)}>
            <Plus />
          </IconButton>
        }
      />
      <PopoverContent align="end" className="w-80">
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            const value = url.trim();
            if (!value || link.isPending) return;
            link.mutate(
              { url: value },
              {
                onSuccess: () => {
                  setOpen(false);
                  setUrl("");
                  onLinked();
                },
              },
            );
          }}
        >
          <label htmlFor={`link-pr-${issueId}`} className="text-caption font-medium">
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
      </PopoverContent>
    </Popover>
  );
}

function AutoCompleteMenu({
  issueId,
  disabled,
  workspaceEnabled,
}: {
  issueId: string;
  disabled: boolean;
  workspaceEnabled: boolean;
}) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const setAutoComplete = useSetIssuePRAutoComplete(issueId);

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
        {/* When the workspace leaves status alone, the issue switch changes nothing. */}
        {workspaceEnabled ? (
          <>
            <DropdownMenuItem
              disabled={setAutoComplete.isPending}
              onClick={() =>
                setAutoComplete.mutate(!disabled, {
                  onError: () => toast.error(t(($) => $.pr_automation.update_failed)),
                })
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
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
