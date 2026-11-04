"use client";

import { ArrowUpRight, X } from "@multica/ui/icons";
import { useConfigStore } from "@multica/core/config";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { CHANGELOG_URL } from "../layout/help-links";
import { HighlightActionButton } from "./highlight-action";
import { highlightCopy, type WhatsNewRelease } from "./releases";

function versionLabel(version: string) {
  return `v${version}`;
}

function releaseAnchor(version: string) {
  return `${CHANGELOG_URL}#release-${version.replace(/\./g, "-")}`;
}

function formatReleaseDate(date: string, language: string) {
  const parsed = new Date(`${date}T00:00:00Z`);
  if (Number.isNaN(parsed.getTime())) return date;
  return new Intl.DateTimeFormat(language, { month: "short", day: "numeric", timeZone: "UTC" }).format(parsed);
}

/** Every announced release, opened from the Help menu. */
export function WhatsNewPanel({
  releases,
  unreadVersions,
  onClose,
}: {
  /** Newest first. */
  releases: WhatsNewRelease[];
  /** Releases to mark as new: captured when the panel opened, before the dismissal is recorded. */
  unreadVersions: ReadonlySet<string>;
  onClose: () => void;
}) {
  const { t, i18n } = useT("layout");
  const serverVersion = useConfigStore((state) => state.serverVersion);

  return (
    <div className="flex flex-col" data-slot="whats-new-panel">
      <div className="flex h-12 shrink-0 items-center gap-1.5 border-b border-border pr-2 pl-4">
        <h2 className="flex-1 text-title-sm font-semibold">{t(($) => $.whats_new.panel.title)}</h2>
        <IconButton label={t(($) => $.whats_new.panel.close)} onClick={onClose}>
          <X />
        </IconButton>
      </div>

      <div className="max-h-[min(70vh,560px)] overflow-y-auto p-4">
        {releases.map((release, index) => {
          const unread = unreadVersions.has(release.version);
          return (
            <section
              key={release.version}
              aria-label={versionLabel(release.version)}
              className={cn(index > 0 && "mt-5 border-t border-border pt-5")}
            >
              <div className="mb-3 flex items-center gap-2 text-label">
                <span className={cn("font-semibold tabular-nums", !unread && "text-muted-foreground")}>
                  {versionLabel(release.version)}
                </span>
                <span className="text-muted-foreground">{formatReleaseDate(release.date, i18n.language)}</span>
                {unread && (
                  <span className="ml-auto inline-flex h-[18px] items-center rounded-xs bg-brand/10 px-1.5 text-micro font-medium text-brand">
                    {t(($) => $.whats_new.panel.new)}
                  </span>
                )}
              </div>
              <ul className="flex flex-col gap-3.5">
                {release.highlights.map((highlight) => {
                  const Icon = highlight.icon;
                  const copy = highlightCopy(highlight, i18n.language);
                  return (
                    <li key={highlight.id} className="flex gap-3">
                      <span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground">
                        <Icon className="size-4" />
                      </span>
                      <div className="min-w-0">
                        <h3 className={cn("text-body font-medium", !unread && "text-muted-foreground")}>
                          {copy.title}
                        </h3>
                        <p className="text-label text-muted-foreground">{copy.body}</p>
                        {highlight.action && (
                          <div className="mt-0.5">
                            <HighlightActionButton action={highlight.action} onDone={onClose} />
                          </div>
                        )}
                      </div>
                    </li>
                  );
                })}
              </ul>
              <a
                href={releaseAnchor(release.version)}
                target="_blank"
                rel="noopener noreferrer"
                className="mt-3.5 inline-flex items-center gap-0.5 text-caption font-medium text-muted-foreground hover:text-foreground"
              >
                {t(($) => $.whats_new.panel.release_notes, { version: release.version })}
                <ArrowUpRight className="size-3 text-faint-foreground" />
              </a>
            </section>
          );
        })}
      </div>

      <div className="flex h-11 shrink-0 items-center gap-3 border-t border-border px-4">
        {serverVersion && (
          <span className="min-w-0 truncate text-caption text-muted-foreground">
            {t(($) => $.help.server_version, { version: serverVersion })}
          </span>
        )}
        <a
          href={CHANGELOG_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="ml-auto inline-flex shrink-0 items-center gap-0.5 text-label font-medium hover:underline"
        >
          {t(($) => $.whats_new.panel.full_changelog)}
          <ArrowUpRight className="size-3.5 text-faint-foreground" />
        </a>
      </div>
    </div>
  );
}
