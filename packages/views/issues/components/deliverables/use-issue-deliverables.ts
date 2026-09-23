"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  collectDeliverableFiles,
  type DeliverableFile,
} from "@multica/core/attachments/deliverables";
import { issuePullRequestsOptions, useGitHubSettings } from "@multica/core/github";
import type { Attachment, GitHubPullRequest, TimelineEntry } from "@multica/core/types";
import {
  useAttachmentPreview,
  useDownloadAttachment,
  usePreviewSequence,
} from "../../../editor";

/** What an issue has delivered as a whole (MUL-7649). */
export interface IssueDeliverables {
  /**
   * The workspace shows PRs in the sidebar. The code group then stays up
   * even with no PR linked yet — it is where a PR gets linked by hand.
   */
  showCode: boolean;
  /** Linked PRs — empty while the workspace hides the PR sidebar. */
  pullRequests: GitHubPullRequest[];
  /** Comment uploads, same-name re-uploads merged into versions, newest first. */
  files: DeliverableFile[];
  /** Code + files: the one number the sidebar and the overview both show. */
  count: number;
}

const NO_PULL_REQUESTS: GitHubPullRequest[] = [];

export function useIssueDeliverables(
  issueId: string,
  timeline: ReadonlyArray<TimelineEntry>,
): IssueDeliverables {
  const { prSidebar } = useGitHubSettings();
  const { data } = useQuery({
    ...issuePullRequestsOptions(issueId),
    enabled: !!issueId && prSidebar,
  });
  const pullRequests = prSidebar
    ? (data?.pull_requests ?? NO_PULL_REQUESTS)
    : NO_PULL_REQUESTS;
  const files = useMemo(() => collectDeliverableFiles(timeline), [timeline]);
  return useMemo(
    () => ({
      showCode: prSidebar,
      pullRequests,
      files,
      count: pullRequests.length + files.length,
    }),
    [prSidebar, pullRequests, files],
  );
}

/**
 * Open a file in the issue's viewer at its place in the page's sequence. A
 * file the sequence doesn't hold opens on its own, and one the viewer can't
 * show downloads. Render `modal` for the on-its-own case.
 */
export function useOpenAttachment() {
  const sequence = usePreviewSequence();
  const preview = useAttachmentPreview();
  const download = useDownloadAttachment();
  const { openAt } = sequence;
  const { tryOpen } = preview;
  const open = useCallback(
    (attachment: Attachment) => {
      if (openAt(attachment.id)) return;
      if (tryOpen({ kind: "full", attachment })) return;
      download(attachment.id);
    },
    [openAt, tryOpen, download],
  );
  return { open, modal: preview.modal };
}
