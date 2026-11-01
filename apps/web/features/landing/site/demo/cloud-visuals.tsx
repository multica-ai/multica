"use client";

import type { ReactNode } from "react";
import { Check, ChevronDown, CircleCheck, GitBranch, GitPullRequest, Laptop, Lock, Monitor, Plus, Server, X, type IconComponent } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { PriorityIcon } from "@multica/views/issues/components/priority-icon";
import { ProviderLogo } from "@multica/views/runtimes/components/provider-logo";
import { Chip, GroupBand } from "./feature-visuals";
import { AppWindow, Avatar, BACKEND, FLOATING, LEO, PROVIDER_MARKS, RAISED, Working, ZoomCanvas } from "./mock-kit";

/*
 * Mockups for the Multica Cloud page: agents that run on cloud agent services
 * connected as runtimes. The hero is the Runtimes page open at multica.ai,
 * with an issue a cloud agent is working floating over it. Strings follow the
 * product's own copy; data is fictional.
 */

const QODER = "Qoder Cloud Agents";

// --- Hero ------------------------------------------------------------------------

function BrowserWindow({ path, className, children }: { path: string; className: string; children: ReactNode }) {
  return (
    <div className={cn("absolute flex flex-col overflow-hidden rounded-xl border border-site-line-strong bg-app-shell shadow-site-float", className)}>
      <div className="flex h-9 shrink-0 items-center gap-1.5 px-3">
        {[0, 1, 2].map((dot) => (
          <span key={dot} className="size-2.5 rounded-full bg-muted-foreground/20" />
        ))}
        <span className="mx-auto flex h-6 w-[42%] min-w-[180px] items-center justify-center gap-1.5 rounded-md bg-background/80 text-caption text-muted-foreground ring-1 ring-border">
          <Lock className="size-3" />
          {path}
        </span>
        <span className="w-[42px]" />
      </div>
      <div className="relative min-h-0 flex-1">{children}</div>
    </div>
  );
}

function Marks({ names }: { names: string[] }) {
  return (
    <span className="flex shrink-0 -space-x-1">
      {names.map((name) => {
        const Mark = PROVIDER_MARKS.get(name);
        return (
          <span key={name} className="inline-flex size-5 items-center justify-center rounded-full bg-background ring-1 ring-border">
            {Mark ? <Mark className="size-3" /> : null}
          </span>
        );
      })}
    </span>
  );
}

function Tile({ online, muted, children }: { online?: boolean; muted?: boolean; children: ReactNode }) {
  return (
    <span className={cn(RAISED, "relative flex size-7 shrink-0 items-center justify-center", muted && "opacity-60")}>
      {children}
      {online ? <span className="absolute -right-0.5 -bottom-0.5 size-2 rounded-full bg-success ring-2 ring-background" /> : null}
    </span>
  );
}

type Machine = { icon: IconComponent; name: string; detail: string; agents: string[]; load: string };

const MACHINES: Machine[] = [
  { icon: Laptop, name: "Mia’s MacBook Pro", detail: "macOS · Mia Chen", agents: ["Claude Code", "Cursor"], load: "1 running" },
  { icon: Server, name: "build-01", detail: "Linux · Leo Park", agents: ["Claude Code", "Codex"], load: "Idle" },
];

const COMING: { provider: string; name: string; vendor: string }[] = [
  { provider: "cursor", name: "Cursor Cloud Agents", vendor: "Cursor" },
  { provider: "claude", name: "Claude Managed Agents", vendor: "Anthropic" },
];

function RuntimeRow({ tile, name, detail, trailing, compact }: { tile: ReactNode; name: string; detail: string; trailing: ReactNode; compact?: boolean }) {
  return (
    <div className={cn("flex shrink-0 items-center gap-3 border-b border-border px-4", compact ? "h-12" : "h-[52px]")}>
      {tile}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-label font-medium">{name}</span>
        <span className="block truncate text-caption text-muted-foreground">{detail}</span>
      </span>
      {trailing}
    </div>
  );
}

