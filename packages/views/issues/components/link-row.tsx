"use client";

import { X } from "lucide-react";
import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { StatusIcon } from "./status-icon";
import { LinkPicker } from "./pickers/link-picker";
import { useDeleteIssueLink } from "@multica/core/links/mutations";
import {
  LINK_LABEL,
  LINK_TYPES,
  type IssueLink,
  type IssueStatus,
} from "@multica/core/types";

/**
 * Inline links row used inside the issue-detail sidebar's "Links" PropRow.
 *
 * Groups attached links by direction-aware display label (e.g. all "blocks"
 * outgoing rows, then all "blocked by" incoming rows). Each link is a
 * clickable AppLink to the target issue with a small remove button.
 *
 * The picker trigger sits at the end so the user can keep adding links.
 */
export function LinkRow({
  issueId,
  links,
}: {
  issueId: string;
  links: IssueLink[];
}) {
  const remove = useDeleteIssueLink();
  const paths = useWorkspacePaths();

  // Bucket links by (link_type × direction) so we can render natural section
  // headings: "blocks ...", "blocked by ...", etc.
  const groups: Array<{
    key: string;
    label: string;
    items: IssueLink[];
  }> = [];

  for (const t of LINK_TYPES) {
    for (const dir of ["outgoing", "incoming"] as const) {
      // For relates_to the incoming/outgoing labels collapse to the same
      // string ("relates to") — merge those into one group.
      if (t === "relates_to" && dir === "incoming") continue;

      const label =
        t === "relates_to"
          ? LINK_LABEL.relates_to.outgoing
          : LINK_LABEL[t][dir];

      const items =
        t === "relates_to"
          ? links.filter((l) => l.link_type === "relates_to")
          : links.filter((l) => l.link_type === t && l.direction === dir);

      if (items.length === 0) continue;
      groups.push({ key: `${t}-${dir}`, label, items });
    }
  }

  const empty = links.length === 0;

  return (
    <div className="flex w-full min-w-0 flex-col gap-1.5">
      {groups.map((g) => (
        <div key={g.key} className="flex flex-col gap-0.5">
          <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
            {g.label}
          </span>
          <div className="flex flex-col gap-0.5">
            {g.items.map((l) => (
              <LinkRowItem
                key={l.id}
                link={l}
                href={paths.issueDetail(l.target_issue_id)}
                onRemove={() => remove.mutate({ issueId, linkId: l.id })}
              />
            ))}
          </div>
        </div>
      ))}

      <LinkPicker
        issueId={issueId}
        attached={links}
        align="start"
        trigger={
          <span className="text-muted-foreground hover:text-foreground">
            {empty ? "Add link" : "+ Add"}
          </span>
        }
      />
    </div>
  );
}

function LinkRowItem({
  link,
  href,
  onRemove,
}: {
  link: IssueLink;
  href: string;
  onRemove: () => void;
}) {
  // Optimistic rows have a synthetic id and aren't in the database yet — show
  // them with reduced opacity and disable the remove button.
  const isOptimistic = link.id.startsWith("optimistic-");

  return (
    <div
      className={`group flex min-w-0 items-center gap-1.5 rounded-md px-1.5 py-1 -mx-1.5 hover:bg-accent/50 ${isOptimistic ? "opacity-60" : ""}`}
    >
      <AppLink
        href={href}
        className="flex min-w-0 flex-1 items-center gap-1.5"
      >
        <StatusIcon status={link.target_status as IssueStatus} className="h-3.5 w-3.5 shrink-0" />
        <span className="font-mono text-[11px] text-muted-foreground shrink-0">
          {link.target_identifier}
        </span>
        <span className="truncate text-xs">{link.target_title}</span>
      </AppLink>
      {!isOptimistic && (
        <button
          type="button"
          onClick={onRemove}
          aria-label={`Remove link to ${link.target_identifier}`}
          className="opacity-0 group-hover:opacity-70 hover:opacity-100 transition-opacity rounded-sm p-0.5 hover:bg-black/10 dark:hover:bg-white/10"
        >
          <X className="h-3 w-3" />
        </button>
      )}
    </div>
  );
}
