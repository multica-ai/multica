"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { useCreateDoc, useCreateDocFolder } from "@multica/core/docs";
import { useWorkspacePaths } from "@multica/core/paths";
import type { DocFolder, DocFormat } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

const HTML_STARTER = `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <style>
      body { font-family: system-ui, sans-serif; margin: 40px; }
    </style>
  </head>
  <body>
    <h1>Untitled</h1>
  </body>
</html>
`;

/** Creating docs and folders from the panel, a folder page, or a menu. */
export function useDocCommands() {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const createDoc = useCreateDoc();
  const createFolder = useCreateDocFolder();

  const newDoc = useCallback(
    (format: DocFormat, folderId: string | null) => {
      createDoc.mutate(
        {
          title: t(($) => $.untitled),
          format,
          folder_id: folderId,
          content: format === "html" ? HTML_STARTER : "",
        },
        {
          onSuccess: (doc) => push(p.docDetail(doc.id)),
          onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.errors.create_doc)),
        },
      );
    },
    [createDoc, p, push, t],
  );

  const newFolder = useCallback(
    (title: string, parentId: string | null, onCreated?: (folder: DocFolder) => void) => {
      createFolder.mutate(
        { title, parent_id: parentId },
        {
          onSuccess: (folder) => onCreated?.(folder),
          onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.errors.create_folder)),
        },
      );
    },
    [createFolder, t],
  );

  return { newDoc, newFolder, creating: createDoc.isPending };
}
