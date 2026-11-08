"use client";

import { useCallback, useEffect, useRef } from "react";
import { toast } from "sonner";
import {
  findDeliverableByName,
  type DeliverableFile,
} from "@multica/core/attachments/deliverables";
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

/**
 * Opens the deliverable a `/deliverables/<name>` link names, once the
 * issue's timeline has loaded. Mount inside the issue's
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
  ready,
  onOpenedInViewer,
  onDone,
}: {
  name: string;
  files: ReadonlyArray<DeliverableFile>;
  /** The timeline has loaded, so a missing file really is missing. */
  ready: boolean;
  onOpenedInViewer: () => void;
  onDone: () => void;
}) {
  const { t } = useT("issues");
  const { openAt } = usePreviewSequence();
  const { open, modal } = useOpenAttachment();
  const handledRef = useRef<string | null>(null);

  useEffect(() => {
    if (!ready || handledRef.current === name) return;
    handledRef.current = name;
    const file = findDeliverableByName(files, name);
    if (!file) {
      toast.error(t(($) => $.deliverables.not_found, { name }));
      onDone();
      return;
    }
    if (openAt(file.latest.id)) {
      onOpenedInViewer();
      return;
    }
    open(file.latest);
    onDone();
  }, [ready, name, files, openAt, open, onOpenedInViewer, onDone, t]);

  return modal;
}
