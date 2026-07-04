"use client";

import { Building2, Cpu, GitBranch, Monitor, Shield, User } from "lucide-react";
import type { AgentAvailability, AgentPresenceDetail } from "@multica/core/agents";
import type { Agent, AgentRuntime } from "@multica/core/types";
import type { MemberWithUser } from "@multica/core/types";
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

function PropRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-3 text-[13px]">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 truncate font-medium text-foreground">
        {children}
      </span>
    </div>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-3 text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">
      {children}
    </div>
  );
}

/**
 * Read the account/host and CLI version off a runtime's loose metadata bag.
 * The daemon reports these under free-form keys, so we probe the common ones
 * and fall back to "—" rather than inventing a value.
 */
function runtimeVersion(runtime: AgentRuntime | null): string | null {
  const meta = runtime?.metadata;
  if (!meta || typeof meta !== "object") return null;
  const bag = meta as Record<string, unknown>;
  const v = bag.version ?? bag.cli_version;
  return typeof v === "string" && v.length > 0 ? v : null;
}

interface IdentityRailProps {
  agent: Agent;
  runtime: AgentRuntime | null;
  owner: MemberWithUser | undefined;
  presence: AgentPresenceDetail | null;
  /** Workspace/organization the runtime bills and reports under. */
  accountName: string | null;
}

export function IdentityRail({
  agent,
  runtime,
  owner,
  presence,
  accountName,
}: IdentityRailProps) {
  const tone = availabilityTone(presence?.availability);
  const initial = (agent.name?.[0] ?? "?").toUpperCase();
  const skills = agent.skills ?? [];
  const skillPreview = skills.slice(0, 12);

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

      {/* properties */}
      <div className="border-b px-5 py-4">
        <SectionLabel>Properties</SectionLabel>
        <div className="flex flex-col gap-3">
          <PropRow label="Runtime">
            <span className="inline-flex items-center gap-1.5">
              <Monitor className="h-3.5 w-3.5 shrink-0" />
              {runtime?.name ?? "—"}
            </span>
          </PropRow>
          <PropRow label="Account">
            <span className="inline-flex items-center gap-1.5">
              <Building2 className="h-3.5 w-3.5 shrink-0" />
              {accountName ?? "—"}
            </span>
          </PropRow>
          <PropRow label="Runtime version">
            <span className="inline-flex items-center gap-1.5">
              <GitBranch className="h-3.5 w-3.5 shrink-0" />
              <span className="font-mono text-xs">
                {runtimeVersion(runtime) ?? "—"}
              </span>
            </span>
          </PropRow>
          <PropRow label="Model">
            <span className="font-mono text-xs">{agent.model || "—"}</span>
          </PropRow>
          <PropRow label="Thinking">
            <span className="inline-flex items-center gap-1.5">
              <Cpu className="h-3.5 w-3.5 shrink-0" />
              {agent.thinking_level ? agent.thinking_level : "Follow CLI config"}
            </span>
          </PropRow>
          <PropRow label="Visibility">
            <span className="inline-flex items-center gap-1.5">
              <Shield className="h-3.5 w-3.5 shrink-0" />
              {agent.visibility === "private" ? "Personal" : "Workspace"}
            </span>
          </PropRow>
          <PropRow label="Concurrency">{agent.max_concurrent_tasks}</PropRow>
        </div>
      </div>

      {/* details */}
      <div className="border-b px-5 py-4">
        <SectionLabel>Details</SectionLabel>
        <div className="flex flex-col gap-3">
          <PropRow label="Owner">
            <span className="inline-flex items-center gap-1.5">
              <User className="h-3.5 w-3.5 shrink-0" />
              {owner?.name ?? "—"}
            </span>
          </PropRow>
          <PropRow label="Created">{timeAgo(agent.created_at)}</PropRow>
          <PropRow label="Updated">{timeAgo(agent.updated_at)}</PropRow>
        </div>
      </div>

      {/* skills */}
      <div className="px-5 py-4">
        <div className="mb-3 flex items-center gap-2">
          <span className="text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">
            Skills
          </span>
          <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10.5px] text-muted-foreground">
            {skills.length}
          </span>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {skillPreview.map((skill) => (
            <span
              key={skill.id}
              title={skill.description || undefined}
              className="rounded-md border bg-background px-2 py-0.5 font-mono text-[11px] text-foreground/70"
            >
              {skill.name}
            </span>
          ))}
          {skills.length > skillPreview.length ? (
            <span className="rounded-md border border-dashed px-2 py-0.5 font-mono text-[11px] text-muted-foreground">
              +{skills.length - skillPreview.length} more
            </span>
          ) : null}
        </div>
      </div>
    </div>
  );
}
