"use client";

import type { ReactNode } from "react";
import { ArrowUpRight, Globe, Lock, Users } from "@multica/ui/icons";
import type { Agent, AgentRuntime, MemberWithUser } from "@multica/core/types";
import {
  type AgentPresenceDetail,
  effectiveAccessScope,
  qoderAgentId,
} from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  ObjectProperty,
  ObjectSection,
  ObjectSectionAction,
} from "../../layout/object-page";
import { AppLink } from "../../navigation";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { SkillIcon } from "../../skills/lib/skill-icon";
import { useLocale, useT } from "../../i18n";
import { AgentPresenceIndicator } from "./agent-presence-indicator";
import { CHIP_CLASS } from "./inspector/chip";
import { AgentPerformanceSummary, AgentWorkSections } from "./tabs/activity-tab";
import type { AgentSection } from "./agent-sections";

const ACCESS_ICONS = {
  workspace: Globe,
  "specific-people": Users,
  "owner-only": Lock,
} as const;

/**
 * What the agent is doing and how it is set up, on one page: its work beside
 * a read-only summary whose values link to the section that edits them.
 */
export function AgentOverview({
  agent,
  runtime,
  presence,
  owner,
  header,
  showSkills,
  scrollParent,
  sectionHref,
  onViewAllWork,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  presence: AgentPresenceDetail | null;
  owner: MemberWithUser | null;
  /** The identity block and notices above the columns. */
  header: ReactNode;
  showSkills: boolean;
  scrollParent: HTMLElement | null;
  sectionHref: (section: AgentSection) => string;
  onViewAllWork: () => void;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();

  return (
    <div className="@container w-full max-w-5xl space-y-8 px-[var(--space-lg,1.5rem)] py-[var(--space-xl,2rem)]">
      {header}
      <div className="space-y-8 @3xl:grid @3xl:grid-cols-[minmax(0,1fr)_16rem] @3xl:items-start @3xl:gap-8 @3xl:space-y-0">
        <div className="min-w-0 space-y-8">
          <AgentWorkSections
            key={`${wsId}:${agent.id}`}
            agent={agent}
            scrollParent={scrollParent}
            onViewAll={onViewAllWork}
          />
        </div>
        <div className="min-w-0 space-y-8">
          <AgentSummary
            agent={agent}
            runtime={runtime}
            presence={presence}
            owner={owner}
            sectionHref={sectionHref}
          />
          <ObjectSection title={t(($) => $.tab_body.activity.section_last_30d)}>
            <AgentPerformanceSummary agent={agent} />
          </ObjectSection>
          {showSkills ? (
            <ObjectSection
              title={t(($) => $.tabs.skills)}
              count={agent.skills.length}
              action={
                <ObjectSectionAction
                  icon={ArrowUpRight}
                  render={<AppLink href={sectionHref("skills")} />}
                  nativeButton={false}
                >
                  {t(($) => $.sections.manage)}
                </ObjectSectionAction>
              }
            >
              {agent.skills.length === 0 ? (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.tab_body.skills.empty_title)}
                </p>
              ) : (
                <ul className="flex flex-wrap gap-1.5">
                  {agent.skills.map((skill) => (
                    <li
                      key={skill.id}
                      className={cn(
                        "flex h-7 max-w-full items-center gap-1.5 rounded-md border bg-card px-2 text-caption",
                        skill.enabled === false && "text-muted-foreground",
                      )}
                    >
                      <SkillIcon className="size-3.5 shrink-0 text-muted-foreground" />
                      <span className="min-w-0 truncate">{skill.name}</span>
                    </li>
                  ))}
                </ul>
              )}
            </ObjectSection>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function AgentSummary({
  agent,
  runtime,
  presence,
  owner,
  sectionHref,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  presence: AgentPresenceDetail | null;
  owner: MemberWithUser | null;
  sectionHref: (section: AgentSection) => string;
}) {
  const { t } = useT("agents");
  const { t: tl } = useT("layout");
  const { t: tr } = useT("runtimes");
  const locale = useLocale();
  const qoderRuntime = runtime?.provider === "qoder_cloud";

  const accessScope = effectiveAccessScope(
    agent.permission_mode,
    agent.invocation_targets,
  );
  const AccessIcon = ACCESS_ICONS[accessScope];
  const accessLabel = t(($) =>
    accessScope === "workspace"
      ? $.access.scope_labels.workspace
      : accessScope === "specific-people"
        ? $.access.scope_labels.specific_people
        : $.access.scope_labels.owner_only,
  );

  return (
    <ObjectSection title={tl(($) => $.object_page.properties)}>
      <div className="space-y-0.5 px-2">
        <ObjectProperty label={t(($) => $.panel.status)}>
          <Value>
            <AgentPresenceIndicator detail={presence} hideCounts />
          </Value>
        </ObjectProperty>

        <ObjectProperty label={t(($) => $.inspector.prop_owner)}>
          <Value>
            {owner ? (
              <>
                <ActorAvatar actorType="member" actorId={owner.user_id} size="xs" />
                <span className="truncate">{owner.name}</span>
              </>
            ) : (
              <span className="text-muted-foreground">—</span>
            )}
          </Value>
        </ObjectProperty>

        <ObjectProperty label={t(($) => $.inspector.prop_runtime)}>
          <AppLink href={sectionHref("runtime")} className={CHIP_CLASS}>
            {runtime ? (
              <>
                <ProviderLogo provider={runtime.provider} className="size-3.5 shrink-0" />
                <span className="truncate">{runtimeDisplayLabel(runtime)}</span>
              </>
            ) : (
              <span className="truncate text-muted-foreground">
                {t(($) => $.pickers.runtime_none)}
              </span>
            )}
          </AppLink>
        </ObjectProperty>

        {/* A Qoder agent's model lives on its agent in Qoder Cloud Agents; the
            bound agent id is what identifies it here. */}
        {qoderRuntime ? (
          <ObjectProperty label={tr(($) => $.qoder.agent)}>
            <AppLink href={sectionHref("runtime_config")} className={CHIP_CLASS}>
              <span className="truncate font-mono">
                {qoderAgentId(agent.runtime_config) || "—"}
              </span>
            </AppLink>
          </ObjectProperty>
        ) : (
          <ObjectProperty label={t(($) => $.inspector.prop_model)}>
            <AppLink href={sectionHref("runtime")} className={CHIP_CLASS}>
              <span className={cn("truncate", !agent.model && "text-muted-foreground")}>
                {agent.model || t(($) => $.pickers.model_default)}
              </span>
            </AppLink>
          </ObjectProperty>
        )}

        <ObjectProperty label={t(($) => $.columns.access)}>
          <AppLink href={sectionHref("access")} className={CHIP_CLASS}>
            <AccessIcon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
            <span className="truncate">{accessLabel}</span>
          </AppLink>
        </ObjectProperty>

        <ObjectProperty label={t(($) => $.inspector.prop_created)}>
          <Value>
            <span className="truncate tabular-nums">
              {new Date(agent.created_at).toLocaleDateString(locale, {
                year: "numeric",
                month: "short",
                day: "numeric",
              })}
            </span>
          </Value>
        </ObjectProperty>
      </div>
    </ObjectSection>
  );
}

/** A read-only value, inset to line up with the links' chip text. */
function Value({ children }: { children: ReactNode }) {
  return (
    <span className="flex min-w-0 items-center gap-1.5 px-1.5">{children}</span>
  );
}
