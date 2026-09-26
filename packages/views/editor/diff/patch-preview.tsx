"use client";

import { useMemo, type ReactNode } from "react";
import { buildDiffFiles, parsePatch } from "@multica/core/code-changes";
import { useT } from "../../i18n";
import type { DiffLayout } from "../viewer-chrome";
import { DiffView } from "./diff-view";

/**
 * An uploaded `.patch` / `.diff` file in the diff viewer (MUL-7651). Text that
 * turns out to hold no diff renders `fallback` instead — the file is still
 * readable, just not as a diff.
 */
export function PatchPreview({
  patch,
  layout,
  fallback,
}: {
  patch: string;
  layout: DiffLayout;
  fallback: ReactNode;
}) {
  const { t } = useT("editor");
  const files = useMemo(() => buildDiffFiles(null, parsePatch(patch)), [patch]);
  if (files.length === 0) return <>{fallback}</>;
  return (
    <DiffView
      files={files}
      layout={layout}
      listLabel={t(($) => $.diff.files_count, { count: files.length })}
    />
  );
}
