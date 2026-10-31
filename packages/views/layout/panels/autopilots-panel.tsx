"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlarmClock, Code, Clock, Plus, Webhook, Zap } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { useAutopilotsViewStore } from "@multica/core/autopilots/stores";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import type { Autopilot } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { AUTOPILOT_TEMPLATES } from "../../autopilots/components/autopilot-templates";
import {
  isWakeupsTab,
  wakeupCountsOptions,
  wakeupsHref,
} from "../../autopilots/components/wakeups-tab";
import {
  PanelBody,
  PanelCount,
  PanelHeader,
  PanelLink,
  PanelList,
  PanelSection,
  isPathWithin,
} from "../panel-nav";

const EMPTY_AUTOPILOTS: Autopilot[] = [];
const PANEL_TEMPLATE_LIMIT = 3;

// Server-driven enum: unknown kinds get the generic icon.
const TRIGGER_ICONS: Record<string, typeof Zap> = {
  schedule: Clock,
  webhook: Webhook,
  api: Code,
};

/** Last run failed on an autopilot that is still meant to run. */
export function isAutopilotFailing(autopilot: Autopilot): boolean {
  return autopilot.status === "active" && autopilot.last_run_status === "failed";
}

/** Workspace autopilots that need attention, for the rail badge and the panel. */
export function useFailingAutopilots(wsId: string | undefined) {
  const { data: autopilots = EMPTY_AUTOPILOTS } = useQuery({
    ...autopilotListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  return useMemo(() => autopilots.filter(isAutopilotFailing), [autopilots]);
}

function hasDescription(autopilot: Autopilot): boolean {
  return (
    isAutopilotFailing(autopilot) ||
    autopilot.status === "paused" ||
    (autopilot.trigger_kinds?.length ?? 0) > 0
  );
}

function AutopilotDescription({ autopilot }: { autopilot: Autopilot }) {
  const { t } = useT("autopilots");
  if (isAutopilotFailing(autopilot)) {
    return <span className="text-destructive">{t(($) => $.run_status.failed)}</span>;
  }
  if (autopilot.status === "paused") return <>{t(($) => $.status.paused)}</>;
  return (
    <>
      {(autopilot.trigger_kinds ?? [])
        .map((kind) =>
          kind === "schedule" || kind === "webhook" || kind === "api"
            ? t(($) => $.trigger_kind[kind])
            : kind,
        )
        .join(" · ")}
    </>
  );
}

export function AutopilotsPanel() {
  const { t } = useT("layout");
  const { t: tAutopilots } = useT("autopilots");
  const p = useWorkspacePaths();
  const { pathname, searchParams, push } = useNavigation();
  const wsId = useCurrentWorkspace()?.id;
  const setScope = useAutopilotsViewStore((s) => s.setScope);
  const { data: autopilots = EMPTY_AUTOPILOTS } = useQuery({
    ...autopilotListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const { data: wakeups } = useQuery({
    ...wakeupCountsOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const visible = useMemo(
    () => autopilots.filter((autopilot) => autopilot.status !== "archived"),
    [autopilots],
  );
  const onList = pathname === p.autopilots();
  const onWakeups = onList && isWakeupsTab(searchParams);

  return (
    <>
      <PanelHeader title={t(($) => $.nav.autopilots)}>
        <IconButton
          label={tAutopilots(($) => $.page.new_autopilot)}
          tooltipSide="bottom"
          className="text-muted-foreground"
          onClick={() => push(`${p.autopilots()}?create=1`)}
        >
          <Plus />
        </IconButton>
      </PanelHeader>
      <PanelBody>
        <PanelList>
          <PanelLink
            href={p.autopilots()}
            icon={<Zap />}
            label={t(($) => $.panels.autopilots.all)}
            active={onList && !onWakeups}
            trailing={<PanelCount value={visible.length} />}
            onNavigate={() => setScope("all")}
          />
          <PanelLink
            href={wakeupsHref(p.autopilots())}
            icon={<AlarmClock />}
            label={tAutopilots(($) => $.wakeups.title)}
            active={onWakeups}
            trailing={<PanelCount value={wakeups?.counts.active ?? 0} />}
          />
        </PanelList>

        {visible.length > 0 && (
          <PanelSection label={t(($) => $.panels.autopilots.list)}>
            <PanelList>
              {visible.map((autopilot) => {
                const href = p.autopilotDetail(autopilot.id);
                const Icon = TRIGGER_ICONS[autopilot.trigger_kinds?.[0] ?? ""] ?? Zap;
                return (
                  <PanelLink
                    key={autopilot.id}
                    href={href}
                    icon={<Icon />}
                    label={autopilot.title}
                    description={
                      hasDescription(autopilot) ? <AutopilotDescription autopilot={autopilot} /> : undefined
                    }
                    active={isPathWithin(pathname, href)}
                  />
                );
              })}
            </PanelList>
          </PanelSection>
        )}

        <PanelSection label={t(($) => $.panels.autopilots.templates)}>
          <PanelList>
            {AUTOPILOT_TEMPLATES.slice(0, PANEL_TEMPLATE_LIMIT).map((template) => {
              const Icon = template.icon;
              return (
                <PanelLink
                  key={template.id}
                  href={`${p.autopilots()}?template=${template.id}`}
                  icon={<Icon />}
                  label={tAutopilots(($) => $.templates[template.id].title)}
                  active={false}
                  className="text-muted-foreground"
                />
              );
            })}
          </PanelList>
        </PanelSection>
      </PanelBody>
    </>
  );
}
