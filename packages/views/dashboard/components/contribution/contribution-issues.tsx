"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { useWorkspaceId } from "@multica/core/hooks";
import { contributionIssuesOptions, type ContributionScope } from "@multica/core/dashboard";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import type { ContributionIssueSort } from "@multica/core/types";
import { AppLink } from "../../../navigation";
import { useT } from "../../../i18n";
import { formatDuration } from "../../utils";
import { Segmented } from "../dashboard-shared";
import { StatusLabel } from "./contribution-members";
import { Card, EmptyRow, Pager } from "./contribution-shared";

const PAGE = 20;

/** Every issue with activity in the window, with its participants,
 *  interactions (comments + status changes) and time to done. */
export function ContributionIssues({ scope }: { scope: ContributionScope }) {
  const { t } = useT("usage");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const [sort, setSort] = useState<ContributionIssueSort>("interactions");
  const [offset, setOffset] = useState(0);
  const { data } = useQuery(contributionIssuesOptions(wsId, scope, { sort, limit: PAGE, offset }));
  const issues = data?.issues ?? [];
  const total = data?.total ?? 0;
  const lessThanMinute = t(($) => $.duration.less_than_minute);

  return (
    <Card
      flush
      title={
        <span className="flex items-center gap-2">
          {t(($) => $.contribution.issues.title)}
          <span className="text-caption font-normal text-muted-foreground">
            {t(($) => $.contribution.issues.caption, { count: total })}
          </span>
        </span>
      }
      aside={
        <Segmented
          label={t(($) => $.contribution.issues.sort_label)}
          value={sort}
          onChange={(next) => {
            setSort(next);
            setOffset(0);
          }}
          options={[
            { label: t(($) => $.contribution.issues.sort.interactions), value: "interactions" },
            { label: t(($) => $.contribution.issues.sort.participants), value: "participants" },
            { label: t(($) => $.contribution.issues.sort.completion), value: "completion" },
            { label: t(($) => $.contribution.issues.sort.recent), value: "recent" },
          ]}
        />
      }
    >
      {issues.length === 0 ? (
        <EmptyRow>{t(($) => $.contribution.issues.empty)}</EmptyRow>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t(($) => $.contribution.issues.issue)}</TableHead>
              <TableHead>{t(($) => $.contribution.issues.issue_title)}</TableHead>
              <TableHead>{t(($) => $.contribution.issues.status)}</TableHead>
              <TableHead className="text-right">{t(($) => $.contribution.issues.interactions)}</TableHead>
              <TableHead className="text-right">{t(($) => $.contribution.issues.participants)}</TableHead>
              <TableHead className="text-right">{t(($) => $.contribution.issues.completion_time)}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {issues.map((i) => (
              <TableRow key={i.issue_id}>
                <TableCell className="text-caption tabular-nums">
                  <AppLink href={wsPaths.issueDetail(issueRouteRef({ id: i.issue_id, identifier: i.identifier || null }))} className="text-muted-foreground hover:text-foreground hover:underline">
                    {i.identifier}
                  </AppLink>
                </TableCell>
                <TableCell className="max-w-80 truncate">{i.title}</TableCell>
                <TableCell>
                  <StatusLabel group={i.status_group} />
                </TableCell>
                <TableCell className="text-right tabular-nums">{i.interactions}</TableCell>
                <TableCell className="text-right tabular-nums">{i.participants}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {i.completion_seconds == null ? "—" : formatDuration(i.completion_seconds, lessThanMinute)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <p className="border-t px-4 py-2.5 text-caption text-muted-foreground">{t(($) => $.contribution.issues.hint)}</p>
      <Pager offset={offset} limit={PAGE} total={total} onChange={setOffset} />
    </Card>
  );
}
