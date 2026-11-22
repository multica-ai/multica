"use client";
import { FileQuestion } from "@multica/ui/icons";
import { buttonVariants } from "@multica/ui/components/ui/button";
import { useDocDraftStore } from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { CollectionPageState } from "../../layout/collection-page";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

/**
 * A doc the server no longer has: deleted, or a link that never pointed at
 * one. Text this browser could not save stays readable so it can be copied.
 */
export function DocNotFound({ docId }: { docId: string }) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const draft = useDocDraftStore((s) => s.draft.entries[`${wsId}:${docId}`]);
  const unsaved =
    draft &&
    (draft.title !== draft.baseTitle || draft.content !== draft.baseContent)
      ? draft
      : undefined;
  const backup = draft?.backup;
  const savedDraftLabel = t(($) => $.editor.draft);
  const previews = [
    {
      key: "current",
      draft: unsaved,
      label: backup ? t(($) => $.editor.unsaved_changes) : savedDraftLabel,
    },
    { key: "backup", draft: backup, label: savedDraftLabel },
  ];
  return (
    <div className="flex h-full flex-col overflow-auto">
      <CollectionPageState
        icon={FileQuestion}
        title={t(($) => $.not_found.title)}
        description={t(($) => $.not_found.description)}
        actions={
          <AppLink
            href={p.docs()}
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            {t(($) => $.kb)}
          </AppLink>
        }
      >
        {previews.map(
          ({ key, draft, label }) =>
            draft && (
              <details
                key={key}
                open
                className="w-full max-w-2xl rounded-md border p-3 text-left text-body"
              >
                <summary className="cursor-pointer">{label}</summary>
                <textarea
                  aria-label={label}
                  readOnly
                  value={`${draft.title}\n\n${draft.content}`}
                  className="mt-2 min-h-40 w-full resize-y rounded-md bg-muted p-2 font-mono text-caption"
                />
              </details>
            ),
        )}
      </CollectionPageState>
    </div>
  );
}
