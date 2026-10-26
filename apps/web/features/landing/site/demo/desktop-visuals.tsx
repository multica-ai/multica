"use client";

import type { ReactNode } from "react";
import { Bot, ChevronDown, ChevronRight, Inbox, Laptop, ListTodo, MessageSquare, Pin, Plus, RotateCw, X, type LucideIcon } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { PriorityIcon } from "@multica/views/issues/components/priority-icon";
import { Chip } from "./feature-visuals";
import { AppWindow, Avatar, BACKEND, FLOATING, MIA, PROVIDER_MARKS, Working, ZoomCanvas } from "./mock-kit";

/*
 * Mockups for the Multica Desktop page: the app as a native window whose
 * title bar holds the workspace's tabs, and the daemon it runs on the
 * computer it is open on. Strings follow the product's own copy; data is
 * fictional.
 */

type Tab = { icon: LucideIcon; title: string; pinned?: boolean };

const ACME_TABS: Tab[] = [
  { icon: Inbox, title: "Inbox", pinned: true },
  { icon: ListTodo, title: "ACM-152 Redesign the settings page" },
  { icon: MessageSquare, title: "Backend Agent" },
  { icon: ListTodo, title: "ACM-148 Fix failing auth tests" },
];

function TabStrip({ tabs, active, compact }: { tabs: Tab[]; active: number; compact?: boolean }) {
  return (
    <div className="flex h-10 min-w-0 flex-1 items-end gap-0.5">
      {tabs.map((tab, index) => {
        const selected = index === active;
        return (
          <span
            key={tab.title}
            className={cn(
              "relative flex h-8 min-w-0 items-center gap-1.5 rounded-t-lg px-2.5 text-caption",
              compact ? "w-36" : "w-44",
              selected ? "bg-background text-foreground ring-1 ring-surface-border" : "text-muted-foreground",
            )}
          >
            <tab.icon className="size-3.5 shrink-0" />
            <span className="min-w-0 flex-1 truncate">{tab.title}</span>
            {tab.pinned ? null : selected ? <X className="size-3 shrink-0 text-muted-foreground" /> : null}
            {selected ? <span className="absolute inset-x-0 -bottom-px h-px bg-background" /> : null}
          </span>
        );
      })}
      <span className="mb-1.5 ml-1 flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground">
        <Plus className="size-3.5" />
      </span>
    </div>
  );
}

function NativeWindow({ className, tabs, active, compact, children }: { className: string; tabs: Tab[]; active: number; compact?: boolean; children: ReactNode }) {
  return (
    <div className={cn("absolute flex flex-col overflow-hidden rounded-xl border border-site-line-strong bg-app-shell shadow-site-float", className)}>
      <div className="flex shrink-0 items-end gap-3 border-b border-surface-border pl-3.5">
        <span className="flex h-10 shrink-0 items-center gap-1.5">
          {[0, 1, 2].map((dot) => (
            <span key={dot} className="size-2.5 rounded-full bg-muted-foreground/20" />
          ))}
        </span>
        <TabStrip tabs={tabs} active={active} compact={compact} />
      </div>
      <div className="relative min-h-0 flex-1">{children}</div>
    </div>
  );
}

function Property({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex h-7 items-center text-caption">
      <span className="w-[72px] shrink-0 text-muted-foreground">{label}</span>
      <span className="flex min-w-0 items-center gap-1.5">{children}</span>
    </div>
  );
}

