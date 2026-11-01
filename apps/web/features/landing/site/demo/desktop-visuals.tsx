"use client";

import type { CSSProperties, ReactNode } from "react";
import {
  ArrowLeft,
  ArrowRight,
  Bot,
  Folder,
  Inbox,
  Laptop,
  ListTodo,
  MessageSquare,
  MoreHorizontal,
  PanelLeft,
  PanelRight,
  Pin,
  Plus,
  RotateCw,
  Search,
  SquarePen,
  type IconComponent,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { PriorityIcon } from "@multica/views/issues/components/priority-icon";
import { Chip } from "./feature-visuals";
import { TimelineRow } from "./hero-board";
import {
  APP_CARD,
  AppRail,
  Avatar,
  AvatarStack,
  BACKEND,
  BreadcrumbHeader,
  FLOATING,
  HeaderButton,
  MIA,
  PROVIDER_MARKS,
  ZoomCanvas,
  type Space,
} from "./mock-kit";

/*
 * Mockups for the Multica Desktop page: the app as a native window, with the
 * window controls and history over the rail, the workspace's tabs as pills in
 * the title bar, and the daemon it runs on the computer it is open on. Each
 * workspace opens in a window of its own. Strings follow the product's own
 * copy; data is fictional.
 */

type Tab = { icon: IconComponent; title: string; pinned?: boolean };

const ACME_TABS: Tab[] = [
  { icon: Inbox, title: "Inbox", pinned: true },
  { icon: ListTodo, title: "ACM-152 Redesign the settings page" },
  { icon: MessageSquare, title: "Backend Agent" },
];

/** Room the window controls and history take at the title bar's start. */
const TOOLBAR_CLEARANCE = 200;
const TITLE_BAR_ACTION = "flex size-7 shrink-0 items-center justify-center rounded-md text-faint-foreground";
const TAB_FADE: CSSProperties = {
  maskImage: "linear-gradient(to right, black calc(100% - 12px), transparent)",
  WebkitMaskImage: "linear-gradient(to right, black calc(100% - 12px), transparent)",
};

function TrafficLights() {
  return (
    <span className="flex items-center gap-2">
      {[0, 1, 2].map((dot) => (
        <span key={dot} className="size-3 rounded-full bg-muted-foreground/20" />
      ))}
    </span>
  );
}

/** Traffic lights, then back, forward and the panel toggle, floating over the rail and the title bar's start. */
function WindowToolbar() {
  return (
    <div className="absolute top-0 left-0 z-10 flex h-10 items-center pl-3.5" style={{ width: TOOLBAR_CLEARANCE }}>
      <TrafficLights />
      <span className="ml-auto flex items-center gap-2 pr-3">
        <span className={cn(TITLE_BAR_ACTION, "opacity-30")}>
          <ArrowLeft className="size-4" />
        </span>
        <span className={TITLE_BAR_ACTION}>
          <ArrowRight className="size-4" />
        </span>
        <span className={TITLE_BAR_ACTION}>
          <PanelLeft className="size-4" />
        </span>
      </span>
    </div>
  );
}

/** Every tab is a pill; the active one is made of the content card's material. */
function TabPill({ tab, active, className }: { tab: Tab; active: boolean; className?: string }) {
  return (
    <span className={cn("relative h-full min-w-0", className)}>
      <span
        aria-hidden
        className={cn(
          "absolute inset-x-0 inset-y-1.5 rounded-lg",
          active ? "bg-page-canvas shadow-[var(--surface-shadow)] ring-1 ring-surface-border" : "bg-page-canvas/50",
        )}
      />
      <span className={cn("relative flex size-full items-center gap-1.5 px-2.5 text-caption", active ? "text-foreground" : "text-muted-foreground")}>
        <tab.icon className="size-3.5 shrink-0" />
        <span className="min-w-0 flex-1 overflow-hidden whitespace-nowrap" style={TAB_FADE}>
          {tab.title}
        </span>
      </span>
    </span>
  );
}

function TabStrip({ tabs, active, tabClassName }: { tabs: Tab[]; active: number; tabClassName: string }) {
  const pinned = tabs.filter((tab) => tab.pinned).length;
  return (
    <div className="flex h-full min-w-0 flex-1 items-center gap-1.5">
      {tabs.map((tab, index) => (
        <span key={tab.title} className="contents">
          <TabPill tab={tab} active={index === active} className={tabClassName} />
          {index === pinned - 1 && pinned < tabs.length ? <span className="h-4 w-px shrink-0 bg-surface-border" /> : null}
        </span>
      ))}
      <span className={TITLE_BAR_ACTION}>
        <Plus className="size-3.5" />
      </span>
    </div>
  );
}

/**
 * Desktop's window: the rail runs the full height under the window toolbar,
 * the title bar holds the tabs and, at its far end, New issue and search, and
 * the page sits on the content card. A window that runs off its frame passes
 * `bleed` so the page keeps its content out of the cropped strip.
 */
function NativeWindow({
  className,
  active,
  tabs,
  activeTab,
  compact,
  bleed = 0,
  children,
}: {
  className: string;
  active: Space;
  tabs: Tab[];
  activeTab: number;
  compact?: boolean;
  bleed?: number;
  children: ReactNode;
}) {
  return (
    <div className={cn("absolute flex overflow-hidden rounded-xl border border-site-line-strong bg-app-shell shadow-site-float", className)}>
      <WindowToolbar />
      <AppRail active={active} desktop />
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex h-10 shrink-0 items-center gap-2" style={{ paddingLeft: TOOLBAR_CLEARANCE - 64 }}>
          <TabStrip tabs={tabs} active={activeTab} tabClassName={compact ? "w-32 shrink-0" : "w-56"} />
          {compact ? null : (
            <span className="flex shrink-0 items-center gap-2" style={{ marginRight: bleed + 12 }}>
              <span className={TITLE_BAR_ACTION}>
                <SquarePen className="size-4" />
              </span>
              <span className={TITLE_BAR_ACTION}>
                <Search className="size-4" />
              </span>
            </span>
          )}
        </div>
        <div className={cn(APP_CARD, "mr-2 mb-2")}>
          <div className="relative flex min-w-0 flex-1 flex-col" style={{ marginRight: bleed, marginBottom: bleed }}>
            {children}
          </div>
        </div>
      </div>
    </div>
  );
}

function Property({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex h-8 items-center text-caption">
      <span className="w-[76px] shrink-0 text-muted-foreground">{label}</span>
      <span className="flex min-w-0 items-center gap-1.5">{children}</span>
    </div>
  );
}

function IssuePage({ compact }: { compact?: boolean }) {
  return (
    <>
      <BreadcrumbHeader
        parent={
          <>
            <Folder className="size-3.5 shrink-0" />
            Acme Web
          </>
        }
        leaf={compact ? "ACM-152" : "ACM-152 Redesign the settings page"}
      >
        <span className="flex items-center gap-1">
          <span className="flex h-7 items-center gap-1.5 rounded-md bg-brand/5 px-1.5">
            <AvatarStack actors={[BACKEND]} ring="ring-page-canvas" />
            <span className="truncate text-caption text-info">{compact ? "Working" : `${BACKEND.name} is working`}</span>
          </span>
          {compact ? null : <span className="h-4 w-px bg-border" />}
        </span>
        {compact ? null : (
          <>
            <HeaderButton icon={Pin} />
            <HeaderButton icon={MoreHorizontal} />
            <HeaderButton icon={PanelRight} />
          </>
        )}
      </BreadcrumbHeader>
      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 px-6 py-5">
          <p className="text-title-lg leading-snug font-semibold tracking-tight">Redesign the settings page</p>
          <p className="mt-2 max-w-[30rem] text-body text-muted-foreground">
            Split settings into Profile, Billing, and Team tabs, and keep every form on the existing components.
          </p>
          <p className="mt-6 text-title-sm font-semibold">Activity</p>
          <div className="mt-3 flex flex-col gap-3">
            <div className="flex items-center text-label text-muted-foreground">
              <span className="mr-2 flex w-4 shrink-0 justify-center">
                <Avatar actor={MIA} size={16} />
              </span>
              <span className="shrink-0 font-medium">{MIA.name}</span>
              <span className="ml-1 truncate">assigned to {BACKEND.name}</span>
              {compact ? null : <span className="ml-auto shrink-0 pl-2">9 min ago</span>}
            </div>
            <TimelineRow
              item={{
                kind: "comment",
                actor: BACKEND,
                body: "The three tabs are in place and the old page redirects to Profile. Running the settings tests before I open a pull request.",
              }}
              statuses={[]}
            />
          </div>
        </div>
        {compact ? null : (
          <div className="w-[260px] shrink-0 border-l border-border px-4 py-4">
            <p className="mb-1 flex h-7 items-center text-caption font-medium text-muted-foreground">Properties</p>
            <Property label="Status">
              <StatusIcon status="in_progress" className="size-3.5" />
              In progress
            </Property>
            <Property label="Assignee">
              <Chip actor={BACKEND} />
            </Property>
            <Property label="Project">
              <Folder className="size-3.5 text-muted-foreground" />
              Acme Web
            </Property>
            <Property label="Priority">
              <PriorityIcon priority="high" />
              High
            </Property>
            <p className="mt-1 flex h-8 items-center gap-1.5 text-caption text-muted-foreground">
              <Plus className="size-3.5" /> Add property
            </p>
          </div>
        )}
      </div>
    </>
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
          <NativeWindow className="top-6 left-6 -right-10 -bottom-10" active="work" tabs={ACME_TABS.slice(1, 2)} activeTab={0} compact bleed={40}>
            <IssuePage compact />
          </NativeWindow>
          <MachineCard className="right-4 bottom-5 w-[260px]" />
        </ZoomCanvas>
      </div>
      <div className="relative hidden aspect-[1100/600] sm:block">
        <ZoomCanvas {...SCENE}>
          <NativeWindow className="top-10 right-[17%] -bottom-12 left-[6%]" active="work" tabs={ACME_TABS} activeTab={1} bleed={48}>
            <IssuePage />
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

/** A workspace's own window, cut down to its title bar and the top of its page. */
function WorkspaceWindow({ initial, tabs, active, front, className }: { initial: string; tabs: Tab[]; active: number; front?: boolean; className?: string }) {
  const page = tabs[active]!;
  return (
    <div
      className={cn(
        "relative flex overflow-hidden rounded-xl border bg-app-shell",
        front ? "border-site-line-strong shadow-site-float" : "border-surface-border",
        className,
      )}
    >
      <WindowToolbar />
      <div className="flex w-16 shrink-0 justify-center pt-10 pb-2">
        <span className="flex size-[34px] items-center justify-center rounded-lg border border-surface-border bg-surface-raised text-body font-semibold shadow-[var(--surface-shadow)]">
          {initial}
        </span>
      </div>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex h-10 shrink-0 items-center pr-2" style={{ paddingLeft: TOOLBAR_CLEARANCE - 64 }}>
          <TabStrip tabs={tabs} active={active} tabClassName="w-56 min-w-0 flex-1" />
        </div>
        <div className={cn(APP_CARD, "mr-2 mb-2 h-12 flex-none items-center gap-2 px-4 text-body font-medium")}>
          <page.icon className="size-4 shrink-0 text-muted-foreground" />
          <span className="truncate">{page.title}</span>
        </div>
      </div>
    </div>
  );
}

/** Two workspaces, each in its own window with the tabs it was left with. */
export function TabsVisual() {
  return (
    <div className="flex w-[470px] flex-col">
      <WorkspaceWindow initial="M" tabs={PERSONAL_TABS} active={1} className="mr-8 opacity-70" />
      <WorkspaceWindow initial="A" tabs={ACME_TABS.slice(0, 2)} active={1} front className="-mt-6 ml-8" />
    </div>
  );
}
