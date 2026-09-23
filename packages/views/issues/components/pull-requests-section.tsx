"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { CircleSlash, MoreHorizontal, Plus, RotateCcw, Settings } from "lucide-react";
import { ApiError } from "@multica/core/api";
import {
  issuePullRequestsOptions,
  useLinkIssuePullRequest,
  useSetIssuePRAutoComplete,
} from "@multica/core/github";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
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

/**
 * The code group of the issue sidebar's Deliverables section (MUL-7649): the
 * linked PRs, what the "every linked PR merged → Done" rule will do for this
 * issue, and the two exceptions a person can make — link a PR by hand, or
 * turn auto-complete off for this one issue (MUL-7429).
 */
export function PullRequestsGroup({
  issueId,
  identifier,
}: {
  issueId: string;
  identifier: string;
}) {
  const { t } = useT("issues");
  const { data } = useQuery(issuePullRequestsOptions(issueId));
  // Older backends have no link / auto-complete endpoints; keep the actions
  // hidden until the server says it supports them.
  const supported = !!data?.auto_complete;

  return (
    <section aria-label={t(($) => $.deliverables.group_code)}>
      <div className="mb-1 flex min-h-6 items-center gap-0.5">
        <p className="min-w-0 flex-1 truncate text-micro font-medium text-muted-foreground">
          {t(($) => $.deliverables.group_code)}
        </p>
        {supported ? (
          <>
            <LinkPullRequestPopover issueId={issueId} identifier={identifier} />
            <AutoCompleteMenu issueId={issueId} disabled={data?.auto_complete?.issue_disabled ?? false} />
          </>
        ) : null}
      </div>
      <PullRequestList issueId={issueId} identifier={identifier} />
    </section>
  );
}

function LinkPullRequestPopover({
  issueId,
  identifier,
}: {
  issueId: string;
  identifier: string;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
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
          <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.pr_automation.link_action)}>
            <Plus />
          </Button>
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
            aria-describedby={`link-pr-${issueId}-hint`}
            onChange={(e) => {
              setUrl(e.target.value);
              if (link.error) link.reset();
            }}
          />
          {error ? (
            <p role="alert" className="text-caption text-destructive">
              {error}
            </p>
          ) : (
            <p id={`link-pr-${issueId}-hint`} className="text-caption text-muted-foreground">
              {t(($) => $.pr_automation.link_hint, { identifier })}
            </p>
          )}
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

function AutoCompleteMenu({ issueId, disabled }: { issueId: string; disabled: boolean }) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const setAutoComplete = useSetIssuePRAutoComplete(issueId);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.pr_automation.section_menu)}>
            <MoreHorizontal />
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="w-60">
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
        <DropdownMenuItem onClick={() => navigation.push(`${paths.settings()}?tab=issue-statuses`)}>
          <Settings />
          {t(($) => $.pr_automation.settings)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
