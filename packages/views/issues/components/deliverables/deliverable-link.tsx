"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  findDeliverableByName,
  type DeliverableFile,
} from "@multica/core/attachments/deliverables";
import { issueTimelineOptions } from "@multica/core/issues/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { copyText } from "@multica/ui/lib/clipboard";
import { useNavigation } from "../../../navigation";
import { useT } from "../../../i18n";
import { usePreviewSequence } from "../../../editor";
import { useOpenAttachment } from "./use-open-attachment";

/**
 * Copy the "latest version" link of a deliverable: the issue's
 * `/deliverables/<name>` route, which always opens the newest upload of the
 * file. Never a link to one upload, so a link shared once keeps showing the
 * file as it is revised.
 *
 * `issueRef` is the issue identifier (`MUL-123`) or, before it is known, the
 * UUID. Returns undefined without one, so callers render no button.
 */
export function useCopyDeliverableLink(
  issueRef: string | undefined,
): ((filename: string) => void) | undefined {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  // `paths` and the navigation adapter are rebuilt every render; read them at
  // click time so the callback stays stable for memoized rows.
  const latest = useRef({ paths, navigation });
  latest.current = { paths, navigation };
  const copy = useCallback(
    (filename: string) => {
      if (!issueRef) return;
      const { paths, navigation } = latest.current;
      const url = navigation.getShareableUrl(paths.issueDeliverable(issueRef, filename));
      void copyText(url).then((ok) => {
        if (ok) toast.success(t(($) => $.detail.link_copied));
        else toast.error(t(($) => $.detail.link_copy_failed));
      });
    },
    [issueRef, t],
  );
  return issueRef ? copy : undefined;
}

/** Where a deliverable link's read of the issue timeline stands. */
export type TimelineReadStatus = "pending" | "ready" | "failed";

/**
 * One current read of the issue's timeline for a `/deliverables/<name>` link.
 *
 * The link must name the file as it is now, not as a cached timeline from an
 * earlier visit remembers it: that cache may predate the upload the link was
 * shared for (open v1, upload v2 elsewhere, follow the link), or be empty. The
 * query's loading flag cannot tell, because it is false whenever cached data
 * exists, and with the app's `staleTime: Infinity` mounting does not refetch.
 * So this asks the server once per link, and reports `ready` only when the
 * cache the page renders from (`dataUpdatedAt`, from `useIssueTimeline`) has
 * taken that answer, so `files` built from it already include any new upload.
 *
 * `failed` means the read itself failed: the caller must not conclude that
 * the file is missing.
 */
export function useFreshTimelineRead(
  issueId: string,
  name: string | undefined,
  dataUpdatedAt: number,
): TimelineReadStatus {
  const qc = useQueryClient();
  const [read, setRead] = useState<{
    key: string;
    status: "pending" | "done" | "failed";
    at: number;
  } | null>(null);
  const key = name === undefined ? null : `${issueId}\u0000${name}`;

  useEffect(() => {
    if (key === null) return;
    let cancelled = false;
    setRead({ key, status: "pending", at: 0 });
    // staleTime 0: fetch even when the cache counts as fresh. `retry: 1`
    // matches the app's default for queries; fetchQuery's own default is none.
    qc.fetchQuery({ ...issueTimelineOptions(issueId), staleTime: 0, retry: 1 }).then(
      () => {
        if (cancelled) return;
        const at = qc.getQueryState(issueTimelineOptions(issueId).queryKey)?.dataUpdatedAt ?? 0;
        setRead({ key, status: "done", at });
      },
      () => {
        if (!cancelled) setRead({ key, status: "failed", at: 0 });
      },
    );
    return () => {
      cancelled = true;
    };
  }, [qc, issueId, key]);

  if (!read || read.key !== key || read.status === "pending") return "pending";
  if (read.status === "failed") return "failed";
  return dataUpdatedAt >= read.at ? "ready" : "pending";
}

/**
 * Opens the deliverable a `/deliverables/<name>` link names, once
 * `useFreshTimelineRead` reports a current timeline. Mount inside the issue's
 * `PreviewSequenceProvider`, so the file opens in the page's viewer with its
 * versions and info panel.
 *
 * `onDone` fires when the link has been dealt with and the address should go
 * back to the plain issue page: at once when the file is missing or opened
 * outside the page's viewer (downloaded, or the standalone preview), and left
 * to the host when it opened in the viewer, which knows when that closes.
 *
 * Runs once per name: the reader closing the viewer, or a new version
 * arriving, must not re-open it.
 */
export function OpenDeliverableOnLoad({
  name,
  files,
  read,
  onOpenedInViewer,
  onDone,
}: {
  name: string;
  files: ReadonlyArray<DeliverableFile>;
  /**
   * From `useFreshTimelineRead`. Only `ready` lets a missing file count as
   * missing; on `failed` the link stays in the address, so reloading retries.
   */
  read: TimelineReadStatus;
  onOpenedInViewer: () => void;
  onDone: () => void;
}) {
  const { t } = useT("issues");
  const { openAt } = usePreviewSequence();
  const { open, modal } = useOpenAttachment();
  const handledRef = useRef<string | null>(null);

  useEffect(() => {
    if (read === "pending" || handledRef.current === name) return;
    handledRef.current = name;
    if (read === "failed") {
      toast.error(t(($) => $.deliverables.link_load_failed, { name }));
      return;
    }
    const file = findDeliverableByName(files, name);
    if (!file) {
      toast.error(t(($) => $.deliverables.not_found, { name }));
      onDone();
      return;
    }
    // The link names this file: if it fails to load, stay on it and say so
    // rather than showing the reader a different one.
    if (openAt(file.latest.id, { stayOnError: true })) {
      onOpenedInViewer();
      return;
    }
    open(file.latest);
    onDone();
  }, [read, name, files, openAt, open, onOpenedInViewer, onDone, t]);

  return modal;
}