function RuntimesPage({ compact }: { compact?: boolean }) {
  return (
    <>
      <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-4">
        <Monitor className="size-4 text-muted-foreground" />
        <span className="flex-1 text-label font-medium">Runtimes</span>
        <span className="flex h-7 items-center gap-1 rounded-md bg-foreground px-2.5 text-caption font-medium text-background">
          <Plus className="size-3.5" />
          Add
        </span>
      </div>
      <GroupBand title="Cloud services" count={3} />
      <RuntimeRow
        compact={compact}
        tile={
          <Tile online>
            <ProviderLogo provider="qoder_cloud" className="size-4" />
          </Tile>
        }
        name={QODER}
        detail="By Qoder"
        trailing={
          <span className="flex shrink-0 items-center gap-3">
            {compact ? null : <span className="text-caption text-muted-foreground tabular-nums">2 of 3 running · 1 queued</span>}
            <span className="flex items-center gap-1.5 text-caption">
              <span className="size-1.5 rounded-full bg-success" />
              Online
            </span>
          </span>
        }
      />
      {COMING.map((service) => (
        <RuntimeRow
          key={service.provider}
          compact={compact}
          tile={
            <Tile muted>
              <ProviderLogo provider={service.provider} className="size-4" />
            </Tile>
          }
          name={service.name}
          detail={`By ${service.vendor}`}
          trailing={
            <span className="shrink-0 rounded-md bg-muted px-1.5 py-0.5 text-micro font-medium text-muted-foreground">Coming soon</span>
          }
        />
      ))}
      <GroupBand title="Machines" count={MACHINES.length} />
      {MACHINES.map((machine) => (
        <RuntimeRow
          key={machine.name}
          compact={compact}
          tile={
            <Tile online>
              <machine.icon className="size-3.5 text-muted-foreground" />
            </Tile>
          }
          name={machine.name}
          detail={machine.detail}
          trailing={
            <span className="flex shrink-0 items-center gap-3">
              {compact ? null : <Marks names={machine.agents} />}
              <span
                className={cn(
                  "w-[60px] text-right text-caption whitespace-nowrap tabular-nums",
                  machine.load === "Idle" && "text-muted-foreground",
                )}
              >
                {machine.load}
              </span>
            </span>
          }
        />
      ))}
    </>
  );
}

function Step({ icon: Icon, children, last }: { icon: IconComponent; children: ReactNode; last?: boolean }) {
  return (
    <div className="relative flex gap-2.5 pb-3 last:pb-0">
      {last ? null : <span aria-hidden className="absolute top-5 bottom-0 left-[7px] w-px bg-border" />}
      <Icon className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1 text-caption leading-snug">{children}</span>
    </div>
  );
}

function RunCard({ className, compact }: { className: string; compact?: boolean }) {
  return (
    <div className={cn(FLOATING, "absolute overflow-hidden", className)}>
      <div className="px-4 pt-3.5 pb-3">
        <p className="flex items-center gap-2 text-caption text-muted-foreground tabular-nums">
          <PriorityIcon priority="high" />
          ACM-152
          <span className="ml-auto">
            <Working />
          </span>
        </p>
        <p className="mt-1.5 text-label font-semibold">Redesign the settings page</p>
        <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
          <Chip actor={BACKEND} />
          <span className="flex items-center gap-1.5 rounded-full border border-border py-0.5 pr-2 pl-1 text-caption">
            <ProviderLogo provider="qoder_cloud" className="size-3.5" />
            {QODER}
          </span>
        </div>
      </div>
      {compact ? null : (
        <div className="border-t border-border bg-muted/30 px-4 py-3">
          <Step icon={CircleCheck}>Started a Qoder session · acme/web</Step>
          <Step icon={GitBranch}>
            Pushed <span className="font-mono text-[11px]">acm-152-settings</span>
          </Step>
          <Step icon={GitPullRequest} last>
            <span className="block font-medium">ACM-152: Redesign the settings page</span>
            <span className="block text-muted-foreground">Pull request #482 · linked to the issue</span>
          </Step>
        </div>
      )}
    </div>
  );
}

function Toast({ className }: { className: string }) {
  return (
    <div className={cn(FLOATING, "absolute flex items-center gap-2.5 px-3.5 py-2.5", className)}>
      <CircleCheck className="size-4 shrink-0 text-success" />
      <span className="text-label whitespace-nowrap">Qoder connected. It will be ready in a few seconds.</span>
    </div>
  );
}

const SCENE = { width: 1100, height: 600 } as const;
const SCENE_NARROW = { width: 440, height: 440 } as const;

/** Hero: cloud services beside the team's machines on the Runtimes page, and an issue a cloud agent is working. */
export function CloudScene() {
  return (
    <>
      <div className="relative aspect-[440/440] sm:hidden">
        <ZoomCanvas {...SCENE_NARROW}>
          <BrowserWindow path="multica.ai/acme/runtimes" className="top-6 left-6 -right-10 -bottom-10">
            <AppWindow className="inset-0 rounded-none border-0 shadow-none" active="runtimes" bleed={40}>
              <RuntimesPage compact />
            </AppWindow>
          </BrowserWindow>
          <RunCard compact className="right-4 bottom-5 w-[264px]" />
        </ZoomCanvas>
      </div>
      <div className="relative hidden aspect-[1100/600] sm:block">
        <ZoomCanvas {...SCENE}>
          <BrowserWindow path="multica.ai/acme/runtimes" className="top-10 right-[19%] -bottom-12 left-[7%]">
            <AppWindow className="inset-0 rounded-none border-0 shadow-none" active="runtimes" bleed={48}>
              <RuntimesPage />
            </AppWindow>
          </BrowserWindow>
          <RunCard className="top-[17%] right-[3.5%] w-[332px]" />
          <Toast className="bottom-[8%] left-[3%]" />
        </ZoomCanvas>
      </div>
    </>
  );
}

