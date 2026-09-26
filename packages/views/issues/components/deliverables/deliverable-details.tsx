"use client";

import { useCallback, type ReactNode } from "react";
import { Check, ChevronDown, FileText, ImageIcon, MessageSquareText } from "lucide-react";
import {
  findDeliverableVersion,
  type DeliverableFile,
} from "@multica/core/attachments/deliverables";
import type { ImageSequenceItem } from "@multica/core/attachments/image-sequence";
import type { Attachment, TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT, useTimeAgo } from "../../../i18n";
import { ActorAvatar } from "../../../common/actor-avatar";
import { formatBytes } from "../../../common/format-bytes";
import { fileTypeLabel } from "../../../editor/utils/preview";
import type {
  PreviewItemDetails,
  PreviewSequenceControls,
} from "../../../editor";
import { descriptionPreview } from "../description-preview";
import { DeliverableThumbnail } from "./deliverable-thumbnail";

/** Block id the issue page gives its description in the preview sequence. */
export const DESCRIPTION_BLOCK_ID = "description";

/** Where a file was posted, as the viewer's "locate" action targets it. */
export type DeliverableOrigin =
  | { kind: "description" }
  | { kind: "comment"; commentId: string };

/**
 * The issue page's `describeItem` for its preview sequence: the version
 * switcher next to the file name, the info panel, and "show in comments".
 */
export function useDeliverableDetails({
  files,
  commentById,
  onLocate,
}: {
  files: ReadonlyArray<DeliverableFile>;
  commentById: ReadonlyMap<string, TimelineEntry>;
  onLocate: (origin: DeliverableOrigin) => void;
}) {
  const { t } = useT("issues");
  return useCallback(
    (item: ImageSequenceItem, controls: PreviewSequenceControls): PreviewItemDetails => {
      const origin: DeliverableOrigin | null =
        item.blockId === DESCRIPTION_BLOCK_ID
          ? { kind: "description" }
          : item.blockId && commentById.has(item.blockId)
            ? { kind: "comment", commentId: item.blockId }
            : null;
      const deliverable = item.attachment
        ? findDeliverableVersion(files, item.attachment.id)
        : undefined;
      const locateLabel =
        origin?.kind === "description"
          ? t(($) => $.deliverables.locate_description)
          : t(($) => $.deliverables.locate_comment);
      // One action for the top bar and the info panel: the viewer covers the
      // page, so it closes before the page scrolls to the file's origin.
      const locate = origin
        ? () => {
            controls.close();
            onLocate(origin);
          }
        : undefined;

      return {
        titleAccessory:
          deliverable && deliverable.file.versions.length > 1 ? (
            <VersionSwitcher
              file={deliverable.file}
              version={deliverable.version}
              controls={controls}
            />
          ) : undefined,
        info: (
          <DeliverableInfo
            item={item}
            controls={controls}
            comment={origin?.kind === "comment" ? commentById.get(origin.commentId) : undefined}
            isDescription={origin?.kind === "description"}
            version={deliverable}
            locateLabel={locateLabel}
            onLocate={locate}
          />
        ),
        locate: locate ? { label: locateLabel, onSelect: locate } : undefined,
      };
    },
    [files, commentById, onLocate, t],
  );
}

// ---------------------------------------------------------------------------
// Version switcher
// ---------------------------------------------------------------------------

