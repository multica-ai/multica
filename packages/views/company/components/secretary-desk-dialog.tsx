"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  companySetupOptions,
  secretaryDeskDirectivesOptions,
  secretaryDeskItems,
  secretaryDeskRequestsOptions,
  type SecretaryDeskItem,
} from "@multica/core/company";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { squadListOptions } from "@multica/core/workspace/queries";
import type { Issue, Squad } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Loader2 } from "@multica/ui/icons";
import { useStatusLabel } from "../../issues/utils/status-label";
import { StatusIcon } from "../../issues/components/status-icon";
import { useT, useTimeAgo } from "../../i18n";
import { AppLink } from "../../navigation";

/** The Secretary desk button in the company header, opening the read-only desk list. */
export function SecretaryDeskButton() {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button type="button" variant="outline" size="sm" onClick={() => setOpen(true)}>
        {t(($) => $.company.desk_open)}
      </Button>
      <SecretaryDeskDialog open={open} onClose={() => setOpen(false)} />
    </>
  );
}

/**
 * Read-only list of the directives the boss gave and the squad requests the
 * Secretary's own issues raised, with status, squad and age. Editing happens on
 * the issue pages.
 */
export function SecretaryDeskDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useT("issues");
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="flex max-h-[min(88dvh,48rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
        <header className="flex shrink-0 items-center border-b px-4 py-3 pr-12">
          <DialogTitle className="text-heading">{t(($) => $.company.desk_open)}</DialogTitle>
        </header>
        <SecretaryDeskList />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so the desk reads its lists on open. */
function SecretaryDeskList() {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const statusLabel = useStatusLabel(wsId);
  const setup = useQuery(companySetupOptions(wsId));
  const secretaryId = setup.data?.secretary_id ?? null;
  const directives = useQuery(secretaryDeskDirectivesOptions(wsId));
  const requests = useQuery(secretaryDeskRequestsOptions(wsId, secretaryId));
  const squads = useQuery(squadListOptions(wsId));

  if (setup.isError || directives.isError || requests.isError) {
    return <p className="px-4 py-6 text-body text-destructive">{t(($) => $.company.desk_load_failed)}</p>;
  }
  if (!setup.isSuccess || !directives.isSuccess || (secretaryId && !requests.isSuccess)) {
    return (
      <div role="status" className="flex justify-center px-4 py-10">
        <Loader2 aria-hidden className="size-4 animate-spin text-muted-foreground" />
        <span className="sr-only">{t(($) => $.company.desk_loading)}</span>
      </div>
    );
  }

  const items = secretaryDeskItems(directives.data, requests.data ?? []);
  if (items.length === 0) {
    return <p className="px-4 py-6 text-body text-muted-foreground">{t(($) => $.company.desk_empty)}</p>;
  }

  return (
    <ul className="min-h-0 divide-y overflow-y-auto">
      {items.map((item) => (
        <DeskRow
          key={item.issue.id}
          item={item}
          href={paths.issueDetail(item.issue.identifier)}
          status={statusLabel(item.issue.status)}
          squad={squadOf(item.issue, squads.data ?? [])}
          age={timeAgo(item.issue.created_at)}
        />
      ))}
    </ul>
  );
}

/** The squad an issue is assigned to, by name; empty for a person or an agent. */
function squadOf(issue: Issue, squads: readonly Squad[]): string | null {
  if (issue.assignee_type !== "squad") return null;
  return squads.find((squad) => squad.id === issue.assignee_id)?.name ?? null;
}

function DeskRow({
  item,
  href,
  status,
  squad,
  age,
}: {
  item: SecretaryDeskItem;
  href: string;
  status: string;
  squad: string | null;
  age: string;
}) {
  const { t } = useT("issues");
  const { issue } = item;
  return (
    <li className="flex flex-col gap-1 px-4 py-3">
      <div className="flex min-w-0 items-center gap-2">
        <span className="shrink-0 text-caption text-muted-foreground">
          {item.kind === "directive" ? t(($) => $.company.desk_directive) : t(($) => $.company.desk_request)}
        </span>
        <AppLink href={href} className="min-w-0 truncate text-body hover:underline">
          <span className="text-muted-foreground">{issue.identifier}</span> {issue.title}
        </AppLink>
      </div>
      {item.quote ? (
        <blockquote className="border-l-2 pl-2 text-body text-muted-foreground">{item.quote}</blockquote>
      ) : null}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
        <span className="inline-flex items-center gap-1.5">
          <StatusIcon status={issue.status} className="size-3.5" />
          {status}
        </span>
        {squad ? <span>{squad}</span> : null}
        <span>{age}</span>
      </div>
    </li>
  );
}
