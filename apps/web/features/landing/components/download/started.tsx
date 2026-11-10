"use client";

import type { ComponentType, ReactNode } from "react";
import { ArrowUpRight, Check, Star } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useLocale } from "../../i18n";
import { PROVIDER_MARKS } from "../../site/demo/mock-kit";
import { ActionLink, Container, Eyebrow, actionClassName, focusRing } from "../../site/primitives";
import { fileName, formatSize } from "../../utils/download-target";
import { formatStarCount, useGithubStars } from "../../utils/use-github-stars";
import type { DownloadKey } from "../../utils/parse-release-assets";
import { DiscordMark, GitHubMark, XMark, discordUrl, githubUrl, twitterUrl } from "../shared";

const AGENTS = ["Claude Code", "Codex", "OpenCode"] as const;

/** Opens the installed desktop app, or does nothing when it is not installed yet. */
const OPEN_APP_URL = "multica://open";

type InstallStep = "dmg" | "zip" | "exe" | "appImage" | "deb" | "rpm";

function installStep(key: DownloadKey): InstallStep {
  if (key.endsWith("Dmg")) return "dmg";
  if (key.endsWith("Zip")) return "zip";
  if (key.endsWith("Exe")) return "exe";
  if (key.endsWith("Deb")) return "deb";
  if (key.endsWith("Rpm")) return "rpm";
  return "appImage";
}

/**
 * What the download page becomes once a file is on its way: confirmation
 * with a retry, the three steps to a working setup, and the places to
 * follow the project while the file downloads.
 */
export function DownloadStarted({
  downloadKey,
  href,
  size,
  onBack,
}: {
  downloadKey: DownloadKey;
  href: string;
  size?: number;
  onBack: () => void;
}) {
  const { t } = useLocale();
  const d = t.download.started;
  const stars = useGithubStars();

  const steps: { title: string; body: ReactNode; extra?: ReactNode }[] = [
    { title: d.install.title, body: d.install[installStep(downloadKey)] },
    {
      title: d.signIn.title,
      body: `${d.signIn.body} ${d.signIn.installed}`,
      extra: (
        <a href={OPEN_APP_URL} className={cn(actionClassName("secondary"), "mt-3")}>
          {d.signIn.open}
          <ArrowUpRight className="size-3.5" aria-hidden />
        </a>
      ),
    },
    {
      title: d.agents.title,
      body: d.agents.body,
      extra: (
        <ul className="mt-3 flex flex-wrap gap-1.5">
          {AGENTS.map((name) => {
            const Mark = PROVIDER_MARKS.get(name);
            return (
              <li
                key={name}
                className="inline-flex h-7 items-center gap-1.5 rounded-site-inner bg-site-sunken px-2.5 text-[0.8125rem] text-site-ink-secondary"
              >
                {Mark ? <Mark className="size-3.5" /> : null}
                {name}
              </li>
            );
          })}
        </ul>
      ),
    },
  ];

  return (
    <>
      <Container className="pt-site-hero pb-site-block">
        <div className="flex items-start gap-4">
          <span className="mt-1 flex size-10 shrink-0 items-center justify-center rounded-full bg-site-inverse text-site-inverse-ink">
            <Check className="size-5" aria-hidden />
          </span>
          <div className="min-w-0">
            <h1 className="font-site-display text-site-heading text-site-ink text-balance">{d.title}</h1>
            <p className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-site-small text-site-muted">
              <span className="break-all">
                {fileName(href)}
                {size ? ` · ${formatSize(size)}` : null}
              </span>
              <span aria-hidden className="text-site-faint">
                ·
              </span>
              <a href={href} className={cn("rounded-sm text-site-ink underline decoration-site-line-strong underline-offset-4", focusRing)}>
                {d.retry}
              </a>
              <span aria-hidden className="text-site-faint">
                ·
              </span>
              <button
                type="button"
                onClick={onBack}
                className={cn("rounded-sm text-site-ink underline decoration-site-line-strong underline-offset-4", focusRing)}
              >
                {d.allDownloads}
              </button>
            </p>
          </div>
        </div>

        <ol className="mt-10 grid border-t border-site-line md:grid-cols-3">
          {steps.map((step, index) => (
            <li
              key={step.title}
              className={cn(
                "relative border-b border-site-rule py-5 md:border-b-0 md:pr-8",
                index === 0 && "before:absolute before:-top-px before:left-0 before:h-px before:w-2/5 before:bg-site-ink",
              )}
            >
              <span className="font-site-mono text-site-label text-site-muted">0{index + 1}</span>
              <h2 className="mt-2 text-site-card text-site-ink">{step.title}</h2>
              <p className="mt-1.5 max-w-[22rem] text-site-small text-site-muted text-pretty">{step.body}</p>
              {step.extra}
            </li>
          ))}
        </ol>
      </Container>

      <Container className="pb-site-section">
        <Eyebrow>{d.community.label}</Eyebrow>
        <div className="mt-4 grid gap-4 lg:grid-cols-[7fr_5fr]">
          <div className="relative flex flex-col justify-between gap-8 overflow-hidden rounded-site-panel bg-site-sunken p-7 sm:flex-row sm:items-end">
            <GitHubMark className="pointer-events-none absolute -top-10 -right-8 size-56 text-site-line" />
            <div className="relative">
              <p className="flex items-center gap-2 font-site-mono text-[0.8125rem] text-site-ink-secondary">
                <GitHubMark className="size-4 text-site-ink" />
                multica-ai/multica
              </p>
              <h2 className="mt-4 font-site-display text-site-title text-site-ink text-balance">{d.community.githubTitle}</h2>
              <p className="mt-2 max-w-[28rem] text-site-small text-site-muted text-pretty">{d.community.githubBody}</p>
              {stars != null ? (
                <p className="mt-4 flex items-center gap-1.5 text-site-small text-site-muted">
                  <Star className="size-3.5" aria-hidden />
                  {d.community.stars.replace("{count}", formatStarCount(stars))}
                </p>
              ) : null}
            </div>
            <ActionLink href={githubUrl} size="lg" className="relative self-start sm:self-auto">
              <Star className="size-4" aria-hidden />
              {d.community.star}
            </ActionLink>
          </div>
          <div className="grid gap-4">
            <CommunityRow
              icon={DiscordMark}
              title={d.community.discordTitle}
              body={d.community.discordBody}
              href={discordUrl}
              action={d.community.join}
            />
            <CommunityRow icon={XMark} title={d.community.xTitle} body={d.community.xBody} href={twitterUrl} action={d.community.follow} />
          </div>
        </div>
      </Container>
    </>
  );
}

function CommunityRow({
  icon: Icon,
  title,
  body,
  href,
  action,
}: {
  icon: ComponentType<{ className?: string }>;
  title: string;
  body: string;
  href: string;
  action: string;
}) {
  return (
    <div className="grid grid-cols-[2.25rem_1fr_auto] items-center gap-4 rounded-site-panel border border-site-line bg-site-surface p-5">
      <span className="flex size-9 items-center justify-center rounded-site-card bg-site-sunken text-site-ink">
        <Icon className="size-[18px]" />
      </span>
      <div className="min-w-0">
        <h2 className="text-site-body text-site-ink">{title}</h2>
        <p className="mt-0.5 text-site-small text-site-muted text-pretty">{body}</p>
      </div>
      <ActionLink href={href} tone="secondary">
        {action}
      </ActionLink>
    </div>
  );
}
