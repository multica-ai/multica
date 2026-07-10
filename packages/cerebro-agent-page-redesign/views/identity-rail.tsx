"use client";

import "@multica/cerebro-types";
import { GitBranch } from "lucide-react";
import type {
  AgentAvailability,
  AgentPresenceDetail,
} from "@multica/core/agents";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
} from "@multica/core/types";
// FIR-2670: reuse the shipped, editable inspector pickers + PropRow instead of
// re-implementing them — see CEREBRO-PATCH(views-agent-redesign-reuse).
import { PropRow } from "@multica/views/common/prop-row";
import { RuntimePicker } from "@multica/views/agents/components/inspector/runtime-picker";
import { ModelPicker } from "@multica/views/agents/components/inspector/model-picker";
import { ThinkingPropRow } from "@multica/views/agents/components/inspector/thinking-prop-row";
import { VisibilityPicker } from "@multica/views/agents/components/inspector/visibility-picker";
import { ConcurrencyPicker } from "@multica/views/agents/components/inspector/concurrency-picker";
import { SkillAttach } from "@multica/views/agents/components/inspector/skill-attach";
import { AgentSurfaceVisibilityPicker } from "@multica/cerebro-access/views";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { RuntimeAccountCell } from "@multica/cerebro-runtime/views";
import { daemonVersion, runtimeVersion } from "./runtime-meta";
import { timeAgo } from "./time-ago";

/** Presence dot + label colour, mapped to the app's semantic tokens. */
function availabilityTone(availability: AgentAvailability | undefined): {
  dot: string;
  text: string;
  label: string;
} {
  switch (availability) {
    case "online":
      return { dot: "bg-success", text: "text-success", label: "Online" };
    case "paused":
      return { dot: "bg-warning", text: "text-warning", label: "Paused" };
    case "unstable":
      return { dot: "bg-warning", text: "text-warning", label: "Unstable" };
    case "archived":
      return {
        dot: "bg-muted-foreground",
        text: "text-muted-foreground",
        label: "Archived",
      };
    default:
      return {
        dot: "bg-muted-foreground",
        text: "text-muted-foreground",
        label: "Offline",
      };
  }
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-2 text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">
      {children}
    </div>
  );
}

interface IdentityRailProps {
  agent: Agent;
  runtime: AgentRuntime | null;
  owner: MemberWithUser | undefined;
  presence: AgentPresenceDetail | null;
  // Below: needed for the editable Properties section (FIR-2670 #9). The rail
  // owns the same inline-edit surface the shipped inspector does, so the page
  // has to pass through everything a write needs.
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  currentUserId: string | null;
  /** When false every picker self-renders a read-only display. */
  canEdit: boolean;
  onUpdate: (data: Record<string, unknown>) => Promise<void> | void;
}

/**
 * Left identity rail of the redesigned agent page. Mirrors the design's left
 * column: identity block, an editable Properties section, read-only Details,
 * and Skills with an Attach affordance.
 *
 * The Properties pickers (runtime / model / thinking / visibility /
 * concurrency) and the Skills Attach control are the SAME shipped components
 * the current agent inspector uses (imported via the FIR-2670 reuse exports),
 * so editing behaves identically and can't drift from the original. Account,
 * Runtime version, and Daemon version are read-only details from the runtime.
 */
