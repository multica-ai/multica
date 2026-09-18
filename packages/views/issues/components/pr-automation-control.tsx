"use client";

import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issuePRPolicyOptions,
  useUpdateIssuePRPolicy,
} from "@multica/core/github";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";

export function PRAutomationControl({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceId();
  const fieldId = useId();
  const { t } = useT("settings");
  const { data } = useQuery(issuePRPolicyOptions(wsId, issueId));
  const mutation = useUpdateIssuePRPolicy(wsId, issueId);
  const [url, setUrl] = useState("");
  const [open, setOpen] = useState(false);
  if (!data) return null;
  if (!data.migrated)
    return (
      <p className="px-2 text-caption text-muted-foreground">
        {t(($) => $.pr_automation.legacy)}
      </p>
    );
  const issue = data.issue;
  const reasons: Record<string, string> = {
    ambiguous: t(($) => $.pr_automation.ambiguous),
    ready: t(($) => $.pr_automation.will_complete),
    terminal: t(($) => $.pr_automation.terminal),
    triage: t(($) => $.pr_automation.triage),
    issue_disabled: t(($) => $.pr_automation.issue_disabled),
    workspace_disabled: t(($) => $.pr_automation.workspace_disabled),
    no_links: t(($) => $.pr_automation.no_links),
    waiting: t(($) => $.pr_automation.waiting),
    sync_required: t(($) => $.pr_automation.pending),
  };
  const states: Record<string, string> = {
    open: t(($) => $.pr_automation.state_open),
    draft: t(($) => $.pr_automation.state_draft),
    closed: t(($) => $.pr_automation.state_closed),
    merged: t(($) => $.pr_automation.state_merged),
  };
  const sources: Record<string, string> = {
    title: t(($) => $.pr_automation.source_title),
    branch: t(($) => $.pr_automation.source_branch),
    body: t(($) => $.pr_automation.source_body),
    manual: t(($) => $.pr_automation.source_manual),
  };
  const summary =
    reasons[issue.decision.reason] ?? t(($) => $.pr_automation.pending);
  return (
    <div className="space-y-1 px-2">
      <p className="text-caption text-muted-foreground">{summary}</p>
      {issue.links
        .filter((l) => issue.decision.waiting.includes(l.prId))
        .map((link) => (
          <a
            key={link.prId}
            className="block truncate text-caption underline underline-offset-2"
            href={link.url}
            target="_blank"
            rel="noreferrer"
          >
            {link.title} ·{" "}
            {states[link.state] ?? t(($) => $.pr_automation.pending)}
          </a>
        ))}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogTrigger render={<Button size="xs" variant="ghost" />}>
          {t(($) => $.pr_automation.manage)}
        </DialogTrigger>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(($) => $.pr_automation.manage)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.pr_automation.action_note)}
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center justify-between gap-4">
            <Label htmlFor={`${fieldId}-inherit`}>
              {t(($) => $.pr_automation.inherit)}
            </Label>
            <Switch
              id={`${fieldId}-inherit`}
              checked={!issue.disabled}
              disabled={mutation.isPending}
              onCheckedChange={(enabled) => {
                mutation.mutate({ disabled: !enabled });
              }}
            />
          </div>
          <div className="max-h-64 space-y-2 overflow-auto">
            {issue.links.map((link) => (
              <div key={link.prId} className="flex min-w-0 items-center gap-2">
                <div className="min-w-0 flex-1">
                  <a
                    href={link.url}
                    target="_blank"
                    rel="noreferrer"
                    className="block truncate text-caption underline"
                  >
                    {link.title}
                  </a>
                  <span className="text-micro text-muted-foreground">
                    {sources[link.source] ?? t(($) => $.pr_automation.pending)}{" "}
                    · {states[link.state] ?? t(($) => $.pr_automation.pending)}
                  </span>
                </div>
                <Button
                  variant="ghost"
                  size="xs"
                  disabled={mutation.isPending}
                  onClick={() =>
                    mutation.mutate({ prId: link.prId, mode: "excluded" })
                  }
                >
                  {t(($) => $.pr_automation.remove)}
                </Button>
              </div>
            ))}
            {issue.excluded.map((link) => (
              <div
                key={link.prId}
                className="flex items-center gap-2 text-caption"
              >
                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                  {link.title}
                </span>
                <Button
                  variant="ghost"
                  size="xs"
                  disabled={mutation.isPending}
                  onClick={() =>
                    mutation.mutate({ prId: link.prId, mode: "automatic" })
                  }
                >
                  {t(($) => $.pr_automation.restore)}
                </Button>
              </div>
            ))}
          </div>
          <form
            className="space-y-2"
            onSubmit={async (e) => {
              e.preventDefault();
              try {
                await mutation.mutateAsync({ url: url.trim(), mode: "manual" });
                setUrl("");
              } catch {
                /* keep input for retry */
              }
            }}
          >
            <Label htmlFor={`${fieldId}-url`}>
              {t(($) => $.pr_automation.url)}
            </Label>
            <Input
              id={`${fieldId}-url`}
              type="url"
              required
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
            <Button
              type="submit"
              disabled={mutation.isPending || !url.trim()}
              aria-busy={mutation.isPending}
            >
              {t(($) => $.pr_automation.link)}
            </Button>
          </form>
          {mutation.error && (
            <p role="alert" className="text-caption text-destructive">
              {mutation.error.message}
            </p>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
