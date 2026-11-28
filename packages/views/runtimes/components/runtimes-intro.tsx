"use client";

import type { ReactNode } from "react";
import { Check, Download, Laptop, Server, SquareTerminal } from "@multica/ui/icons";
import { buttonVariants } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { providerDisplayName } from "@multica/core/runtimes";
import {
  FeatureIntro,
  FeatureIntroCarousel,
  IntroArtCard,
  IntroArtTag,
  type FeatureIntroSlide,
} from "../../layout/feature-intro";
import { useT } from "../../i18n";
import { ProviderLogo } from "./provider-logo";

const MACHINE = "studio-mbp";
const REMOTE_MACHINE = "ci-runner-02";
const DETECTED = ["claude", "codex", "cursor", "copilot"];
const SETUP_COMMANDS = [
  "curl -fsSL …/multica/main/scripts/n | bash",
  "multica setup",
];
export function RuntimesIntro({
  action,
}: {
  /** The page's add-runtime menu, so every way to add one stays reachable. */
  action: ReactNode;
}) {
  const { t } = useT("runtimes");

  const slides: FeatureIntroSlide[] = [
    {
      id: "local",
      title: t(($) => $.intro.slides.local.title),
      description: t(($) => $.intro.slides.local.description),
      art: <LocalArt />,
    },
    {
      id: "remote",
      title: t(($) => $.intro.slides.remote.title),
      description: t(($) => $.intro.slides.remote.description),
      art: <RemoteArt />,
    },
  ];

  return (
    <FeatureIntro
      icon={Server}
      label={t(($) => $.page.title)}
      title={t(($) => $.intro.title)}
      description={t(($) => $.intro.description)}
      docs="daemon-runtimes"
      columns={[
        t(($) => $.page.columns.name),
        t(($) => $.page.columns.harness),
        t(($) => $.page.columns.agents),
        t(($) => $.page.columns.workload),
        t(($) => $.page.columns.status),
      ]}
      action={action}
      secondaryAction={
        <a
          href="/download"
          target="_blank"
          rel="noreferrer"
          className={buttonVariants({ variant: "outline" })}
        >
          <Download aria-hidden="true" />
          {t(($) => $.page.rows.download_desktop)}
        </a>
      }
    >
      <FeatureIntroCarousel label={t(($) => $.intro.title)} slides={slides} />
    </FeatureIntro>
  );
}

function StatusDot({ online }: { online: boolean }) {
  return (
    <span
      className={cn("size-1.5 shrink-0 rounded-full", online ? "bg-success" : "bg-warning")}
    />
  );
}

function MachineIcon({ children }: { children: ReactNode }) {
  return (
    <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted">
      {children}
    </span>
  );
}

function LocalArt() {
  const { t } = useT("runtimes");
  return (
    <IntroArtCard className="p-3">
      <div className="flex items-center gap-2">
        <MachineIcon>
          <Laptop className="size-4" />
        </MachineIcon>
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate font-medium">{MACHINE}</span>
          <span className="truncate text-micro text-muted-foreground">
            {t(($) => $.intro.art.this_computer)}
          </span>
        </span>
        <span className="flex shrink-0 items-center gap-1">
          <StatusDot online />
          {t(($) => $.health.online.label)}
        </span>
      </div>
      <div className="mt-3 mb-1.5 text-muted-foreground">{t(($) => $.intro.art.detected)}</div>
      <div className="flex flex-wrap gap-1.5">
        {DETECTED.map((provider) => (
          <IntroArtTag key={provider}>
            <ProviderLogo provider={provider} className="size-3" />
            {providerDisplayName(provider)}
          </IntroArtTag>
        ))}
      </div>
    </IntroArtCard>
  );
}

function RemoteArt() {
  const { t } = useT("runtimes");
  return (
    <IntroArtCard className="overflow-hidden p-0">
      <div className="flex items-center gap-1.5 border-b px-2.5 py-1.5 font-medium text-muted-foreground">
        <SquareTerminal className="size-3" />
        {t(($) => $.intro.art.run_there)}
      </div>
      <div className="bg-muted/60 px-2.5 py-2 font-mono text-micro leading-5">
        {SETUP_COMMANDS.map((command, i) => (
          <div key={command} className="flex gap-2">
            <span className="text-faint-foreground">{i + 1}</span>
            <span className="truncate">{command}</span>
          </div>
        ))}
        <div className="mt-1.5 flex items-center gap-1 text-success">
          <Check className="size-3" />
          {t(($) => $.intro.art.connected, { name: REMOTE_MACHINE })}
        </div>
      </div>
    </IntroArtCard>
  );
}
