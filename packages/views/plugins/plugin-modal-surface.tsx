"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plug } from "lucide-react";
import { useFeatureEnabled } from "@multica/core/config";
import { PLUGINS_V1_FLAG } from "@multica/core/feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import { pluginInstallationsOptions } from "@multica/core/plugins";
import type { PluginInstallation, PluginSurface } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { PluginSurfaceFrame } from "./plugin-surface-frame";

/**
 * The mount point for `modal` surfaces.
 *
 * A modal is the same sandboxed iframe as a panel — `allow-scripts` without
 * `allow-same-origin`, host-generated srcdoc, CSP derived from the granted
 * `net:` scopes — differing only in where it appears. What it is NOT is a way
 * for a plugin to interrupt somebody: it opens because a person picked it from
 * the issue menu, never on the plugin's own initiative.
 */

export interface PluginModalTarget {
  installation: PluginInstallation;
  surface: PluginSurface;
}

export function pluginModalKey(target: PluginModalTarget): string {
  return `${target.installation.id}:${target.surface.key}`;
}

/**
 * Collects modal surfaces from enabled installations.
 *
 * Exported for its own test: the filter is the product rule and belongs in one
 * place rather than re-derived by each menu that offers these.
 */
export function collectModalSurfaces(installations: readonly PluginInstallation[]): PluginModalTarget[] {
  const targets: PluginModalTarget[] = [];
  for (const installation of installations) {
    // Explicit === true: a backend that stops sending the field must read as
    // off, not as truthy enough to open a third party's UI.
    if (installation.enabled !== true) continue;
    for (const surface of installation.surfaces ?? []) {
      if (surface.type === "modal") targets.push({ installation, surface });
    }
  }
  return targets;
}

export function usePluginModalSurfaces(): PluginModalTarget[] {
  const pluginsEnabled = useFeatureEnabled(PLUGINS_V1_FLAG, false);
  const wsId = useCurrentWorkspace()?.id ?? "";
  const { data } = useQuery({
    ...pluginInstallationsOptions(wsId),
    enabled: pluginsEnabled && wsId.length > 0,
  });

  return useMemo(
    () => (pluginsEnabled ? collectModalSurfaces(data?.plugins ?? []) : []),
    [data, pluginsEnabled],
  );
}

interface PluginModalSurfaceProps {
  target: PluginModalTarget | null;
  issueId?: string;
  onOpenChange: (open: boolean) => void;
}

export function PluginModalSurface({ target, issueId, onOpenChange }: PluginModalSurfaceProps) {
  // Same source as usePluginModalSurfaces above: a modal only ever opens from
  // the issue menu, which is inside the workspace route.
  const workspace = useCurrentWorkspace();
  if (!target) return null;
  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Plug className="size-4 text-muted-foreground" />
            {target.surface.name}
          </DialogTitle>
          {/* Names the plugin, always. Someone looking at a dialog full of
              third-party UI is entitled to know whose it is without guessing
              from the styling. */}
          <DialogDescription>{target.installation.name}</DialogDescription>
        </DialogHeader>
        <PluginSurfaceFrame
          // Keyed on the issue as well as the surface: the frame carries the
          // issue into its context call, and an unchanged document would not
          // reload for a different one.
          key={`${pluginModalKey(target)}:${issueId ?? ""}`}
          wsId={workspace?.id ?? ""}
          installation={target.installation}
          surface={target.surface}
          issueId={issueId}
        />
      </DialogContent>
    </Dialog>
  );
}

/** Menu entries that open a modal, in the same shape the hook entries use. */
export function PluginModalMenuItems({
  issueId,
  Item,
}: {
  issueId: string;
  Item: React.ComponentType<{ onClick?: () => void; children?: React.ReactNode }>;
}) {
  const targets = usePluginModalSurfaces();
  const [open, setOpen] = useState<PluginModalTarget | null>(null);

  if (targets.length === 0) return null;

  return (
    <>
      {targets.map((target) => (
        <Item key={pluginModalKey(target)} onClick={() => setOpen(target)}>
          <Plug className="h-3.5 w-3.5" />
          <span className="truncate">{target.surface.name}</span>
          <span className="ml-auto truncate pl-3 text-caption text-muted-foreground">
            {target.installation.name}
          </span>
        </Item>
      ))}
      <PluginModalSurface target={open} issueId={issueId} onOpenChange={(next) => !next && setOpen(null)} />
    </>
  );
}
