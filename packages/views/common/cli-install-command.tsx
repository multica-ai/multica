"use client";

import type { ReactNode } from "react";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";

/**
 * The Multica CLI install commands, one per platform family.
 *
 * Three surfaces render install instructions — the runtimes "add computer"
 * dialog, onboarding's CLI card, and the landing download page. They read the
 * commands from here instead of each hardcoding a string, which is how Windows
 * silently went missing from all of them while scripts/install.ps1 sat in the
 * repo. The scripts' own headers are the source of truth.
 */
export const CLI_INSTALL_COMMANDS = {
  macosLinux:
    "curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash",
  windows:
    "irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex",
} as const;

export type CliInstallPlatform = keyof typeof CLI_INSTALL_COMMANDS;

/** Tab order. macOS / Linux stays first so the default matches the old single command. */
const PLATFORMS = [
  "macosLinux",
  "windows",
] as const satisfies readonly CliInstallPlatform[];

export interface CliInstallCommandLabels {
  /** Accessible name for the platform switch. */
  group: string;
  macosLinux: string;
  windows: string;
}

/**
 * Platform switch for the install command, built on the shared Tabs primitive
 * so tab semantics and arrow-key navigation come for free.
 *
 * The switch is the only shared UI: `children` receives the selected command
 * and renders it, so each surface keeps its own command row — the product
 * surfaces their token-styled row with an icon copy button, the landing page
 * its marketing palette with a spelled-out one.
 */
export function CliInstallCommand({
  labels,
  classNames,
  children,
}: {
  labels: CliInstallCommandLabels;
  /** Palette overrides for surfaces that do not use the product tokens. */
  classNames?: { list?: string; trigger?: string };
  children: (command: string) => ReactNode;
}) {
  return (
    <Tabs defaultValue={PLATFORMS[0]}>
      <TabsList
        aria-label={labels.group}
        activateOnFocus
        className={classNames?.list}
      >
        {PLATFORMS.map((platform) => (
          <TabsTrigger
            key={platform}
            value={platform}
            className={classNames?.trigger}
          >
            {labels[platform]}
          </TabsTrigger>
        ))}
      </TabsList>
      {PLATFORMS.map((platform) => (
        <TabsContent key={platform} value={platform}>
          {children(CLI_INSTALL_COMMANDS[platform])}
        </TabsContent>
      ))}
    </Tabs>
  );
}