// --- Getting started ----------------------------------------------------------

function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: ReactNode }) {
  return (
    <div className="mt-3">
      <p className="text-caption font-medium">{label}</p>
      <div className="mt-1.5">{children}</div>
      {hint ? <p className="mt-1 text-micro text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function Input({ children, focused }: { children: ReactNode; focused?: boolean }) {
  return (
    <div
      className={cn(
        "flex h-8 items-center gap-2 rounded-md border border-border bg-background px-2.5 text-label",
        focused && "ring-2 ring-brand/15",
      )}
    >
      {children}
    </div>
  );
}

function DialogHeader({ title, subtitle }: { title: ReactNode; subtitle: string }) {
  return (
    <div className="flex items-start gap-2">
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2 text-label font-semibold">{title}</span>
        <span className="mt-0.5 block text-caption text-muted-foreground">{subtitle}</span>
      </span>
      <X className="size-3.5 text-muted-foreground" />
    </div>
  );
}

export function ConnectVisual() {
  return (
    <div className={cn(FLOATING, "w-[288px] p-4")}>
      <DialogHeader
        title={
          <>
            <ProviderLogo provider="qoder_cloud" className="size-4" />
            {QODER}
          </>
        }
        subtitle="Run agents in Qoder Cloud using a shared connection."
      />
      <Field label="Region">
        <div className="flex rounded-md bg-muted p-0.5 text-caption">
          <span className="flex-1 rounded-[5px] bg-background py-0.5 text-center font-medium shadow-xs">Global (qoder.com)</span>
          <span className="flex-1 py-0.5 text-center text-muted-foreground">China</span>
        </div>
      </Field>
      <Field label="Personal access token">
        <Input focused>
          <span className="flex-1 tracking-widest text-muted-foreground">••••••••••••</span>
          <Check className="size-3.5 text-success" />
        </Input>
      </Field>
      <Field label="Environment">
        <Input>
          <span className="flex-1">acme-prod</span>
          <ChevronDown className="size-3.5 text-muted-foreground" />
        </Input>
      </Field>
      <div className="mt-4 flex justify-end">
        <span className="flex h-7 items-center rounded-md bg-foreground px-3 text-caption font-medium text-background">Connect</span>
      </div>
    </div>
  );
}

export function LinkAgentVisual() {
  return (
    <div className={cn(FLOATING, "w-[288px] p-4")}>
      <DialogHeader title="Add agent" subtitle="Link a Multica agent to an existing QCA Agent." />
      <Field label="Agent name">
        <Input>
          <Avatar actor={BACKEND} size={16} />
          Backend Agent
        </Input>
      </Field>
      <Field label="QCA Agent">
        <Input focused>
          <span className="flex-1">backend-dev</span>
          <ChevronDown className="size-3.5 text-muted-foreground" />
        </Input>
      </Field>
      <Field label="Default repository" hint="Mounted when the issue’s project has none.">
        <Input>
          <span className="flex-1 truncate">github.com/acme/web</span>
          <span className="text-caption text-muted-foreground">main</span>
        </Input>
      </Field>
    </div>
  );
}

export function AssignVisual() {
  return (
    <div className={cn(FLOATING, "w-[288px] overflow-hidden")}>
      <div className="px-4 pt-4 pb-3">
        <p className="flex items-center justify-between text-caption text-muted-foreground tabular-nums">
          ACM-152
          <Working />
        </p>
        <p className="mt-1 text-label font-semibold">Redesign the settings page</p>
        <div className="mt-2.5 flex flex-col gap-1.5 text-caption">
          <span className="flex items-center">
            <span className="w-16 shrink-0 text-muted-foreground">Status</span>
            <span className="flex items-center gap-1.5">
              <StatusIcon status="in_progress" className="size-3.5" />
              In progress
            </span>
          </span>
          <span className="flex items-center">
            <span className="w-16 shrink-0 text-muted-foreground">Assignee</span>
            <Chip actor={BACKEND} />
          </span>
        </div>
      </div>
      <div className="flex flex-col gap-2.5 border-t border-border bg-muted/30 px-4 py-3 text-caption">
        <span className="flex items-center gap-2">
          <Avatar actor={LEO} size={16} />
          <span className="min-w-0 flex-1 truncate">
            <span className="font-medium">Leo Park</span> <span className="text-muted-foreground">assigned Backend Agent</span>
          </span>
        </span>
        <span className="flex items-center gap-2">
          <Avatar actor={BACKEND} size={16} />
          <span className="min-w-0 flex-1 truncate">
            <span className="font-medium">Backend Agent</span> <span className="text-muted-foreground">started on Qoder</span>
          </span>
        </span>
        <span className="flex items-center gap-2">
          <GitPullRequest className="size-4 shrink-0 text-success" />
          <span className="min-w-0 flex-1 truncate">
            <span className="font-medium">#482</span> <span className="text-muted-foreground">linked to ACM-152</span>
          </span>
        </span>
      </div>
    </div>
  );
}
