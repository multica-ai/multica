"use client";

import { useNavigation } from "@multica/views/navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  getObjectRenderer,
  useReferencesByObject,
  type IssueReference,
} from "@multica/cerebro-references/core";
import { ObjectIcon } from "../components/object-icon";
import { ReferenceBadge } from "../components/reference-badge";

// Reverse-lookup page: "which issues reference this object?" Given an
// object kind + ref id, lists every issue that links to it. The object's
// title/badge/url all flow through the renderer registry, so an unknown
// object kind renders via the fallback renderer rather than breaking.
export function ReferencesByObjectPage({
  object,
  refId,
}: {
  object: string;
  refId: string;
}) {
  const router = useNavigation();
  const paths = useWorkspacePaths();
  const { data: references = [], isLoading } = useReferencesByObject(object, refId);

  const renderer = getObjectRenderer(object);
  const headerTitle =
    references[0] != null ? renderer.formatTitle(references[0]) : refId;
  const headerUrl =
    references[0] != null ? renderer.resolveUrl(references[0]) : null;

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto w-full max-w-3xl px-4 py-4 md:px-8 md:py-6">
        <div className="mb-4 flex items-center gap-2">
          <ObjectIcon object={object} className="size-5 text-muted-foreground" />
          <h1 className="break-words text-xl font-semibold leading-tight">
            {headerUrl ? (
              <a
                href={headerUrl}
                target="_blank"
                rel="noreferrer"
                className="hover:underline"
              >
                {headerTitle}
              </a>
            ) : (
              headerTitle
            )}
          </h1>
          {references[0] != null && <ReferenceBadge reference={references[0]} />}
        </div>

        <p className="mb-3 text-xs uppercase tracking-wide text-muted-foreground">
          Referenced by
        </p>

        {isLoading ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : references.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No issues reference this object.
          </p>
        ) : (
          <div className="flex flex-col gap-1">
            {references.map((reference: IssueReference) => (
              <button
                key={reference.id}
                type="button"
                onClick={() => router.push(paths.issueDetail(reference.issue_id))}
                className="flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1.5 text-left text-sm transition-colors hover:bg-muted"
              >
                <span className="min-w-0 flex-1 truncate">
                  {reference.label ?? reference.issue_id}
                </span>
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