function IssuePage({ compact }: { compact?: boolean }) {
  return (
    <div className="flex min-h-0 flex-1">
      <div className="min-w-0 flex-1 px-6 py-5">
        <p className="flex items-center gap-1 text-caption text-muted-foreground">
          Acme Web
          <ChevronRight className="size-3" />
          <span className="tabular-nums">ACM-152</span>
        </p>
        <p className="mt-3 text-[17px] font-semibold tracking-tight">Redesign the settings page</p>
        <p className="mt-2 max-w-[30rem] text-label text-muted-foreground">
          Split settings into Profile, Billing, and Team tabs, and keep every form on the existing components.
        </p>
        <p className="mt-6 text-caption font-medium text-muted-foreground">Activity</p>
        <div className="mt-3 flex flex-col gap-3">
          <div className="flex items-center gap-2 text-caption">
            <Avatar actor={MIA} size={16} />
            <span className="font-medium">Mia Chen</span>
            <span className="text-muted-foreground">assigned to Backend Agent · 9 min ago</span>
          </div>
          <div className="rounded-lg border border-border bg-background p-3">
            <p className="flex items-center gap-2 text-caption">
              <Avatar actor={BACKEND} size={16} />
              <span className="font-medium">Backend Agent</span>
              <span className="text-muted-foreground">2 min ago</span>
              <span className="ml-auto">
                <Working />
              </span>
            </p>
            <p className="mt-1.5 text-label leading-relaxed">
              The three tabs are in place and the old page redirects to Profile. Running the settings tests before I
              open a pull request.
            </p>
          </div>
        </div>
      </div>
      {compact ? null : (
        <div className="w-[248px] shrink-0 border-l border-border px-4 py-5">
          <p className="mb-1.5 text-caption font-medium text-muted-foreground">Properties</p>
          <Property label="Status">
            <StatusIcon status="in_progress" className="size-3.5" />
            In progress
          </Property>
          <Property label="Priority">
            <PriorityIcon priority="high" />
            High
          </Property>
          <Property label="Assignee">
            <Chip actor={BACKEND} />
          </Property>
          <Property label="Runtime">
            <Laptop className="size-3.5 text-muted-foreground" />
            <span className="truncate">Mia’s MacBook Pro</span>
          </Property>
        </div>
      )}
    </div>
  );
}

const DETECTED = [
  { name: "Claude Code", state: "1 running" },
  { name: "Codex", state: "Ready" },
  { name: "Cursor", state: "Ready" },
] as const;

function DetectedRow({ name, state }: { name: string; state: string }) {
  const Mark = PROVIDER_MARKS.get(name);
  return (
    <div className="flex h-9 items-center gap-2.5 text-caption">
      <span className="inline-flex size-6 items-center justify-center rounded-md bg-background ring-1 ring-border">
        {Mark ? <Mark className="size-3.5" /> : <Bot className="size-3.5" />}
      </span>
      <span className="flex-1 font-medium">{name}</span>
      <span className={cn("tabular-nums", state === "Ready" ? "text-muted-foreground" : "text-foreground")}>{state}</span>
    </div>
  );
}

/** The computer Desktop is open on, as a runtime: online, managed by the app, with the tools it found. */
function MachineCard({ className }: { className: string }) {
  return (
    <div className={cn(FLOATING, "absolute overflow-hidden", className)}>
      <div className="flex items-center gap-3 px-4 pt-3.5 pb-3">
        <span className="relative flex size-8 items-center justify-center rounded-lg bg-muted">
          <Laptop className="size-4 text-muted-foreground" />
          <span className="absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full bg-success ring-2 ring-background" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-label font-semibold">Mia’s MacBook Pro</span>
          <span className="block text-caption text-muted-foreground">Local · this machine</span>
        </span>
        <span className="rounded-md bg-muted px-1.5 py-0.5 text-micro font-medium text-muted-foreground">Desktop</span>
      </div>
      <div className="border-t border-border bg-muted/30 px-4 py-2">
        {DETECTED.map((tool) => (
          <DetectedRow key={tool.name} {...tool} />
        ))}
      </div>
    </div>
  );
}

const SCENE = { width: 1100, height: 600 } as const;
const SCENE_NARROW = { width: 440, height: 440 } as const;

/** Hero: an issue open in a Desktop tab, worked by an agent on the same computer. */
export function DesktopScene() {
  return (
    <>
      <div className="relative aspect-square sm:hidden">
        <ZoomCanvas {...SCENE_NARROW}>
          <NativeWindow className="top-6 left-6 -right-10 -bottom-10" tabs={ACME_TABS.slice(0, 2)} active={1} compact>
            <AppWindow className="inset-0 rounded-none border-0 shadow-none" active="issues" bleed={40}>
              <IssuePage compact />
            </AppWindow>
          </NativeWindow>
          <MachineCard className="right-4 bottom-5 w-[260px]" />
        </ZoomCanvas>
      </div>
      <div className="relative hidden aspect-[1100/600] sm:block">
        <ZoomCanvas {...SCENE}>
          <NativeWindow className="top-10 right-[17%] -bottom-12 left-[6%]" tabs={ACME_TABS} active={1}>
            <AppWindow className="inset-0 rounded-none border-0 shadow-none" active="issues" bleed={48}>
              <IssuePage />
            </AppWindow>
          </NativeWindow>
          <MachineCard className="top-[24%] right-[3.5%] w-[288px]" />
        </ZoomCanvas>
      </div>
    </>
  );
}

