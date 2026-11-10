"use client";

import type { ReactNode } from "react";
import { Check, ChevronDown, CircleCheck, GitBranch, GitPullRequest, Laptop, Lock, Plus, Server, X, type IconComponent } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { PriorityIcon } from "@multica/views/issues/components/priority-icon";
import { ProviderLogo } from "@multica/views/runtimes/components/provider-logo";
import { Chip } from "./feature-visuals";
import {
  AppWindow,
  Avatar,
  BACKEND,
  FLOATING,
  HeaderButton,
  LEO,
  LIST,
  LIST_CELL,
  ListGroup,
  ListHead,
  ListRow,
  NameCell,
  Online,
  PageHeader,
  ProviderStack,
  Working,
  ZoomCanvas,
} from "./mock-kit";

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

type Machine = { icon: IconComponent; name: string; detail: string; tools: string[]; agents: number; workload: string; tag?: string };

const MACHINES: Machine[] = [
  { icon: Laptop, name: "Mia’s MacBook Pro", detail: "macOS", tools: ["Claude Code", "Cursor"], agents: 2, workload: "1 running · 0 queued", tag: "This device" },
  { icon: Server, name: "build-01", detail: "Linux · Leo Park", tools: ["Claude Code", "Codex"], agents: 3, workload: "All idle" },
];

const COMING: { provider: string; name: string; vendor: string }[] = [
  { provider: "cursor", name: "Cursor Cloud Agents", vendor: "Cursor" },
  { provider: "claude", name: "Claude Managed Agents", vendor: "Anthropic" },
];

const WIDE_COLUMNS = "minmax(0,1fr) 64px 48px 120px 80px";
const COMPACT_COLUMNS = "minmax(0,1fr) 88px";

/** Name, harness, agents, workload, status; the compact list keeps the bare name and status. */
function RuntimeRow({ compact, name, cells, className }: { compact?: boolean; name: ReactNode; cells: [ReactNode, ReactNode, ReactNode, ReactNode]; className?: string }) {
  const [harness, agents, workload, status] = cells;
  return (
    <ListRow columns={compact ? COMPACT_COLUMNS : WIDE_COLUMNS} className={className}>
      {name}
      {compact ? null : (
        <>
          <span className={LIST_CELL}>{harness}</span>
          <span className={cn(LIST_CELL, "justify-end tabular-nums")}>{agents}</span>
          <span className={cn(LIST_CELL, "tabular-nums")}>{workload}</span>
        </>
      )}
      <span className={LIST_CELL}>{status}</span>
    </ListRow>
  );
}

const EMPTY = <span className="text-faint-foreground">—</span>;

function RuntimesPage({ compact }: { compact?: boolean }) {
  return (
    <>
      <PageHeader icon={Server} title="All runtimes" filter={false}>
        <HeaderButton icon={Plus} />
      </PageHeader>
      <div className={LIST}>
        <ListHead columns={compact ? COMPACT_COLUMNS : WIDE_COLUMNS}>
          <span>Name</span>
          {compact ? null : (
            <>
              <span>Harness</span>
              <span className="text-right">Agents</span>
              <span>Workload</span>
            </>
          )}
          <span>Status</span>
        </ListHead>
        <ListGroup title="Machines" count={MACHINES.length} />
        {MACHINES.map((machine) => (
          <RuntimeRow
            key={machine.name}
            compact={compact}
            name={
              <NameCell
                icon={<machine.icon className="size-4 shrink-0 text-muted-foreground" />}
                title={machine.name}
                tag={compact ? undefined : machine.tag}
                description={compact ? undefined : machine.detail}
              />
            }
            cells={[
              <ProviderStack key="harness" names={machine.tools} />,
              machine.agents,
              <span key="workload" className={cn("truncate", machine.workload !== "All idle" && "text-foreground")}>
                {machine.workload}
              </span>,
              <Online key="status" />,
            ]}
          />
        ))}
        <ListGroup title="Cloud services" count={1} />
        <RuntimeRow
          compact={compact}
          className="bg-surface-selected"
          name={<NameCell icon={<ProviderLogo provider="qoder_cloud" className="size-4 shrink-0" />} title={QODER} description={compact ? undefined : "By Qoder"} />}
          cells={[
            EMPTY,
            3,
            <span key="workload" className="truncate text-foreground">2 running · 1 queued</span>,
            <>
              <span className="size-1.5 shrink-0 rounded-full bg-success" />
              <span className="truncate text-foreground">Online</span>
            </>,
          ]}
        />
        {COMING.map((service) => (
          <RuntimeRow
            key={service.provider}
            compact={compact}
            name={
              <NameCell
                icon={
                  <span className="shrink-0 opacity-60">
                    <ProviderLogo provider={service.provider} className="size-4" />
                  </span>
                }
                title={service.name}
                description={compact ? undefined : `By ${service.vendor}`}
                muted
              />
            }
            cells={[
              null,
              null,
              null,
              <span key="status" className="rounded-md bg-secondary px-1.5 py-0.5 text-micro font-medium text-secondary-foreground">
                Coming soon
              </span>,
            ]}
          />
        ))}
      </div>
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
          <span className="flex items-center gap-1.5 rounded-md border border-border py-0.5 pr-2 pl-1 text-caption">
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
            <AppWindow className="inset-0 rounded-none border-0 shadow-none" active="team" bleed={40}>
              <RuntimesPage compact />
            </AppWindow>
          </BrowserWindow>
          <RunCard compact className="right-4 bottom-5 w-[264px]" />
        </ZoomCanvas>
      </div>
      <div className="relative hidden aspect-[1100/600] sm:block">
        <ZoomCanvas {...SCENE}>
          <BrowserWindow path="multica.ai/acme/runtimes" className="top-10 right-[19%] -bottom-12 left-[7%]">
            <AppWindow className="inset-0 rounded-none border-0 shadow-none" active="team" bleed={48}>
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
          <span className="flex-1 rounded-sm bg-background py-0.5 text-center font-medium shadow-xs">Global (qoder.com)</span>
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
