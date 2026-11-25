"use client";

import { useCallback, useState } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { useT } from "../i18n";
import {
  storageLimitRefusal,
  useStorageLimitToast,
  type StorageLimitRefusal,
} from "../billing/storage-limit-notice";

/**
 * `useFileUpload` wired to the failure toast the upload gate depends on.
 *
 * The gate's failure fallback is "drop the placeholder, say so, let them
 * submit again". Dropping the placeholder is handled in the upload extension,
 * but without the toast the file just vanishes mid-upload with no explanation
 * — which is what every composer did, since `uploadWithToast` only toasts if
 * the caller supplies `onError` and no caller ever did (MUL-4808).
 *
 * `useFileUpload` lives in `@multica/core`, which may not import a UI library,
 * so the toast is supplied here — once, rather than per composer.
 *
 * A file the attachment storage limit refused (MUL-9489) is reported with
 * what to do about it: as `storageRefusal` for a host that renders a
 * StorageLimitNotice under its editor (`inlineStorageNotice`), otherwise as a
 * toast carrying the same notice.
 */
function useEditorUpload(opts?: { inlineStorageNotice?: boolean }) {
  const { t } = useT("editor");
  const inline = opts?.inlineStorageNotice === true;
  const [storageRefusal, setStorageRefusal] = useState<StorageLimitRefusal | null>(null);
  const toastStorageLimit = useStorageLimitToast();
  const onError = useCallback(
    (error: Error, file: File) => {
      if (inline) {
        const refusal = storageLimitRefusal(error, file.name);
        if (refusal) {
          setStorageRefusal(refusal);
          return;
        }
      } else if (toastStorageLimit(error, file.name)) {
        return;
      }
      toast.error(
        t(($) => $.upload.failed, { filename: file.name, reason: error.message }),
      );
    },
    [inline, t, toastStorageLimit],
  );
  const dismissStorageRefusal = useCallback(() => setStorageRefusal(null), []);
  return { ...useFileUpload(api, onError), storageRefusal, dismissStorageRefusal };
}

export { useEditorUpload };