// --- Feature rows ------------------------------------------------------------

/** Settings → Daemon: the runtime Desktop started on its own after sign-in. */
export function DaemonVisual() {
  return (
    <div className={cn(FLOATING, "w-[320px] overflow-hidden")}>
      <div className="flex items-center gap-2 border-b border-border px-4 py-3">
        <span className="flex-1 text-label font-semibold">Daemon</span>
        <span className="flex items-center gap-1.5 text-caption">
          <span className="size-1.5 rounded-full bg-success" />
          Running
        </span>
      </div>
      <div className="flex flex-col gap-2 px-4 py-3 text-caption">
        <p className="flex justify-between gap-3">
          <span className="text-muted-foreground">Profile</span>
          <span className="truncate font-mono text-[11px]">desktop-multica.ai</span>
        </p>
        <p className="flex justify-between gap-3">
          <span className="text-muted-foreground">CLI</span>
          <span>Managed by Desktop</span>
        </p>
      </div>
      <div className="border-t border-border bg-muted/30 px-4 py-2">
        <p className="pt-1 text-micro font-medium text-muted-foreground">Detected on this computer</p>
        {DETECTED.map((tool) => (
          <DetectedRow key={tool.name} name={tool.name} state="Ready" />
        ))}
      </div>
      <div className="flex justify-end gap-2 border-t border-border px-4 py-2.5">
        <span className="flex h-7 items-center rounded-md px-2.5 text-caption font-medium text-muted-foreground ring-1 ring-border">
          View logs
        </span>
        <span className="flex h-7 items-center gap-1.5 rounded-md bg-foreground px-2.5 text-caption font-medium text-background">
          <RotateCw className="size-3" />
          Restart
        </span>
      </div>
    </div>
  );
}

const PERSONAL_TABS: Tab[] = [
  { icon: Inbox, title: "Inbox", pinned: true },
  { icon: ListTodo, title: "MIA-12 Draft the launch post" },
];

function WorkspaceRow({ name, initial, tabs, active, current }: { name: string; initial: string; tabs: Tab[]; active: number; current?: boolean }) {
  return (
    <div className={cn("overflow-hidden rounded-xl border bg-app-shell", current ? "border-site-line-strong shadow-site-float" : "border-surface-border opacity-70")}>
      <div className="flex items-center gap-2 px-3 pt-2.5 pb-1 text-caption">
        <span className="flex size-5 items-center justify-center rounded-md bg-surface text-micro font-semibold shadow-[var(--surface-shadow)]">
          {initial}
        </span>
        <span className="font-medium">{name}</span>
        <ChevronDown className="size-3 text-muted-foreground" />
        <span className="ml-auto text-micro text-muted-foreground tabular-nums">{tabs.length} tabs</span>
      </div>
      <div className="flex items-end gap-0.5 border-b border-surface-border px-2">
        {tabs.map((tab, index) => (
          <span
            key={tab.title}
            className={cn(
              "relative flex h-8 max-w-40 min-w-0 flex-1 items-center gap-1.5 rounded-t-lg px-2.5 text-caption",
              index === active ? "bg-background text-foreground ring-1 ring-surface-border" : "text-muted-foreground",
            )}
          >
            {tab.pinned ? <Pin className="size-3 shrink-0" /> : <tab.icon className="size-3.5 shrink-0" />}
            <span className="min-w-0 flex-1 truncate">{tab.title}</span>
          </span>
        ))}
      </div>
      <div className="h-6 bg-background" />
    </div>
  );
}

/** Two workspaces, each with the tabs it was left with. */
export function TabsVisual() {
  return (
    <div className="flex w-[420px] flex-col gap-3">
      <WorkspaceRow name="Acme Labs" initial="A" tabs={ACME_TABS.slice(0, 2)} active={1} current />
      <WorkspaceRow name="Mia’s workspace" initial="M" tabs={PERSONAL_TABS} active={1} />
    </div>
  );
}