export function IdentityRail({
  agent,
  runtime,
  owner,
  presence,
  runtimes,
  members,
  currentUserId,
  canEdit,
  onUpdate,
}: IdentityRailProps) {
  const tone = availabilityTone(presence?.availability);
  const initial = (agent.name?.[0] ?? "?").toUpperCase();
  const isOnline = runtime?.status === "online";
  const skills = agent.skills ?? [];
  const surfaceVisibilityEnabled = useFeatureFlag("cerebro_agent_surface_visibility");

  return (
    <div className="w-full flex-shrink-0 border-b bg-muted/30 md:w-80 md:border-b-0 md:border-r">
      {/* identity */}
      <div className="border-b px-5 py-5">
        <div className="flex flex-col items-start gap-2.5">
          <div className="relative flex h-16 w-16 items-center justify-center overflow-hidden rounded-2xl bg-gradient-to-br from-info to-primary text-2xl font-semibold text-primary-foreground">
            {agent.avatar_url ? (
              <img
                src={agent.avatar_url}
                alt={agent.name}
                className="h-full w-full object-cover"
              />
            ) : (
              initial
            )}
            <span
              className={`absolute -bottom-0.5 -right-0.5 h-4 w-4 rounded-full border-[3px] border-background ${tone.dot}`}
            />
          </div>
        </div>
        <div className="mt-3.5 flex items-center gap-2.5">
          <h1 className="m-0 text-[22px] font-bold tracking-tight">
            {agent.name}
          </h1>
          <span className={`inline-flex items-center gap-1.5 text-xs ${tone.text}`}>
            <span className={`h-[7px] w-[7px] rounded-full ${tone.dot}`} />
            {tone.label}
          </span>
        </div>
        {agent.description ? (
          <p className="mt-2.5 text-[13.5px] leading-relaxed text-muted-foreground">
            {agent.description}
          </p>
        ) : null}
      </div>

      {/* properties — editable via the shipped inspector pickers */}
      <div className="border-b px-5 py-4">
        <SectionLabel>Properties</SectionLabel>
        <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5">
          <PropRow label="Runtime" interactive={false}>
            <RuntimePicker
              value={agent.runtime_id}
              runtimes={runtimes}
              members={members}
              currentUserId={currentUserId}
              canEdit={canEdit}
              onChange={(id) => onUpdate({ runtime_id: id })}
            />
          </PropRow>
          <PropRow label="Model" interactive={false}>
            <ModelPicker
              runtimeId={agent.runtime_id}
              runtimeOnline={!!isOnline}
              value={agent.model ?? ""}
              canEdit={canEdit}
              onChange={(m) => onUpdate({ model: m })}
            />
          </PropRow>
          <ThinkingPropRow
            runtimeId={agent.runtime_id}
            runtimeOnline={!!isOnline}
            model={agent.model ?? ""}
            value={agent.thinking_level ?? ""}
            canEdit={canEdit}
            onChange={(v) => onUpdate({ thinking_level: v })}
          />
          <PropRow label="Visibility" interactive={false}>
            <VisibilityPicker
              value={agent.visibility}
              canEdit={canEdit}
              onChange={(v) => onUpdate({ visibility: v })}
            />
          </PropRow>
          {surfaceVisibilityEnabled && (
            <PropRow label="Discovery" interactive={false}>
              <AgentSurfaceVisibilityPicker
                value={agent.surface_visibility}
                canEdit={canEdit}
                onChange={(map) => onUpdate({ surface_visibility: map })}
              />
            </PropRow>
          )}
          <PropRow label="Concurrency" interactive={false}>
            <ConcurrencyPicker
              value={agent.max_concurrent_tasks}
              canEdit={canEdit}
              onChange={(n) => onUpdate({ max_concurrent_tasks: n })}
            />
          </PropRow>
        </div>
      </div>

      {/* details — read-only */}
      <div className="border-b px-5 py-4">
        <SectionLabel>Details</SectionLabel>
        <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5">
          <PropRow label="Account" interactive={false}>
            {runtime ? <RuntimeAccountCell runtime={runtime} /> : <span>—</span>}
          </PropRow>
          <PropRow label="Runtime version" interactive={false}>
            <span className="inline-flex min-w-0 items-center gap-1.5">
              <GitBranch className="h-3.5 w-3.5 shrink-0" />
              <span className="truncate font-mono text-[11px]">
                {runtimeVersion(runtime) ?? "—"}
              </span>
            </span>
          </PropRow>
          <PropRow label="Daemon version" interactive={false}>
            <span className="inline-flex min-w-0 items-center gap-1.5">
              <GitBranch className="h-3.5 w-3.5 shrink-0" />
              <span className="truncate font-mono text-[11px]">
                {daemonVersion(runtime) ?? "—"}
              </span>
            </span>
          </PropRow>
          <PropRow label="Owner" interactive={false}>
            <span className="truncate">{owner?.name ?? "—"}</span>
          </PropRow>
          <PropRow label="Created" interactive={false}>
            <span className="text-muted-foreground">
              {timeAgo(agent.created_at)}
            </span>
          </PropRow>
          <PropRow label="Updated" interactive={false}>
            <span className="text-muted-foreground">
              {timeAgo(agent.updated_at)}
            </span>
          </PropRow>
        </div>
      </div>

      {/* skills — chips + the shipped Attach control */}
      <div className="px-5 py-4">
        <div className="mb-3 flex items-center gap-2">
          <span className="text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">
            Skills
          </span>
          <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10.5px] text-muted-foreground">
            {skills.length}
          </span>
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          {skills.map((skill) => (
            <span
              key={skill.id}
              title={skill.description || undefined}
              className="rounded-md border bg-background px-2 py-0.5 font-mono text-[11px] text-foreground/70"
            >
              {skill.name}
            </span>
          ))}
          <SkillAttach agent={agent} canEdit={canEdit} />
        </div>
      </div>
    </div>
  );
}
