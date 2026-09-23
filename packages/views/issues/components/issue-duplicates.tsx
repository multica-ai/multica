"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Copy } from "lucide-react";
import type { Issue } from "@multica/core/types";
import { issueStatusCategory } from "@multica/core/issues";
import { issueDuplicatesOptions } from "@multica/core/issues/queries";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { StatusIcon } from "./status-icon";
import { useT } from "../../i18n";

// Duplicate marks (MUL-7349). A duplicate is a cancelled issue that remembers
// its original; the server clears the mark whenever the status leaves
// cancelled, so both surfaces below also key off the status.

/**
 * Banner shown above a duplicate's title, pointing at its original. Removing
 * the mark is a plain status change to todo, done through the caller so it
 * shares the detail page's status write path.
 */
export function IssueDuplicateBanner({
  issue,
  onUnmark,
}: {
  issue: Issue;
  onUnmark: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data } = useQuery(issueDuplicatesOptions(wsId, issue.id));
  const original = data?.duplicate_of;
  if (issue.status !== "cancelled" || !original) return null;

  return (
    <div className="mb-4 flex items-center gap-2 rounded-md border bg-muted/40 px-3 py-2 text-body">
      <Copy className="h-4 w-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1 truncate">
        {t(($) => $.duplicates.banner_prefix)}{" "}
        <AppLink
          href={paths.issueDetail(original.id)}
          className="font-medium underline-offset-4 hover:underline"
        >
          {original.identifier} {original.title}
        </AppLink>
      </span>
      <Button type="button" variant="ghost" size="sm" onClick={onUnmark}>
        {t(($) => $.duplicates.unmark)}
      </Button>
    </div>
  );
}

/** Sidebar list of the issues marked as duplicates of this one. */
export function IssueDuplicatesSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { colorOf, iconOf } = useIssueStatuses(wsId);
  const [open, setOpen] = useState(true);
  const { data } = useQuery(issueDuplicatesOptions(wsId, issueId));
  const duplicates = data?.duplicates ?? [];
  if (duplicates.length === 0) return null;

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.duplicates.section_title)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2">
          {duplicates.map((duplicate) => (
            <AppLink
              key={duplicate.id}
              href={paths.issueDetail(duplicate.id)}
              className="group flex items-center gap-1.5 rounded-md px-2 -mx-2 py-1.5 text-caption hover:bg-accent/50 transition-colors"
            >
              <StatusIcon
                status={duplicate.status}
                color={colorOf(duplicate.status)}
                icon={iconOf(duplicate.status)}
                category={issueStatusCategory(duplicate) ?? undefined}
                className="h-3.5 w-3.5 shrink-0"
              />
              <span className="text-muted-foreground shrink-0">{duplicate.identifier}</span>
              <span className="truncate group-hover:text-foreground">{duplicate.title}</span>
            </AppLink>
          ))}
        </div>
      )}
    </div>
  );
}