function VersionSwitcher({
  file,
  version,
  controls,
}: {
  file: DeliverableFile;
  version: number;
  controls: PreviewSequenceControls;
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const inSession = new Set(controls.items.map((i) => i.key));
  // Newest first, like every version picker people know.
  const entries = file.versions.map((attachment, i) => ({ attachment, n: i + 1 })).reverse();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className="flex shrink-0 items-center gap-0.5 rounded-xs bg-secondary px-1.5 py-0.5 text-micro font-medium tabular-nums text-muted-foreground transition-colors hover:text-foreground"
            aria-label={t(($) => $.deliverables.version_label, {
              version,
              total: file.versions.length,
            })}
          >
            {t(($) => $.deliverables.version_short, { version })}
            <ChevronDown className="size-3" />
          </button>
        }
      />
      {/* Portaled out of the viewer's dark chrome, so it opts in itself. */}
      <DropdownMenuContent className="dark min-w-56" align="start">
        {entries.map(({ attachment, n }) => (
          <DropdownMenuItem
            key={attachment.id}
            disabled={!inSession.has(attachment.id)}
            onClick={() => controls.goTo(attachment.id)}
          >
            <span className="w-7 font-medium tabular-nums">
              {t(($) => $.deliverables.version_short, { version: n })}
            </span>
            <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground tabular-nums">
              {new Date(attachment.created_at).toLocaleString(locale, {
                dateStyle: "medium",
                timeStyle: "short",
              })}
              {attachment.size_bytes > 0 ? ` · ${formatBytes(attachment.size_bytes)}` : ""}
            </span>
            {n === version && <Check className="size-3.5 text-foreground" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// ---------------------------------------------------------------------------
// Info panel
// ---------------------------------------------------------------------------

function DeliverableInfo({
  item,
  controls,
  comment,
  isDescription,
  version,
  locateLabel,
  onLocate,
}: {
  item: ImageSequenceItem;
  controls: PreviewSequenceControls;
  comment?: TimelineEntry;
  isDescription: boolean;
  version?: { file: DeliverableFile; version: number };
  locateLabel: string;
  onLocate?: () => void;
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const attachment = item.attachment;

  // Files posted in the same comment (or the description), in page order.
  const siblings = item.blockId
    ? controls.items.filter((other) => other.blockId === item.blockId)
    : [];
  const excerpt = comment?.content ? descriptionPreview(comment.content) : "";

  return (
    <div className="space-y-6 px-4 py-4">
      {(comment || isDescription) && (
        <InfoSection title={t(($) => $.deliverables.info_source)}>
          <div className="space-y-2 rounded-lg bg-secondary/60 p-3">
            {comment ? (
              <div className="flex min-w-0 items-center gap-1.5 text-caption">
                <ActorAvatar
                  actorType={comment.actor_type}
                  actorId={comment.actor_id}
                  name={comment.actor_name}
                  avatarUrl={comment.actor_avatar_url}
                  size="xs"
                  profileLink={false}
                />
                <span className="truncate font-medium">
                  {comment.actor_name ?? getActorName(comment.actor_type, comment.actor_id)}
                </span>
                <span
                  className="shrink-0 text-muted-foreground"
                  title={new Date(comment.created_at).toLocaleString(locale)}
                >
                  · {timeAgo(comment.created_at)}
                </span>
              </div>
            ) : (
              <div className="flex items-center gap-1.5 text-caption font-medium">
                <FileText className="size-3.5 text-muted-foreground" />
                {t(($) => $.deliverables.source_description)}
              </div>
            )}
            {excerpt && (
              <p className="line-clamp-4 text-caption text-muted-foreground">{excerpt}</p>
            )}
            {onLocate && (
              <button
                type="button"
                className="inline-flex items-center gap-1.5 rounded-md border border-input px-2 py-1 text-caption text-foreground transition-colors hover:bg-secondary"
                onClick={onLocate}
              >
                <MessageSquareText className="size-3.5" />
                {locateLabel}
              </button>
            )}
          </div>
        </InfoSection>
      )}

      {siblings.length > 1 && (
        <InfoSection
          title={
            isDescription
              ? t(($) => $.deliverables.info_siblings_description, { count: siblings.length })
              : t(($) => $.deliverables.info_siblings_comment, { count: siblings.length })
          }
        >
          <div className="grid grid-cols-4 gap-1.5">
            {siblings.map((sibling) => {
              const current = sibling.key === item.key;
              return (
                <button
                  key={sibling.key}
                  type="button"
                  className={cn(
                    "aspect-square overflow-hidden rounded-md ring-1 transition-shadow",
                    current
                      ? "ring-2 ring-foreground"
                      : "ring-border hover:ring-foreground/40",
                  )}
                  title={sibling.filename}
                  aria-label={sibling.filename}
                  aria-current={current || undefined}
                  onClick={() => controls.goTo(sibling.key)}
                >
                  {sibling.attachment ? (
                    <DeliverableThumbnail
                      attachment={sibling.attachment}
                      showTypeLabel={false}
                      className="size-full"
                    />
                  ) : (
                    <span className="flex size-full items-center justify-center bg-muted text-muted-foreground">
                      <ImageIcon className="size-4" />
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        </InfoSection>
      )}

      {attachment && (
        <InfoSection title={t(($) => $.deliverables.info_file)}>
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-caption">
            <InfoRow label={t(($) => $.deliverables.info_type)}>
              {fileTypeLabel(attachment.filename) || attachment.content_type || "—"}
            </InfoRow>
            {attachment.size_bytes > 0 && (
              <InfoRow label={t(($) => $.deliverables.info_size)}>
                {formatBytes(attachment.size_bytes)}
              </InfoRow>
            )}
            <InfoRow label={t(($) => $.deliverables.info_uploader)}>
              <UploaderName attachment={attachment} />
            </InfoRow>
            <InfoRow label={t(($) => $.deliverables.info_uploaded_at)}>
              {new Date(attachment.created_at).toLocaleString(locale, {
                dateStyle: "medium",
                timeStyle: "short",
              })}
            </InfoRow>
            {version && version.file.versions.length > 1 && (
              <InfoRow label={t(($) => $.deliverables.info_version)}>
                {t(($) => $.deliverables.version_of, {
                  version: version.version,
                  total: version.file.versions.length,
                })}
              </InfoRow>
            )}
          </dl>
        </InfoSection>
      )}
    </div>
  );
}

function InfoSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h3 className="mb-2 text-micro font-medium text-muted-foreground">{title}</h3>
      {children}
    </section>
  );
}

function InfoRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 truncate">{children}</dd>
    </>
  );
}

function UploaderName({ attachment }: { attachment: Attachment }) {
  const { getActorName } = useActorName();
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5 align-middle">
      <ActorAvatar
        actorType={attachment.uploader_type}
        actorId={attachment.uploader_id}
        size="xs"
        profileLink={false}
      />
      <span className="truncate">
        {getActorName(attachment.uploader_type, attachment.uploader_id)}
      </span>
    </span>
  );
}
