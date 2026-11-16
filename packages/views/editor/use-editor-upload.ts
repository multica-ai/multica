"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { useT } from "../i18n";

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
 */
function useEditorUpload() {
  const { t } = useT("editor");
  const onError = useCallback(
    (error: Error, file: File) => {
      toast.error(
        t(($) => $.upload.failed, { filename: file.name, reason: error.message }),
      );
    },
    [t],
  );
  return useFileUpload(api, onError);
}

export { useEditorUpload };
