"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { pluginInstallationsOptions } from "@multica/core/plugins";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureEnabled } from "@multica/core/config";
import { PLUGINS_V1_FLAG } from "@multica/core/feature-flags";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { isDesktopShell } from "../platform/local-directory";
import { IssueSidebarSectionHeader } from "../issues/components/issue-sidebar-section";
import { PluginSurfaceFrame } from "./plugin-surface-frame";

/**
 * Renders every enabled `issue_panel` surface on an issue.
 *
 * Disabled installations are filtered here AND refused by the Action API — the
 * client filter is presentation, the server check is the control. A surface
 * left open in a stale tab stops working even though this component never
 * re-rendered.
 */
export function PluginPanelSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const platform = isDesktopShell() ? "desktop" : "web";
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const pluginsEnabled = useFeatureEnabled(PLUGINS_V1_FLAG, false);

  const { data } = useQuery({ ...pluginInstallationsOptions(wsId), enabled: pluginsEnabled && wsId.length > 0 });

  const panels = useMemo(() => {
    const installations = data?.plugins ?? [];
    return installations
      .filter((installation) => installation.enabled === true)
      .flatMap((installation) =>
        installation.surfaces
          .filter((surface) => surface.type === "issue_panel")
          // An empty list means "anywhere"; a declared list is a filter, not a
          // hint, or a desktop-only panel renders on web anyway.
          .filter((surface) => (surface.platforms ?? []).length === 0 || (surface.platforms ?? []).includes(platform))
          .map((surface) => ({ installation, surface })),
      );
  }, [data, platform]);

  if (!pluginsEnabled || panels.length === 0) return null;

  return (
    <section>
      <IssueSidebarSectionHeader title={t(($) => $.plugins.panels)} open={open} onOpenChange={setOpen} count={panels.length} />
      <div
        inert={!open}
        aria-hidden={!open}
        style={{ visibility: open ? "visible" : "hidden" }}
        className={cn(
          "grid transition-[grid-template-rows,opacity,visibility] duration-[180ms] ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:transition-none",
          open ? "grid-rows-[1fr] opacity-100" : "grid-rows-[0fr] opacity-0",
        )}
      >
        <div className="min-h-0 overflow-hidden">
          <div className="space-y-2 px-2">
            {panels.map(({ installation, surface }) => (
              <PluginSurfaceFrame
                key={`${installation.id}:${surface.key}`}
                wsId={wsId}
                installation={installation}
                surface={surface}
                issueId={issueId}
              />
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
